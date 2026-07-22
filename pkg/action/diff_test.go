package action

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rickliujh/loom/pkg/config"
)

func diffExecCtx(t *testing.T, moduleDir, targetDir string) (*ExecutionContext, *DiffCollector) {
	t.Helper()
	diffs := &DiffCollector{}
	return &ExecutionContext{
		ModuleDir: moduleDir,
		TargetDir: targetDir,
		Params:    map[string]string{},
		DryRun:    true,
		ShowDiff:  true,
		Diffs:     diffs,
		Logger:    slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn})),
	}, diffs
}

// diffString renders a collector's diffs the way the run would print them.
func diffString(c *DiffCollector) string {
	var b bytes.Buffer
	c.Print(&b)
	return b.String()
}

// --- NewFiles diff ---

func TestNewFilesAction_DiffShowsNewFileContent(t *testing.T) {
	moduleDir := t.TempDir()
	srcDir := filepath.Join(moduleDir, "templates")
	os.MkdirAll(srcDir, 0o755)
	os.WriteFile(filepath.Join(srcDir, "hello.txt"), []byte("hello world\n"), 0o644)

	targetDir := t.TempDir()

	action := &NewFilesAction{
		Config: configNewFiles("templates", ""),
	}

	execCtx, diffs := diffExecCtx(t, moduleDir, targetDir)
	err := action.Execute(context.Background(), execCtx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	diffOutput := diffString(diffs)
	if !strings.Contains(diffOutput, "hello world") {
		t.Errorf("expected diff to contain file content, got:\n%s", diffOutput)
	}
	if !strings.Contains(diffOutput, "/dev/null") {
		t.Errorf("expected diff to show /dev/null for new file, got:\n%s", diffOutput)
	}
	if !strings.Contains(diffOutput, "@@") {
		t.Errorf("expected unified diff hunk header, got:\n%s", diffOutput)
	}
}

func TestNewFilesAction_DiffShowsTemplatedContent(t *testing.T) {
	moduleDir := t.TempDir()
	srcDir := filepath.Join(moduleDir, "templates")
	os.MkdirAll(srcDir, 0o755)
	os.WriteFile(filepath.Join(srcDir, "greeting.txt"), []byte("Hello {{ .name }}!\n"), 0o644)

	targetDir := t.TempDir()

	action := &NewFilesAction{
		Config: configNewFiles("templates", ""),
	}

	execCtx, diffs := diffExecCtx(t, moduleDir, targetDir)
	execCtx.Params = map[string]string{"name": "World"}

	err := action.Execute(context.Background(), execCtx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	diffOutput := diffString(diffs)
	if !strings.Contains(diffOutput, "Hello World!") {
		t.Errorf("expected diff to contain rendered content, got:\n%s", diffOutput)
	}
}

func TestNewFilesAction_DiffWithoutShowDiff_NoDiffOutput(t *testing.T) {
	moduleDir := t.TempDir()
	srcDir := filepath.Join(moduleDir, "templates")
	os.MkdirAll(srcDir, 0o755)
	os.WriteFile(filepath.Join(srcDir, "file.txt"), []byte("content\n"), 0o644)

	targetDir := t.TempDir()

	action := &NewFilesAction{
		Config: configNewFiles("templates", ""),
	}

	execCtx := testExecCtx(t, moduleDir, targetDir)
	execCtx.DryRun = true
	execCtx.ShowDiff = false
	// Diffs intentionally nil — no diff output.

	err := action.Execute(context.Background(), execCtx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// No panic and no diff output — test passes if it gets here.
}

// --- Patch diff ---

func TestPatchAction_DiffShowsPatchResult(t *testing.T) {
	moduleDir := t.TempDir()
	patchDir := filepath.Join(moduleDir, "__functions", "patches")
	os.MkdirAll(patchDir, 0o755)

	patchContent := `apiVersion: v1
kind: ConfigMap
metadata:
  name: test
  labels:
    added-by: loom
`
	os.WriteFile(filepath.Join(patchDir, "patch.yaml"), []byte(patchContent), 0o644)

	targetDir := t.TempDir()
	targetContent := `apiVersion: v1
kind: ConfigMap
metadata:
  name: test
data:
  key: value
`
	os.WriteFile(filepath.Join(targetDir, "target.yaml"), []byte(targetContent), 0o644)

	action := &PatchAction{
		Config: config.Patch{
			Engine: "smp",
			Path:   "__functions/patches/patch.yaml",
			Target: "target.yaml",
		},
	}

	execCtx, diffs := diffExecCtx(t, moduleDir, targetDir)
	err := action.Execute(context.Background(), execCtx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	diffOutput := diffString(diffs)
	if !strings.Contains(diffOutput, "added-by") {
		t.Errorf("expected diff to show added label, got:\n%s", diffOutput)
	}
	if !strings.Contains(diffOutput, "@@") {
		t.Errorf("expected unified diff hunk header, got:\n%s", diffOutput)
	}
}

func TestPatchAction_DiffJSON6902(t *testing.T) {
	moduleDir := t.TempDir()
	patchDir := filepath.Join(moduleDir, "__functions", "patches")
	os.MkdirAll(patchDir, 0o755)

	patchContent := `- op: replace
  path: /metadata/name
  value: "new-name"
`
	os.WriteFile(filepath.Join(patchDir, "patch.yaml"), []byte(patchContent), 0o644)

	targetDir := t.TempDir()
	targetContent := `apiVersion: v1
kind: ConfigMap
metadata:
  name: old-name
`
	os.WriteFile(filepath.Join(targetDir, "target.yaml"), []byte(targetContent), 0o644)

	action := &PatchAction{
		Config: config.Patch{
			Engine: "json6902",
			Path:   "__functions/patches/patch.yaml",
			Target: "target.yaml",
		},
	}

	execCtx, diffs := diffExecCtx(t, moduleDir, targetDir)
	err := action.Execute(context.Background(), execCtx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	diffOutput := diffString(diffs)
	if !strings.Contains(diffOutput, "old-name") || !strings.Contains(diffOutput, "new-name") {
		t.Errorf("expected diff to show old->new name change, got:\n%s", diffOutput)
	}
	if !strings.Contains(diffOutput, "target.yaml") {
		t.Errorf("expected diff to reference target.yaml, got:\n%s", diffOutput)
	}
}

func TestPatchAction_DiffWithoutShowDiff_NoDiffOutput(t *testing.T) {
	moduleDir := t.TempDir()
	patchDir := filepath.Join(moduleDir, "__functions", "patches")
	os.MkdirAll(patchDir, 0o755)
	os.WriteFile(filepath.Join(patchDir, "patch.yaml"), []byte("apiVersion: v1\n"), 0o644)

	targetDir := t.TempDir()
	os.WriteFile(filepath.Join(targetDir, "target.yaml"), []byte("apiVersion: v1\n"), 0o644)

	action := &PatchAction{
		Config: config.Patch{
			Path:   "__functions/patches/patch.yaml",
			Target: "target.yaml",
		},
	}

	execCtx := testExecCtx(t, moduleDir, targetDir)
	execCtx.DryRun = true
	execCtx.ShowDiff = false
	// Diffs intentionally nil — no diff output.

	err := action.Execute(context.Background(), execCtx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// --- Diff implies dry-run (no writes) ---

func TestNewFilesAction_DiffDoesNotWriteFiles(t *testing.T) {
	moduleDir := t.TempDir()
	srcDir := filepath.Join(moduleDir, "templates")
	os.MkdirAll(srcDir, 0o755)
	os.WriteFile(filepath.Join(srcDir, "file.txt"), []byte("content"), 0o644)

	targetDir := t.TempDir()

	action := &NewFilesAction{
		Config: configNewFiles("templates", ""),
	}

	execCtx, _ := diffExecCtx(t, moduleDir, targetDir)
	err := action.Execute(context.Background(), execCtx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, err := os.Stat(filepath.Join(targetDir, "file.txt")); !os.IsNotExist(err) {
		t.Error("diff mode should not write files")
	}
}

func TestPatchAction_DiffDoesNotModifyTarget(t *testing.T) {
	moduleDir := t.TempDir()
	patchDir := filepath.Join(moduleDir, "__functions", "patches")
	os.MkdirAll(patchDir, 0o755)
	os.WriteFile(filepath.Join(patchDir, "patch.yaml"), []byte("apiVersion: v1\nmetadata:\n  labels:\n    new: label\n"), 0o644)

	targetDir := t.TempDir()
	originalContent := "apiVersion: v1\nmetadata:\n  name: test\n"
	os.WriteFile(filepath.Join(targetDir, "target.yaml"), []byte(originalContent), 0o644)

	action := &PatchAction{
		Config: config.Patch{
			Path:   "__functions/patches/patch.yaml",
			Target: "target.yaml",
		},
	}

	execCtx, _ := diffExecCtx(t, moduleDir, targetDir)
	err := action.Execute(context.Background(), execCtx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	content, err := os.ReadFile(filepath.Join(targetDir, "target.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != originalContent {
		t.Errorf("target file should not be modified in diff mode, got:\n%s", string(content))
	}
}
