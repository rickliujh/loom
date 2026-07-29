package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeTree builds a parent module referencing ./child, where the child renders
// a template file. childTmpl is the body of that file, so a test can plant a
// violation only the child's own validation would find.
func writeTree(t *testing.T, childTmpl string) string {
	t.Helper()
	root := t.TempDir()
	child := filepath.Join(root, "child", "templates")
	if err := os.MkdirAll(child, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(child, "app.yaml"), []byte(childTmpl), 0o644); err != nil {
		t.Fatal(err)
	}

	writeLoomYAML(t, root, `
apiVersion: loom.rickliujh.github.io/v1beta1
kind: Loom
metadata:
  name: parent
spec:
  modules:
    - name: kid
      source: ./child
  operations: []
`)
	writeLoomYAML(t, filepath.Join(root, "child"), `
apiVersion: loom.rickliujh.github.io/v1beta1
kind: Loom
metadata:
  name: child
spec:
  params:
    - name: svc
  operations:
    - name: create-files
      newFiles:
        source: "templates"
        dest: ""
`)
	return root
}

// A referenced module is a separate config; by default it is not opened, so a
// violation inside it does not fail the parent.
func TestValidate_DefaultSkipsReferencedModules(t *testing.T) {
	resetFlags()
	root := writeTree(t, "name: {{ .typo }}\n")

	if err := validateModule(nil, []string{root}); err != nil {
		t.Fatalf("child violations must not fail a non-recursive validate: %v", err)
	}
}

func TestValidate_RecursiveReportsReferencedModules(t *testing.T) {
	resetFlags()
	validateRecursive = true
	root := writeTree(t, "name: {{ .typo }}\n")

	err := validateModule(nil, []string{root})
	if err == nil {
		t.Fatal("expected the child's violation to fail a recursive validate")
	}
	if !strings.Contains(err.Error(), "kid:") {
		t.Errorf("violation should name the module it came from: %v", err)
	}
	if !strings.Contains(err.Error(), `references undeclared param "typo"`) {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidate_RecursiveAcceptsCleanTree(t *testing.T) {
	resetFlags()
	validateRecursive = true
	root := writeTree(t, "name: {{ .svc }}\n")

	if err := validateModule(nil, []string{root}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// A templated source has no value until a run resolves params, so there is
// nothing to fetch — it is reported and skipped, not treated as a failure.
func TestValidate_RecursiveSkipsTemplatedSource(t *testing.T) {
	resetFlags()
	validateRecursive = true
	root := t.TempDir()
	writeLoomYAML(t, root, `
apiVersion: loom.rickliujh.github.io/v1beta1
kind: Loom
metadata:
  name: parent
spec:
  params:
    - name: which
      default: child
  modules:
    - name: kid
      source: "./{{ .which }}"
  operations: []
`)

	if err := validateModule(nil, []string{root}); err != nil {
		t.Fatalf("templated source must be skipped, not fail: %v", err)
	}
}

// An unused param is advisory: it never makes a config invalid.
func TestValidate_UnusedParamDoesNotFail(t *testing.T) {
	resetFlags()
	root := t.TempDir()
	writeLoomYAML(t, root, `
apiVersion: loom.rickliujh.github.io/v1beta1
kind: Loom
metadata:
  name: solo
spec:
  params:
    - name: unused
      default: x
  operations:
    - name: noop
      shell:
        command: "true"
`)

	if err := validateModule(nil, []string{root}); err != nil {
		t.Fatalf("unused param must not fail validate: %v", err)
	}
}
