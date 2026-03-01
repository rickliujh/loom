package module

import (
	"fmt"
	"log/slog"

	"github.com/rickliujh/loom/pkg/action"
	"github.com/rickliujh/loom/pkg/config"
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

	params, err := resolveParams(cfg.Spec.Params, providedParams)
	if err != nil {
		return nil, fmt.Errorf("resolving params for %s: %w", cfg.Metadata.Name, err)
	}

	return &Module{
		Dir:    dir,
		Config: cfg,
		Params: params,
		Logger: logger.With("module", cfg.Metadata.Name),
	}, nil
}

// resolveParams merges provided params with declared defaults, checking required params.
func resolveParams(declared []config.ParamDef, provided map[string]string) (map[string]string, error) {
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

// NewExecutionContext creates an ExecutionContext for this module.
func (m *Module) NewExecutionContext(targetDir string, dryRun bool) *action.ExecutionContext {
	return &action.ExecutionContext{
		ModuleDir: m.Dir,
		TargetDir: targetDir,
		Params:    m.Params,
		DryRun:    dryRun,
		Logger:    m.Logger,
	}
}
