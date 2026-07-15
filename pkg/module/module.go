package module

import (
	"bytes"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"

	"github.com/rickliujh/loom/pkg/action"
	"github.com/rickliujh/loom/pkg/config"
	tmpl "github.com/rickliujh/loom/pkg/template"
)

// Module represents a loaded and resolved loom module.
type Module struct {
	// Dir is the directory containing the module's loom.yaml.
	Dir string
	// Config is the parsed loom.yaml.
	Config *config.LoomFile
	// Params are the resolved parameters for this module.
	Params map[string]string
	// Logger is the structured logger.
	Logger *slog.Logger
}

// Load loads a module from a directory, merging provided params with defaults.
func Load(dir string, providedParams map[string]string, logger *slog.Logger) (*Module, error) {
	cfg, err := config.Load(dir)
	if err != nil {
		return nil, err
	}

	if err := config.ValidateInDir(cfg, dir); err != nil {
		return nil, fmt.Errorf("validating %s: %w", dir, err)
	}

	params, err := resolveParams(cfg.Spec.Params, cfg.Spec.DynamicParams, providedParams, logger)
	if err != nil {
		return nil, fmt.Errorf("resolving params for %s: %w", cfg.Metadata.Name, err)
	}

	if err := resolveDynamicParams(cfg.Spec.DynamicParams, params, providedParams, dir, logger); err != nil {
		return nil, fmt.Errorf("resolving dynamic params for %s: %w", cfg.Metadata.Name, err)
	}

	// T4: exclude/include patterns are templatable with resolved params.
	if err := renderPatterns("excludes", cfg.Spec.Excludes, params); err != nil {
		return nil, fmt.Errorf("module %s: %w", cfg.Metadata.Name, err)
	}
	if err := renderPatterns("includes", cfg.Spec.Includes, params); err != nil {
		return nil, fmt.Errorf("module %s: %w", cfg.Metadata.Name, err)
	}

	return &Module{
		Dir:    dir,
		Config: cfg,
		Params: params,
		Logger: logger.With("module", cfg.Metadata.Name),
	}, nil
}

// renderPatterns templates each glob pattern in place with the resolved params.
func renderPatterns(field string, patterns []string, params map[string]string) error {
	for i, p := range patterns {
		rendered, err := tmpl.RenderString(p, params)
		if err != nil {
			return fmt.Errorf("rendering %s[%d]: %w", field, i, err)
		}
		patterns[i] = rendered
	}
	return nil
}

// resolveParams merges provided params with declared defaults, checking required params.
// Undeclared params (not in declared or dynamicDeclared) are rejected per P3.
func resolveParams(declared []config.ParamDef, dynamicDeclared []config.DynamicParamDef, provided map[string]string, logger *slog.Logger) (map[string]string, error) {
	result := make(map[string]string)

	for _, p := range declared {
		if val, ok := provided[p.Name]; ok {
			result[p.Name] = val
		} else if p.Default != "" {
			result[p.Name] = p.Default
		} else if p.Required {
			return nil, fmt.Errorf("required parameter %q not provided", p.Name)
		}
	}

	// Build set of all declared names (static + dynamic) for P3 validation.
	declaredNames := make(map[string]bool, len(declared)+len(dynamicDeclared))
	for _, p := range declared {
		declaredNames[p.Name] = true
	}
	for _, dp := range dynamicDeclared {
		declaredNames[dp.Name] = true
	}

	// P3: Reject undeclared params.
	for k := range provided {
		if !declaredNames[k] {
			return nil, fmt.Errorf("undeclared parameter %q", k)
		}
	}

	return result, nil
}

// resolveDynamicParams evaluates dynamic parameters after all regular params
// are resolved. The command string is templated with the resolved params before
// execution. Provided params override dynamic evaluation.
func resolveDynamicParams(declared []config.DynamicParamDef, resolved map[string]string, provided map[string]string, moduleDir string, logger *slog.Logger) error {
	for _, dp := range declared {
		// P6: Provided params always take priority; log warning.
		if val, ok := provided[dp.Name]; ok {
			logger.Warn("CLI override skipping dynamic param command", "param", dp.Name)
			resolved[dp.Name] = val
			continue
		}

		// Template the command with all currently resolved params.
		renderedCmd, err := tmpl.RenderString(dp.Command, resolved)
		if err != nil {
			return fmt.Errorf("templating command for dynamic param %q: %w", dp.Name, err)
		}

		val, err := evalParamCommand(dp.Name, renderedCmd, moduleDir, logger)
		if err != nil {
			if dp.Default != "" {
				renderedDefault, tmplErr := tmpl.RenderString(dp.Default, resolved)
				if tmplErr != nil {
					return fmt.Errorf("templating default for dynamic param %q: %w", dp.Name, tmplErr)
				}
				logger.Warn("dynamic param command failed, using default", "param", dp.Name, "error", err)
				resolved[dp.Name] = renderedDefault
				continue
			}
			return err
		}
		resolved[dp.Name] = val
	}
	return nil
}

// evalParamCommand runs a shell command and returns its trimmed stdout as the param value.
func evalParamCommand(name, command, moduleDir string, logger *slog.Logger) (string, error) {
	logger.Info("evaluating dynamic parameter", "param", name, "command", command)
	cmd := exec.Command("sh", "-c", command)
	cmd.Dir = moduleDir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("dynamic parameter %q command failed: %w\nstderr: %s", name, err, stderr.String())
	}
	val := strings.TrimRight(stdout.String(), "\n")
	logger.Info("dynamic parameter resolved", "param", name, "value", val)
	return val, nil
}

// NewExecutionContext creates an ExecutionContext for this module.
func (m *Module) NewExecutionContext(targetDir string, opts RunOptions) *action.ExecutionContext {
	diffWriter := opts.DiffWriter
	if diffWriter == nil && opts.ShowDiff {
		diffWriter = os.Stdout
	}
	return &action.ExecutionContext{
		ModuleName: m.Config.Metadata.Name,
		ModuleDir:  m.Dir,
		TargetDir:  targetDir,
		Params:     m.Params,
		Excludes:   m.Config.Spec.Excludes,
		Includes:   m.Config.Spec.Includes,
		DryRun:     opts.DryRun,
		LocalRun:   opts.LocalRun,
		ShowDiff:   opts.ShowDiff,
		DiffWriter: diffWriter,
		GitAuthor:  opts.GitAuthor,
		GitEmail:   opts.GitEmail,
		Summary:    opts.Summary,
		Logger:     m.Logger,
	}
}
