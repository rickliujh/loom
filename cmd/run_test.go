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
	targetPath = ""
	params = nil
	paramsFile = ""
	verbose = false
	logLevel = "info"
	logFormat = "pretty"

	diffQuick = false
	diffPartial = false
	diffTargetPath = ""
	diffParams = nil
	diffParamsFile = ""
	diffAuthor = ""
	diffEmail = ""

	aliasParams = nil
	aliasForce = false
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

// Run with default "." (no args) resolves current-dir module.
func TestRun_DefaultDot(t *testing.T) {
	resetFlags()

	moduleDir := t.TempDir()
	targetDir := t.TempDir()

	srcDir := filepath.Join(moduleDir, "templates")
	os.MkdirAll(srcDir, 0o755)
	os.WriteFile(filepath.Join(srcDir, "dot.txt"), []byte("from dot"), 0o644)

	writeLoomYAML(t, moduleDir, `
apiVersion: loom.rickliujh.github.io/v1beta1
kind: Loom
metadata:
  name: test-dot
spec:
  operations:
    - name: write-files
      newFiles:
        source: "templates"
        dest: ""
`)

	// Change to moduleDir so "." resolves to it.
	orig, _ := os.Getwd()
	os.Chdir(moduleDir)
	defer os.Chdir(orig)

	rootCmd.SetArgs([]string{"run", "--target-path", targetDir})
	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, err := os.Stat(filepath.Join(targetDir, "dot.txt")); err != nil {
		t.Error("expected dot.txt in target dir")
	}
}

// Run with git URL (local file:// repo) clones and loads module.
func TestRun_GitSource(t *testing.T) {
	resetFlags()

	// Create a bare-ish git repo with a loom.yaml.
	repoDir := t.TempDir()
	initGitRepo(t, repoDir)

	srcDir := filepath.Join(repoDir, "templates")
	os.MkdirAll(srcDir, 0o755)
	os.WriteFile(filepath.Join(srcDir, "git.txt"), []byte("from git"), 0o644)

	writeLoomYAML(t, repoDir, `
apiVersion: loom.rickliujh.github.io/v1beta1
kind: Loom
metadata:
  name: test-git
spec:
  operations:
    - name: write-files
      newFiles:
        source: "templates"
        dest: ""
`)

	// Commit loom.yaml + templates so clone picks them up.
	for _, args := range [][]string{
		{"git", "-C", repoDir, "add", "."},
		{"git", "-C", repoDir, "commit", "-m", "add module"},
	} {
		if out, err := exec.Command(args[0], args[1:]...).CombinedOutput(); err != nil {
			t.Fatalf("git %v failed: %v\n%s", args, err, out)
		}
	}

	targetDir := t.TempDir()
	rootCmd.SetArgs([]string{"run", repoDir, "--target-path", targetDir})
	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	content, err := os.ReadFile(filepath.Join(targetDir, "git.txt"))
	if err != nil {
		t.Fatal("expected git.txt in target dir")
	}
	if string(content) != "from git" {
		t.Errorf("expected 'from git', got %q", string(content))
	}
}

// Run with git URL + //subdir resolves to subdirectory.
func TestRun_GitSourceWithSubdir(t *testing.T) {
	resetFlags()

	repoDir := t.TempDir()
	initGitRepo(t, repoDir)

	// Module lives in subdir "modules/networking".
	modDir := filepath.Join(repoDir, "modules", "networking")
	os.MkdirAll(modDir, 0o755)

	srcDir := filepath.Join(modDir, "templates")
	os.MkdirAll(srcDir, 0o755)
	os.WriteFile(filepath.Join(srcDir, "sub.txt"), []byte("from subdir"), 0o644)

	writeLoomYAML(t, modDir, `
apiVersion: loom.rickliujh.github.io/v1beta1
kind: Loom
metadata:
  name: test-subdir
spec:
  operations:
    - name: write-files
      newFiles:
        source: "templates"
        dest: ""
`)

	for _, args := range [][]string{
		{"git", "-C", repoDir, "add", "."},
		{"git", "-C", repoDir, "commit", "-m", "add subdir module"},
	} {
		if out, err := exec.Command(args[0], args[1:]...).CombinedOutput(); err != nil {
			t.Fatalf("git %v failed: %v\n%s", args, err, out)
		}
	}

	targetDir := t.TempDir()
	// Use //modules/networking to point at subdir.
	rootCmd.SetArgs([]string{"run", repoDir + "//modules/networking", "--target-path", targetDir})
	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	content, err := os.ReadFile(filepath.Join(targetDir, "sub.txt"))
	if err != nil {
		t.Fatal("expected sub.txt in target dir")
	}
	if string(content) != "from subdir" {
		t.Errorf("expected 'from subdir', got %q", string(content))
	}
}
