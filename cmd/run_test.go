package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// writeLoomYAML creates a minimal loom.yaml in dir.
func writeLoomYAML(t *testing.T, dir, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "loom.yaml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// initGitRepo creates a git repo with an initial commit.
func initGitRepo(t *testing.T, dir string) {
	t.Helper()
	for _, args := range [][]string{
		{"git", "init", dir},
		{"git", "-C", dir, "config", "user.email", "test@test.com"},
		{"git", "-C", dir, "config", "user.name", "Test"},
	} {
		if out, err := exec.Command(args[0], args[1:]...).CombinedOutput(); err != nil {
			t.Fatalf("git %v failed: %v\n%s", args, err, out)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"git", "-C", dir, "add", "."},
		{"git", "-C", dir, "commit", "-m", "init"},
	} {
		if out, err := exec.Command(args[0], args[1:]...).CombinedOutput(); err != nil {
			t.Fatalf("git %v failed: %v\n%s", args, err, out)
		}
	}
}

// resetFlags resets package-level flag vars to defaults before each test,
// since cobra flag parsing mutates them.
func resetFlags() {
	dryRun = false
	localRun = false
	showDiff = false
	targetPath = ""
	params = nil
	paramsFile = ""
	verbose = false
	logLevel = "info"
	logFormat = "pretty"
}

// L1: --local-run without --target-path errors (with target spec).
func TestRun_LocalWithoutTargetPath_Errors(t *testing.T) {
	resetFlags()

	moduleDir := t.TempDir()
	writeLoomYAML(t, moduleDir, `
apiVersion: loom.rickliujh.github.io/v1beta1
kind: Loom
metadata:
  name: test-mod
spec:
  target:
    url: "https://github.com/example/repo.git"
  operations: []
`)

	rootCmd.SetArgs([]string{"run", moduleDir, "--local-run"})
	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("expected error when --local-run is used without --target-path")
	}
	if !strings.Contains(err.Error(), "--local-run requires --target-path") {
		t.Errorf("expected '--local-run requires --target-path' error, got: %v", err)
	}
}

// L1: --local-run without --target-path errors even without target spec.
func TestRun_LocalWithoutTargetPath_NoTargetSpec_Errors(t *testing.T) {
	resetFlags()

	moduleDir := t.TempDir()
	writeLoomYAML(t, moduleDir, `
apiVersion: loom.rickliujh.github.io/v1beta1
kind: Loom
metadata:
  name: test-mod
spec:
  operations: []
`)

	rootCmd.SetArgs([]string{"run", moduleDir, "--local-run"})
	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("expected error when --local-run is used without --target-path")
	}
	if !strings.Contains(err.Error(), "--local-run requires --target-path") {
		t.Errorf("expected '--local-run requires --target-path' error, got: %v", err)
	}
}

// Integration: --local-run with --target-path runs newFiles successfully.
func TestRun_LocalWithTargetPath_Succeeds(t *testing.T) {
	resetFlags()

	moduleDir := t.TempDir()
	targetDir := t.TempDir()
	initGitRepo(t, targetDir)

	srcDir := filepath.Join(moduleDir, "templates")
	os.MkdirAll(srcDir, 0o755)
	os.WriteFile(filepath.Join(srcDir, "hello.txt"), []byte("hello world"), 0o644)

	writeLoomYAML(t, moduleDir, `
apiVersion: loom.rickliujh.github.io/v1beta1
kind: Loom
metadata:
  name: test-local
spec:
  operations:
    - name: write-files
      newFiles:
        source: "templates"
        dest: ""
`)

	rootCmd.SetArgs([]string{"run", moduleDir, "--local-run", "--target-path", targetDir})
	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	content, err := os.ReadFile(filepath.Join(targetDir, "hello.txt"))
	if err != nil {
		t.Fatal("expected hello.txt in target dir")
	}
	if string(content) != "hello world" {
		t.Errorf("expected 'hello world', got %q", string(content))
	}
}

// DR2: --diff implies --dry-run — no files written.
func TestRun_DiffImpliesDryRun(t *testing.T) {
	resetFlags()

	moduleDir := t.TempDir()
	targetDir := t.TempDir()

	srcDir := filepath.Join(moduleDir, "templates")
	os.MkdirAll(srcDir, 0o755)
	os.WriteFile(filepath.Join(srcDir, "new.txt"), []byte("new content\n"), 0o644)

	writeLoomYAML(t, moduleDir, `
apiVersion: loom.rickliujh.github.io/v1beta1
kind: Loom
metadata:
  name: test-diff
spec:
  operations:
    - name: write-files
      newFiles:
        source: "templates"
        dest: ""
`)

	rootCmd.SetArgs([]string{"run", moduleDir, "--diff", "--target-path", targetDir})
	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, err := os.Stat(filepath.Join(targetDir, "new.txt")); !os.IsNotExist(err) {
		t.Error("--diff should imply --dry-run, file should not be written")
	}
}
