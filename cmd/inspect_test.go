package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rickliujh/loom/pkg/module"
)

// inspectFixture writes a two-level module tree and returns the parent's dir.
func inspectFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()

	child := filepath.Join(root, "child")
	if err := os.MkdirAll(child, 0o755); err != nil {
		t.Fatal(err)
	}
	writeLoomYAML(t, child, `apiVersion: loom.rickliujh.github.io/v1beta1
kind: Loom
metadata:
  name: service-onboard
spec:
  params:
    - name: service
      required: true
    - name: region
      required: true
  target:
    url: "https://github.com/acme/{{ .service }}.git"
    branch: main
  operations:
    - name: check
      shell:
        command: "echo checking {{ .service }}"
`)

	parent := filepath.Join(root, "parent")
	if err := os.MkdirAll(parent, 0o755); err != nil {
		t.Fatal(err)
	}
	writeLoomYAML(t, parent, `apiVersion: loom.rickliujh.github.io/v1beta1
kind: Loom
metadata:
  name: rollout
spec:
  params:
    - name: env
      required: true
  modules:
    - name: "api-{{ .env }}"
      source: ../child
      params:
        service: api
`)
	return parent
}

// inspectTreeOutput renders a fully-expanded inspection, the view the tests
// that care about content want; depth is exercised separately.
func inspectTreeOutput(t *testing.T, dir string, params map[string]string) string {
	t.Helper()
	tree := inspectAll(t, dir, params)
	var buf bytes.Buffer
	printInspectTree(&buf, tree, []string{tree.Instance})
	return buf.String()
}

func inspectAll(t *testing.T, dir string, params map[string]string) *module.Inspection {
	t.Helper()
	tree, err := module.Inspect(dir, module.InspectOptions{Params: params, Logger: newLogger()})
	if err != nil {
		t.Fatal(err)
	}
	return tree
}

// The tree output carries the hierarchy, each module's operations, and the
// parameter requirements — the three things inspect exists to show.
func TestInspectTree_ShowsHierarchyOperationsAndParams(t *testing.T) {
	out := inspectTreeOutput(t, inspectFixture(t), map[string]string{"env": "prod"})

	for _, want := range []string{
		"rollout",         // the root module
		"▸ api-prod",      // the child, under its rendered instance name
		"service-onboard", // the child's own metadata name alongside it
		"params",
		"operations",
		"check",
		"shell",
		"echo checking {{ .service }}", // an unresolvable template kept as authored
		"https://github.com/acme/api.git (main)",
		`= "prod"`, // a supplied value
		`= "api"`,  // a value handed down by the parent
	} {
		if !strings.Contains(out, want) {
			t.Errorf("tree output missing %q:\n%s", want, out)
		}
	}
}

// IN9: The summary names every required parameter left unsupplied, with the
// breadcrumb of the module that declares it — and reports a satisfied tree.
func TestInspectTree_IN9_SummaryListsMissingParams(t *testing.T) {
	dir := inspectFixture(t)

	out := inspectTreeOutput(t, dir, map[string]string{"env": "prod"})
	if !strings.Contains(out, "region") || !strings.Contains(out, "rollout › api-prod") {
		t.Errorf("summary should locate the missing param at the child:\n%s", out)
	}

	// Supplied params reach the root only, so the child's requirement is
	// satisfied by inspecting that module directly with both values.
	child := filepath.Join(filepath.Dir(dir), "child")
	out = inspectTreeOutput(t, child, map[string]string{"service": "api", "region": "eu"})
	if !strings.Contains(out, "every required parameter is satisfied") {
		t.Errorf("a fully parameterized tree should say so:\n%s", out)
	}
	if strings.Contains(out, "a run would fail") {
		t.Errorf("no parameters are missing, so nothing should be reported:\n%s", out)
	}
}

// IN16: The default view describes one module and lists what it composes,
// without claiming to know the requirements of what it did not read.
func TestInspectTree_IN16_DefaultListsSubmodules(t *testing.T) {
	dir := inspectFixture(t)
	tree, err := module.Inspect(dir, module.InspectOptions{
		Params:   map[string]string{"env": "prod"},
		MaxDepth: 1,
		Logger:   newLogger(),
	})
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	printInspectTree(&buf, tree, []string{tree.Instance})
	out := buf.String()

	if !strings.Contains(out, "▸ api-prod") || !strings.Contains(out, "…") {
		t.Errorf("the submodule should be listed with a marker:\n%s", out)
	}
	// Read but not described: the child's own contents must not appear.
	if strings.Contains(out, "service-onboard") || strings.Contains(out, "region") {
		t.Errorf("a listed submodule must not be described:\n%s", out)
	}
	if !strings.Contains(out, "1 submodule(s) not expanded") || !strings.Contains(out, "--full") {
		t.Errorf("the summary should say what was skipped and how to see it:\n%s", out)
	}
	// The unqualified claim belongs only to a fully-described tree.
	if strings.Contains(out, "every required parameter is satisfied") {
		t.Errorf("must not claim the whole tree is satisfied when modules went unread:\n%s", out)
	}
	if !strings.Contains(out, "module(s) shown is satisfied") {
		t.Errorf("the satisfied claim should be scoped to what was shown:\n%s", out)
	}
}

// IN17: A focused module is headed by its breadcrumb, so its position in the
// tree is never in doubt, and the hint offered names a module that resolves.
func TestInspectTree_IN17_FocusedModuleShowsBreadcrumb(t *testing.T) {
	tree := inspectAll(t, inspectFixture(t), map[string]string{"env": "prod"})

	subject, path, err := tree.FindModule("api-prod")
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	printInspectTree(&buf, subject, path)
	out := buf.String()

	if !strings.Contains(out, "in rollout › api-prod") {
		t.Errorf("a focused module should be located in the tree:\n%s", out)
	}
	// Its missing params keep the full breadcrumb, not one relative to itself.
	if !strings.Contains(out, "region") || !strings.Contains(out, "rollout › api-prod") {
		t.Errorf("missing params should stay located from the root:\n%s", out)
	}
}

// The --module hint the summary prints has to be a query that resolves; a bare
// name is nicer, but not when submodules share it.
func TestInspectTree_HintNamesAResolvableModule(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"svc", "docs"} {
		dir := filepath.Join(root, name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeLoomYAML(t, filepath.Join(root, "docs"), `apiVersion: loom.rickliujh.github.io/v1beta1
kind: Loom
metadata:
  name: docs
spec: {}
`)
	writeLoomYAML(t, filepath.Join(root, "svc"), `apiVersion: loom.rickliujh.github.io/v1beta1
kind: Loom
metadata:
  name: svc
spec:
  modules:
    - name: docs
      source: ../docs
`)
	top := filepath.Join(root, "top")
	if err := os.MkdirAll(top, 0o755); err != nil {
		t.Fatal(err)
	}
	writeLoomYAML(t, top, `apiVersion: loom.rickliujh.github.io/v1beta1
kind: Loom
metadata:
  name: top
spec:
  modules:
    - name: a
      source: ../svc
    - name: b
      source: ../svc
`)

	// Both "a" and "b" compose a module called "docs", so at depth 2 the two
	// unexpanded modules share a name and the hint must qualify it.
	tree, err := module.Inspect(top, module.InspectOptions{MaxDepth: 2, Logger: newLogger()})
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	printInspectTree(&buf, tree, []string{tree.Instance})
	out := buf.String()

	if !strings.Contains(out, "--module a/docs") {
		t.Errorf("hint should qualify an ambiguous name:\n%s", out)
	}
	// And the suggestion must actually resolve.
	if _, _, err := tree.FindModule("a/docs"); err != nil {
		t.Errorf("the suggested query does not resolve: %v", err)
	}
}

// Without a terminal to write to, the tree carries no escape codes, so piped
// and captured output stays plain text.
func TestInspectTree_PlainWhenNotATerminal(t *testing.T) {
	out := inspectTreeOutput(t, inspectFixture(t), map[string]string{"env": "prod"})
	if strings.Contains(out, "\033[") {
		t.Errorf("output to a non-terminal should carry no escape codes:\n%q", out)
	}
}

// The JSON document carries the tree plus the roll-ups the tree view prints as
// footers, so a caller need not walk the tree to find them.
func TestInspectJSON_ReportShape(t *testing.T) {
	tree := inspectAll(t, inspectFixture(t), map[string]string{"env": "prod"})
	var buf bytes.Buffer
	if err := printInspectJSON(&buf, tree, []string{tree.Instance}); err != nil {
		t.Fatal(err)
	}

	var report inspectReport
	if err := json.Unmarshal(buf.Bytes(), &report); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, buf.String())
	}
	if report.Module.Instance != "rollout" {
		t.Errorf("module.instance = %q, want %q", report.Module.Instance, "rollout")
	}
	if len(report.Module.Children) != 1 || report.Module.Children[0].Instance != "api-prod" {
		t.Errorf("expected one child named api-prod, got %+v", report.Module.Children)
	}
	if len(report.MissingParams) != 1 || report.MissingParams[0].Name != "region" {
		t.Errorf("missingParams = %+v, want the child's region", report.MissingParams)
	}
	// Empty roll-ups are arrays, not null, so consumers can range over them.
	if !bytes.Contains(buf.Bytes(), []byte(`"problems": []`)) {
		t.Errorf("problems should marshal as an empty array:\n%s", buf.String())
	}
}

// IN15: A module that could not be described exits non-zero; missing parameters
// alone do not, and neither does a module that was never read.
func TestInspect_IN15_ExitStatus(t *testing.T) {
	dir := inspectFixture(t)

	// Missing "region" throughout, yet everything is describable.
	if err := runInspectFor(t, dir, "--full", "-p", "env=prod"); err != nil {
		t.Errorf("missing params must not fail the inspection: %v", err)
	}

	// Point the parent at a module that is not there.
	writeLoomYAML(t, dir, `apiVersion: loom.rickliujh.github.io/v1beta1
kind: Loom
metadata:
  name: rollout
spec:
  modules:
    - name: gone
      source: ../nowhere
`)
	if err := runInspectFor(t, dir, "--full"); err == nil {
		t.Error("a module that could not be inspected should fail the command")
	}
	// At the default depth that module is only listed, never resolved, so there
	// is nothing to have failed at — the summary reports it as unexpanded.
	if err := runInspectFor(t, dir); err != nil {
		t.Errorf("a listed module is not a failure: %v", err)
	}
}

// A --module query that matches no module, or several, fails rather than
// describing something the caller did not ask for.
func TestInspect_ModuleQueryErrors(t *testing.T) {
	dir := inspectFixture(t)

	if err := runInspectFor(t, dir, "-p", "env=prod", "-m", "nope"); err == nil {
		t.Error("an unknown --module should fail")
	}
	if err := runInspectFor(t, dir, "-p", "env=prod", "-m", "api-prod"); err != nil {
		t.Errorf("a resolvable --module should succeed: %v", err)
	}
}

// --full and --depth express the same limit two ways; setting both is a
// contradiction worth rejecting rather than silently resolving.
func TestInspect_FullAndDepthConflict(t *testing.T) {
	dir := inspectFixture(t)
	if err := runInspectFor(t, dir, "--full", "--depth", "2"); err == nil {
		t.Error("--full with an explicit --depth should fail")
	}
}

// runInspectFor drives the cobra command end to end with stdout captured, so
// the exit-status contract is exercised through the real entry point.
func runInspectFor(t *testing.T, dir string, args ...string) error {
	t.Helper()
	stdout := os.Stdout
	devnull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = devnull
	defer func() {
		os.Stdout = stdout
		devnull.Close()
	}()

	argv := []string{"inspect", dir}
	for _, a := range args {
		if a != "" {
			argv = append(argv, a)
		}
	}
	// Flags are package-level and persist across cobra invocations; reset the
	// ones this command owns so tests do not leak settings into each other.
	inspectParams, inspectParamsFile, inspectOutput = nil, "", "tree"
	inspectDepth, inspectFull, inspectModule, inspectNoFetch = 1, false, "", false
	rootCmd.SetArgs(argv)
	rootCmd.SetOut(devnull)
	rootCmd.SetErr(devnull)
	return rootCmd.Execute()
}
