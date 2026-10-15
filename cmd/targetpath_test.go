package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// WP1: a destination built from a param is confined at run time — validate
// cannot check it, because the value is unknown until the run.
func TestRun_WP1_TemplatedDestEscapes(t *testing.T) {
	resetFlags()

	root := t.TempDir()
	targetDir := filepath.Join(root, "target")
	outside := filepath.Join(root, "outside")
	for _, d := range []string{targetDir, outside} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	moduleDir := t.TempDir()
	srcDir := filepath.Join(moduleDir, "templates")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "note.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeLoomYAML(t, moduleDir, `
apiVersion: loom.rickliujh.github.io/v1beta1
kind: Loom
metadata:
  name: test-mod
spec:
  params:
    - name: dest
      required: true
  operations:
    - name: files
      newFiles:
        source: "templates"
        dest: "{{ .dest }}"
`)

	rootCmd.SetArgs([]string{"run", moduleDir, "-p", "dest=../outside", "--target-path", targetDir})
	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("expected an error for a dest that escapes the target directory")
	}
	if !strings.Contains(err.Error(), `newFiles dest "../outside" escapes the target directory`) {
		t.Errorf("unexpected error: %v", err)
	}
	entries, readErr := os.ReadDir(outside)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if len(entries) != 0 {
		t.Errorf("file written outside the target directory: %s", entries[0].Name())
	}
}
