package module

import (
	"fmt"
	"log/slog"
	"path/filepath"
	"sort"
	"strings"

	"github.com/rickliujh/loom/pkg/action"
	"github.com/rickliujh/loom/pkg/config"
	tmpl "github.com/rickliujh/loom/pkg/template"
)

// noValue is what text/template substitutes for a key absent from the param
// map. Inspect renders with a deliberately partial map — params whose values
// only exist at run time (dynamic ones, unsupplied required ones) are simply
// not there — so this marker is how a template that could not be fully
// resolved is recognized, rather than an error.
const noValue = "<no value>"

// ParamState says where an inspected parameter's value comes from, or why the
// module does not have one yet.
type ParamState string

const (
	// ParamProvided: a value came from the CLI (root) or the parent's modules
	// entry (child).
	ParamProvided ParamState = "provided"
	// ParamDefault: nothing was supplied, so the declared default applies.
	ParamDefault ParamState = "default"
	// ParamDynamic: the value is produced by a shell command at run time.
	// Inspect never runs that command, so no value is known.
	ParamDynamic ParamState = "dynamic"
	// ParamMissing: required, with no default and nothing supplying it. A run
	// would fail here.
	ParamMissing ParamState = "missing"
	// ParamUnset: optional, with no default and nothing supplying it. It renders
	// as the empty string.
	ParamUnset ParamState = "unset"
	// ParamUnresolved: supplied by the parent, but the expression references
	// something the parent itself does not know yet — typically one of its own
	// dynamic params. The value exists at run time; inspect cannot compute it.
	ParamUnresolved ParamState = "unresolved"
)

// Param is one parameter a module declares, together with what inspect could
// work out about its value without executing anything.
type Param struct {
	Name     string     `json:"name"`
	State    ParamState `json:"state"`
	Required bool       `json:"required,omitempty"`
	// Value is the known value, empty unless State is provided or default.
	Value string `json:"value,omitempty"`
	// Default is the declared fallback, if any.
	Default string `json:"default,omitempty"`
	// Command is the shell command of a dynamic param, as authored.
	Command string `json:"command,omitempty"`
	// From is the parent's expression supplying this param, kept as authored so
	// a templated hand-off ("{{ .env }}-ns") stays visible next to its result.
	// Empty at the root and when the parent passed a literal.
	From string `json:"from,omitempty"`
}

// OpSummary is one operation, reduced to what a reader needs to see the shape
// of a module: its name, action kind, a one-line detail, and the condition
// gating it. Inspect never evaluates the condition.
type OpSummary struct {
	Name   string `json:"name"`
	Kind   string `json:"kind"`
	Detail string `json:"detail,omitempty"`
	If     string `json:"if,omitempty"`
	// Error is set when the operation declares no recognized action — the same
	// failure a run would hit, reported here instead of aborting the walk.
	Error string `json:"error,omitempty"`
}

// Target is a module's target spec with every field rendered against the params
// inspect could resolve. Fields that depend on a run-time value keep their
// template text.
type Target struct {
	URL           string `json:"url,omitempty"`
	Branch        string `json:"branch,omitempty"`
	FeatureBranch string `json:"featureBranch,omitempty"`
}

// Inspection is one module in the inspected tree: what it is, what it needs,
// what it does, and what it composes. A failure anywhere below the root is
// recorded on the node it happened at rather than aborting the walk, so a
// broken submodule never hides the rest of the tree.
type Inspection struct {
	// Instance is the name this module is dispatched under — the parent's
	// modules[].name, rendered. At the root, where no parent names it, it is the
	// module's own metadata.name. This is the identity run logs and diffs use.
	Instance string `json:"instance"`
	// Name is metadata.name. Several instances can share one, which is exactly
	// why Instance exists.
	Name string `json:"name,omitempty"`
	// Source is the parent's modules[].source, rendered. Empty at the root.
	Source string `json:"source,omitempty"`
	// SourceTemplate is the source as authored, set only when templating
	// changed it, so both the expression and its result stay visible.
	SourceTemplate string `json:"sourceTemplate,omitempty"`
	// Dir is the local directory the module was read from. Empty for a module
	// fetched from a remote source: it lived in a temp clone that is deleted
	// once the walk finishes.
	Dir string `json:"dir,omitempty"`
	// Remote reports that Source is a git URL rather than a local path.
	Remote bool `json:"remote,omitempty"`
	// If is the parent's condition gating this module, as authored. Inspect
	// does not run it, so the module is described either way.
	If string `json:"if,omitempty"`

	Target     *Target       `json:"target,omitempty"`
	Params     []Param       `json:"params,omitempty"`
	Excludes   []string      `json:"excludes,omitempty"`
	Includes   []string      `json:"includes,omitempty"`
	Operations []OpSummary   `json:"operations,omitempty"`
	Children   []*Inspection `json:"modules,omitempty"`

	// Warnings are problems that do not stop the walk but would bite at run
	// time, such as a param the parent passes that the child never declares.
	Warnings []string `json:"warnings,omitempty"`
	// Error is set when this module could not be read at all — an unresolvable
	// source or an invalid config. Its own contents are then unknown.
	Error string `json:"error,omitempty"`
	// Listed reports a module named but not read: it sits past the depth limit,
	// so only what its parent declares about it — instance, source, condition —
	// is known. Its parameters and operations are not.
	Listed bool `json:"listed,omitempty"`
	// Cycle reports that this module's source already appears among its own
	// ancestors, so the walk stopped rather than recursing forever.
	Cycle bool `json:"cycle,omitempty"`
	// Unfetched reports a remote module left unvisited because fetching was
	// disabled.
	Unfetched bool `json:"unfetched,omitempty"`
}

// InspectOptions controls how far and how eagerly Inspect walks.
type InspectOptions struct {
	// Params are the values supplied for the root module, as `loom run -p` would.
	Params map[string]string
	// MaxDepth limits how many levels of module are read: 1 is the root alone,
	// 2 adds its direct children, and zero or less means unlimited. Modules past
	// the limit are still listed by name — see Inspection.Listed — since knowing
	// what a module composes is most of the value of a shallow look.
	MaxDepth int
	// NoFetch keeps the walk offline: modules sourced from a git URL are listed
	// but not cloned, and so not described.
	NoFetch bool
	// Logger receives source-resolution logs. Required.
	Logger *slog.Logger
}

// Inspect describes a module and everything it composes, without running any
// of it. No operation executes, no dynamic parameter command runs, and no
// condition is evaluated — the only side effect is cloning remote module
// sources to read their configs, and those clones are removed before Inspect
// returns.
//
// The error result is reserved for a root that cannot be described at all;
// every deeper failure is reported on its node in the returned tree.
func Inspect(dir string, opts InspectOptions) (*Inspection, error) {
	var cleanups []func()
	defer func() {
		// Deepest-first, mirroring how the clones nest.
		for i := len(cleanups) - 1; i >= 0; i-- {
			cleanups[i]()
		}
	}()

	node := &Inspection{Dir: dir}
	w := &inspector{opts: opts, cleanups: &cleanups}
	w.describe(node, dir, opts.Params, []string{localIdentity(dir)})
	if node.Error != "" {
		return nil, fmt.Errorf("inspecting %s: %s", dir, node.Error)
	}
	return node, nil
}

// inspector carries the walk-wide state so describe/walkChildren stay readable.
type inspector struct {
	opts     InspectOptions
	cleanups *[]func()
}

// describe fills in one node from the module config in dir, then recurses.
//
// ancestry is the chain of source identities from the root down to and
// including this module. It serves twice over: comparing a child against it is
// what stops a module that (transitively) composes itself from recursing
// forever, and its length is the module's depth.
func (w *inspector) describe(node *Inspection, dir string, provided map[string]string, ancestry []string) {
	cfg, err := config.Load(dir)
	if err != nil {
		node.Error = err.Error()
		return
	}
	// A structurally invalid config cannot be described — its params and
	// operations are not trustworthy — so it fails the node outright.
	if err := config.Validate(cfg); err != nil {
		node.Error = err.Error()
		return
	}

	node.Name = cfg.Metadata.Name
	if node.Instance == "" {
		node.Instance = cfg.Metadata.Name
	}
	node.Excludes = cfg.Spec.Excludes
	node.Includes = cfg.Spec.Includes

	// known holds only the params whose values inspect can actually compute.
	// Templates referencing anything else render to noValue and are reported as
	// unresolved rather than as a wrong value.
	node.Params, node.Warnings = describeParams(cfg.Spec, provided)
	known := knownValues(node.Params)

	// The filesystem half of the run path's validation — a newFiles source or
	// patch file that is not there. Structural validation has already passed, so
	// anything left is a missing file: real, and worth reporting, but no reason
	// to withhold the description of a module the reader is trying to
	// understand. It downgrades to a warning for that reason.
	if err := config.ValidateInDir(cfg, dir); err != nil {
		node.Warnings = append(node.Warnings, strings.Split(err.Error(), "\n")...)
	}

	node.Target = describeTarget(cfg.Spec.Target, known)
	node.Operations = describeOperations(cfg.Spec.Operations)

	if len(cfg.Spec.Modules) == 0 {
		return
	}
	// Past the depth limit the children are listed rather than dropped: what a
	// module composes, and under which names, is most of what a shallow look is
	// for — and it is the reader's cue that there is more to expand.
	expand := w.opts.MaxDepth <= 0 || len(ancestry) < w.opts.MaxDepth
	w.walkChildren(node, cfg.Spec.Modules, dir, known, ancestry, expand)
}

// walkChildren describes each module the parent composes, in declaration order
// — the order a run dispatches them in. When expand is false the children are
// recorded from what the parent declares and left unread.
func (w *inspector) walkChildren(parent *Inspection, refs []config.ModuleRef, parentDir string, known map[string]string, ancestry []string, expand bool) {
	for _, ref := range refs {
		child := &Inspection{}
		parent.Children = append(parent.Children, child)

		child.Instance, _ = render(ref.Name, known)
		if child.Instance == "" {
			child.Instance = ref.Name
		}
		child.If = ref.If

		source, sourceOK := render(ref.Source, known)
		if !sourceOK {
			// Show the expression rather than the "<no value>" it rendered to:
			// the reader can act on the former.
			source = ref.Source
		} else if source != ref.Source {
			child.SourceTemplate = ref.Source
		}
		child.Source = source
		child.Remote = !isLocalSource(source)

		if !expand {
			child.Listed = true
			continue
		}
		if !sourceOK {
			child.Error = fmt.Sprintf("source %q depends on a value known only at run time", ref.Source)
			continue
		}

		// Hand the child's params down as the parent's run would: each value
		// rendered through the parent's params. One that cannot be rendered is
		// still listed — as unresolved — instead of being passed on wrong.
		childParams := make(map[string]string, len(ref.Params))
		unresolved := make(map[string]bool)
		for k, v := range ref.Params {
			rendered, ok := render(v, known)
			if !ok {
				unresolved[k] = true
			}
			childParams[k] = rendered
		}

		if child.Remote && w.opts.NoFetch {
			child.Unfetched = true
			continue
		}
		// The identity has to be computed here, against the parent directory a
		// relative source is written relative to — the child's own directory is
		// not a base this source was ever resolved from.
		identity := sourceIdentity(source, parentDir)
		if contains(ancestry, identity) {
			child.Cycle = true
			continue
		}

		childDir, cleanup, err := ResolveSource(source, parentDir, w.opts.Logger)
		if err != nil {
			child.Error = err.Error()
			continue
		}
		if cleanup != nil {
			*w.cleanups = append(*w.cleanups, cleanup)
		}
		// A remote module's directory is a temp clone this walk deletes on the
		// way out, so naming it would only mislead; the source identifies it.
		if !child.Remote {
			child.Dir = childDir
		}

		// Copy rather than append in place: siblings each extend the same
		// ancestry, and a shared backing array would let one overwrite another's
		// last element.
		childAncestry := append(append([]string(nil), ancestry...), identity)
		w.describe(child, childDir, childParams, childAncestry)
		markFrom(child, ref.Params, unresolved)
	}
}

// describeParams builds the parameter table for one module: what it declares,
// what is supplying each value, and what a run would be missing. Warnings cover
// params supplied to the module that it never declares — the case run rejects
// outright (P3).
func describeParams(spec config.Spec, provided map[string]string) ([]Param, []string) {
	params := make([]Param, 0, len(spec.Params)+len(spec.DynamicParams))
	declared := make(map[string]bool, len(spec.Params)+len(spec.DynamicParams))

	for _, p := range spec.Params {
		declared[p.Name] = true
		out := Param{Name: p.Name, Required: p.Required, Default: p.Default}
		switch {
		case hasValue(provided, p.Name):
			out.State, out.Value = ParamProvided, provided[p.Name]
		case p.Default != "":
			out.State, out.Value = ParamDefault, p.Default
		case p.Required:
			out.State = ParamMissing
		default:
			out.State = ParamUnset
		}
		params = append(params, out)
	}

	// Dynamic params resolve last at run time, but a supplied value overrides
	// the command entirely (P6) — so a provided one is reported as provided.
	for _, dp := range spec.DynamicParams {
		declared[dp.Name] = true
		out := Param{Name: dp.Name, Command: dp.Command, Default: dp.Default}
		if hasValue(provided, dp.Name) {
			out.State, out.Value = ParamProvided, provided[dp.Name]
		} else {
			out.State = ParamDynamic
		}
		params = append(params, out)
	}

	var warnings []string
	for _, name := range sortedKeys(provided) {
		if !declared[name] {
			warnings = append(warnings, fmt.Sprintf("parameter %q is supplied but not declared by this module; a run would reject it", name))
		}
	}
	return params, warnings
}

// markFrom records, on each param the parent supplied, the expression it came
// from — and demotes a param whose expression could not be rendered from
// "provided" to "unresolved", so a placeholder is never mistaken for a value.
func markFrom(child *Inspection, refParams map[string]string, unresolved map[string]bool) {
	for i := range child.Params {
		p := &child.Params[i]
		expr, ok := refParams[p.Name]
		if !ok {
			continue
		}
		if expr != p.Value {
			p.From = expr
		}
		if unresolved[p.Name] {
			p.State, p.Value = ParamUnresolved, ""
			p.From = expr
		}
	}
}

// describeTarget renders a target spec with the params inspect knows. A field
// that depends on a run-time value keeps its template text, so the reader sees
// the expression rather than a "<no value>" placeholder.
func describeTarget(spec *config.TargetSpec, known map[string]string) *Target {
	if spec == nil {
		return nil
	}
	return &Target{
		URL:           renderOrKeep(spec.URL, known),
		Branch:        renderOrKeep(spec.Branch, known),
		FeatureBranch: renderOrKeep(spec.FeatureBranch, known),
	}
}

func describeOperations(ops []config.Operation) []OpSummary {
	out := make([]OpSummary, 0, len(ops))
	for _, op := range ops {
		summary := OpSummary{Name: op.Name, If: op.If}
		kind, detail, err := action.DescribeOperation(op)
		if err != nil {
			summary.Error = err.Error()
		} else {
			summary.Kind, summary.Detail = kind, detail
		}
		out = append(out, summary)
	}
	return out
}

// MissingParam is one required parameter that nothing supplies, located by the
// instance breadcrumb of the module that declares it.
type MissingParam struct {
	Path []string `json:"path"`
	Name string   `json:"name"`
}

// MissingParams lists every required parameter left unsatisfied anywhere in the
// tree, in walk order. This is the answer to "what do I have to pass to run
// this?" — though only for the modules actually read: a tree holding listed or
// unfetched modules may need more, which is what Unexpanded reports.
func (i *Inspection) MissingParams() []MissingParam {
	var out []MissingParam
	i.walk(nil, func(path []string, node *Inspection) {
		for _, p := range node.Params {
			if p.State == ParamMissing {
				out = append(out, MissingParam{Path: append([]string(nil), path...), Name: p.Name})
			}
		}
	})
	return out
}

// Problems lists every node-level error in the tree, prefixed with the
// breadcrumb of the module it occurred at, plus any operation that declares no
// recognized action. These are the failures that make an inspection incomplete.
func (i *Inspection) Problems() []string {
	var out []string
	i.walk(nil, func(path []string, node *Inspection) {
		crumb := strings.Join(path, " › ")
		if node.Error != "" {
			out = append(out, fmt.Sprintf("%s: %s", crumb, node.Error))
		}
		for _, op := range node.Operations {
			if op.Error != "" {
				out = append(out, fmt.Sprintf("%s: %s", crumb, op.Error))
			}
		}
	})
	return out
}

// Unexpanded lists the breadcrumb of every module that was named but not read —
// past the depth limit, or a remote left unfetched. While this is non-empty, no
// statement about the tree's parameters is complete.
func (i *Inspection) Unexpanded() [][]string {
	var out [][]string
	i.walk(nil, func(path []string, node *Inspection) {
		if node.Listed || node.Unfetched {
			out = append(out, append([]string(nil), path...))
		}
	})
	return out
}

// FindModule locates one module by instance name, or by a "/"-separated path of
// instance names. The query matches against the tail of a module's breadcrumb,
// so "docs" finds a module wherever it sits and "svc-a/docs" picks between two
// that share a name. The breadcrumb of the match is returned with it.
//
// Not finding exactly one is an error naming the candidates, since a query that
// silently picked one of several would describe the wrong module.
func (i *Inspection) FindModule(query string) (*Inspection, []string, error) {
	want := strings.Split(query, "/")
	var matches []*Inspection
	var paths [][]string
	var all []string

	i.walk(nil, func(path []string, node *Inspection) {
		all = append(all, strings.Join(path, "/"))
		if hasSuffix(path, want) {
			matches = append(matches, node)
			paths = append(paths, append([]string(nil), path...))
		}
	})

	switch len(matches) {
	case 1:
		return matches[0], paths[0], nil
	case 0:
		return nil, nil, fmt.Errorf("no module %q in this tree; it holds: %s", query, strings.Join(all, ", "))
	default:
		var found []string
		for _, p := range paths {
			found = append(found, strings.Join(p, "/"))
		}
		return nil, nil, fmt.Errorf("%q matches %d modules (%s); name one of them in full", query, len(matches), strings.Join(found, ", "))
	}
}

// Prune reduces an already-walked tree to depth levels of described module,
// turning everything below into the same listed stubs a depth-limited walk
// produces. It exists for the walks that cannot know their limit up front —
// finding a module by name means reading the tree that holds it — so the result
// still reads like one asked for at that depth. A depth of zero or less is a
// no-op.
func (i *Inspection) Prune(depth int) {
	if i == nil || depth <= 0 {
		return
	}
	if depth == 1 {
		for _, child := range i.Children {
			child.reduceToStub()
		}
		return
	}
	for _, child := range i.Children {
		child.Prune(depth - 1)
	}
}

// reduceToStub strips a node back to what its parent declares about it, so a
// pruned module is indistinguishable from one the walk never opened.
func (i *Inspection) reduceToStub() {
	stub := Inspection{
		Instance:       i.Instance,
		Source:         i.Source,
		SourceTemplate: i.SourceTemplate,
		Remote:         i.Remote,
		If:             i.If,
		Listed:         true,
	}
	*i = stub
}

// hasSuffix reports whether path ends with the segments in want.
func hasSuffix(path, want []string) bool {
	if len(want) > len(path) {
		return false
	}
	return equalSegments(path[len(path)-len(want):], want)
}

func equalSegments(a, b []string) bool {
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// walk visits every node depth-first in declaration order, passing the instance
// breadcrumb from the root down to and including the visited node.
func (i *Inspection) walk(prefix []string, fn func(path []string, node *Inspection)) {
	if i == nil {
		return
	}
	path := append(append([]string(nil), prefix...), i.Instance)
	fn(path, i)
	for _, child := range i.Children {
		child.walk(path, fn)
	}
}

// render templates s with the params known so far. The bool reports whether
// every referenced key was available: on a parse/execute error or a
// "<no value>" substitution it is false, and the caller decides whether to show
// the expression, warn, or stop.
func render(s string, params map[string]string) (string, bool) {
	if s == "" || !strings.Contains(s, "{{") {
		return s, true
	}
	out, err := tmpl.RenderString(s, params)
	if err != nil {
		return s, false
	}
	if strings.Contains(out, noValue) {
		return out, false
	}
	return out, true
}

// renderOrKeep renders s, falling back to the template text when it references
// a value inspect does not have.
func renderOrKeep(s string, params map[string]string) string {
	if out, ok := render(s, params); ok {
		return out
	}
	return s
}

// knownValues reduces a param table to the values templates may safely use.
// A missing, unset, unresolved, or dynamic param is deliberately absent rather
// than empty, so a template touching it renders to noValue and is caught.
func knownValues(params []Param) map[string]string {
	known := make(map[string]string, len(params))
	for _, p := range params {
		switch p.State {
		case ParamProvided, ParamDefault:
			known[p.Name] = p.Value
		}
	}
	return known
}

// sourceIdentity is what a module is compared against for cycle detection: the
// absolute directory a local source resolves to — mirroring how ResolveSource
// resolves it, relative to the parent — and the URL itself for a remote one,
// since each remote fetch lands in a fresh temp dir that no path could match.
func sourceIdentity(source, parentDir string) string {
	if !isLocalSource(source) {
		return source
	}
	path := source
	if strings.HasPrefix(source, ".") {
		path = filepath.Join(parentDir, source)
	}
	return localIdentity(path)
}

// localIdentity reduces a directory to the absolute path two sources reaching
// the same module would share, falling back to the path as given when it cannot
// be made absolute.
func localIdentity(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	return abs
}

// isLocalSource mirrors ResolveSource's rule: a source is a local path when it
// starts with "." or "/", and a git URL otherwise.
func isLocalSource(source string) bool {
	return strings.HasPrefix(source, ".") || strings.HasPrefix(source, "/")
}

func hasValue(m map[string]string, k string) bool {
	_, ok := m[k]
	return ok
}

func contains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}

// sortedKeys keeps map-derived output (undeclared-param warnings) stable across
// runs, since Go map iteration order is not.
func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
