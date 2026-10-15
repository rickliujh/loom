package action

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rickliujh/loom/pkg/config"
	"github.com/rickliujh/loom/pkg/llm"
)

// escapeFixture lays out <root>/target (the target dir) next to
// <root>/outside, so a test can tell a write that stayed inside the target
// from one that climbed out of it.
func escapeFixture(t *testing.T) (moduleDir, targetDir, outside string) {
	t.Helper()
	root := t.TempDir()
	targetDir = filepath.Join(root, "target")
	outside = filepath.Join(root, "outside")
	for _, d := range []string{targetDir, outside} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	moduleDir = t.TempDir()
	srcDir := filepath.Join(moduleDir, "templates")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "note.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	return moduleDir, targetDir, outside
}

// assertEmptyDir fails when anything was written into dir.
func assertEmptyDir(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected nothing written to %s, found %q", dir, entries[0].Name())
	}
}

func assertEscapeError(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil {
		t.Fatal("expected an error for a path escaping the target directory, got nil")
	}
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("expected error containing %q, got: %v", want, err)
	}
}

func TestResolveTargetPath_WP1_Lexical(t *testing.T) {
	target := filepath.Join(t.TempDir(), "target")

	inside := []string{"", ".", "a", "a/b/c.yaml", "a/../b", "./a", "..a/file", "a/..b"}
	for _, rel := range inside {
		got, err := resolveTargetPath(target, "patch target", rel)
		if err != nil {
			t.Errorf("rel %q: unexpected error: %v", rel, err)
			continue
		}
		if want := filepath.Join(target, rel); got != want {
			t.Errorf("rel %q: got %q, want %q", rel, got, want)
		}
	}

	outside := []string{"..", "../x", "a/../../x", "../target-sibling/x", "a/b/../../../x"}
	for _, rel := range outside {
		_, err := resolveTargetPath(target, "patch target", rel)
		if err == nil {
			t.Errorf("rel %q: expected an escape error, got nil", rel)
			continue
		}
		if want := `patch target "` + rel + `" escapes the target directory`; err.Error() != want {
			t.Errorf("rel %q: got error %q, want %q", rel, err.Error(), want)
		}
	}
}

// An absolute value is joined beneath the target like any other, so it names
// a path inside the target rather than the absolute location.
func TestResolveTargetPath_WP1_AbsoluteStaysInside(t *testing.T) {
	target := filepath.Join(t.TempDir(), "target")
	got, err := resolveTargetPath(target, "newFiles dest", "/etc")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if want := filepath.Join(target, "etc"); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestNewFiles_WP1_DestEscapes(t *testing.T) {
	moduleDir, targetDir, outside := escapeFixture(t)

	action := &NewFilesAction{Config: configNewFiles("templates", "../outside")}
	err := action.Execute(context.Background(), testExecCtx(t, moduleDir, targetDir))

	assertEscapeError(t, err, `newFiles dest "../outside" escapes the target directory`)
	assertEmptyDir(t, outside)
}

// File and directory names are rendered too, so the escape can come from the
// walked path rather than from dest.
func TestNewFiles_WP1_RenderedFileNameEscapes(t *testing.T) {
	moduleDir, targetDir, outside := escapeFixture(t)
	srcDir := filepath.Join(moduleDir, "named")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// "{{ .. }}" cannot be a file name, so the name is built from a template
	// that renders to a parent reference.
	name := `{{ print ".." }}`
	if err := os.MkdirAll(filepath.Join(srcDir, name, "outside"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, name, "outside", "note.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	action := &NewFilesAction{Config: configNewFiles("named", "")}
	err := action.Execute(context.Background(), testExecCtx(t, moduleDir, targetDir))

	assertEscapeError(t, err, `newFiles destination "../outside/note.txt" escapes the target directory`)
	assertEmptyDir(t, outside)
}

func TestNewFiles_WP1_InsideStillWrites(t *testing.T) {
	moduleDir, targetDir, _ := escapeFixture(t)

	action := &NewFilesAction{Config: configNewFiles("templates", "apps/../services")}
	if err := action.Execute(context.Background(), testExecCtx(t, moduleDir, targetDir)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(targetDir, "services", "note.txt")); err != nil {
		t.Fatalf("expected file inside the target: %v", err)
	}
}

func TestPatch_WP1_TargetEscapes(t *testing.T) {
	moduleDir, targetDir, outside := escapeFixture(t)
	patchDir := filepath.Join(moduleDir, "__functions")
	if err := os.MkdirAll(patchDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(patchDir, "p.yaml"), []byte("a: patched\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	victim := filepath.Join(outside, "victim.yaml")
	if err := os.WriteFile(victim, []byte("a: original\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	action := &PatchAction{Config: config.Patch{Path: "__functions/p.yaml", Target: "../outside/victim.yaml"}}
	err := action.Execute(context.Background(), testExecCtx(t, moduleDir, targetDir))

	assertEscapeError(t, err, `patch target "../outside/victim.yaml" escapes the target directory`)
	got, readErr := os.ReadFile(victim)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(got) != "a: original\n" {
		t.Fatalf("file outside the target was modified: %q", got)
	}
}

func TestLLM_WP1_TargetEscapes(t *testing.T) {
	moduleDir, targetDir, outside := escapeFixture(t)

	called := false
	action := &LLMAction{
		Config: config.LLM{Provider: "openai", Model: "m", Prompt: "p", Target: "../outside/out.txt"},
		Infer: func(context.Context, llm.InferenceOptions) (string, error) {
			called = true
			return "generated", nil
		},
	}
	err := action.Execute(context.Background(), testExecCtx(t, moduleDir, targetDir))

	assertEscapeError(t, err, `llm target "../outside/out.txt" escapes the target directory`)
	if called {
		t.Error("the model was invoked for a target that escapes the target directory")
	}
	assertEmptyDir(t, outside)
}

// WP2: a preview must refuse what the real run would refuse, or it would
// report success for a run that then fails.
func TestNewFiles_WP2_DryRunRejectsEscape(t *testing.T) {
	moduleDir, targetDir, outside := escapeFixture(t)

	execCtx := testExecCtx(t, moduleDir, targetDir)
	execCtx.DryRun = true
	action := &NewFilesAction{Config: configNewFiles("templates", "../outside")}

	assertEscapeError(t, action.Execute(context.Background(), execCtx), "escapes the target directory")
	assertEmptyDir(t, outside)
}

func TestPatch_WP2_DryRunRejectsEscape(t *testing.T) {
	moduleDir, targetDir, _ := escapeFixture(t)

	execCtx := testExecCtx(t, moduleDir, targetDir)
	execCtx.DryRun = true
	action := &PatchAction{Config: config.Patch{Path: "__functions/p.yaml", Target: "../outside/victim.yaml"}}

	assertEscapeError(t, action.Execute(context.Background(), execCtx), "escapes the target directory")
}

func TestLLM_WP2_DryRunRejectsEscape(t *testing.T) {
	moduleDir, targetDir, _ := escapeFixture(t)

	execCtx := testExecCtx(t, moduleDir, targetDir)
	execCtx.DryRun = true
	action := &LLMAction{Config: config.LLM{Provider: "openai", Model: "m", Prompt: "p", Target: "../outside/out.txt"}}

	assertEscapeError(t, action.Execute(context.Background(), execCtx), "escapes the target directory")
}

// WP3: a path can stay inside the target lexically and still leave it through
// a symlink that lives in the target.
func TestNewFiles_WP3_SymlinkEscapes(t *testing.T) {
	moduleDir, targetDir, outside := escapeFixture(t)
	if err := os.Symlink(outside, filepath.Join(targetDir, "link")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	action := &NewFilesAction{Config: configNewFiles("templates", "link/sub")}
	err := action.Execute(context.Background(), testExecCtx(t, moduleDir, targetDir))

	assertEscapeError(t, err, `newFiles dest "link/sub" escapes the target directory through a symlink`)
	assertEmptyDir(t, outside)
}

func TestNewFiles_WP3_SymlinkInsideTargetAllowed(t *testing.T) {
	moduleDir, targetDir, _ := escapeFixture(t)
	real := filepath.Join(targetDir, "real")
	if err := os.MkdirAll(real, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, filepath.Join(targetDir, "alias")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	action := &NewFilesAction{Config: configNewFiles("templates", "alias")}
	if err := action.Execute(context.Background(), testExecCtx(t, moduleDir, targetDir)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(real, "note.txt")); err != nil {
		t.Fatalf("expected file written through the in-target symlink: %v", err)
	}
}

// The target directory itself may sit behind a symlink (a temp dir on macOS,
// a linked workspace); that must not read as an escape.
func TestNewFiles_WP3_TargetBehindSymlink(t *testing.T) {
	moduleDir, targetDir, _ := escapeFixture(t)
	linked := filepath.Join(filepath.Dir(targetDir), "linked-target")
	if err := os.Symlink(targetDir, linked); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	action := &NewFilesAction{Config: configNewFiles("templates", "apps")}
	if err := action.Execute(context.Background(), testExecCtx(t, moduleDir, linked)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(targetDir, "apps", "note.txt")); err != nil {
		t.Fatalf("expected file inside the target: %v", err)
	}
}
