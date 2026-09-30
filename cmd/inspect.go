package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	prettylog "github.com/rickliujh/loom/internal/log"
	"github.com/rickliujh/loom/pkg/engine"
	"github.com/rickliujh/loom/pkg/module"
	"github.com/rickliujh/loom/pkg/params"
	"github.com/spf13/cobra"
)

var (
	inspectParams     []string
	inspectParamsFile string
	inspectDepth      int
	inspectFull       bool
	inspectModules    []string
	inspectNoFetch    bool
	inspectOutput     string
)

var inspectCmd = &cobra.Command{
	Use:   "inspect [path]",
	Short: "Show a module's parameters, operations, and submodules",
	Long: `Describe what a loom module is made of, without running any of it.

By default inspect describes one module — its parameters and where their values
come from, its target repository, and the operations it runs, in execution
order — and lists the submodules it composes by name, without opening them.
That is the usual question ("what is this module, and what does it need?"), and
it stays fast because a listed submodule is never fetched.

To go deeper:

  --full            describe every module in the tree
  --depth N         describe N levels (1 is this module alone, the default)
  --module NAME     describe a submodule as the subject, by name or by a
                    "parent/child" path. Repeat it to describe several — to
                    compare two siblings, say — and one summary covers them all.

Nothing is executed. Operations do not run, dynamic parameter commands are
shown but never evaluated, and "if" conditions are shown but never tested.
Modules sourced from a git URL are cloned to read their contents, into
temporary directories removed before inspect exits; --no-fetch lists them
without cloning.

Parameters you pass with -p are resolved exactly as a run would resolve them,
so the report tells you which values are still missing before you commit to a
run. Templates that depend on a value only a run can produce — a dynamic
parameter, say — are reported as unresolved and shown as authored.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runInspect,
}

func init() {
	inspectCmd.Flags().StringArrayVarP(&inspectParams, "param", "p", nil, "Parameter in key=value format (can be repeated)")
	inspectCmd.Flags().StringVar(&inspectParamsFile, "params-file", "", "YAML file with parameters")
	inspectCmd.Flags().IntVar(&inspectDepth, "depth", 1, "Levels of module to describe: 1 is this module alone, 0 means all of them")
	inspectCmd.Flags().BoolVar(&inspectFull, "full", false, "Describe every module in the tree (same as --depth 0)")
	inspectCmd.Flags().StringArrayVarP(&inspectModules, "module", "m", nil, "Describe this submodule instead of the root, by instance name or \"parent/child\" path (can be repeated)")
	inspectCmd.Flags().BoolVar(&inspectNoFetch, "no-fetch", false, "Do not clone modules sourced from a git URL; list them without descending")
	inspectCmd.Flags().StringVarP(&inspectOutput, "output", "o", "tree", "Output format (tree, json)")
	rootCmd.AddCommand(inspectCmd)
}

func runInspect(cmd *cobra.Command, args []string) error {
	if inspectOutput != "tree" && inspectOutput != "json" {
		return fmt.Errorf("invalid --output %q, expected tree or json", inspectOutput)
	}
	if inspectFull && cmd.Flags().Changed("depth") {
		return fmt.Errorf("--full and --depth set different limits; use one")
	}
	depth := inspectDepth
	if inspectFull {
		depth = 0
	}

	logger := newLogger()

	source := "."
	if len(args) > 0 {
		source = args[0]
	}

	// Resolve the root the same way run does, so `loom inspect <git-url>//sub`
	// works on a module you have not cloned yet.
	moduleDir, cleanup, err := module.ResolveSourceContext(cmd.Context(), source, ".", logger)
	if err != nil {
		return err
	}
	if cleanup != nil {
		defer cleanup()
	}

	paramMap, err := params.Parse(inspectParams, inspectParamsFile)
	if err != nil {
		return err
	}

	res, err := engine.Inspect(cmd.Context(), engine.InspectRequest{
		ModuleDir: moduleDir,
		Params:    paramMap,
		Depth:     depth,
		Modules:   inspectModules,
		NoFetch:   inspectNoFetch,
		Logger:    logger,
	})
	if err != nil {
		return err
	}

	if inspectOutput == "json" {
		if err := printInspectJSON(os.Stdout, res.Subjects); err != nil {
			return err
		}
	} else {
		printInspectTree(os.Stdout, res.Tree, res.Subjects)
	}

	// A module that cannot be described is a failure of the inspection itself,
	// so it sets the exit code; the report is printed first all the same.
	return res.Report.Err()
}

// printInspectJSON writes the --output json document.
func printInspectJSON(w io.Writer, subjects []engine.Subject) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(engine.BuildReport(subjects))
}

// printInspectTree writes the human-readable report: each described module,
// then one summary of what a run would still need. The summary is shared rather
// than repeated per module, because what the caller has to supply is a single
// list no matter how many modules they asked to see.
func printInspectTree(w io.Writer, tree *module.Inspection, subjects []engine.Subject) {
	p := &inspectPrinter{w: w, style: prettylog.NewStyle(w), tree: tree}
	for _, s := range subjects {
		p.root(s.Module, s.Path)
	}
	p.summary(subjects)
}
