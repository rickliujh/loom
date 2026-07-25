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

func inspectTreeOutput(t *testing.T, dir string, params map[string]string) string {
	t.Helper()
	tree, err := module.Inspect(dir, module.InspectOptions{Params: params, Logger: newLogger()})
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	printInspectTree(&buf, tree)
	return buf.String()
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
	tree, err := module.Inspect(inspectFixture(t), module.InspectOptions{
		Params: map[string]string{"env": "prod"},
		Logger: newLogger(),
	})
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := printInspectJSON(&buf, tree); err != nil {
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

// IN15: A tree that could not be fully described exits non-zero; missing
// parameters alone do not.
func TestInspect_IN15_ExitStatus(t *testing.T) {
	dir := inspectFixture(t)

	// Missing "region" throughout, yet everything is describable.
	if err := runInspectFor(t, dir, "-p", "env=prod"); err != nil {
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
	if err := runInspectFor(t, dir, ""); err == nil {
		t.Error("a module that could not be inspected should fail the command")
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
	inspectParams, inspectParamsFile, inspectDepth, inspectNoFetch, inspectOutput = nil, "", 0, false, "tree"
	rootCmd.SetArgs(argv)
	rootCmd.SetOut(devnull)
	rootCmd.SetErr(devnull)
	return rootCmd.Execute()
}
