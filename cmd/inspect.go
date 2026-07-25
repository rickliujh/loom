package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	prettylog "github.com/rickliujh/loom/internal/log"
	"github.com/rickliujh/loom/pkg/module"
	"github.com/spf13/cobra"
)

var (
	inspectParams     []string
	inspectParamsFile string
	inspectDepth      int
	inspectNoFetch    bool
	inspectOutput     string
)

var inspectCmd = &cobra.Command{
	Use:   "inspect [path]",
	Short: "Show a module's structure: submodules, operations, and parameters",
	Long: `Describe what a loom module is made of, without running any of it.

Inspect reads a module and every module it composes, and prints the resulting
tree: each module's parameters and where their values come from, its target
repository, and the operations it runs, in execution order.

Nothing is executed. Operations do not run, dynamic parameter commands are
shown but never evaluated, and "if" conditions are shown but never tested.
Modules sourced from a git URL are cloned so their contents can be read, into
temporary directories that are removed before inspect exits; --no-fetch lists
them without cloning.

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
	inspectCmd.Flags().IntVar(&inspectDepth, "depth", 0, "Maximum module depth to walk: 1 is the root alone, 0 means unlimited")
	inspectCmd.Flags().BoolVar(&inspectNoFetch, "no-fetch", false, "Do not clone modules sourced from a git URL; list them without descending")
	inspectCmd.Flags().StringVarP(&inspectOutput, "output", "o", "tree", "Output format (tree, json)")
	rootCmd.AddCommand(inspectCmd)
}

func runInspect(cmd *cobra.Command, args []string) error {
	if inspectOutput != "tree" && inspectOutput != "json" {
		return fmt.Errorf("invalid --output %q, expected tree or json", inspectOutput)
	}

	logger := newLogger()

	source := "."
	if len(args) > 0 {
		source = args[0]
	}

	// Resolve the root the same way run does, so `loom inspect <git-url>//sub`
	// works on a module you have not cloned yet.
	moduleDir, cleanup, err := module.ResolveSource(source, ".", logger)
	if err != nil {
		return err
	}
	if cleanup != nil {
		defer cleanup()
	}

	paramMap, err := parseParams(inspectParams, inspectParamsFile)
	if err != nil {
		return err
	}

	tree, err := module.Inspect(moduleDir, module.InspectOptions{
		Params:   paramMap,
		MaxDepth: inspectDepth,
		NoFetch:  inspectNoFetch,
		Logger:   logger,
	})
	if err != nil {
		return err
	}

	if inspectOutput == "json" {
		if err := printInspectJSON(os.Stdout, tree); err != nil {
			return err
		}
	} else {
		printInspectTree(os.Stdout, tree)
	}

	// A module that cannot be described is a failure of the inspection itself,
	// so it sets the exit code. Missing parameters are not: reporting them is
	// the point of the command, and a caller may well be inspecting precisely
	// to find out what to pass.
	if problems := tree.Problems(); len(problems) > 0 {
		return fmt.Errorf("%d module(s) could not be inspected", len(problems))
	}
	return nil
}

// inspectReport is the --output json document: the module tree plus the two
// roll-ups the tree view prints as footers, so a caller does not have to walk
// the tree to answer "what must I supply?" and "what is broken?".
type inspectReport struct {
	Module        *module.Inspection    `json:"module"`
	MissingParams []module.MissingParam `json:"missingParams"`
	Problems      []string              `json:"problems"`
}

func printInspectJSON(w io.Writer, tree *module.Inspection) error {
	report := inspectReport{
		Module:        tree,
		MissingParams: tree.MissingParams(),
		Problems:      tree.Problems(),
	}
	if report.MissingParams == nil {
		report.MissingParams = []module.MissingParam{}
	}
	if report.Problems == nil {
		report.Problems = []string{}
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(report)
}

// printInspectTree writes the human-readable report: the module tree, then a
// summary of what a run would still need.
func printInspectTree(w io.Writer, tree *module.Inspection) {
	p := &inspectPrinter{w: w, style: prettylog.NewStyle(w)}
	p.root(tree)
	p.summary(tree)
}
