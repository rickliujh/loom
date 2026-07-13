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

func configNewFiles(source, dest string) config.NewFiles {
	return config.NewFiles{Source: source, Dest: dest}
}

func testExecCtx(t *testing.T, moduleDir, targetDir string) *ExecutionContext {
	t.Helper()
	return &ExecutionContext{
		ModuleDir: moduleDir,
		TargetDir: targetDir,
		Params:    map[string]string{},
		Logger:    slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn})),
	}
}

func TestNewFilesAction_ErrorsWhenFileExists(t *testing.T) {
	// Setup source directory with a template file.
	moduleDir := t.TempDir()
	srcDir := filepath.Join(moduleDir, "templates")
	os.MkdirAll(srcDir, 0o755)
	os.WriteFile(filepath.Join(srcDir, "config.yaml"), []byte("key: value"), 0o644)

	// Setup target directory with the same file already present.
	targetDir := t.TempDir()
	os.WriteFile(filepath.Join(targetDir, "config.yaml"), []byte("existing"), 0o644)

	action := &NewFilesAction{
		Config: configNewFiles("templates", ""),
	}

	execCtx := testExecCtx(t, moduleDir, targetDir)
	err := action.Execute(context.Background(), execCtx)
	if err == nil {
		t.Fatal("expected error when destination file already exists")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("expected 'already exists' in error, got: %v", err)
	}
}

func TestNewFilesAction_SucceedsWhenFileDoesNotExist(t *testing.T) {
	moduleDir := t.TempDir()
	srcDir := filepath.Join(moduleDir, "templates")
	os.MkdirAll(srcDir, 0o755)
	os.WriteFile(filepath.Join(srcDir, "newfile.txt"), []byte("hello"), 0o644)

	targetDir := t.TempDir()

	action := &NewFilesAction{
		Config: configNewFiles("templates", ""),
	}

	execCtx := testExecCtx(t, moduleDir, targetDir)
	err := action.Execute(context.Background(), execCtx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	content, err := os.ReadFile(filepath.Join(targetDir, "newfile.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "hello" {
		t.Errorf("expected 'hello', got %q", string(content))
	}
}

func TestNewFilesAction_DryRunSkipsWrite(t *testing.T) {
	moduleDir := t.TempDir()
	srcDir := filepath.Join(moduleDir, "templates")
	os.MkdirAll(srcDir, 0o755)
	os.WriteFile(filepath.Join(srcDir, "dryrun.txt"), []byte("data"), 0o644)

	targetDir := t.TempDir()

	action := &NewFilesAction{
		Config: configNewFiles("templates", ""),
	}

	execCtx := testExecCtx(t, moduleDir, targetDir)
	execCtx.DryRun = true

	err := action.Execute(context.Background(), execCtx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// File should NOT have been created.
	if _, err := os.Stat(filepath.Join(targetDir, "dryrun.txt")); !os.IsNotExist(err) {
		t.Error("expected file to not exist in dry-run mode")
	}
}

func TestNewFilesAction_DryRunWarnsOnExistingFile(t *testing.T) {
	moduleDir := t.TempDir()
	srcDir := filepath.Join(moduleDir, "templates")
	os.MkdirAll(srcDir, 0o755)
	os.WriteFile(filepath.Join(srcDir, "conflict.txt"), []byte("data"), 0o644)

	targetDir := t.TempDir()
	// Pre-create the file — dry-run should NOT error but SHOULD warn.
	os.WriteFile(filepath.Join(targetDir, "conflict.txt"), []byte("existing"), 0o644)

	action := &NewFilesAction{
		Config: configNewFiles("templates", ""),
	}

	var buf bytes.Buffer
	execCtx := testExecCtx(t, moduleDir, targetDir)
	execCtx.DryRun = true
	execCtx.Logger = slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn}))

	err := action.Execute(context.Background(), execCtx)
	if err != nil {
		t.Fatalf("dry-run should not error on existing file, got: %v", err)
	}

	logOutput := buf.String()
	if !strings.Contains(logOutput, "already exists") {
		t.Errorf("expected warning about existing file in log, got:\n%s", logOutput)
	}
}

func TestNewFilesAction_WithDest(t *testing.T) {
	moduleDir := t.TempDir()
	srcDir := filepath.Join(moduleDir, "src")
	os.MkdirAll(srcDir, 0o755)
	os.WriteFile(filepath.Join(srcDir, "app.conf"), []byte("conf"), 0o644)

	targetDir := t.TempDir()

	action := &NewFilesAction{
		Config: configNewFiles("src", "output"),
	}

	execCtx := testExecCtx(t, moduleDir, targetDir)
	err := action.Execute(context.Background(), execCtx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	content, err := os.ReadFile(filepath.Join(targetDir, "output", "app.conf"))
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "conf" {
		t.Errorf("expected 'conf', got %q", string(content))
	}
}

func TestNewFilesAction_TemplatedDest(t *testing.T) {
	moduleDir := t.TempDir()
	srcDir := filepath.Join(moduleDir, "src")
	os.MkdirAll(srcDir, 0o755)
	os.WriteFile(filepath.Join(srcDir, "app.conf"), []byte("conf"), 0o644)

	action := &NewFilesAction{
		Config: configNewFiles("src", `{{ if eq .anthos "true" }}ACM{{ else }}.{{ end }}`),
	}

	tests := []struct {
		anthos  string
		wantRel string
	}{
		{"true", filepath.Join("ACM", "app.conf")},
		{"false", "app.conf"},
	}
	for _, tc := range tests {
		targetDir := t.TempDir()
		execCtx := testExecCtx(t, moduleDir, targetDir)
		execCtx.Params = map[string]string{"anthos": tc.anthos}

		if err := action.Execute(context.Background(), execCtx); err != nil {
			t.Fatalf("anthos=%s: unexpected error: %v", tc.anthos, err)
		}
		if _, err := os.Stat(filepath.Join(targetDir, tc.wantRel)); err != nil {
			t.Errorf("anthos=%s: expected file at %s: %v", tc.anthos, tc.wantRel, err)
		}
	}
}

func TestNewFilesAction_TemplatedSource(t *testing.T) {
	moduleDir := t.TempDir()
	srcDir := filepath.Join(moduleDir, "variants", "prod")
	os.MkdirAll(srcDir, 0o755)
	os.WriteFile(filepath.Join(srcDir, "app.conf"), []byte("conf"), 0o644)

	targetDir := t.TempDir()

	action := &NewFilesAction{
		Config: configNewFiles("variants/{{ .env }}", ""),
	}

	execCtx := testExecCtx(t, moduleDir, targetDir)
	execCtx.Params = map[string]string{"env": "prod"}

	if err := action.Execute(context.Background(), execCtx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(targetDir, "app.conf")); err != nil {
		t.Errorf("expected file at app.conf: %v", err)
	}
}

func TestNewFilesAction_WithTemplating(t *testing.T) {
	moduleDir := t.TempDir()
	srcDir := filepath.Join(moduleDir, "tpl")
	os.MkdirAll(srcDir, 0o755)
	os.WriteFile(filepath.Join(srcDir, "greeting.txt"), []byte("Hello {{ .name }}!"), 0o644)

	targetDir := t.TempDir()

	action := &NewFilesAction{
		Config: configNewFiles("tpl", ""),
	}

	execCtx := testExecCtx(t, moduleDir, targetDir)
	execCtx.Params = map[string]string{"name": "World"}

	err := action.Execute(context.Background(), execCtx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	content, err := os.ReadFile(filepath.Join(targetDir, "greeting.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "Hello World!" {
		t.Errorf("expected 'Hello World!', got %q", string(content))
	}
}

// NF3: Existing directory gets files merged; NF2 still applies per-file.
func TestNewFilesAction_DirectoryMerge(t *testing.T) {
	moduleDir := t.TempDir()
	srcDir := filepath.Join(moduleDir, "templates", "services")
	os.MkdirAll(srcDir, 0o755)
	os.WriteFile(filepath.Join(srcDir, "new-service.yaml"), []byte("new"), 0o644)

	targetDir := t.TempDir()
	// Pre-create the directory with an existing file.
	os.MkdirAll(filepath.Join(targetDir, "services"), 0o755)
	os.WriteFile(filepath.Join(targetDir, "services", "existing.yaml"), []byte("old"), 0o644)

	action := &NewFilesAction{
		Config: configNewFiles("templates", ""),
	}

	execCtx := testExecCtx(t, moduleDir, targetDir)
	err := action.Execute(context.Background(), execCtx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// New file should be written.
	content, err := os.ReadFile(filepath.Join(targetDir, "services", "new-service.yaml"))
	if err != nil {
		t.Fatal("expected new-service.yaml to exist")
	}
	if string(content) != "new" {
		t.Errorf("expected 'new', got %q", string(content))
	}

	// Existing file should be untouched.
	content, err = os.ReadFile(filepath.Join(targetDir, "services", "existing.yaml"))
	if err != nil {
		t.Fatal("expected existing.yaml to still exist")
	}
	if string(content) != "old" {
		t.Errorf("expected 'old', got %q", string(content))
	}
}

// NF3: Directory merge + NF2 — fails if individual file already exists.
func TestNewFilesAction_DirectoryMerge_FailsOnFileConflict(t *testing.T) {
	moduleDir := t.TempDir()
	srcDir := filepath.Join(moduleDir, "templates", "services")
	os.MkdirAll(srcDir, 0o755)
	os.WriteFile(filepath.Join(srcDir, "conflict.yaml"), []byte("new"), 0o644)

	targetDir := t.TempDir()
	os.MkdirAll(filepath.Join(targetDir, "services"), 0o755)
	os.WriteFile(filepath.Join(targetDir, "services", "conflict.yaml"), []byte("old"), 0o644)

	action := &NewFilesAction{
		Config: configNewFiles("templates", ""),
	}

	execCtx := testExecCtx(t, moduleDir, targetDir)
	err := action.Execute(context.Background(), execCtx)
	if err == nil {
		t.Fatal("expected error when file conflicts in merged directory")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("expected 'already exists' error, got: %v", err)
	}
}

func TestNewFilesAction_InvalidSource(t *testing.T) {
	moduleDir := t.TempDir()
	targetDir := t.TempDir()

	action := &NewFilesAction{
		Config: configNewFiles("nonexistent", ""),
	}

	execCtx := testExecCtx(t, moduleDir, targetDir)
	err := action.Execute(context.Background(), execCtx)
	if err == nil {
		t.Fatal("expected error for nonexistent source directory")
	}
}

func TestNewFilesAction_NestedFiles(t *testing.T) {
	moduleDir := t.TempDir()
	srcDir := filepath.Join(moduleDir, "nested")
	os.MkdirAll(filepath.Join(srcDir, "sub", "deep"), 0o755)
	os.WriteFile(filepath.Join(srcDir, "root.txt"), []byte("root"), 0o644)
	os.WriteFile(filepath.Join(srcDir, "sub", "mid.txt"), []byte("mid"), 0o644)
	os.WriteFile(filepath.Join(srcDir, "sub", "deep", "leaf.txt"), []byte("leaf"), 0o644)

	targetDir := t.TempDir()

	action := &NewFilesAction{
		Config: configNewFiles("nested", ""),
	}

	execCtx := testExecCtx(t, moduleDir, targetDir)
	err := action.Execute(context.Background(), execCtx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, tc := range []struct {
		path    string
		content string
	}{
		{"root.txt", "root"},
		{"sub/mid.txt", "mid"},
		{"sub/deep/leaf.txt", "leaf"},
	} {
		data, err := os.ReadFile(filepath.Join(targetDir, tc.path))
		if err != nil {
			t.Errorf("expected %s to exist: %v", tc.path, err)
			continue
		}
		if string(data) != tc.content {
			t.Errorf("%s: expected %q, got %q", tc.path, tc.content, string(data))
		}
	}
}
