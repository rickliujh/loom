package module

import (
	"bytes"
	"fmt"
	"log/slog"
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

	if err := config.Validate(cfg); err != nil {
		return nil, fmt.Errorf("validating %s: %w", dir, err)
	}

	params, err := resolveParams(cfg.Spec.Params, providedParams, logger)
	if err != nil {
		return nil, fmt.Errorf("resolving params for %s: %w", cfg.Metadata.Name, err)
	}

	if err := resolveDynamicParams(cfg.Spec.DynamicParams, params, providedParams, logger); err != nil {
		return nil, fmt.Errorf("resolving dynamic params for %s: %w", cfg.Metadata.Name, err)
	}

	return &Module{
		Dir:    dir,
		Config: cfg,
		Params: params,
		Logger: logger.With("module", cfg.Metadata.Name),
	}, nil
}

// resolveParams merges provided params with declared defaults, checking required params.
func resolveParams(declared []config.ParamDef, provided map[string]string, logger *slog.Logger) (map[string]string, error) {
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

	// Also pass through any extra params not declared (for flexibility).
	for k, v := range provided {
		if _, exists := result[k]; !exists {
			result[k] = v
		}
	}

	return result, nil
}

// resolveDynamicParams evaluates dynamic parameters after all regular params
// are resolved. The command string is templated with the resolved params before
// execution. Provided params override dynamic evaluation.
func resolveDynamicParams(declared []config.DynamicParamDef, resolved map[string]string, provided map[string]string, logger *slog.Logger) error {
	for _, dp := range declared {
		// Provided params always take priority.
		if val, ok := provided[dp.Name]; ok {
			resolved[dp.Name] = val
			continue
		}

		// Template the command with all currently resolved params.
		renderedCmd, err := tmpl.RenderString(dp.Command, resolved)
		if err != nil {
			return fmt.Errorf("templating command for dynamic param %q: %w", dp.Name, err)
		}

		val, err := evalParamCommand(dp.Name, renderedCmd, logger)
		if err != nil {
			if dp.Default != "" {
				logger.Warn("dynamic param command failed, using default", "param", dp.Name, "error", err)
				resolved[dp.Name] = dp.Default
				continue
			}
			return err
		}
		resolved[dp.Name] = val
	}
	return nil
}

// evalParamCommand runs a shell command and returns its trimmed stdout as the param value.
func evalParamCommand(name, command string, logger *slog.Logger) (string, error) {
	logger.Info("evaluating dynamic parameter", "param", name, "command", command)
	cmd := exec.Command("sh", "-c", command)
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
	return &action.ExecutionContext{
		ModuleDir: m.Dir,
		TargetDir: targetDir,
		Params:    m.Params,
		Excludes:  m.Config.Spec.Excludes,
		Includes:  m.Config.Spec.Includes,
		DryRun:    opts.DryRun,
		LocalOnly: opts.LocalOnly,
		ShowDiff:  opts.ShowDiff,
		Logger:    m.Logger,
	}
}
