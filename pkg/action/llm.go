package action

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/rickliujh/loom/internal/util"
	"github.com/rickliujh/loom/pkg/config"
	"github.com/rickliujh/loom/pkg/llm"
	tmpl "github.com/rickliujh/loom/pkg/template"
)

// InferFunc is the signature for LLM inference. Defaults to llm.Infer.
type InferFunc func(ctx context.Context, opts llm.InferenceOptions) (string, error)

// LLMAction uses LLM inference to generate or modify a file.
type LLMAction struct {
	Config config.LLM
	// Infer overrides the inference function (for testing). Nil uses llm.Infer.
	Infer InferFunc
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

	var tokenEnv, project, location string
	if pc := a.Config.ProviderConfig; pc != nil {
		tokenEnv, err = render("providerConfig.tokenEnv", pc.TokenEnv)
		if err != nil {
			return err
		}
		project, err = render("providerConfig.project", pc.Project)
		if err != nil {
			return err
		}
		location, err = render("providerConfig.location", pc.Location)
		if err != nil {
			return err
		}
	}

	targetPath := filepath.Join(execCtx.TargetDir, targetRel)

	modeStr, err := render("mode", a.Config.Mode)
	if err != nil {
		return err
	}
	mode := modeStr
	if mode == "" {
		mode = "generate"
	}

	log := execCtx.Logger

	// Log template resolution details at debug level.
	log.Debug("llm template resolved",
		"provider", fmt.Sprintf("%s -> %s", a.Config.Provider, provider),
		"model", fmt.Sprintf("%s -> %s", a.Config.Model, model),
		"target", fmt.Sprintf("%s -> %s", a.Config.Target, targetRel),
	)
	log.Debug("llm prompt rendered",
		"promptLength", len(prompt),
		"prompt", prompt,
	)
	if systemPrompt != "" {
		log.Debug("llm systemPrompt rendered",
			"systemPromptLength", len(systemPrompt),
			"systemPrompt", systemPrompt,
		)
	}
	if tokenEnv != "" || project != "" || location != "" {
		log.Debug("llm providerConfig",
			"tokenEnv", tokenEnv,
			"project", project,
			"location", location,
		)
	}

	log.Info("invoking LLM",
		"provider", provider,
		"model", model,
		"mode", mode,
		"target", targetPath,
		"promptLength", len(prompt),
	)

	if execCtx.DryRun {
		log.Info("dry-run: would invoke LLM and write result",
			"target", targetPath,
			"promptLength", len(prompt),
		)
		return nil
	}

	// In generate mode, fail if the target already exists.
	if mode == "generate" {
		if _, err := os.Stat(targetPath); err == nil {
			return actionError("llm", fmt.Errorf("target already exists: %s", targetPath))
		}
	}

	// In modify mode, read the existing file and prepend its content to the prompt.
	if mode == "modify" {
		existing, err := os.ReadFile(targetPath)
		if err != nil {
			return actionError("llm", fmt.Errorf("reading target for modify: %w", err))
		}
		log.Debug("llm modify mode: read existing file",
			"path", targetPath,
			"existingSize", len(existing),
		)
		prompt = fmt.Sprintf("Here is the existing file content:\n\n```\n%s\n```\n\n%s", string(existing), prompt)
		log.Debug("llm modify mode: final prompt composed",
			"finalPromptLength", len(prompt),
		)
	}

	retryDelayStr, err := render("retryDelay", a.Config.RetryDelay)
	if err != nil {
		return err
	}
	var retryDelay time.Duration
	if retryDelayStr != "" {
		retryDelay, err = time.ParseDuration(retryDelayStr)
		if err != nil {
			return actionError("llm", fmt.Errorf("parsing retryDelay %q: %w", retryDelayStr, err))
		}
	}

	if a.Config.Retries > 0 {
		log.Info("retry enabled",
			"maxRetries", a.Config.Retries,
			"initialDelay", retryDelay.String(),
		)
	}

	// Build provider options.
	opts := llm.InferenceOptions{
		Provider:     provider,
		Model:        model,
		Prompt:       prompt,
		SystemPrompt: systemPrompt,
		MaxTokens:    a.Config.MaxTokens,
		Retries:      a.Config.Retries,
		RetryDelay:   retryDelay,
		Logger:       log,
		TokenEnv:     tokenEnv,
		Project:      project,
		Location:     location,
	}

	inferFn := a.Infer
	if inferFn == nil {
		inferFn = llm.Infer
	}

	result, err := inferFn(ctx, opts)
	if err != nil {
		return actionError("llm", err)
	}

	log.Info("LLM inference complete",
		"provider", provider,
		"model", model,
		"outputSize", len(result),
	)
	log.Debug("llm output content", "result", result)
	log.Info("writing LLM output", "path", targetPath, "size", len(result))
	if err := util.WriteFile(targetPath, []byte(result), 0o644); err != nil {
		return actionError("llm", fmt.Errorf("writing output: %w", err))
	}

	return nil
}
