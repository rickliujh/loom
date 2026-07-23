package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// initGitRepoBranch creates a git repo on branch `branch` with one commit that
// contains the given files (path -> content).
func initGitRepoBranch(t *testing.T, dir, branch string, files map[string]string) {
	t.Helper()
	for _, args := range [][]string{
		{"git", "init", "-b", branch, dir},
		{"git", "-C", dir, "config", "user.email", "test@test.com"},
		{"git", "-C", dir, "config", "user.name", "Test"},
	} {
		if out, err := exec.Command(args[0], args[1:]...).CombinedOutput(); err != nil {
			t.Fatalf("git %v failed: %v\n%s", args, err, out)
		}
	}
	for name, content := range files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
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

// captureStdout redirects os.Stdout to a temp file for the duration of fn and
// returns what was written. Diffs are printed to stdout; the logger and the
// success line go to stderr and are left alone.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "stdout-*")
	if err != nil {
		t.Fatal(err)
	}
	orig := os.Stdout
	os.Stdout = f
	defer func() { os.Stdout = orig }()

	fn()

	if err := f.Sync(); err != nil {
		t.Fatal(err)
	}
	os.Stdout = orig
	data, err := os.ReadFile(f.Name())
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// DF1: full mode runs the module in local mode and prints a git diff of each
// cloned target — including files rewritten by a pure shell command.
func TestDiff_DF1_FullShowsPatchAndShellChanges(t *testing.T) {
	resetFlags()

	upstream := t.TempDir()
	initGitRepoBranch(t, upstream, "main", map[string]string{
		"a.yaml":    "name: cm\ndata:\n  k: v\n",
		"notes.txt": "hello\n",
	})

	moduleDir := t.TempDir()
	patchDir := filepath.Join(moduleDir, "__functions", "patches")
	os.MkdirAll(patchDir, 0o755)
	os.WriteFile(filepath.Join(patchDir, "p.yaml"),
		[]byte("name: cm\ndata:\n  k: v\n  added: loom\n"), 0o644)

	writeLoomYAML(t, moduleDir, `
apiVersion: loom.rickliujh.github.io/v1beta1
kind: Loom
metadata:
  name: demo
spec:
  target:
    url: "file://`+upstream+`"
    branch: main
  operations:
    - name: label
      patch:
        engine: smp
        path: __functions/patches/p.yaml
        target: a.yaml
    - name: format
      shell:
        command: "printf 'HELLO\n' > notes.txt"
        pure: true
`)

	out := captureStdout(t, func() {
		rootCmd.SetArgs([]string{"diff", moduleDir})
		if err := rootCmd.Execute(); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	if !strings.Contains(out, "+  added: loom") {
		t.Errorf("expected patch change in diff, got:\n%s", out)
	}
	// The pure shell command's file rewrite must show — this is what quick mode
	// (dry-run) cannot capture.
	if !strings.Contains(out, "-hello") || !strings.Contains(out, "+HELLO") {
		t.Errorf("expected shell-op change (notes.txt) in diff, got:\n%s", out)
	}
	// DF4: the diff must carry a header identifying the module and target repo,
	// so it stays legible away from the surrounding operation logs.
	if !strings.Contains(out, "demo") || !strings.Contains(out, upstream) {
		t.Errorf("expected module/repo header in diff, got:\n%s", out)
	}
}

// DF2: --quick simulates the run (dry-run) — it prints newFiles/patch unified
// diffs but executes nothing, so no files are written to the target.
func TestDiff_DF2_QuickShowsDiffWithoutWriting(t *testing.T) {
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
  name: quick-demo
spec:
  operations:
    - name: write-files
      newFiles:
        source: "templates"
        dest: ""
`)

	out := captureStdout(t, func() {
		rootCmd.SetArgs([]string{"diff", moduleDir, "--quick", "--target-path", targetDir})
		if err := rootCmd.Execute(); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	if !strings.Contains(out, "new content") {
		t.Errorf("expected rendered content in quick diff, got:\n%s", out)
	}
	// Quick diffs carry a module header for context, just like full mode.
	if !strings.Contains(out, "quick-demo") {
		t.Errorf("expected module header in quick diff, got:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(targetDir, "new.txt")); !os.IsNotExist(err) {
		t.Error("--quick must not write files to the target")
	}
}

// DF5: on failure, the error is reported and the run exits non-zero. By
// default no diff is printed; with --partial the pre-failure diff follows a
// warning.
func TestDiff_DF5_FailureOrdering(t *testing.T) {
	upstream := t.TempDir()
	initGitRepoBranch(t, upstream, "main", map[string]string{"a.yaml": "name: cm\ndata:\n  k: v\n"})

	moduleDir := t.TempDir()
	patchDir := filepath.Join(moduleDir, "__functions", "patches")
	os.MkdirAll(patchDir, 0o755)
	os.WriteFile(filepath.Join(patchDir, "p.yaml"),
		[]byte("name: cm\ndata:\n  k: v\n  added: loom\n"), 0o644)

	// First op patches an existing file (succeeds), second targets a missing
	// file (fails), so there is a pre-failure change to show.
	writeLoomYAML(t, moduleDir, `
apiVersion: loom.rickliujh.github.io/v1beta1
kind: Loom
metadata:
  name: failz
spec:
  target:
    url: "file://`+upstream+`"
    branch: main
  operations:
    - name: label
      patch:
        engine: smp
        path: __functions/patches/p.yaml
        target: a.yaml
    - name: boom
      patch:
        engine: smp
        path: __functions/patches/p.yaml
        target: does-not-exist.yaml
`)

	// Default: no diff on failure, non-zero exit.
	resetFlags()
	out := captureStdout(t, func() {
		rootCmd.SetArgs([]string{"diff", moduleDir})
		if err := rootCmd.Execute(); err == nil {
			t.Fatal("expected non-zero exit on a failed run")
		}
	})
	if strings.Contains(out, "added: loom") {
		t.Errorf("default failure should not print the diff, got:\n%s", out)
	}

	// --partial: the pre-failure diff is printed (below a warning on stderr).
	resetFlags()
	diffPartial = true
	out = captureStdout(t, func() {
		rootCmd.SetArgs([]string{"diff", moduleDir, "--partial"})
		if err := rootCmd.Execute(); err == nil {
			t.Fatal("expected non-zero exit on a failed run")
		}
	})
	if !strings.Contains(out, "+  added: loom") {
		t.Errorf("--partial should print the pre-failure diff, got:\n%s", out)
	}
}

// A bulk run fans one child source out to several items that share a metadata
// name, so each item's diff must be headed by its unique instance breadcrumb
// (root name › item name) plus its own repo — otherwise the diffs are
// indistinguishable. This guards the whole full-mode path: the executor records
// each clone dir's breadcrumb, and the header reads it back.
func TestDiff_BulkItemsHeadedByInstanceBreadcrumb(t *testing.T) {
	resetFlags()

	repoA := t.TempDir()
	initGitRepoBranch(t, repoA, "main", map[string]string{"f.txt": "alpha\n"})
	repoB := t.TempDir()
	initGitRepoBranch(t, repoB, "main", map[string]string{"f.txt": "beta\n"})

	moduleDir := t.TempDir()
	childDir := filepath.Join(moduleDir, "child")
	if err := os.Mkdir(childDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeLoomYAML(t, childDir, `
apiVersion: loom.rickliujh.github.io/v1beta1
kind: Loom
metadata:
  name: patch
spec:
  params:
    - name: repo
      required: true
  target:
    url: "{{ .repo }}"
    branch: main
  operations:
    - name: edit
      shell:
        command: "printf 'CHANGED\n' > f.txt"
        pure: true
`)
	writeLoomYAML(t, moduleDir, `
apiVersion: loom.rickliujh.github.io/v1beta1
kind: Loom
metadata:
  name: bulk-demo
spec:
  modules:
    - name: patch-alpha
      source: ./child
      params:
        repo: "file://`+repoA+`"
    - name: patch-beta
      source: ./child
      params:
        repo: "file://`+repoB+`"
  operations: []
`)

	out := captureStdout(t, func() {
		rootCmd.SetArgs([]string{"diff", moduleDir})
		if err := rootCmd.Execute(); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	// Each item's header carries the root marker, its own instance name, and its
	// own repo — so the two diffs can be told apart.
	for _, want := range []string{
		"≡ bulk-demo ≡", "patch-alpha", "patch-beta", repoA, repoB,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in bulk diff, got:\n%s", want, out)
		}
	}
	// The shared metadata name "patch" must not stand in as the header identity.
	if strings.Contains(out, "[patch]") || strings.Contains(out, "≡ patch ≡") {
		t.Errorf("bulk diff headed by shared metadata name instead of instance breadcrumb:\n%s", out)
	}
}

// DF3: full mode cleans up its temp workspace when --target-path is not given,
// and keeps a caller-supplied --target-path for inspection.
func TestDiff_DF3_WorkspaceCleanupAndKeep(t *testing.T) {
	resetFlags()

	upstream := t.TempDir()
	initGitRepoBranch(t, upstream, "main", map[string]string{"a.yaml": "k: v\n"})

	moduleDir := t.TempDir()
	writeLoomYAML(t, moduleDir, `
apiVersion: loom.rickliujh.github.io/v1beta1
kind: Loom
metadata:
  name: cleanup-demo
spec:
  target:
    url: "file://`+upstream+`"
    branch: main
  operations: []
`)

	// No --target-path: the temp workspace must not survive the run.
	before, _ := filepath.Glob(filepath.Join(os.TempDir(), "loom-diff-*"))
	captureStdout(t, func() {
		rootCmd.SetArgs([]string{"diff", moduleDir})
		if err := rootCmd.Execute(); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
	after, _ := filepath.Glob(filepath.Join(os.TempDir(), "loom-diff-*"))
	if len(after) > len(before) {
		t.Errorf("temp workspace not cleaned up: before=%d after=%d", len(before), len(after))
	}

	// With --target-path: the numbered clone is kept for inspection.
	resetFlags()
	keep := t.TempDir()
	captureStdout(t, func() {
		rootCmd.SetArgs([]string{"diff", moduleDir, "--target-path", keep})
		if err := rootCmd.Execute(); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
	if _, err := os.Stat(filepath.Join(keep, "00-cleanup-demo")); err != nil {
		t.Errorf("expected clone kept at %s/00-cleanup-demo: %v", keep, err)
	}
}
