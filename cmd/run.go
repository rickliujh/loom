package cmd

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/rickliujh/loom/pkg/git"
	"github.com/rickliujh/loom/pkg/module"
	tmpl "github.com/rickliujh/loom/pkg/template"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var (
	params     []string
	paramsFile string
	targetPath string
	gitAuthor  string
	gitEmail   string
)

var runCmd = &cobra.Command{
	Use:   "run [path]",
	Short: "Run a loom module",
	Long:  "Execute the operations defined in a loom module. Path defaults to current directory.",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runModule,
}

func init() {
	runCmd.Flags().StringArrayVarP(&params, "param", "p", nil, "Parameter in key=value format (can be repeated)")
	runCmd.Flags().StringVar(&paramsFile, "params-file", "", "YAML file with parameters")
	runCmd.Flags().StringVar(&targetPath, "target-path", "", "Local path to use as target directory (skips git clone)")
	runCmd.Flags().StringVar(&gitAuthor, "author", "", "Default git author name for commitPush operations")
	runCmd.Flags().StringVar(&gitEmail, "email", "", "Default git author email for commitPush operations")
	rootCmd.AddCommand(runCmd)
}

func runModule(cmd *cobra.Command, args []string) error {
	logger := newLogger()

	source := "."
	if len(args) > 0 {
		source = args[0]
	}

	// Resolve source — handles git URLs, //subdir, and local paths.
	moduleDir, cleanup, err := module.ResolveSource(source, ".", logger)
	if err != nil {
		return err
	}
	if cleanup != nil {
		defer cleanup()
	}

	// Parse parameters.
	paramMap, err := parseParams(params, paramsFile)
	if err != nil {
		return err
	}

	// Load module.
	mod, err := module.Load(moduleDir, paramMap, logger)
	if err != nil {
		return err
	}

	// --diff implies --dry-run.
	if showDiff {
		dryRun = true
	}

	// In --local-run mode, require --target-path so the user can inspect results.
	if localRun && targetPath == "" {
		return fmt.Errorf("--local-run requires --target-path: provide a local directory to write results into")
	}

	opts := module.RunOptions{
		DryRun:     dryRun,
		LocalRun:   localRun,
		ShowDiff:   showDiff,
		TargetPath: targetPath,
		GitAuthor:  gitAuthor,
		GitEmail:   gitEmail,
	}

	// Resolve target directory.
	var targetDir string
	if mod.Config.Spec.Target != nil {
		cloneDir, cleanup, err := cloneTarget(cmd.Context(), mod, mod.Params, &opts, logger)
		if err != nil {
			return err
		}
		if cleanup != nil {
			defer cleanup()
		}
		targetDir = cloneDir
	}

	if targetDir == "" && targetPath != "" {
		// No target spec but --target-path provided — use it directly.
		targetDir = targetPath
	}
	if targetDir == "" {
		// No target specified — default to the module directory.
		// This supports modules that only use local operations (shell, newFiles)
		// without needing a git clone.
		targetDir = moduleDir
	}

	ctx := context.Background()
	return module.Execute(ctx, mod, targetDir, opts)
}

// cloneTarget clones the module's target repo. In --local-run mode, it clones into
// a numbered subdirectory of TargetPath (no cleanup). Otherwise, it clones into
// a temp directory and returns a cleanup function.
func cloneTarget(ctx context.Context, mod *module.Module, params map[string]string, opts *module.RunOptions, logger *slog.Logger) (string, func(), error) {
	target := mod.Config.Spec.Target

	// Determine clone destination.
	var cloneDir string
	var cleanup func()
	if opts.LocalRun && opts.TargetPath != "" {
		cloneDir = opts.NextLocalDir(mod.Config.Metadata.Name)
		if err := os.MkdirAll(cloneDir, 0o755); err != nil {
			return "", nil, fmt.Errorf("creating local target dir: %w", err)
		}
	} else {
		tmpDir, err := os.MkdirTemp("", "loom-target-*")
		if err != nil {
			return "", nil, fmt.Errorf("creating temp dir: %w", err)
		}
		cloneDir = tmpDir
		cleanup = func() { os.RemoveAll(tmpDir) }
	}

	targetURL, err := tmpl.RenderString(target.URL, params)
	if err != nil {
		if cleanup != nil {
			cleanup()
		}
		return "", nil, fmt.Errorf("rendering target URL: %w", err)
	}
	targetBranch, err := tmpl.RenderString(target.Branch, params)
	if err != nil {
		if cleanup != nil {
			cleanup()
		}
		return "", nil, fmt.Errorf("rendering target branch: %w", err)
	}

	repo, err := git.Clone(ctx, targetURL, cloneDir, targetBranch, logger)
	if err != nil {
		if cleanup != nil {
			cleanup()
		}
		return "", nil, err
	}

	if target.FeatureBranch != "" {
		branchName, err := tmpl.RenderString(target.FeatureBranch, params)
		if err != nil {
			if cleanup != nil {
				cleanup()
			}
			return "", nil, fmt.Errorf("rendering featureBranch: %w", err)
		}
		logger.Info("creating feature branch", "branch", branchName)
		if err := repo.CreateBranch(branchName); err != nil {
			if cleanup != nil {
				cleanup()
			}
			return "", nil, fmt.Errorf("creating feature branch %q: %w", branchName, err)
		}
	}

	return cloneDir, cleanup, nil
}

// parseParams merges CLI params and params file into a map.
func parseParams(cliParams []string, paramsFile string) (map[string]string, error) {
	result := make(map[string]string)

	// Load from file first (CLI params override).
	if paramsFile != "" {
		data, err := os.ReadFile(paramsFile)
		if err != nil {
			return nil, fmt.Errorf("reading params file: %w", err)
		}

		var fileParams map[string]string
		if err := yaml.Unmarshal(data, &fileParams); err != nil {
			return nil, fmt.Errorf("parsing params file: %w", err)
		}
		for k, v := range fileParams {
			result[k] = v
		}
	}

	// Parse CLI params.
	for _, p := range cliParams {
		parts := strings.SplitN(p, "=", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid param format %q, expected key=value", p)
		}
		result[parts[0]] = parts[1]
	}

	return result, nil
}
