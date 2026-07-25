package module

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rickliujh/loom/pkg/config"
)

// writeModule writes a loom.yaml under root/name and returns its directory.
func writeModule(t *testing.T, root, name, body string) string {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	header := "apiVersion: loom.rickliujh.github.io/v1beta1\nkind: Loom\nmetadata:\n  name: " + name + "\n"
	if err := os.WriteFile(filepath.Join(dir, "loom.yaml"), []byte(header+body), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func inspectDir(t *testing.T, dir string, opts InspectOptions) *Inspection {
	t.Helper()
	opts.Logger = testLogger()
	tree, err := Inspect(dir, opts)
	if err != nil {
		t.Fatalf("Inspect(%s): %v", dir, err)
	}
	return tree
}

// findParam returns the named parameter from a node, failing if absent.
func findParam(t *testing.T, node *Inspection, name string) Param {
	t.Helper()
	for _, p := range node.Params {
		if p.Name == name {
			return p
		}
	}
	t.Fatalf("param %q not found in module %q", name, node.Instance)
	return Param{}
}

// IN1: Inspect executes nothing — no dynamic param command, no if condition.
func TestInspect_IN1_ExecutesNothing(t *testing.T) {
	root := t.TempDir()
	// The commands below would leave these files behind if they ever ran.
	sentinel := filepath.Join(root, "dynamic-ran")
	gate := filepath.Join(root, "condition-ran")
	dir := writeModule(t, root, "side-effects", `spec:
  dynamicParams:
    - name: stamp
      command: "touch `+sentinel+` && echo ran"
  operations:
    - name: gated
      if: "touch `+gate+`"
      shell:
        command: "echo never"
`)

	tree := inspectDir(t, dir, InspectOptions{})

	if _, err := os.Stat(sentinel); err == nil {
		t.Error("dynamic parameter command ran during inspect")
	}
	if _, err := os.Stat(gate); err == nil {
		t.Error("if condition ran during inspect")
	}

	stamp := findParam(t, tree, "stamp")
	if stamp.State != ParamDynamic {
		t.Errorf("stamp state = %q, want %q", stamp.State, ParamDynamic)
	}
	if stamp.Value != "" {
		t.Errorf("stamp has value %q; inspect must not produce one", stamp.Value)
	}
	// The gated operation is still described — inspect shows what could run.
	if len(tree.Operations) != 1 || tree.Operations[0].Name != "gated" {
		t.Fatalf("operations = %+v, want the gated operation described", tree.Operations)
	}
	if tree.Operations[0].If == "" {
		t.Error("the operation's if condition should be reported as authored")
	}
}

// IN2: Submodules and operations keep declaration order — the order a run
// dispatches them in.
func TestInspect_IN2_DeclarationOrderPreserved(t *testing.T) {
	root := t.TempDir()
	writeModule(t, root, "zeta", "spec: {}\n")
	writeModule(t, root, "alpha", "spec: {}\n")
	dir := writeModule(t, root, "ordered", `spec:
  modules:
    - name: second
      source: ../zeta
    - name: first
      source: ../alpha
  operations:
    - name: op-c
      shell:
        command: "echo c"
    - name: op-a
      shell:
        command: "echo a"
`)

	tree := inspectDir(t, dir, InspectOptions{})

	gotModules := []string{tree.Children[0].Instance, tree.Children[1].Instance}
	if want := []string{"second", "first"}; !equalStrings(gotModules, want) {
		t.Errorf("module order = %v, want %v", gotModules, want)
	}
	gotOps := []string{tree.Operations[0].Name, tree.Operations[1].Name}
	if want := []string{"op-c", "op-a"}; !equalStrings(gotOps, want) {
		t.Errorf("operation order = %v, want %v", gotOps, want)
	}
}

// IN3: A child is identified by its rendered instance name, with metadata.name
// reported alongside; the root falls back to its own metadata.name.
func TestInspect_IN3_InstanceNameIdentifiesModule(t *testing.T) {
	root := t.TempDir()
	writeModule(t, root, "worker", "spec: {}\n")
	dir := writeModule(t, root, "namer", `spec:
  params:
    - name: env
      default: staging
  modules:
    - name: "deploy-{{ .env }}"
      source: ../worker
`)

	tree := inspectDir(t, dir, InspectOptions{})

	if tree.Instance != "namer" {
		t.Errorf("root instance = %q, want the module's own metadata.name", tree.Instance)
	}
	child := tree.Children[0]
	if child.Instance != "deploy-staging" {
		t.Errorf("child instance = %q, want the rendered modules[].name", child.Instance)
	}
	if child.Name != "worker" {
		t.Errorf("child metadata name = %q, want %q", child.Name, "worker")
	}
}

// IN4: A module that composes an ancestor is reported as a cycle and not
// expanded; the same module composed twice off one parent is not a cycle.
func TestInspect_IN4_CycleStopsTheWalk(t *testing.T) {
	root := t.TempDir()
	writeModule(t, root, "b", `spec:
  modules:
    - name: back-to-a
      source: ../a
`)
	dir := writeModule(t, root, "a", `spec:
  modules:
    - name: down
      source: ../b
`)

	tree := inspectDir(t, dir, InspectOptions{})

	backToA := tree.Children[0].Children[0]
	if !backToA.Cycle {
		t.Errorf("module %q should be reported as a cycle", backToA.Instance)
	}
	if len(backToA.Children) != 0 {
		t.Error("a cycle must not be expanded")
	}

	// The same source twice under one parent is composition, not recursion.
	writeModule(t, root, "leaf", "spec: {}\n")
	twice := writeModule(t, root, "twice", `spec:
  modules:
    - name: one
      source: ../leaf
    - name: two
      source: ../leaf
`)
	tree = inspectDir(t, twice, InspectOptions{})
	for _, child := range tree.Children {
		if child.Cycle {
			t.Errorf("module %q wrongly reported as a cycle", child.Instance)
		}
		if child.Name != "leaf" {
			t.Errorf("module %q should be described in full, got name %q", child.Instance, child.Name)
		}
	}
}

// IN5: A module past the depth limit is listed from what its parent declares,
// not dropped and not read.
func TestInspect_IN5_ModulesPastTheLimitAreListed(t *testing.T) {
	root := t.TempDir()
	writeModule(t, root, "grandchild", "spec: {}\n")
	writeModule(t, root, "child", `spec:
  params:
    - name: needed
      required: true
  modules:
    - name: gc
      source: ../grandchild
`)
	dir := writeModule(t, root, "top", `spec:
  modules:
    - name: c
      source: ../child
      if: "test -d somewhere"
`)

	tree := inspectDir(t, dir, InspectOptions{MaxDepth: 1})
	if len(tree.Children) != 1 {
		t.Fatalf("depth 1 should still list the submodule, got %d children", len(tree.Children))
	}
	listed := tree.Children[0]
	if !listed.Listed {
		t.Error("a module past the limit should be marked listed")
	}
	// What the parent declares is known; what the module itself says is not.
	if listed.Instance != "c" || listed.Source != "../child" || listed.If != "test -d somewhere" {
		t.Errorf("listed module = %+v, want the parent's declaration carried through", listed)
	}
	if listed.Name != "" || len(listed.Params) != 0 || len(listed.Children) != 0 {
		t.Error("a listed module must not be read")
	}
	// Its requirements are therefore unknown, not absent.
	if len(tree.MissingParams()) != 0 {
		t.Error("an unread module's params must not be reported as satisfied or missing")
	}
	if crumbs := tree.Unexpanded(); len(crumbs) != 1 || strings.Join(crumbs[0], "/") != "top/c" {
		t.Errorf("Unexpanded() = %v, want the listed module", crumbs)
	}

	tree = inspectDir(t, dir, InspectOptions{MaxDepth: 2})
	child := tree.Children[0]
	if child.Listed || child.Name != "child" {
		t.Error("depth 2 should describe the child")
	}
	if len(child.Children) != 1 || !child.Children[0].Listed {
		t.Errorf("depth 2 should list the grandchild, got %+v", child.Children)
	}
	if len(tree.MissingParams()) != 1 {
		t.Error("the described child's missing param should now be reported")
	}

	tree = inspectDir(t, dir, InspectOptions{})
	if gc := tree.Children[0].Children[0]; gc.Listed || gc.Name != "grandchild" {
		t.Error("depth 0 should describe the whole tree")
	}
	if len(tree.Unexpanded()) != 0 {
		t.Error("nothing is unexpanded once the whole tree is described")
	}
}

// IN16: One module is described by default — the subject, with what it composes
// listed rather than read.
func TestInspect_IN16_DefaultDescribesOneModule(t *testing.T) {
	root := t.TempDir()
	writeModule(t, root, "child", `spec:
  operations:
    - name: op
      shell:
        command: "echo hi"
`)
	dir := writeModule(t, root, "top", `spec:
  params:
    - name: env
      default: dev
  operations:
    - name: announce
      shell:
        command: "echo go"
  modules:
    - name: c
      source: ../child
`)

	// MaxDepth 1 is what the CLI defaults to.
	tree := inspectDir(t, dir, InspectOptions{MaxDepth: 1})

	if len(tree.Params) != 1 || len(tree.Operations) != 1 {
		t.Error("the subject itself should be described in full")
	}
	if len(tree.Children) != 1 || !tree.Children[0].Listed {
		t.Errorf("what it composes should be listed, got %+v", tree.Children)
	}
}

// IN17: Any module in the tree can be made the subject, by name or by path.
func TestInspect_IN17_FindModuleSelectsTheSubject(t *testing.T) {
	root := t.TempDir()
	writeModule(t, root, "docs", `spec:
  params:
    - name: title
      required: true
`)
	writeModule(t, root, "svc", `spec:
  params:
    - name: service
      required: true
  modules:
    - name: docs
      source: ../docs
      params:
        title: "{{ .service }} docs"
`)
	dir := writeModule(t, root, "top", `spec:
  modules:
    - name: a
      source: ../svc
      params:
        service: alpha
    - name: b
      source: ../svc
      params:
        service: beta
`)

	tree := inspectDir(t, dir, InspectOptions{})

	// A bare name selects a uniquely-named module wherever it sits.
	node, path, err := tree.FindModule("a")
	if err != nil {
		t.Fatalf("FindModule(a): %v", err)
	}
	if node.Name != "svc" || strings.Join(path, "/") != "top/a" {
		t.Errorf("got %s at %v, want the svc module at top/a", node.Name, path)
	}

	// A shared name needs qualifying, and the error says so.
	if _, _, err := tree.FindModule("docs"); err == nil {
		t.Error("an ambiguous name should be an error")
	} else if !strings.Contains(err.Error(), "top/a/docs") || !strings.Contains(err.Error(), "top/b/docs") {
		t.Errorf("ambiguity error should name the candidates, got: %v", err)
	}

	node, path, err = tree.FindModule("b/docs")
	if err != nil {
		t.Fatalf("FindModule(b/docs): %v", err)
	}
	if strings.Join(path, "/") != "top/b/docs" {
		t.Errorf("path = %v, want top/b/docs", path)
	}
	// Values resolved on the way down are what this module actually receives.
	if title := findParam(t, node, "title"); title.Value != "beta docs" {
		t.Errorf("title = %q, want the value its parent hands it", title.Value)
	}

	if _, _, err := tree.FindModule("nope"); err == nil {
		t.Error("an unknown name should be an error")
	}

	// Pruning a found subtree yields the same listed stubs a shallow walk does.
	node, _, _ = tree.FindModule("a")
	node.Prune(1)
	if len(node.Children) != 1 || !node.Children[0].Listed {
		t.Errorf("Prune(1) should list what the subject composes, got %+v", node.Children)
	}
	if node.Children[0].Name != "" || len(node.Children[0].Params) != 0 {
		t.Error("a pruned module should be indistinguishable from one never read")
	}
}

// IN6: Each declared parameter is reported with the origin of its value, and a
// supplied value wins over both a default (P1) and a dynamic command (P6).
func TestInspect_IN6_ParamStates(t *testing.T) {
	root := t.TempDir()
	dir := writeModule(t, root, "params", `spec:
  params:
    - name: supplied
      required: true
    - name: fromDefault
      default: fallback
    - name: needed
      required: true
    - name: optional
  dynamicParams:
    - name: stamp
      command: "git rev-parse HEAD"
    - name: overridden
      command: "hostname"
`)

	tree := inspectDir(t, dir, InspectOptions{Params: map[string]string{
		"supplied":   "yes",
		"overridden": "cli-wins",
	}})

	cases := []struct {
		name  string
		state ParamState
		value string
	}{
		{"supplied", ParamProvided, "yes"},
		{"fromDefault", ParamDefault, "fallback"},
		{"needed", ParamMissing, ""},
		{"optional", ParamUnset, ""},
		{"stamp", ParamDynamic, ""},
		{"overridden", ParamProvided, "cli-wins"},
	}
	for _, tc := range cases {
		p := findParam(t, tree, tc.name)
		if p.State != tc.state {
			t.Errorf("%s state = %q, want %q", tc.name, p.State, tc.state)
		}
		if p.Value != tc.value {
			t.Errorf("%s value = %q, want %q", tc.name, p.Value, tc.value)
		}
	}
	if cmd := findParam(t, tree, "stamp").Command; cmd != "git rev-parse HEAD" {
		t.Errorf("dynamic param command = %q, want it reported as authored", cmd)
	}
}

// IN7: A parent-supplied value carries the expression that produced it.
func TestInspect_IN7_ParamTracedToParentExpression(t *testing.T) {
	root := t.TempDir()
	writeModule(t, root, "child", `spec:
  params:
    - name: namespace
    - name: literal
`)
	dir := writeModule(t, root, "parent", `spec:
  params:
    - name: env
      default: prod
  modules:
    - name: c
      source: ../child
      params:
        namespace: "{{ .env }}-apps"
        literal: fixed
`)

	child := inspectDir(t, dir, InspectOptions{}).Children[0]

	ns := findParam(t, child, "namespace")
	if ns.Value != "prod-apps" {
		t.Errorf("namespace = %q, want the rendered value", ns.Value)
	}
	if ns.From != "{{ .env }}-apps" {
		t.Errorf("namespace From = %q, want the authored expression", ns.From)
	}
	// A literal hand-off adds nothing to trace, so From stays empty.
	if lit := findParam(t, child, "literal"); lit.From != "" {
		t.Errorf("literal From = %q, want empty for a non-templated value", lit.From)
	}
}

// IN8: Templates needing run-time values are reported as unresolved, never as a
// placeholder value.
func TestInspect_IN8_UnresolvedTemplatesNeverBecomeValues(t *testing.T) {
	root := t.TempDir()
	writeModule(t, root, "child", `spec:
  params:
    - name: stamp
`)
	dir := writeModule(t, root, "parent", `spec:
  dynamicParams:
    - name: commitHash
      command: "git rev-parse --short HEAD"
  target:
    url: "https://example.com/repo.git"
    featureBranch: "loom/{{ .commitHash }}"
  modules:
    - name: c
      source: ../child
      params:
        stamp: "{{ .commitHash }}"
`)

	tree := inspectDir(t, dir, InspectOptions{})

	stamp := findParam(t, tree.Children[0], "stamp")
	if stamp.State != ParamUnresolved {
		t.Errorf("stamp state = %q, want %q", stamp.State, ParamUnresolved)
	}
	if stamp.Value != "" {
		t.Errorf("stamp value = %q, want empty rather than a placeholder", stamp.Value)
	}
	if strings.Contains(stamp.Value, noValue) {
		t.Error("a template placeholder leaked into a parameter value")
	}
	if got := tree.Target.FeatureBranch; got != "loom/{{ .commitHash }}" {
		t.Errorf("featureBranch = %q, want the template text kept", got)
	}

	// An unresolvable source is an error, and the child is not descended into.
	unresolvableSrc := writeModule(t, root, "bad-source", `spec:
  dynamicParams:
    - name: which
      command: "echo child"
  modules:
    - name: c
      source: "../{{ .which }}"
`)
	child := inspectDir(t, unresolvableSrc, InspectOptions{}).Children[0]
	if child.Error == "" {
		t.Error("a source depending on a run-time value should be an error")
	}
	if child.Name != "" {
		t.Error("inspect must not descend into a module whose source it cannot resolve")
	}
}

// IN9: Missing required params are collected tree-wide, located by breadcrumb,
// and do not fail the inspection.
func TestInspect_IN9_MissingParamsCollectedTreeWide(t *testing.T) {
	root := t.TempDir()
	writeModule(t, root, "child", `spec:
  params:
    - name: region
      required: true
`)
	dir := writeModule(t, root, "parent", `spec:
  params:
    - name: env
      required: true
  modules:
    - name: a
      source: ../child
    - name: b
      source: ../child
`)

	tree := inspectDir(t, dir, InspectOptions{})

	missing := tree.MissingParams()
	if len(missing) != 3 {
		t.Fatalf("missing params = %+v, want 3", missing)
	}
	want := []struct {
		name string
		path string
	}{
		{"env", "parent"},
		{"region", "parent › a"},
		{"region", "parent › b"},
	}
	for i, w := range want {
		if missing[i].Name != w.name || strings.Join(missing[i].Path, " › ") != w.path {
			t.Errorf("missing[%d] = %s at %v, want %s at %s", i, missing[i].Name, missing[i].Path, w.name, w.path)
		}
	}
	if problems := tree.Problems(); len(problems) != 0 {
		t.Errorf("missing params must not be problems, got %v", problems)
	}
}

// IN10: A param supplied to a module that does not declare it warns, and the
// module is still described.
func TestInspect_IN10_UndeclaredParamWarns(t *testing.T) {
	root := t.TempDir()
	writeModule(t, root, "child", `spec:
  params:
    - name: known
  operations:
    - name: op
      shell:
        command: "echo hi"
`)
	dir := writeModule(t, root, "parent", `spec:
  modules:
    - name: c
      source: ../child
      params:
        known: fine
        surprise: nope
`)

	child := inspectDir(t, dir, InspectOptions{}).Children[0]

	if len(child.Warnings) != 1 || !strings.Contains(child.Warnings[0], `"surprise"`) {
		t.Fatalf("warnings = %v, want one naming the undeclared param", child.Warnings)
	}
	if len(child.Operations) != 1 {
		t.Error("the module should still be described in full")
	}
	if child.Error != "" {
		t.Errorf("an undeclared param must not fail the module: %s", child.Error)
	}
}

// IN11: Operations are reported with name, kind, summary, and condition.
func TestInspect_IN11_OperationsDescribed(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "ops", "templates"), 0o755); err != nil {
		t.Fatal(err)
	}
	dir := writeModule(t, root, "ops", `spec:
  params:
    - name: service
      required: true
  operations:
    - name: render
      newFiles:
        source: templates
        dest: manifests
    - name: run-it
      if: "test -d manifests"
      shell:
        command: "kubeconform manifests"
    - name: open
      pr:
        provider: github
        title: "Onboard {{ .service }}"
`)

	tree := inspectDir(t, dir, InspectOptions{})

	want := []struct{ name, kind, detail, cond string }{
		{"render", "newFiles", "templates → manifests", ""},
		{"run-it", "shell", "kubeconform manifests", "test -d manifests"},
		{"open", "pr", "github: Onboard {{ .service }}", ""},
	}
	if len(tree.Operations) != len(want) {
		t.Fatalf("got %d operations, want %d", len(tree.Operations), len(want))
	}
	for i, w := range want {
		op := tree.Operations[i]
		if op.Name != w.name || op.Kind != w.kind || op.Detail != w.detail || op.If != w.cond {
			t.Errorf("operation[%d] = %+v, want name=%s kind=%s detail=%s if=%s", i, op, w.name, w.kind, w.detail, w.cond)
		}
	}
}

// IN12: An operation with no recognized action is never silently dropped —
// validation rejects the module naming it, and the describing step itself
// reports rather than skips it.
func TestInspect_IN12_OperationNeverSilentlyDropped(t *testing.T) {
	root := t.TempDir()
	writeModule(t, root, "child", `spec:
  operations:
    - name: empty
`)
	dir := writeModule(t, root, "parent", `spec:
  modules:
    - name: c
      source: ../child
`)

	tree := inspectDir(t, dir, InspectOptions{})

	child := tree.Children[0]
	if child.Error == "" || !strings.Contains(child.Error, `"empty"`) {
		t.Errorf("child error = %q, want it to name the actionless operation", child.Error)
	}
	problems := tree.Problems()
	if len(problems) != 1 || !strings.Contains(problems[0], "parent › c") {
		t.Errorf("problems = %v, want one located at the child", problems)
	}

	// The describing step's own guarantee, for a config that reaches it without
	// having been validated: the operation is listed, carrying its error.
	ops := describeOperations([]config.Operation{{Name: "actionless"}})
	if len(ops) != 1 {
		t.Fatalf("got %d operations, want the actionless one still listed", len(ops))
	}
	if ops[0].Error == "" {
		t.Error("an unrecognized operation should carry an error rather than be skipped")
	}
}

// IN13: A failure below the root is contained to its node; a root that cannot
// be described fails outright.
func TestInspect_IN13_FailureContainedToItsNode(t *testing.T) {
	root := t.TempDir()
	writeModule(t, root, "good", `spec:
  operations:
    - name: op
      shell:
        command: "echo ok"
`)
	dir := writeModule(t, root, "parent", `spec:
  modules:
    - name: broken
      source: ../does-not-exist
    - name: fine
      source: ../good
`)

	tree := inspectDir(t, dir, InspectOptions{})

	if tree.Children[0].Error == "" {
		t.Error("the unresolvable submodule should carry an error")
	}
	if got := tree.Children[1]; got.Name != "good" || len(got.Operations) != 1 {
		t.Error("a broken sibling must not stop the rest of the tree being described")
	}

	// A root that cannot be read is a hard failure — there is no tree.
	if _, err := Inspect(filepath.Join(root, "does-not-exist"), InspectOptions{Logger: testLogger()}); err == nil {
		t.Error("expected an error inspecting a nonexistent root")
	}
}

// IN14: Structural invalidity fails a module; a missing newFiles source only
// warns, and the module is still described.
func TestInspect_IN14_StructuralErrorsFailFilesystemOnesWarn(t *testing.T) {
	root := t.TempDir()
	// Duplicate operation names — structurally invalid.
	writeModule(t, root, "invalid", `spec:
  operations:
    - name: dup
      shell:
        command: "echo one"
    - name: dup
      shell:
        command: "echo two"
`)
	// Structurally valid, but the newFiles source directory is not there.
	writeModule(t, root, "missing-files", `spec:
  operations:
    - name: render
      newFiles:
        source: templates
`)
	dir := writeModule(t, root, "parent", `spec:
  modules:
    - name: bad
      source: ../invalid
    - name: incomplete
      source: ../missing-files
`)

	tree := inspectDir(t, dir, InspectOptions{})

	bad := tree.Children[0]
	if bad.Error == "" {
		t.Error("a structurally invalid module should be an error")
	}
	if len(bad.Operations) != 0 {
		t.Error("an invalid module's contents must not be described")
	}

	incomplete := tree.Children[1]
	if incomplete.Error != "" {
		t.Errorf("a missing newFiles source must not fail the module: %s", incomplete.Error)
	}
	if len(incomplete.Warnings) == 0 || !strings.Contains(strings.Join(incomplete.Warnings, "\n"), "templates") {
		t.Errorf("warnings = %v, want one naming the missing source", incomplete.Warnings)
	}
	if len(incomplete.Operations) != 1 {
		t.Error("the module should still be described in full")
	}
}

// A remote module is listed but not cloned when fetching is disabled.
func TestInspect_NoFetchSkipsRemoteModules(t *testing.T) {
	root := t.TempDir()
	dir := writeModule(t, root, "parent", `spec:
  modules:
    - name: remote
      source: https://example.invalid/mods.git//services/api
`)

	tree := inspectDir(t, dir, InspectOptions{NoFetch: true})

	child := tree.Children[0]
	if !child.Remote || !child.Unfetched {
		t.Errorf("child = %+v, want it marked remote and unfetched", child)
	}
	if child.Error != "" {
		t.Errorf("--no-fetch must not be an error: %s", child.Error)
	}
	if child.Source != "https://example.invalid/mods.git//services/api" {
		t.Errorf("source = %q, want it reported so the module is still identifiable", child.Source)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
