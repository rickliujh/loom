package action

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rickliujh/loom/pkg/config"
)

// S1: Shell command runs in target directory.
func TestShellAction_RunsInTargetDir(t *testing.T) {
	targetDir := t.TempDir()

	action := &ShellAction{
		Config: config.Shell{Command: "pwd > result.txt"},
	}

	execCtx := testExecCtx(t, t.TempDir(), targetDir)
	if err := action.Execute(context.Background(), execCtx); err != nil {
		t.Fatal(err)
	}

	content, err := os.ReadFile(filepath.Join(targetDir, "result.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), targetDir) {
		t.Errorf("expected pwd output to contain %q, got %q", targetDir, string(content))
	}
}

// S2: Shell command is templated before execution.
func TestShellAction_Templated(t *testing.T) {
	targetDir := t.TempDir()

	action := &ShellAction{
		Config: config.Shell{Command: "echo {{ .greeting }} > out.txt"},
	}

	execCtx := testExecCtx(t, t.TempDir(), targetDir)
	execCtx.Params = map[string]string{"greeting": "hello-world"}

	if err := action.Execute(context.Background(), execCtx); err != nil {
		t.Fatal(err)
	}

	content, err := os.ReadFile(filepath.Join(targetDir, "out.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), "hello-world") {
		t.Errorf("expected 'hello-world', got %q", string(content))
	}
}

// S4: Dry-run logs but does not execute.
func TestShellAction_DryRun_NotExecuted(t *testing.T) {
	targetDir := t.TempDir()

	action := &ShellAction{
		Config: config.Shell{Command: "touch should-not-exist.txt"},
	}

	execCtx := testExecCtx(t, t.TempDir(), targetDir)
	execCtx.DryRun = true

	if err := action.Execute(context.Background(), execCtx); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(filepath.Join(targetDir, "should-not-exist.txt")); !os.IsNotExist(err) {
		t.Error("dry-run should not execute the shell command")
	}
}

// S5: Timeout kills the command.
func TestShellAction_Timeout(t *testing.T) {
	targetDir := t.TempDir()

	action := &ShellAction{
		Config: config.Shell{
			Command: "sleep 10",
			Timeout: "100ms",
		},
	}

	execCtx := testExecCtx(t, t.TempDir(), targetDir)
	err := action.Execute(context.Background(), execCtx)
	if err == nil {
		t.Fatal("expected error for timed-out command")
	}
	if !strings.Contains(err.Error(), "command failed") {
		t.Errorf("expected 'command failed' error, got: %v", err)
	}
}

// S5: Invalid timeout format errors.
func TestShellAction_InvalidTimeout(t *testing.T) {
	targetDir := t.TempDir()

	action := &ShellAction{
		Config: config.Shell{
			Command: "echo hi",
			Timeout: "not-a-duration",
		},
	}

	execCtx := testExecCtx(t, t.TempDir(), targetDir)
	err := action.Execute(context.Background(), execCtx)
	if err == nil {
		t.Fatal("expected error for invalid timeout")
	}
	if !strings.Contains(err.Error(), "invalid timeout") {
		t.Errorf("expected 'invalid timeout' error, got: %v", err)
	}
}

// Shell command that exits non-zero returns error.
func TestShellAction_NonZeroExit(t *testing.T) {
	targetDir := t.TempDir()

	action := &ShellAction{
		Config: config.Shell{Command: "exit 1"},
	}

	execCtx := testExecCtx(t, t.TempDir(), targetDir)
	err := action.Execute(context.Background(), execCtx)
	if err == nil {
		t.Fatal("expected error for non-zero exit")
	}
	if !strings.Contains(err.Error(), "command failed") {
		t.Errorf("expected 'command failed' error, got: %v", err)
	}
}
