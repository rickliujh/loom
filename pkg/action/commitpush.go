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

	author, err := tmpl.RenderString(a.Config.Author, execCtx.Params)
	if err != nil {
		return actionError("commitPush", err)
	}
	if author == "" {
		author = execCtx.GitAuthor
	}
	email, err := tmpl.RenderString(a.Config.Email, execCtx.Params)
	if err != nil {
		return actionError("commitPush", err)
	}
	if email == "" {
		email = execCtx.GitEmail
	}

	if execCtx.DryRun {
		execCtx.Logger.Info("dry-run: would commit and push", "message", msg, "author", author)
		return nil
	}

	if execCtx.LocalRun {
		execCtx.Logger.Info("local-run: committing without push", "message", msg)
		return commitOnly(ctx, execCtx, msg, author, email)
	}

	execCtx.Logger.Info("committing and pushing", "message", msg)
	return commitAndPush(ctx, execCtx, msg, author, email)
}
