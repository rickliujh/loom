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

	baseBranch, err := tmpl.RenderString(a.Config.BaseBranch, execCtx.Params)
	if err != nil {
		return actionError("pr", err)
	}

	provider, err := tmpl.RenderString(a.Config.Provider, execCtx.Params)
	if err != nil {
		return actionError("pr", err)
	}

	tokenEnv, err := tmpl.RenderString(a.Config.TokenEnv, execCtx.Params)
	if err != nil {
		return actionError("pr", err)
	}

	labels := make([]string, 0, len(a.Config.Labels))
	for _, l := range a.Config.Labels {
		rendered, err := tmpl.RenderString(l, execCtx.Params)
		if err != nil {
			return actionError("pr", err)
		}
		labels = append(labels, rendered)
	}

	if execCtx.DryRun {
		execCtx.Logger.Info("dry-run: would open PR", "title", title, "provider", provider)
		return nil
	}

	if execCtx.LocalRun {
		execCtx.Logger.Info("local-run: skipping PR creation", "title", title)
		return nil
	}

	execCtx.Logger.Info("opening PR", "title", title, "provider", provider)

	return openPR(ctx, execCtx, provider, tokenEnv, baseBranch, labels, title, body)
}
