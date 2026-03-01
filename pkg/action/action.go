package action

import (
	"context"
	"fmt"
	"log/slog"
)

// ExecutionContext holds runtime state shared across actions.
type ExecutionContext struct {
	// ModuleDir is the path to the module directory containing loom.yaml.
	ModuleDir string
	// TargetDir is the path to the target repository working directory.
	TargetDir string
	// Params are the resolved template parameters.
	Params map[string]string
	// DryRun indicates whether to simulate operations.
	DryRun bool
	// Logger is the structured logger.
	Logger *slog.Logger
}

// Action is the interface that all operation types implement.
type Action interface {
	Execute(ctx context.Context, execCtx *ExecutionContext) error
}

// ActionFactory creates an Action from raw operation config.
type ActionFactory func() Action

// actionError wraps an error with operation context.
func actionError(opName string, err error) error {
	return fmt.Errorf("operation %q: %w", opName, err)
}
