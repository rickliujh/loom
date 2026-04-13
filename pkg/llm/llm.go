// Package llm provides a unified interface for LLM inference across
// multiple providers (OpenAI, Anthropic, Google Vertex AI, Google Gemini, OpenRouter, AWS Bedrock).
package llm

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/tmc/langchaingo/llms"
	"github.com/tmc/langchaingo/llms/anthropic"
	"github.com/tmc/langchaingo/llms/bedrock"
	"github.com/tmc/langchaingo/llms/googleai"
	"github.com/tmc/langchaingo/llms/googleai/vertex"
	"github.com/tmc/langchaingo/llms/openai"
)

// InferenceOptions holds the configuration for a single LLM inference call.
type InferenceOptions struct {
	Provider     string // "openai", "anthropic", "vertex", "gemini", "openrouter", "bedrock"
	Model        string
	Prompt       string
	SystemPrompt string
	MaxTokens    int

	// Retry
	Retries    int           // Max retry attempts (0 = no retry)
	RetryDelay time.Duration // Initial delay between retries (doubles each attempt)
	Logger     *slog.Logger  // Logger for retry logging (optional)

	// Provider-specific
	TokenEnv string // Env var name holding the API key (openai, anthropic, gemini, openrouter)
	Project  string // GCP project ID (vertex only)
	Location string // GCP region (vertex only)
}

// Infer performs a single LLM inference and returns the text result.
// If opts.Retries > 0, failed attempts are retried with exponential backoff.
func Infer(ctx context.Context, opts InferenceOptions) (string, error) {
	model, err := newModel(ctx, opts)
	if err != nil {
		return "", fmt.Errorf("creating %s client: %w", opts.Provider, err)
	}

	return inferWithModel(ctx, model, opts)
}

func inferWithModel(ctx context.Context, model llms.Model, opts InferenceOptions) (string, error) {
	var callOpts []llms.CallOption
	if opts.MaxTokens > 0 {
		callOpts = append(callOpts, llms.WithMaxTokens(opts.MaxTokens))
	}

	var messages []llms.MessageContent
	if opts.SystemPrompt != "" {
		messages = append(messages, llms.TextParts(llms.ChatMessageTypeSystem, opts.SystemPrompt))
	}
	messages = append(messages, llms.TextParts(llms.ChatMessageTypeHuman, opts.Prompt))

	log := opts.Logger
	if log == nil {
		log = slog.Default()
	}

	maxAttempts := 1 + opts.Retries
	delay := opts.RetryDelay
	if delay == 0 {
		delay = 2 * time.Second
	}

	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		resp, err := model.GenerateContent(ctx, messages, callOpts...)
		if err != nil {
			lastErr = fmt.Errorf("%s inference failed: %w", opts.Provider, err)
			if attempt < maxAttempts {
				log.Warn("LLM inference failed, retrying",
					"attempt", attempt,
					"maxAttempts", maxAttempts,
					"retryDelay", delay.String(),
					"error", lastErr,
				)
				select {
				case <-ctx.Done():
					return "", fmt.Errorf("%s inference cancelled during retry: %w", opts.Provider, ctx.Err())
				case <-time.After(delay):
				}
				delay *= 2
				continue
			}
			return "", lastErr
		}

		if len(resp.Choices) == 0 {
			lastErr = fmt.Errorf("%s returned no content", opts.Provider)
			if attempt < maxAttempts {
				log.Warn("LLM returned no content, retrying",
					"attempt", attempt,
					"maxAttempts", maxAttempts,
					"retryDelay", delay.String(),
				)
				select {
				case <-ctx.Done():
					return "", fmt.Errorf("%s inference cancelled during retry: %w", opts.Provider, ctx.Err())
				case <-time.After(delay):
				}
				delay *= 2
				continue
			}
			return "", lastErr
		}

		if attempt > 1 {
			log.Info("LLM inference succeeded after retry", "attempt", attempt)
		}
		return resp.Choices[0].Content, nil
	}

	return "", lastErr
}

// resolveToken reads an API key from the given env var name, falling back
// to a provider-specific default.
func resolveToken(tokenEnv, fallback string) string {
	if tokenEnv != "" {
		return os.Getenv(tokenEnv)
	}
	return os.Getenv(fallback)
}

func newModel(ctx context.Context, opts InferenceOptions) (llms.Model, error) {
	switch opts.Provider {
	case "openai":
		token := resolveToken(opts.TokenEnv, "OPENAI_API_KEY")
		return openai.New(
			openai.WithModel(opts.Model),
			openai.WithToken(token),
		)

	case "anthropic":
		token := resolveToken(opts.TokenEnv, "ANTHROPIC_API_KEY")
		return anthropic.New(
			anthropic.WithModel(opts.Model),
			anthropic.WithToken(token),
		)

	case "vertex":
		location := opts.Location
		if location == "" {
			location = "us-central1"
		}
		// Vertex AI uses ADC (Application Default Credentials) by default.
		// No API key needed when running on GCP or with gcloud auth configured.
		return vertex.New(ctx,
			googleai.WithDefaultModel(opts.Model),
			googleai.WithCloudProject(opts.Project),
			googleai.WithCloudLocation(location),
		)

	case "gemini":
		token := resolveToken(opts.TokenEnv, "GEMINI_API_KEY")
		return googleai.New(ctx,
			googleai.WithDefaultModel(opts.Model),
			googleai.WithAPIKey(token),
		)

	case "openrouter":
		token := resolveToken(opts.TokenEnv, "OPENROUTER_API_KEY")
		return openai.New(
			openai.WithModel(opts.Model),
			openai.WithToken(token),
			openai.WithBaseURL("https://openrouter.ai/api/v1"),
		)

	case "bedrock":
		// Bedrock uses AWS SDK credentials (env vars, shared config, IAM role).
		// No API key needed. Location maps to AWS region.
		return bedrock.New(
			bedrock.WithModel(opts.Model),
		)

	default:
		return nil, fmt.Errorf("unsupported provider %q", opts.Provider)
	}
}
