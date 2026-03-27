package action

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rickliujh/loom/pkg/config"
)

// initLocalRepo creates a git repo with an initial commit (no remote).
func initLocalRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
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
	return dir
}

func gitLog(t *testing.T, dir string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", dir, "log", "--oneline").CombinedOutput()
	if err != nil {
		t.Fatalf("git log failed: %v\n%s", err, out)
	}
	return strings.TrimSpace(string(out))
}

func localExecCtx(t *testing.T, targetDir string) *ExecutionContext {
	t.Helper()
	return &ExecutionContext{
		ModuleDir: t.TempDir(),
		TargetDir: targetDir,
		Params:    map[string]string{},
		LocalRun: true,
		Logger:    slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn})),
	}
}

// --- CommitPush ---

func TestCommitPushAction_LocalRun_CommitsWithoutPush(t *testing.T) {
	repoDir := initLocalRepo(t)

	// Create a new file to commit.
	os.WriteFile(filepath.Join(repoDir, "new.txt"), []byte("local change"), 0o644)

	action := &CommitPushAction{
		Config: config.CommitPush{
			Message: "local commit",
			Author:  "Test",
			Email:   "test@test.com",
		},
	}

	execCtx := localExecCtx(t, repoDir)
	err := action.Execute(context.Background(), execCtx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify the commit was created.
	log := gitLog(t, repoDir)
	if !strings.Contains(log, "local commit") {
		t.Errorf("expected commit message in log, got:\n%s", log)
	}
}

func TestCommitPushAction_LocalRun_NoPushToRemote(t *testing.T) {
	// Set up a repo with a remote to verify push is NOT called.
	bareDir := t.TempDir()
	if out, err := exec.Command("git", "init", "--bare", bareDir).CombinedOutput(); err != nil {
		t.Fatalf("bare init failed: %v\n%s", err, out)
	}

	repoDir := initLocalRepo(t)
	for _, args := range [][]string{
		{"git", "-C", repoDir, "remote", "add", "origin", bareDir},
		{"git", "-C", repoDir, "push", "-u", "origin", "HEAD"},
	} {
		if out, err := exec.Command(args[0], args[1:]...).CombinedOutput(); err != nil {
			t.Fatalf("git %v failed: %v\n%s", args, err, out)
		}
	}

	// Get remote commit count before.
	beforeOut, _ := exec.Command("git", "-C", bareDir, "rev-list", "--count", "HEAD").CombinedOutput()
	beforeCount := strings.TrimSpace(string(beforeOut))

	// Make a change and run local-only commit.
	os.WriteFile(filepath.Join(repoDir, "change.txt"), []byte("change"), 0o644)

	action := &CommitPushAction{
		Config: config.CommitPush{
			Message: "local only commit",
			Author:  "Test",
			Email:   "test@test.com",
		},
	}
	execCtx := localExecCtx(t, repoDir)
	if err := action.Execute(context.Background(), execCtx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify remote did NOT receive the new commit.
	afterOut, _ := exec.Command("git", "-C", bareDir, "rev-list", "--count", "HEAD").CombinedOutput()
	afterCount := strings.TrimSpace(string(afterOut))

	if beforeCount != afterCount {
		t.Errorf("expected remote commit count to stay at %s, got %s", beforeCount, afterCount)
	}

	// But local repo should have the commit.
	log := gitLog(t, repoDir)
	if !strings.Contains(log, "local only commit") {
		t.Errorf("expected local commit in log, got:\n%s", log)
	}
}

func TestCommitPushAction_LocalRun_TemplatesMessage(t *testing.T) {
	repoDir := initLocalRepo(t)
	os.WriteFile(filepath.Join(repoDir, "file.txt"), []byte("data"), 0o644)

	action := &CommitPushAction{
		Config: config.CommitPush{
			Message: "update {{ .env }}",
			Author:  "Test",
			Email:   "test@test.com",
		},
	}

	execCtx := localExecCtx(t, repoDir)
	execCtx.Params = map[string]string{"env": "staging"}

	if err := action.Execute(context.Background(), execCtx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	log := gitLog(t, repoDir)
	if !strings.Contains(log, "update staging") {
		t.Errorf("expected templated commit message, got:\n%s", log)
	}
}

// --- PR ---

func TestPRAction_LocalRun_SkipsPRCreation(t *testing.T) {
	action := &PRAction{
		Config: config.PR{
			Provider: "github",
			Title:    "test PR",
			Body:     "body",
		},
	}

	var buf bytes.Buffer
	execCtx := localExecCtx(t, t.TempDir())
	execCtx.Logger = slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))

	err := action.Execute(context.Background(), execCtx)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	logOutput := buf.String()
	if !strings.Contains(logOutput, "skipping PR creation") {
		t.Errorf("expected 'skipping PR creation' in log, got:\n%s", logOutput)
	}
}

func TestPRAction_LocalRun_TemplatesBeforeSkipping(t *testing.T) {
	action := &PRAction{
		Config: config.PR{
			Provider: "github",
			Title:    "deploy {{ .env }}",
			Body:     "deploy to {{ .env }}",
		},
	}

	var buf bytes.Buffer
	execCtx := localExecCtx(t, t.TempDir())
	execCtx.Params = map[string]string{"env": "prod"}
	execCtx.Logger = slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))

	err := action.Execute(context.Background(), execCtx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	logOutput := buf.String()
	if !strings.Contains(logOutput, "deploy prod") {
		t.Errorf("expected templated title in log, got:\n%s", logOutput)
	}
}

// --- Shell ---

func TestShellAction_LocalRun_SkippedByDefault(t *testing.T) {
	targetDir := t.TempDir()

	action := &ShellAction{
		Config: config.Shell{
			Command: "echo hello > output.txt",
		},
	}

	var buf bytes.Buffer
	execCtx := localExecCtx(t, targetDir)
	execCtx.Logger = slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))

	err := action.Execute(context.Background(), execCtx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// File should NOT exist — shell is skipped by default in local mode.
	if _, err := os.Stat(filepath.Join(targetDir, "output.txt")); !os.IsNotExist(err) {
		t.Error("expected shell command to be skipped in local mode")
	}

	logOutput := buf.String()
	if !strings.Contains(logOutput, "skipping shell command") {
		t.Errorf("expected 'skipping shell command' in log, got:\n%s", logOutput)
	}
}

func TestShellAction_LocalRun_RunsWhenMarkedPure(t *testing.T) {
	targetDir := t.TempDir()

	action := &ShellAction{
		Config: config.Shell{
			Command: "echo hello > output.txt",
			Pure:    true,
		},
	}

	execCtx := localExecCtx(t, targetDir)
	err := action.Execute(context.Background(), execCtx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	content, err := os.ReadFile(filepath.Join(targetDir, "output.txt"))
	if err != nil {
		t.Fatal("expected output.txt to exist when pure: true")
	}
	if !strings.Contains(string(content), "hello") {
		t.Errorf("expected 'hello' in output, got %q", string(content))
	}
}

func TestShellAction_LocalFalse_RunsNormally(t *testing.T) {
	// Without --local-run, shell commands run regardless of the pure field.
	targetDir := t.TempDir()

	action := &ShellAction{
		Config: config.Shell{
			Command: "echo world > normal.txt",
		},
	}

	execCtx := testExecCtx(t, t.TempDir(), targetDir)
	err := action.Execute(context.Background(), execCtx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	content, err := os.ReadFile(filepath.Join(targetDir, "normal.txt"))
	if err != nil {
		t.Fatal("expected normal.txt to exist in non-local mode")
	}
	if !strings.Contains(string(content), "world") {
		t.Errorf("expected 'world' in output, got %q", string(content))
	}
}

// --- NewFiles ---

func TestNewFilesAction_LocalRun_StillWritesFiles(t *testing.T) {
	moduleDir := t.TempDir()
	srcDir := filepath.Join(moduleDir, "templates")
	os.MkdirAll(srcDir, 0o755)
	os.WriteFile(filepath.Join(srcDir, "local.txt"), []byte("local content"), 0o644)

	targetDir := t.TempDir()

	action := &NewFilesAction{
		Config: configNewFiles("templates", ""),
	}

	execCtx := localExecCtx(t, targetDir)
	execCtx.ModuleDir = moduleDir

	err := action.Execute(context.Background(), execCtx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	content, err := os.ReadFile(filepath.Join(targetDir, "local.txt"))
	if err != nil {
		t.Fatal("expected local.txt to exist")
	}
	if string(content) != "local content" {
		t.Errorf("expected 'local content', got %q", string(content))
	}
}

// --- DryRun takes precedence over LocalRun ---

func TestCommitPushAction_DryRunTakesPrecedenceOverLocal(t *testing.T) {
	repoDir := initLocalRepo(t)
	os.WriteFile(filepath.Join(repoDir, "file.txt"), []byte("data"), 0o644)

	action := &CommitPushAction{
		Config: config.CommitPush{
			Message: "should not commit",
			Author:  "Test",
			Email:   "test@test.com",
		},
	}

	execCtx := localExecCtx(t, repoDir)
	execCtx.DryRun = true

	if err := action.Execute(context.Background(), execCtx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// With dry-run + local, dry-run wins: no commit should exist.
	log := gitLog(t, repoDir)
	if strings.Contains(log, "should not commit") {
		t.Error("dry-run should prevent commit even when local is set")
	}
}

func TestPRAction_DryRunTakesPrecedenceOverLocal(t *testing.T) {
	action := &PRAction{
		Config: config.PR{
			Provider: "github",
			Title:    "test",
		},
	}

	var buf bytes.Buffer
	execCtx := localExecCtx(t, t.TempDir())
	execCtx.DryRun = true
	execCtx.Logger = slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))

	if err := action.Execute(context.Background(), execCtx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	logOutput := buf.String()
	if !strings.Contains(logOutput, "dry-run") {
		t.Errorf("expected dry-run log message, got:\n%s", logOutput)
	}
	if strings.Contains(logOutput, "skipping PR creation") {
		t.Error("dry-run should take precedence, not local-only path")
	}
}
