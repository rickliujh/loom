package module

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"strings"

	prettylog "github.com/rickliujh/loom/internal/log"
	"github.com/rickliujh/loom/internal/proc"
	"github.com/rickliujh/loom/pkg/action"
	"github.com/rickliujh/loom/pkg/config"
	"github.com/rickliujh/loom/pkg/params"
	tmpl "github.com/rickliujh/loom/pkg/template"
)

// Module represents a loaded and resolved loom module.
type Module struct {
	// Dir is the directory containing the module's loom.yaml.
	Dir string
	// Config is the parsed loom.yaml.
	Config *config.LoomFile
	// Params are the resolved parameters for this module.
	Params map[string]any
	// Logger is the structured logger.
	Logger *slog.Logger
}

// Load loads a module from a directory, merging provided params with defaults.
// It is LoadContext under context.Background().
func Load(dir string, providedParams map[string]any, logger *slog.Logger) (*Module, error) {
	return LoadContext(context.Background(), dir, providedParams, logger)
}

// LoadContext loads a module from a directory, merging provided params with
// defaults. ctx bounds the dynamic-param commands, the only part of loading
// that runs anything.
func LoadContext(ctx context.Context, dir string, providedParams map[string]any, logger *slog.Logger) (*Module, error) {
	cfg, err := config.Load(dir)
	if err != nil {
		return nil, err
	}

	if err := config.ValidateInDir(cfg, dir); err != nil {
		return nil, fmt.Errorf("validating %s: %w", dir, err)
	}

	resolved, err := resolveParams(cfg.Spec.Params, cfg.Spec.DynamicParams, providedParams, logger)
	if err != nil {
		return nil, fmt.Errorf("resolving params for %s: %w", cfg.Metadata.Name, err)
	}

	if err := resolveDynamicParams(ctx, cfg.Spec.DynamicParams, resolved, providedParams, dir, logger); err != nil {
		return nil, fmt.Errorf("resolving dynamic params for %s: %w", cfg.Metadata.Name, err)
	}

	// T4: exclude/include patterns are templatable with resolved params.
	if err := renderPatterns("excludes", cfg.Spec.Excludes, resolved); err != nil {
		return nil, fmt.Errorf("module %s: %w", cfg.Metadata.Name, err)
	}
	if err := renderPatterns("includes", cfg.Spec.Includes, resolved); err != nil {
		return nil, fmt.Errorf("module %s: %w", cfg.Metadata.Name, err)
	}

	return &Module{
		Dir:    dir,
		Config: cfg,
		Params: resolved,
		Logger: logger.With(prettylog.KeyModule, cfg.Metadata.Name),
	}, nil
}

// renderPatterns templates each glob pattern in place with the resolved params.
func renderPatterns(field string, patterns []string, values map[string]any) error {
	for i, p := range patterns {
		rendered, err := tmpl.RenderString(p, values)
		if err != nil {
			return fmt.Errorf("rendering %s[%d]: %w", field, i, err)
		}
		patterns[i] = rendered
	}
	return nil
}

// resolveParams merges provided params with declared defaults, checking required params.
// Undeclared params (not in declared or dynamicDeclared) are rejected per P3.
// A provided value is coerced to its param's declared type (SP10), and every
// declared static param ends up present: an unset optional one as its type's
// empty value (SP6).
func resolveParams(declared []config.ParamDef, dynamicDeclared []config.DynamicParamDef, provided map[string]any, logger *slog.Logger) (map[string]any, error) {
	// Build set of all declared names (static + dynamic) for P3 validation.
	declaredNames := make(map[string]bool, len(declared)+len(dynamicDeclared))
	for _, p := range declared {
		declaredNames[p.Name] = true
	}
	for _, dp := range dynamicDeclared {
		declaredNames[dp.Name] = true
	}

	// P3: Reject undeclared params. Sorted so the error names the same param
	// on every run.
	for _, k := range sortedKeys(provided) {
		if !declaredNames[k] {
			return nil, fmt.Errorf("undeclared parameter %q", k)
		}
	}

	result := make(map[string]any)
	for _, p := range declared {
		if val, ok := provided[p.Name]; ok {
			v, err := params.Coerce(p.Name, p.Type, val)
			if err != nil {
				return nil, err
			}
			result[p.Name] = v
		} else if p.HasDefault() {
			result[p.Name] = p.Default
		} else if p.Required {
			return nil, fmt.Errorf("required parameter %q not provided", p.Name)
		} else {
			// Present-but-empty, not absent: a template reading an optional
			// param nobody set must print nothing, not "<no value>".
			result[p.Name] = params.Zero(p.Type)
		}
	}

	return result, nil
}

// resolveDynamicParams evaluates dynamic parameters after all regular params
// are resolved. The command string is templated with the resolved params before
// execution. Provided params override dynamic evaluation. Whichever value wins
// — provided, the command's output, or the rendered fallback default — is
// coerced to the param's declared type, so a list param's command prints YAML.
func resolveDynamicParams(ctx context.Context, declared []config.DynamicParamDef, resolved map[string]any, provided map[string]any, moduleDir string, logger *slog.Logger) error {
	for _, dp := range declared {
		val, err := dynamicValue(ctx, dp, resolved, provided, moduleDir, logger)
		if err != nil {
			return err
		}
		v, err := params.Coerce(dp.Name, dp.Type, val)
		if err != nil {
			return fmt.Errorf("dynamic param %q: %w", dp.Name, err)
		}
		resolved[dp.Name] = v
	}
	return nil
}

// dynamicValue produces a dynamic param's raw value, before coercion.
func dynamicValue(ctx context.Context, dp config.DynamicParamDef, resolved, provided map[string]any, moduleDir string, logger *slog.Logger) (any, error) {
	// P6: Provided params always take priority; log warning.
	if val, ok := provided[dp.Name]; ok {
		logger.Warn("CLI override skipping dynamic param command", "param", dp.Name)
		return val, nil
	}

	// Template the command with all currently resolved params.
	renderedCmd, err := tmpl.RenderString(dp.Command, resolved)
	if err != nil {
		return nil, fmt.Errorf("templating command for dynamic param %q: %w", dp.Name, err)
	}

	val, err := evalParamCommand(ctx, dp.Name, renderedCmd, moduleDir, logger)
	if err != nil {
		// A stopped command did not fail on its own merits, so the fallback
		// default does not apply: loading must stop too.
		if ctx.Err() != nil {
			return nil, err
		}
		if dp.Default != "" {
			renderedDefault, tmplErr := tmpl.RenderString(dp.Default, resolved)
			if tmplErr != nil {
				return nil, fmt.Errorf("templating default for dynamic param %q: %w", dp.Name, tmplErr)
			}
			logger.Warn("dynamic param command failed, using default", "param", dp.Name, "error", err)
			return renderedDefault, nil
		}
		return nil, err
	}
	return val, nil
}

// evalParamCommand runs a shell command and returns its trimmed stdout as the param value.
func evalParamCommand(ctx context.Context, name, command, moduleDir string, logger *slog.Logger) (string, error) {
	logger.Info("evaluating dynamic parameter", "param", name, "command", command)
	cmd := proc.Command(ctx, "sh", "-c", command)
	cmd.Dir = moduleDir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return "", fmt.Errorf("dynamic parameter %q command stopped: %w", name, context.Cause(ctx))
		}
		return "", fmt.Errorf("dynamic parameter %q command failed: %w\nstderr: %s", name, err, stderr.String())
	}
	val := strings.TrimRight(stdout.String(), "\n")
	logger.Info("dynamic parameter resolved", "param", name, "value", val)
	return val, nil
}

// NewExecutionContext creates an ExecutionContext for this module.
func (m *Module) NewExecutionContext(targetDir string, opts RunOptions) *action.ExecutionContext {
	repo, branch := m.targetRepo()
	return &action.ExecutionContext{
		ModuleName:   m.Config.Metadata.Name,
		ModulePath:   append([]string(nil), opts.ModulePath...),
		ModuleDir:    m.Dir,
		TargetDir:    targetDir,
		TargetLabel:  m.targetLabel(targetDir),
		TargetRepo:   repo,
		TargetBranch: branch,
		Params:       m.Params,
		Excludes:     m.Config.Spec.Excludes,
		Includes:     m.Config.Spec.Includes,
		DryRun:       opts.DryRun,
		LocalRun:     opts.LocalRun,
		ShowDiff:     opts.ShowDiff,
		Diffs:        opts.Diffs,
		GitAuthor:    opts.GitAuthor,
		GitEmail:     opts.GitEmail,
		Summary:      opts.Summary,
		Events:       opts.Events,
		Logger:       m.Logger,
	}
}

// targetLabel is a human-readable identity for the module's target, used as a
// header above collected diffs: the rendered repo URL and branch when the
// module has a target spec, otherwise the target directory it runs against.
func (m *Module) targetLabel(targetDir string) string {
	url, branch := m.targetRepo()
	if url == "" {
		return targetDir
	}
	if branch != "" {
		return url + " (" + branch + ")"
	}
	return url
}

// targetRepo renders the target spec's repo URL and branch, each empty when
// there is no target spec or it does not render.
func (m *Module) targetRepo() (url, branch string) {
	t := m.Config.Spec.Target
	if t == nil {
		return "", ""
	}
	url, err := tmpl.RenderString(t.URL, m.Params)
	if err != nil || url == "" {
		return "", ""
	}
	if branch, err = tmpl.RenderString(t.Branch, m.Params); err != nil {
		branch = ""
	}
	return url, branch
}
