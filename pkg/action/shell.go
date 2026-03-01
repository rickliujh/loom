package action

import (
	"context"
	"fmt"
	"os/exec"
	"time"

	"github.com/rickliujh/loom/pkg/config"
	tmpl "github.com/rickliujh/loom/pkg/template"
)

// ShellAction runs a shell command on the host.
type ShellAction struct {
	Config config.Shell
}

func (a *ShellAction) Execute(ctx context.Context, execCtx *ExecutionContext) error {
	cmdStr, err := tmpl.RenderString(a.Config.Command, execCtx.Params)
	if err != nil {
		return actionError("shell", err)
	}

	execCtx.Logger.Info("running shell command", "command", cmdStr)
	if execCtx.DryRun {
		execCtx.Logger.Info("dry-run: would execute", "command", cmdStr)
		return nil
	}

	if a.Config.Timeout != "" {
		dur, err := time.ParseDuration(a.Config.Timeout)
		if err != nil {
			return actionError("shell", fmt.Errorf("invalid timeout %q: %w", a.Config.Timeout, err))
		}
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, dur)
		defer cancel()
	}

	cmd := exec.CommandContext(ctx, "sh", "-c", cmdStr)
	cmd.Dir = execCtx.TargetDir
	output, err := cmd.CombinedOutput()
	if len(output) > 0 {
		execCtx.Logger.Info("shell output", "output", string(output))
	}
	if err != nil {
		return actionError("shell", fmt.Errorf("command failed: %w\noutput: %s", err, output))
	}

	return nil
}
