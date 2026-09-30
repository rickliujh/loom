package cmd

import (
	"fmt"
	"os"

	prettylog "github.com/rickliujh/loom/internal/log"
	"github.com/rickliujh/loom/pkg/action"
	"github.com/rickliujh/loom/pkg/engine"
	"github.com/rickliujh/loom/pkg/module"
	"github.com/rickliujh/loom/pkg/params"
	"github.com/spf13/cobra"
)

var (
	runParams   []string
	paramsFile  string
	targetPath  string
	gitAuthor   string
	gitEmail    string
	showSummary bool
)

var runCmd = &cobra.Command{
	Use:   "run [path]",
	Short: "Run a loom module",
	Long:  "Execute the operations defined in a loom module. Path defaults to current directory.",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runModule,
}

func init() {
	runCmd.Flags().StringArrayVarP(&runParams, "param", "p", nil, "Parameter in key=value format (can be repeated)")
	runCmd.Flags().StringVar(&paramsFile, "params-file", "", "YAML file with parameters")
	runCmd.Flags().StringVar(&targetPath, "target-path", "", "Directory for target files: with --local-run, target repos are cloned into numbered subdirectories here; modules without a target spec use it directly")
	runCmd.Flags().StringVar(&gitAuthor, "author", "", "Default git author name for commitPush operations")
	runCmd.Flags().StringVar(&gitEmail, "email", "", "Default git author email for commitPush operations")
	runCmd.Flags().BoolVar(&showSummary, "summary", false, "Print a list of PRs/MRs created during the run at the end")
	rootCmd.AddCommand(runCmd)
}

func runModule(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	logger := newLogger()

	source := "."
	if len(args) > 0 {
		source = args[0]
	}

	// Resolve source — handles git URLs, //subdir, and local paths.
	moduleDir, cleanup, err := module.ResolveSourceContext(ctx, source, ".", logger)
	if err != nil {
		return err
	}
	if cleanup != nil {
		defer cleanup()
	}

	paramMap, err := params.Parse(runParams, paramsFile)
	if err != nil {
		return err
	}

	res, execErr := engine.Run(ctx, engine.RunRequest{
		ModuleDir:  moduleDir,
		Params:     paramMap,
		DryRun:     dryRun,
		LocalRun:   localRun,
		TargetPath: targetPath,
		GitAuthor:  gitAuthor,
		GitEmail:   gitEmail,
		Logger:     logger,
	})

	// Print even when the run failed partway — PRs opened before the
	// failure are exactly what the user needs to track down.
	if showSummary {
		(&action.RunSummary{PRs: res.PRs}).Print(os.Stdout)
	}
	if execErr == nil {
		fmt.Fprintln(os.Stderr)
		switch {
		case dryRun:
			prettylog.Successf(os.Stderr, "dry run of %q complete — no changes were made", res.ModuleName)
		case localRun:
			prettylog.Successf(os.Stderr, "run of %q complete — results in %s", res.ModuleName, targetPath)
		default:
			prettylog.Successf(os.Stderr, "run of %q complete", res.ModuleName)
		}
	}
	return execErr
}
