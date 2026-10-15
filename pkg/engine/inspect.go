package engine

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/rickliujh/loom/pkg/module"
)

// InspectRequest describes one `loom inspect`.
type InspectRequest struct {
	// ModuleDir is the root module's local directory.
	ModuleDir string
	// Params are the values supplied for the root module.
	Params map[string]any
	// Depth is how many levels each described module covers: 1 is the module
	// alone, 0 means all of them.
	Depth int
	// Modules selects submodules to describe instead of the root, by instance
	// name or "parent/child" path, in the order given.
	Modules []string
	// NoFetch lists modules sourced from a git URL without cloning them.
	NoFetch bool
	// Logger receives source-resolution logs. Required.
	Logger *slog.Logger
}

// Subject is one module the report describes, with its breadcrumb from the
// root. There is one unless Modules named others.
type Subject struct {
	Path   []string           `json:"path"`
	Module *module.Inspection `json:"module"`
}

// InspectReport is the `loom inspect -o json` document: the described modules
// plus the roll-ups the tree view prints as footers, so a caller does not have
// to walk the tree to answer "what must I supply?", "what is broken?", and
// "what did I not look at?".
//
// Modules is always a list, whether one module was described or several, so a
// consumer indexes it the same way either way; the roll-ups are empty lists,
// never null.
type InspectReport struct {
	Modules       []Subject             `json:"modules"`
	MissingParams []module.MissingParam `json:"missingParams"`
	Problems      []string              `json:"problems"`
	// Unexpanded names the modules listed but not read. While it is non-empty,
	// missingParams is a statement about part of the tree, not all of it.
	Unexpanded [][]string `json:"unexpanded"`
}

// Err is the inspection's own failure: a module that cannot be described.
// Missing parameters are not one — reporting them is the point, and a caller
// may well be inspecting precisely to find out what to pass.
func (r InspectReport) Err() error {
	if len(r.Problems) > 0 {
		return fmt.Errorf("%d module(s) could not be inspected", len(r.Problems))
	}
	return nil
}

// InspectResult is a finished inspection.
type InspectResult struct {
	// Tree is the whole walked tree; the subjects are nodes of it (pruned
	// copies, when Modules named them).
	Tree     *module.Inspection
	Subjects []Subject
	Report   InspectReport
}

// Inspect describes a module without running any of it. The error is for an
// inspection that could not produce a report: an unreadable root, or a
// Modules query that names no module or more than one. A report whose modules
// have problems is still returned, with Report.Err set.
func Inspect(ctx context.Context, req InspectRequest) (*InspectResult, error) {
	// Finding a submodule by name means reading the tree that holds it, so a
	// focused inspection walks in full and trims afterwards. Without Modules
	// the limit goes to the walker instead, and a listed submodule is never
	// fetched.
	walkDepth := req.Depth
	if len(req.Modules) > 0 {
		walkDepth = 0
	}
	tree, err := module.InspectContext(ctx, req.ModuleDir, module.InspectOptions{
		Params:   req.Params,
		MaxDepth: walkDepth,
		NoFetch:  req.NoFetch,
		Logger:   req.Logger,
	})
	if err != nil {
		return nil, err
	}

	subjects, err := SelectSubjects(tree, req.Modules, req.Depth)
	if err != nil {
		return nil, err
	}
	return &InspectResult{Tree: tree, Subjects: subjects, Report: BuildReport(subjects)}, nil
}

// SelectSubjects resolves the queries against the walked tree, in the order
// they were given: a report that reordered them would be answering a different
// question than the one asked. A query that names no module, or more than one,
// is an error — with several subjects in play, quietly dropping the one that
// did not resolve would be easy to miss. No queries selects the root.
func SelectSubjects(tree *module.Inspection, queries []string, depth int) ([]Subject, error) {
	if len(queries) == 0 {
		return []Subject{{Path: []string{tree.Instance}, Module: tree}}, nil
	}

	var subjects []Subject
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
		subjects = append(subjects, Subject{Path: path, Module: node})
	}
	return subjects, nil
}

// BuildReport rolls the subjects up into the report.
//
// The roll-ups aggregate across every described module, re-rooting each
// breadcrumb at the run's root and dropping repeats. Subjects can overlap — one
// can sit inside another — and counting the same missing parameter twice would
// overstate what is actually needed.
func BuildReport(subjects []Subject) InspectReport {
	r := InspectReport{
		Modules:       subjects,
		MissingParams: collectMissing(subjects),
		Problems:      collectProblems(subjects),
		Unexpanded:    collectUnexpanded(subjects),
	}
	if r.Modules == nil {
		r.Modules = []Subject{}
	}
	if r.MissingParams == nil {
		r.MissingParams = []module.MissingParam{}
	}
	if r.Problems == nil {
		r.Problems = []string{}
	}
	if r.Unexpanded == nil {
		r.Unexpanded = [][]string{}
	}
	return r
}

func collectMissing(subjects []Subject) []module.MissingParam {
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

func collectProblems(subjects []Subject) []string {
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

func collectUnexpanded(subjects []Subject) [][]string {
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
