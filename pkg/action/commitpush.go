package action

import (
	"context"

	"github.com/rickliujh/loom/pkg/config"
	tmpl "github.com/rickliujh/loom/pkg/template"
)

// CommitPushAction commits all changes and pushes to the remote.
type CommitPushAction struct {
	Config config.CommitPush
}

func (a *CommitPushAction) Execute(ctx context.Context, execCtx *ExecutionContext) error {
	msg, err := tmpl.RenderString(a.Config.Message, execCtx.Params)
	if err != nil {
		return actionError("commitPush", err)
	}

	execCtx.Logger.Info("commit and push", "message", msg)
	if execCtx.DryRun {
		execCtx.Logger.Info("dry-run: would commit and push", "message", msg, "author", a.Config.Author)
		return nil
	}

	// Delegate to git package — implemented in Phase 2.
	return commitAndPush(ctx, execCtx, msg, a.Config.Author, a.Config.Email)
}
