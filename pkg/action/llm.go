package action

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/rickliujh/loom/internal/util"
	"github.com/rickliujh/loom/pkg/config"
	"github.com/rickliujh/loom/pkg/llm"
	tmpl "github.com/rickliujh/loom/pkg/template"
)

// LLMAction uses LLM inference to generate or modify a file.
type LLMAction struct {
	Config config.LLM
}

func (a *LLMAction) Execute(ctx context.Context, execCtx *ExecutionContext) error {
	render := func(name, val string) (string, error) {
		out, err := tmpl.RenderString(val, execCtx.Params)
		if err != nil {
			return "", actionError("llm", fmt.Errorf("rendering %s: %w", name, err))
		}
		return out, nil
	}

	provider, err := render("provider", a.Config.Provider)
	if err != nil {
		return err
	}
	model, err := render("model", a.Config.Model)
	if err != nil {
		return err
	}
	prompt, err := render("prompt", a.Config.Prompt)
	if err != nil {
		return err
	}
	systemPrompt, err := render("systemPrompt", a.Config.SystemPrompt)
	if err != nil {
		return err
	}
	targetRel, err := render("target", a.Config.Target)
	if err != nil {
		return err
	}
	project, err := render("project", a.Config.Project)
	if err != nil {
		return err
	}
	location, err := render("location", a.Config.Location)
	if err != nil {
		return err
	}

	targetPath := filepath.Join(execCtx.TargetDir, targetRel)

	mode := a.Config.Mode
	if mode == "" {
		mode = "generate"
	}

	execCtx.Logger.Info("invoking LLM",
		"provider", provider,
		"model", model,
		"mode", mode,
		"target", targetPath,
	)

	if execCtx.DryRun {
		execCtx.Logger.Info("dry-run: would invoke LLM and write result", "target", targetPath)
		return nil
	}

	// In modify mode, read the existing file and prepend its content to the prompt.
	if mode == "modify" {
		existing, err := os.ReadFile(targetPath)
		if err != nil {
			return actionError("llm", fmt.Errorf("reading target for modify: %w", err))
		}
		prompt = fmt.Sprintf("Here is the existing file content:\n\n```\n%s\n```\n\n%s", string(existing), prompt)
	}

	// Build provider options.
	opts := llm.InferenceOptions{
		Provider:     provider,
		Model:        model,
		Prompt:       prompt,
		SystemPrompt: systemPrompt,
		TokenEnv:     a.Config.TokenEnv,
		Project:      project,
		Location:     location,
		MaxTokens:    a.Config.MaxTokens,
	}

	result, err := llm.Infer(ctx, opts)
	if err != nil {
		return actionError("llm", err)
	}

	execCtx.Logger.Info("writing LLM output", "path", targetPath, "size", len(result))
	if err := util.WriteFile(targetPath, []byte(result), 0o644); err != nil {
		return actionError("llm", fmt.Errorf("writing output: %w", err))
	}

	return nil
}
