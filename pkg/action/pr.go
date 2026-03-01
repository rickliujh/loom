package action

import (
	"context"

	"github.com/rickliujh/loom/pkg/config"
	tmpl "github.com/rickliujh/loom/pkg/template"
)

// PRAction opens a pull request or merge request.
type PRAction struct {
	Config config.PR
}

func (a *PRAction) Execute(ctx context.Context, execCtx *ExecutionContext) error {
	title, err := tmpl.RenderString(a.Config.Title, execCtx.Params)
	if err != nil {
		return actionError("pr", err)
	}

	body, err := tmpl.RenderString(a.Config.Body, execCtx.Params)
	if err != nil {
		return actionError("pr", err)
	}

	execCtx.Logger.Info("opening PR", "title", title, "provider", a.Config.Provider)
	if execCtx.DryRun {
		execCtx.Logger.Info("dry-run: would open PR", "title", title, "provider", a.Config.Provider)
		return nil
	}

	// Delegate to git/provider — implemented in Phase 3.
	return openPR(ctx, execCtx, a.Config, title, body)
}
