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

	author := a.Config.Author
	if author == "" {
		author = execCtx.GitAuthor
	}
	email := a.Config.Email
	if email == "" {
		email = execCtx.GitEmail
	}

	execCtx.Logger.Info("commit and push", "message", msg)
	if execCtx.DryRun {
		execCtx.Logger.Info("dry-run: would commit and push", "message", msg, "author", author)
		return nil
	}

	if execCtx.LocalRun {
		execCtx.Logger.Info("local: committing without push", "message", msg, "author", author)
		return commitOnly(ctx, execCtx, msg, author, email)
	}

	return commitAndPush(ctx, execCtx, msg, author, email)
}
