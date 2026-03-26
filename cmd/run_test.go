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
	localOnly = false
	targetPath = ""
	params = nil
	paramsFile = ""
	verbose = false
	logLevel = "info"
	logFormat = "pretty"
}

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

	rootCmd.SetArgs([]string{"run", moduleDir, "--local"})
	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("expected error when --local is used without --target-path")
	}
	if !strings.Contains(err.Error(), "--local requires --target-path") {
		t.Errorf("expected '--local requires --target-path' error, got: %v", err)
	}
}

func TestRun_LocalWithoutTargetPath_NoTargetSpec_Errors(t *testing.T) {
	resetFlags()

	// Module without target spec — targetDir defaults to moduleDir.
	// --local should still require --target-path for clarity.
	moduleDir := t.TempDir()
	writeLoomYAML(t, moduleDir, `
apiVersion: loom.rickliujh.github.io/v1beta1
kind: Loom
metadata:
  name: test-mod
spec:
  operations: []
`)

	rootCmd.SetArgs([]string{"run", moduleDir, "--local"})
	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("expected error when --local is used without --target-path")
	}
	if !strings.Contains(err.Error(), "--local requires --target-path") {
		t.Errorf("expected '--local requires --target-path' error, got: %v", err)
	}
}

func TestRun_LocalWithTargetPath_Succeeds(t *testing.T) {
	resetFlags()

	moduleDir := t.TempDir()
	targetDir := t.TempDir()
	initGitRepo(t, targetDir)

	// Module with a newFiles operation and a shell marked local.
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

	rootCmd.SetArgs([]string{"run", moduleDir, "--local", "--target-path", targetDir})
	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify newFiles wrote to the target.
	content, err := os.ReadFile(filepath.Join(targetDir, "hello.txt"))
	if err != nil {
		t.Fatal("expected hello.txt in target dir")
	}
	if string(content) != "hello world" {
		t.Errorf("expected 'hello world', got %q", string(content))
	}
}

func TestRun_LocalWithTargetPath_ShellSkippedByDefault(t *testing.T) {
	resetFlags()

	moduleDir := t.TempDir()
	targetDir := t.TempDir()

	writeLoomYAML(t, moduleDir, `
apiVersion: loom.rickliujh.github.io/v1beta1
kind: Loom
metadata:
  name: test-local-shell
spec:
  operations:
    - name: remote-cmd
      shell:
        command: touch should-not-exist.txt
`)

	rootCmd.SetArgs([]string{"run", moduleDir, "--local", "--target-path", targetDir})
	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, err := os.Stat(filepath.Join(targetDir, "should-not-exist.txt")); !os.IsNotExist(err) {
		t.Error("shell command should be skipped in --local mode by default")
	}
}

func TestRun_LocalWithTargetPath_ShellRunsWhenMarkedLocal(t *testing.T) {
	resetFlags()

	moduleDir := t.TempDir()
	targetDir := t.TempDir()

	writeLoomYAML(t, moduleDir, `
apiVersion: loom.rickliujh.github.io/v1beta1
kind: Loom
metadata:
  name: test-local-shell
spec:
  operations:
    - name: local-cmd
      shell:
        command: touch local-output.txt
        local: true
`)

	rootCmd.SetArgs([]string{"run", moduleDir, "--local", "--target-path", targetDir})
	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, err := os.Stat(filepath.Join(targetDir, "local-output.txt")); err != nil {
		t.Error("shell command with local: true should run in --local mode")
	}
}

func TestRun_LocalWithTargetPath_CommitCreatedNoPush(t *testing.T) {
	resetFlags()

	moduleDir := t.TempDir()
	targetDir := t.TempDir()
	initGitRepo(t, targetDir)

	srcDir := filepath.Join(moduleDir, "templates")
	os.MkdirAll(srcDir, 0o755)
	os.WriteFile(filepath.Join(srcDir, "new.txt"), []byte("data"), 0o644)

	writeLoomYAML(t, moduleDir, `
apiVersion: loom.rickliujh.github.io/v1beta1
kind: Loom
metadata:
  name: test-local-commit
spec:
  operations:
    - name: write-files
      newFiles:
        source: "templates"
        dest: ""
    - name: commit
      commitPush:
        message: "local commit test"
        author: "test-bot"
        email: "bot@test.com"
`)

	rootCmd.SetArgs([]string{"run", moduleDir, "--local", "--target-path", targetDir})
	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify commit exists locally.
	out, err := exec.Command("git", "-C", targetDir, "log", "--oneline").CombinedOutput()
	if err != nil {
		t.Fatalf("git log failed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "local commit test") {
		t.Errorf("expected 'local commit test' in git log, got:\n%s", string(out))
	}
}

func TestRun_LocalWithTargetPath_PRSkipped(t *testing.T) {
	resetFlags()

	moduleDir := t.TempDir()
	targetDir := t.TempDir()

	writeLoomYAML(t, moduleDir, `
apiVersion: loom.rickliujh.github.io/v1beta1
kind: Loom
metadata:
  name: test-local-pr
spec:
  operations:
    - name: open-pr
      pr:
        provider: github
        title: "test PR"
        tokenEnv: FAKE_TOKEN
`)

	// PR action with --local should succeed (skip) without needing a real token or remote.
	rootCmd.SetArgs([]string{"run", moduleDir, "--local", "--target-path", targetDir})
	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("expected PR to be skipped in --local mode, got error: %v", err)
	}
}

func TestRun_WithoutLocal_ShellRuns(t *testing.T) {
	resetFlags()

	moduleDir := t.TempDir()
	targetDir := t.TempDir()

	writeLoomYAML(t, moduleDir, `
apiVersion: loom.rickliujh.github.io/v1beta1
kind: Loom
metadata:
  name: test-normal-shell
spec:
  operations:
    - name: run-cmd
      shell:
        command: touch normal-output.txt
`)

	// Without --local, shell runs normally.
	rootCmd.SetArgs([]string{"run", moduleDir, "--target-path", targetDir})
	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, err := os.Stat(filepath.Join(targetDir, "normal-output.txt")); err != nil {
		t.Error("shell command should run normally without --local")
	}
}
