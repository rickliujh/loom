package config

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"text/template"
	"time"

	tmpl "github.com/rickliujh/loom/pkg/template"
)

const (
	ExpectedAPIVersion = "loom.rickliujh.github.io/v1beta1"
	ExpectedKind       = "Loom"
)

// Validate checks that a LoomFile has required fields and valid structure.
// All violations are collected and returned as a single joined error so a
// config can be fixed in one pass.
func Validate(lf *LoomFile) error {
	var errs []error
	fail := func(format string, args ...any) {
		errs = append(errs, fmt.Errorf(format, args...))
	}
	// checkTmpl parses value as a Go template with the loom function map and
	// records a violation on syntax errors. Values without template
	// expressions always parse, so they are skipped.
	checkTmpl := func(field, value string) {
		if !isTemplated(value) {
			return
		}
		if _, err := template.New("").Funcs(tmpl.FuncMap()).Parse(value); err != nil {
			fail("%s: invalid template: %v", field, err)
		}
	}

	if lf.APIVersion != ExpectedAPIVersion {
		fail("unsupported apiVersion %q, expected %q", lf.APIVersion, ExpectedAPIVersion)
	}
	if lf.Kind != ExpectedKind {
		fail("unsupported kind %q, expected %q", lf.Kind, ExpectedKind)
	}
	if lf.Metadata.Name == "" {
		fail("metadata.name is required")
	}

	paramNames := make(map[string]bool)
	for _, p := range lf.Spec.Params {
		if p.Name == "" {
			fail("param name cannot be empty")
			continue
		}
		if paramNames[p.Name] {
			fail("duplicate param name %q", p.Name)
		}
		paramNames[p.Name] = true
	}

	for _, dp := range lf.Spec.DynamicParams {
		if dp.Name == "" {
			fail("dynamicParam name cannot be empty")
			continue
		}
		if paramNames[dp.Name] {
			fail("duplicate param name %q (declared in both params and dynamicParams)", dp.Name)
		}
		if dp.Command == "" {
			fail("dynamicParam %q: command is required", dp.Name)
		}
		paramNames[dp.Name] = true
		checkTmpl(fmt.Sprintf("dynamicParam %q command", dp.Name), dp.Command)
		checkTmpl(fmt.Sprintf("dynamicParam %q default", dp.Name), dp.Default)
	}

	for i, e := range lf.Spec.Excludes {
		checkTmpl(fmt.Sprintf("spec.excludes[%d]", i), e)
	}
	for i, e := range lf.Spec.Includes {
		checkTmpl(fmt.Sprintf("spec.includes[%d]", i), e)
	}

	if lf.Spec.Target != nil {
		if lf.Spec.Target.URL == "" {
			fail("spec.target.url is required")
		}
		checkTmpl("spec.target.url", lf.Spec.Target.URL)
		checkTmpl("spec.target.branch", lf.Spec.Target.Branch)
		checkTmpl("spec.target.featureBranch", lf.Spec.Target.FeatureBranch)
	}

	moduleNames := make(map[string]bool)
	for _, m := range lf.Spec.Modules {
		if m.Name == "" {
			fail("module name cannot be empty")
		} else {
			if moduleNames[m.Name] {
				fail("duplicate module name %q", m.Name)
			}
			moduleNames[m.Name] = true
		}
		if m.Source == "" {
			fail("module %q: source is required", m.Name)
		}
		checkTmpl(fmt.Sprintf("module %q name", m.Name), m.Name)
		checkTmpl(fmt.Sprintf("module %q source", m.Name), m.Source)
		paramKeys := make([]string, 0, len(m.Params))
		for k := range m.Params {
			paramKeys = append(paramKeys, k)
		}
		sort.Strings(paramKeys)
		for _, k := range paramKeys {
			checkTmpl(fmt.Sprintf("module %q param %q", m.Name, k), m.Params[k])
		}
	}

	opNames := make(map[string]bool)
	for _, op := range lf.Spec.Operations {
		if op.Name == "" {
			fail("operation name cannot be empty")
		} else {
			if opNames[op.Name] {
				fail("duplicate operation name %q", op.Name)
			}
			opNames[op.Name] = true
		}

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
			fail("operation %q must have exactly one action type, got %d", op.Name, count)
		}

		if op.NewFiles != nil {
			if op.NewFiles.Source == "" {
				fail("operation %q: newFiles source is required", op.Name)
			}
			checkTmpl(fmt.Sprintf("operation %q newFiles.source", op.Name), op.NewFiles.Source)
			checkTmpl(fmt.Sprintf("operation %q newFiles.dest", op.Name), op.NewFiles.Dest)
		}

		if op.Patch != nil {
			if op.Patch.Path == "" {
				fail("operation %q: patch path is required", op.Name)
			}
			if op.Patch.Target == "" {
				fail("operation %q: patch target is required", op.Name)
			}
			// Skip enum validation for templated values (resolved at run time).
			if op.Patch.Engine != "" && !isTemplated(op.Patch.Engine) {
				switch op.Patch.Engine {
				case "smp", "json6902":
					// valid
				default:
					fail("operation %q: unknown patch engine %q (supported: smp, json6902)", op.Name, op.Patch.Engine)
				}
			}
			checkTmpl(fmt.Sprintf("operation %q patch.engine", op.Name), op.Patch.Engine)
			checkTmpl(fmt.Sprintf("operation %q patch.path", op.Name), op.Patch.Path)
			checkTmpl(fmt.Sprintf("operation %q patch.target", op.Name), op.Patch.Target)
		}

		if op.Shell != nil {
			if op.Shell.Command == "" {
				fail("operation %q: shell command is required", op.Name)
			}
			// Skip duration validation for templated values (resolved at run time).
			if op.Shell.Timeout != "" && !isTemplated(op.Shell.Timeout) {
				if _, err := time.ParseDuration(op.Shell.Timeout); err != nil {
					fail("operation %q: invalid shell timeout %q: %v", op.Name, op.Shell.Timeout, err)
				}
			}
			checkTmpl(fmt.Sprintf("operation %q shell.command", op.Name), op.Shell.Command)
			checkTmpl(fmt.Sprintf("operation %q shell.timeout", op.Name), op.Shell.Timeout)
		}

		if op.CommitPush != nil {
			if op.CommitPush.Message == "" {
				fail("operation %q: commitPush message is required", op.Name)
			}
			checkTmpl(fmt.Sprintf("operation %q commitPush.message", op.Name), op.CommitPush.Message)
			checkTmpl(fmt.Sprintf("operation %q commitPush.author", op.Name), op.CommitPush.Author)
			checkTmpl(fmt.Sprintf("operation %q commitPush.email", op.Name), op.CommitPush.Email)
		}

		if op.PR != nil {
			if op.PR.Provider == "" {
				fail("operation %q: pr provider is required", op.Name)
			} else if !isTemplated(op.PR.Provider) {
				switch op.PR.Provider {
				case "github", "gitlab":
					// valid
				default:
					fail("operation %q: unknown pr provider %q (supported: github, gitlab)", op.Name, op.PR.Provider)
				}
			}
			if op.PR.Title == "" {
				fail("operation %q: pr title is required", op.Name)
			}
			checkTmpl(fmt.Sprintf("operation %q pr.provider", op.Name), op.PR.Provider)
			checkTmpl(fmt.Sprintf("operation %q pr.title", op.Name), op.PR.Title)
			checkTmpl(fmt.Sprintf("operation %q pr.body", op.Name), op.PR.Body)
			checkTmpl(fmt.Sprintf("operation %q pr.baseBranch", op.Name), op.PR.BaseBranch)
			checkTmpl(fmt.Sprintf("operation %q pr.tokenEnv", op.Name), op.PR.TokenEnv)
			for i, l := range op.PR.Labels {
				checkTmpl(fmt.Sprintf("operation %q pr.labels[%d]", op.Name, i), l)
			}
		}

		if op.LLM != nil {
			// Skip enum validation for templated values (resolved at run time).
			if !isTemplated(op.LLM.Provider) {
				switch op.LLM.Provider {
				case "openai", "anthropic", "vertex", "gemini", "openrouter", "bedrock":
					// valid
				default:
					fail("operation %q: unknown llm provider %q (supported: openai, anthropic, vertex, gemini, openrouter, bedrock)", op.Name, op.LLM.Provider)
				}
			}
			if op.LLM.Model == "" {
				fail("operation %q: llm model is required", op.Name)
			}
			if op.LLM.Prompt == "" {
				fail("operation %q: llm prompt is required", op.Name)
			}
			if op.LLM.Target == "" {
				fail("operation %q: llm target is required", op.Name)
			}
			if op.LLM.Mode != "" && !isTemplated(op.LLM.Mode) && op.LLM.Mode != "generate" && op.LLM.Mode != "modify" {
				fail("operation %q: unknown llm mode %q (supported: generate, modify)", op.Name, op.LLM.Mode)
			}
			if op.LLM.Retries < 0 {
				fail("operation %q: llm retries must be >= 0", op.Name)
			}
			if op.LLM.RetryDelay != "" && !isTemplated(op.LLM.RetryDelay) {
				if _, err := time.ParseDuration(op.LLM.RetryDelay); err != nil {
					fail("operation %q: invalid llm retryDelay %q: %v", op.Name, op.LLM.RetryDelay, err)
				}
			}
			if !isTemplated(op.LLM.Provider) && op.LLM.Provider == "vertex" {
				if op.LLM.ProviderConfig == nil || op.LLM.ProviderConfig.Project == "" {
					fail("operation %q: llm providerConfig.project is required for vertex provider", op.Name)
				}
			}
			checkTmpl(fmt.Sprintf("operation %q llm.provider", op.Name), op.LLM.Provider)
			checkTmpl(fmt.Sprintf("operation %q llm.model", op.Name), op.LLM.Model)
			checkTmpl(fmt.Sprintf("operation %q llm.prompt", op.Name), op.LLM.Prompt)
			checkTmpl(fmt.Sprintf("operation %q llm.systemPrompt", op.Name), op.LLM.SystemPrompt)
			checkTmpl(fmt.Sprintf("operation %q llm.target", op.Name), op.LLM.Target)
			checkTmpl(fmt.Sprintf("operation %q llm.mode", op.Name), op.LLM.Mode)
			checkTmpl(fmt.Sprintf("operation %q llm.retryDelay", op.Name), op.LLM.RetryDelay)
			if pc := op.LLM.ProviderConfig; pc != nil {
				checkTmpl(fmt.Sprintf("operation %q llm.providerConfig.tokenEnv", op.Name), pc.TokenEnv)
				checkTmpl(fmt.Sprintf("operation %q llm.providerConfig.project", op.Name), pc.Project)
				checkTmpl(fmt.Sprintf("operation %q llm.providerConfig.location", op.Name), pc.Location)
			}
		}
	}

	return errors.Join(errs...)
}

// isTemplated returns true if the string contains Go template expressions.
func isTemplated(s string) bool {
	return strings.Contains(s, "{{")
}
