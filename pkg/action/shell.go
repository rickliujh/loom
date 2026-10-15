package action

import (
	"context"
	"fmt"
	"time"

	"github.com/rickliujh/loom/internal/proc"
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

	timeout, err := tmpl.RenderString(a.Config.Timeout, execCtx.Params)
	if err != nil {
		return actionError("shell", err)
	}

	if execCtx.DryRun {
		execCtx.Logger.Info("dry-run: would run shell command", "command", cmdStr)
		return nil
	}

	if execCtx.LocalRun && !a.Config.Pure {
		execCtx.Logger.Info("local-run: skipping shell command (not marked pure)", "command", cmdStr)
		return nil
	}

	execCtx.Logger.Info("running shell command", "command", cmdStr)

	// runCtx is the run's own context; ctx may yet gain the step's timeout.
	// Only the former stopping the command is a cancellation — a timeout is
	// the step failing.
	runCtx := ctx
	if timeout != "" {
		dur, err := time.ParseDuration(timeout)
		if err != nil {
			return actionError("shell", fmt.Errorf("invalid timeout %q: %w", timeout, err))
		}
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, dur)
		defer cancel()
	}

	cmd := proc.Command(ctx, "sh", "-c", cmdStr)
	cmd.Dir = execCtx.TargetDir
	output, err := cmd.CombinedOutput()
	if len(output) > 0 {
		execCtx.Logger.Info("shell output", "output", string(output))
	}
	if err != nil && runCtx.Err() != nil {
		return actionError("shell", fmt.Errorf("command stopped: %w", context.Cause(runCtx)))
	}
	if err != nil {
		return actionError("shell", fmt.Errorf("command failed: %w\noutput: %s", err, output))
	}

	return nil
}
