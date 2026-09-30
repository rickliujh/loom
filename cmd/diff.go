package cmd

import (
	"fmt"
	"os"
	"strings"

	prettylog "github.com/rickliujh/loom/internal/log"
	"github.com/rickliujh/loom/pkg/action"
	"github.com/rickliujh/loom/pkg/engine"
	"github.com/rickliujh/loom/pkg/module"
	"github.com/rickliujh/loom/pkg/params"
	"github.com/spf13/cobra"
)

var (
	diffParams     []string
	diffParamsFile string
	diffTargetPath string
	diffAuthor     string
	diffEmail      string
	diffQuick      bool
	diffPartial    bool
)

var diffCmd = &cobra.Command{
	Use:   "diff [path]",
	Short: "Show the diffs a module run would produce",
	Long: `Show every change a loom module would make.

By default, diff runs the module in local mode — it clones each target, executes
all operations (including pure shell commands), commits locally, and skips push
and PR creation — then prints a git diff of each target against its base branch.
This is the complete, accurate picture, including files rewritten by shell ops.

With --quick, diff instead simulates the run (dry-run): it prints unified diffs
for newFiles and patch operations without executing anything. It is fast and has
no side effects, but cannot show changes made by shell commands.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runDiff,
}

func init() {
	diffCmd.Flags().StringArrayVarP(&diffParams, "param", "p", nil, "Parameter in key=value format (can be repeated)")
	diffCmd.Flags().StringVar(&diffParamsFile, "params-file", "", "YAML file with parameters")
	diffCmd.Flags().StringVar(&diffTargetPath, "target-path", "", "Directory for target clones. When set, it is kept for inspection instead of a cleaned-up temp dir")
	diffCmd.Flags().StringVar(&diffAuthor, "author", "", "Default git author name for commitPush operations")
	diffCmd.Flags().StringVar(&diffEmail, "email", "", "Default git author email for commitPush operations")
	diffCmd.Flags().BoolVar(&diffQuick, "quick", false, "Simulate the run (dry-run) and show newFiles/patch diffs only, without executing anything")
	diffCmd.Flags().BoolVar(&diffPartial, "partial", false, "When the run fails, still print the diff of changes made before the error (below a warning)")
	rootCmd.AddCommand(diffCmd)
}

func runDiff(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	logger := newLogger()

	source := "."
	if len(args) > 0 {
		source = args[0]
	}

	paramMap, err := params.Parse(diffParams, diffParamsFile)
	if err != nil {
		return err
	}

	// Full mode cannot work without git; say so before cloning anything.
	if !diffQuick {
		if err := engine.RequireGit(); err != nil {
			return err
		}
	}

	moduleDir, cleanup, err := module.ResolveSourceContext(ctx, source, ".", logger)
	if err != nil {
		return err
	}
	if cleanup != nil {
		defer cleanup()
	}

	color := prettylog.IsTerminal(os.Stdout)
	res, err := engine.Diff(ctx, engine.DiffRequest{
		ModuleDir:        moduleDir,
		Params:           paramMap,
		Quick:            diffQuick,
		Workspace:        diffTargetPath,
		GitAuthor:        diffAuthor,
		GitEmail:         diffEmail,
		CollectOnFailure: diffPartial,
		// The CLI prints git's own output and nothing parsed from it.
		Raw:    true,
		Color:  color,
		Logger: logger,
	})
	printDiffs := func() {
		if res.Mode == engine.ModeQuick {
			action.PrintDiffEntries(os.Stdout, res.Entries)
			return
		}
		for _, t := range res.Targets {
			fmt.Fprint(os.Stdout, action.DiffHeader(t.Path, t.Label, color))
			fmt.Fprint(os.Stdout, t.Raw)
			if !strings.HasSuffix(t.Raw, "\n") {
				fmt.Fprintln(os.Stdout)
			}
		}
	}
	if res.Incomplete {
		return reportDiffFailure(err, printDiffs)
	}
	// Targets read before a failure to read the rest are still printed.
	printDiffs()
	if err != nil {
		return err
	}

	fmt.Fprintln(os.Stderr)
	switch {
	case res.Mode == engine.ModeQuick:
		prettylog.Successf(os.Stderr, "diff of %q complete — no changes were made", res.ModuleName)
	case len(res.Targets) == 0:
		prettylog.Successf(os.Stderr, "diff of %q complete — no changes (local-only modules: try --quick)", res.ModuleName)
	default:
		prettylog.Successf(os.Stderr, "diff of %q complete — %d target(s) changed", res.ModuleName, len(res.Targets))
	}
	return nil
}

// reportDiffFailure handles a failed run. By default no diff is printed and the
// returned error is reported once by Execute. With --partial, the error is shown
// first — next to the failing module's logs — then the changes made before it,
// beneath a warning; Execute prints the error again as the closing status line.
func reportDiffFailure(execErr error, printDiffs func()) error {
	if diffPartial {
		fmt.Fprintln(os.Stderr)
		prettylog.Failuref(os.Stderr, "%v", execErr)
		fmt.Fprintln(os.Stderr)
		prettylog.Warningf(os.Stderr, "run failed before finishing — the diff below shows only the changes made before the error")
		printDiffs()
	}
	return execErr
}
