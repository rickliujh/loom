package config

import (
	"fmt"
	"strings"
	"time"
)

const (
	ExpectedAPIVersion = "loom.rickliujh.github.io/v1beta1"
	ExpectedKind       = "Loom"
)

// Validate checks that a LoomFile has required fields and valid structure.
func Validate(lf *LoomFile) error {
	if lf.APIVersion != ExpectedAPIVersion {
		return fmt.Errorf("unsupported apiVersion %q, expected %q", lf.APIVersion, ExpectedAPIVersion)
	}
	if lf.Kind != ExpectedKind {
		return fmt.Errorf("unsupported kind %q, expected %q", lf.Kind, ExpectedKind)
	}
	if lf.Metadata.Name == "" {
		return fmt.Errorf("metadata.name is required")
	}

	paramNames := make(map[string]bool)
	for _, p := range lf.Spec.Params {
		if p.Name == "" {
			return fmt.Errorf("param name cannot be empty")
		}
		if paramNames[p.Name] {
			return fmt.Errorf("duplicate param name %q", p.Name)
		}
		paramNames[p.Name] = true
	}

	for _, dp := range lf.Spec.DynamicParams {
		if dp.Name == "" {
			return fmt.Errorf("dynamicParam name cannot be empty")
		}
		if paramNames[dp.Name] {
			return fmt.Errorf("duplicate param name %q (declared in both params and dynamicParams)", dp.Name)
		}
		if dp.Command == "" {
			return fmt.Errorf("dynamicParam %q: command is required", dp.Name)
		}
		paramNames[dp.Name] = true
	}

	opNames := make(map[string]bool)
	for _, op := range lf.Spec.Operations {
		if op.Name == "" {
			return fmt.Errorf("operation name cannot be empty")
		}
		if opNames[op.Name] {
			return fmt.Errorf("duplicate operation name %q", op.Name)
		}
		opNames[op.Name] = true

		count := 0
		if op.NewFiles != nil {
			count++
		}
		if op.Patch != nil {
			count++
		}
		if op.Shell != nil {
			count++
		}
		if op.CommitPush != nil {
			count++
		}
		if op.PR != nil {
			count++
		}
		if op.LLM != nil {
			count++
		}
		if count != 1 {
			return fmt.Errorf("operation %q must have exactly one action type, got %d", op.Name, count)
		}

		if op.NewFiles != nil && op.NewFiles.Source == "" {
			return fmt.Errorf("operation %q: newFiles source is required", op.Name)
		}

		if op.Patch != nil {
			if op.Patch.Path == "" {
				return fmt.Errorf("operation %q: patch path is required", op.Name)
			}
			if op.Patch.Target == "" {
				return fmt.Errorf("operation %q: patch target is required", op.Name)
			}
			if op.Patch.Engine != "" {
				switch op.Patch.Engine {
				case "smp", "json6902":
					// valid
				default:
					return fmt.Errorf("operation %q: unknown patch engine %q (supported: smp, json6902)", op.Name, op.Patch.Engine)
				}
			}
		}

		if op.Shell != nil {
			if op.Shell.Command == "" {
				return fmt.Errorf("operation %q: shell command is required", op.Name)
			}
			// Skip duration validation for templated values (resolved at run time).
			if op.Shell.Timeout != "" && !isTemplated(op.Shell.Timeout) {
				if _, err := time.ParseDuration(op.Shell.Timeout); err != nil {
					return fmt.Errorf("operation %q: invalid shell timeout %q: %w", op.Name, op.Shell.Timeout, err)
				}
			}
		}

		if op.CommitPush != nil && op.CommitPush.Message == "" {
			return fmt.Errorf("operation %q: commitPush message is required", op.Name)
		}

		if op.PR != nil {
			if op.PR.Provider == "" {
				return fmt.Errorf("operation %q: pr provider is required", op.Name)
			}
			if !isTemplated(op.PR.Provider) {
				switch op.PR.Provider {
				case "github", "gitlab":
					// valid
				default:
					return fmt.Errorf("operation %q: unknown pr provider %q (supported: github, gitlab)", op.Name, op.PR.Provider)
				}
			}
			if op.PR.Title == "" {
				return fmt.Errorf("operation %q: pr title is required", op.Name)
			}
		}

		if op.LLM != nil {
			// Skip enum validation for templated values (contain "{{").
			if !isTemplated(op.LLM.Provider) {
				switch op.LLM.Provider {
				case "openai", "anthropic", "vertex", "gemini", "openrouter", "bedrock":
					// valid
				default:
					return fmt.Errorf("operation %q: unknown llm provider %q (supported: openai, anthropic, vertex, gemini, openrouter, bedrock)", op.Name, op.LLM.Provider)
				}
			}
			if op.LLM.Model == "" {
				return fmt.Errorf("operation %q: llm model is required", op.Name)
			}
			if op.LLM.Prompt == "" {
				return fmt.Errorf("operation %q: llm prompt is required", op.Name)
			}
			if op.LLM.Target == "" {
				return fmt.Errorf("operation %q: llm target is required", op.Name)
			}
			if op.LLM.Mode != "" && op.LLM.Mode != "generate" && op.LLM.Mode != "modify" {
				return fmt.Errorf("operation %q: unknown llm mode %q (supported: generate, modify)", op.Name, op.LLM.Mode)
			}
			if op.LLM.Retries < 0 {
				return fmt.Errorf("operation %q: llm retries must be >= 0", op.Name)
			}
			if op.LLM.RetryDelay != "" {
				if _, err := time.ParseDuration(op.LLM.RetryDelay); err != nil {
					return fmt.Errorf("operation %q: invalid llm retryDelay %q: %w", op.Name, op.LLM.RetryDelay, err)
				}
			}
			if !isTemplated(op.LLM.Provider) && op.LLM.Provider == "vertex" {
				if op.LLM.ProviderConfig == nil || op.LLM.ProviderConfig.Project == "" {
					return fmt.Errorf("operation %q: llm providerConfig.project is required for vertex provider", op.Name)
				}
			}
		}
	}

	return nil
}

// isTemplated returns true if the string contains Go template expressions.
func isTemplated(s string) bool {
	return strings.Contains(s, "{{")
}
