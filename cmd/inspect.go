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
	inspectFull       bool
	inspectModule     string
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
  --module NAME     describe one submodule as the subject, by name or by a
                    "parent/child" path

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
	inspectCmd.Flags().StringVarP(&inspectModule, "module", "m", "", "Describe this submodule instead, by instance name or \"parent/child\" path")
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

	// Finding a submodule by name means reading the tree that holds it, so a
	// focused inspection walks in full and trims afterwards. Without --module the
	// limit goes to the walker instead, and a listed submodule is never fetched.
	walkDepth := depth
	if inspectModule != "" {
		walkDepth = 0
	}
	tree, err := module.Inspect(moduleDir, module.InspectOptions{
		Params:   paramMap,
		MaxDepth: walkDepth,
		NoFetch:  inspectNoFetch,
		Logger:   logger,
	})
	if err != nil {
		return err
	}

	subject, path := tree, []string{tree.Instance}
	if inspectModule != "" {
		subject, path, err = tree.FindModule(inspectModule)
		if err != nil {
			return err
		}
		subject.Prune(depth)
	}

	if inspectOutput == "json" {
		if err := printInspectJSON(os.Stdout, subject, path); err != nil {
			return err
		}
	} else {
		printInspectTree(os.Stdout, subject, path)
	}

	// A module that cannot be described is a failure of the inspection itself,
	// so it sets the exit code. Missing parameters are not: reporting them is
	// the point of the command, and a caller may well be inspecting precisely
	// to find out what to pass.
	if problems := subject.Problems(); len(problems) > 0 {
		return fmt.Errorf("%d module(s) could not be inspected", len(problems))
	}
	return nil
}

// inspectReport is the --output json document: the described module plus the
// roll-ups the tree view prints as footers, so a caller does not have to walk
// the tree to answer "what must I supply?", "what is broken?", and "what did I
// not look at?".
type inspectReport struct {
	// Path is the breadcrumb of the described module from the run's root, so a
	// --module report says where its subject sits.
	Path          []string              `json:"path"`
	Module        *module.Inspection    `json:"module"`
	MissingParams []module.MissingParam `json:"missingParams"`
	Problems      []string              `json:"problems"`
	// Unexpanded names the modules listed but not read. While it is non-empty,
	// missingParams is a statement about part of the tree, not all of it.
	Unexpanded [][]string `json:"unexpanded"`
}

func printInspectJSON(w io.Writer, subject *module.Inspection, path []string) error {
	report := inspectReport{
		Path:          path,
		Module:        subject,
		MissingParams: prefixPaths(subject.MissingParams(), path),
		Problems:      subject.Problems(),
		Unexpanded:    prefixCrumbs(subject.Unexpanded(), path),
	}
	if report.MissingParams == nil {
		report.MissingParams = []module.MissingParam{}
	}
	if report.Problems == nil {
		report.Problems = []string{}
	}
	if report.Unexpanded == nil {
		report.Unexpanded = [][]string{}
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(report)
}

// printInspectTree writes the human-readable report: the module, then a summary
// of what a run would still need.
func printInspectTree(w io.Writer, subject *module.Inspection, path []string) {
	p := &inspectPrinter{w: w, style: prettylog.NewStyle(w)}
	p.root(subject, path)
	p.summary(subject, path)
}

// prefixPaths re-roots breadcrumbs at the run's root. The roll-ups walk from the
// described module, so a --module report would otherwise locate a parameter
// relative to a subject the reader has to remember the position of. It copies
// rather than rewrites in place, so callers keep the subject-relative form too.
func prefixPaths(missing []module.MissingParam, path []string) []module.MissingParam {
	prefix := ancestry(path)
	if len(prefix) == 0 {
		return missing
	}
	out := make([]module.MissingParam, len(missing))
	for i, m := range missing {
		out[i] = module.MissingParam{Name: m.Name, Path: join(prefix, m.Path)}
	}
	return out
}

func prefixCrumbs(crumbs [][]string, path []string) [][]string {
	prefix := ancestry(path)
	if len(prefix) == 0 {
		return crumbs
	}
	out := make([][]string, len(crumbs))
	for i, c := range crumbs {
		out[i] = join(prefix, c)
	}
	return out
}

func join(prefix, rest []string) []string {
	return append(append([]string(nil), prefix...), rest...)
}

// ancestry is the described module's path minus the module itself, which the
// roll-up breadcrumbs already start with.
func ancestry(path []string) []string {
	if len(path) < 2 {
		return nil
	}
	return path[:len(path)-1]
}
