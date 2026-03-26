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

func diffExecCtx(t *testing.T, moduleDir, targetDir string) (*ExecutionContext, *bytes.Buffer) {
	t.Helper()
	var buf bytes.Buffer
	return &ExecutionContext{
		ModuleDir: moduleDir,
		TargetDir: targetDir,
		Params:    map[string]string{},
		DryRun:    true,
		ShowDiff:  true,
		Logger:    slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})),
	}, &buf
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

	execCtx, buf := diffExecCtx(t, moduleDir, targetDir)
	err := action.Execute(context.Background(), execCtx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	logOutput := buf.String()
	// Should show diff output with the new file content.
	if !strings.Contains(logOutput, "hello world") {
		t.Errorf("expected diff to contain file content, got:\n%s", logOutput)
	}
	if !strings.Contains(logOutput, "/dev/null") {
		t.Errorf("expected diff to show /dev/null for new file, got:\n%s", logOutput)
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

	execCtx, buf := diffExecCtx(t, moduleDir, targetDir)
	execCtx.Params = map[string]string{"name": "World"}

	err := action.Execute(context.Background(), execCtx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	logOutput := buf.String()
	// Should show rendered content, not raw template.
	if !strings.Contains(logOutput, "Hello World!") {
		t.Errorf("expected diff to contain rendered content, got:\n%s", logOutput)
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

	var buf bytes.Buffer
	execCtx := testExecCtx(t, moduleDir, targetDir)
	execCtx.DryRun = true
	execCtx.ShowDiff = false
	execCtx.Logger = slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))

	err := action.Execute(context.Background(), execCtx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	logOutput := buf.String()
	if strings.Contains(logOutput, "/dev/null") {
		t.Error("diff output should not appear when ShowDiff is false")
	}
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

	execCtx, buf := diffExecCtx(t, moduleDir, targetDir)
	err := action.Execute(context.Background(), execCtx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	logOutput := buf.String()
	// Diff should show the added label.
	if !strings.Contains(logOutput, "added-by") {
		t.Errorf("expected diff to show added label, got:\n%s", logOutput)
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

	execCtx, buf := diffExecCtx(t, moduleDir, targetDir)
	err := action.Execute(context.Background(), execCtx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	logOutput := buf.String()
	// The diff shows character-level changes; verify the replacement is visible.
	if !strings.Contains(logOutput, "old") || !strings.Contains(logOutput, "new") {
		t.Errorf("expected diff to show old->new name change, got:\n%s", logOutput)
	}
	if !strings.Contains(logOutput, "target.yaml") {
		t.Errorf("expected diff to reference target.yaml, got:\n%s", logOutput)
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

	var buf bytes.Buffer
	execCtx := testExecCtx(t, moduleDir, targetDir)
	execCtx.DryRun = true
	execCtx.ShowDiff = false
	execCtx.Logger = slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))

	err := action.Execute(context.Background(), execCtx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	logOutput := buf.String()
	if strings.Contains(logOutput, "---") && strings.Contains(logOutput, "+++") {
		t.Error("diff output should not appear when ShowDiff is false")
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

	// File should NOT be written — diff implies dry-run.
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

	// Target file should be unchanged.
	content, err := os.ReadFile(filepath.Join(targetDir, "target.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != originalContent {
		t.Errorf("target file should not be modified in diff mode, got:\n%s", string(content))
	}
}
