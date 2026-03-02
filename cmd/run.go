package cmd

import (
	"context"
	"fmt"
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
	rootCmd.AddCommand(runCmd)
}

func runModule(cmd *cobra.Command, args []string) error {
	logger := newLogger()

	moduleDir := "."
	if len(args) > 0 {
		moduleDir = args[0]
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

	// Resolve target directory.
	targetDir := targetPath
	if targetDir == "" && mod.Config.Spec.Target != nil {
		// Clone the target repo.
		tmpDir, err := os.MkdirTemp("", "loom-target-*")
		if err != nil {
			return fmt.Errorf("creating temp dir: %w", err)
		}
		defer os.RemoveAll(tmpDir)

		repo, err := git.Clone(cmd.Context(), mod.Config.Spec.Target.URL, tmpDir, mod.Config.Spec.Target.Branch, logger)
		if err != nil {
			return err
		}

		// Create and checkout a feature branch if configured.
		if mod.Config.Spec.Target.FeatureBranch != "" {
			branchName, err := tmpl.RenderString(mod.Config.Spec.Target.FeatureBranch, paramMap)
			if err != nil {
				return fmt.Errorf("rendering featureBranch: %w", err)
			}
			logger.Info("creating feature branch", "branch", branchName)
			if err := repo.CreateBranch(branchName); err != nil {
				return fmt.Errorf("creating feature branch %q: %w", branchName, err)
			}
		}

		targetDir = tmpDir
	}

	if targetDir == "" {
		return fmt.Errorf("no target specified: use --target-path or define spec.target in loom.yaml")
	}

	ctx := context.Background()
	return module.Execute(ctx, mod, targetDir, dryRun)
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
