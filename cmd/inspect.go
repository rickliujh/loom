package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	prettylog "github.com/rickliujh/loom/internal/log"
	"github.com/rickliujh/loom/pkg/module"
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
	if len(inspectModules) > 0 {
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

	subjects, err := selectSubjects(tree, inspectModules, depth)
	if err != nil {
		return err
	}

	if inspectOutput == "json" {
		if err := printInspectJSON(os.Stdout, subjects); err != nil {
			return err
		}
	} else {
		printInspectTree(os.Stdout, tree, subjects)
	}

	// A module that cannot be described is a failure of the inspection itself,
	// so it sets the exit code. Missing parameters are not: reporting them is
	// the point of the command, and a caller may well be inspecting precisely
	// to find out what to pass.
	if problems := collectProblems(subjects); len(problems) > 0 {
		return fmt.Errorf("%d module(s) could not be inspected", len(problems))
	}
	return nil
}

// subject is one module the report describes, with its breadcrumb from the
// root. There is one unless --module named others.
type subject struct {
	Path   []string           `json:"path"`
	Module *module.Inspection `json:"module"`
}

// selectSubjects resolves the --module queries against the walked tree, in the
// order they were given: a report that reordered them would be answering a
// different question than the one asked. A query that names no module, or more
// than one, fails the command — with several subjects in play, quietly dropping
// the one that did not resolve would be easy to miss.
func selectSubjects(tree *module.Inspection, queries []string, depth int) ([]subject, error) {
	if len(queries) == 0 {
		return []subject{{Path: []string{tree.Instance}, Module: tree}}, nil
	}

	var subjects []subject
	seen := make(map[string]bool)
	for _, q := range queries {
		node, path, err := tree.FindModule(q)
		if err != nil {
			return nil, err
		}
		// Naming one module twice — directly and by another spelling — should
		// not describe it twice.
		key := strings.Join(path, "/")
		if seen[key] {
			continue
		}
		seen[key] = true
		// Copy before pruning: two subjects can overlap (a module and one it
		// composes), and trimming one in place would hollow out the other.
		node = node.Clone()
		node.Prune(depth)
		subjects = append(subjects, subject{Path: path, Module: node})
	}
	return subjects, nil
}

// inspectReport is the --output json document: the described modules plus the
// roll-ups the tree view prints as footers, so a caller does not have to walk
// the tree to answer "what must I supply?", "what is broken?", and "what did I
// not look at?".
//
// modules is always a list, whether one module was described or several, so a
// consumer indexes it the same way either way.
type inspectReport struct {
	Modules       []subject             `json:"modules"`
	MissingParams []module.MissingParam `json:"missingParams"`
	Problems      []string              `json:"problems"`
	// Unexpanded names the modules listed but not read. While it is non-empty,
	// missingParams is a statement about part of the tree, not all of it.
	Unexpanded [][]string `json:"unexpanded"`
}

func printInspectJSON(w io.Writer, subjects []subject) error {
	report := inspectReport{
		Modules:       subjects,
		MissingParams: collectMissing(subjects),
		Problems:      collectProblems(subjects),
		Unexpanded:    collectUnexpanded(subjects),
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

// printInspectTree writes the human-readable report: each described module,
// then one summary of what a run would still need. The summary is shared rather
// than repeated per module, because what the caller has to supply is a single
// list no matter how many modules they asked to see.
func printInspectTree(w io.Writer, tree *module.Inspection, subjects []subject) {
	p := &inspectPrinter{w: w, style: prettylog.NewStyle(w), tree: tree}
	for _, s := range subjects {
		p.root(s.Module, s.Path)
	}
	p.summary(subjects)
}

// The roll-ups below aggregate across every described module, re-rooting each
// breadcrumb at the run's root and dropping repeats. Subjects can overlap — one
// can sit inside another — and counting the same missing parameter twice would
// overstate what is actually needed.

func collectMissing(subjects []subject) []module.MissingParam {
	var out []module.MissingParam
	seen := make(map[string]bool)
	for _, s := range subjects {
		for _, m := range prefixPaths(s.Module.MissingParams(), s.Path) {
			key := strings.Join(m.Path, "/") + "\x00" + m.Name
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, m)
		}
	}
	return out
}

func collectProblems(subjects []subject) []string {
	var out []string
	seen := make(map[string]bool)
	for _, s := range subjects {
		for _, p := range s.Module.Problems() {
			if seen[p] {
				continue
			}
			seen[p] = true
			out = append(out, p)
		}
	}
	return out
}

func collectUnexpanded(subjects []subject) [][]string {
	var out [][]string
	seen := make(map[string]bool)
	for _, s := range subjects {
		for _, crumb := range prefixCrumbs(s.Module.Unexpanded(), s.Path) {
			key := strings.Join(crumb, "/")
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, crumb)
		}
	}
	return out
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
