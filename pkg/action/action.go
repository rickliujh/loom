package action

import (
	"context"
	"fmt"
	"io"
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
	// Excludes are glob patterns for files/dirs to exclude from template walking.
	Excludes []string
	// Includes are glob patterns that override excludes (including implicit ones).
	Includes []string
	// DryRun indicates whether to simulate operations.
	DryRun bool
	// LocalRun runs all operations locally but skips remote push and PR creation.
	LocalRun bool
	// ShowDiff displays file diffs during dry-run.
	ShowDiff bool
	// DiffWriter is the destination for diff output (bypasses the logger).
	// When nil, diff output is suppressed.
	DiffWriter io.Writer
	// GitAuthor is the default git author name for commitPush when not set in loom.yaml.
	GitAuthor string
	// GitEmail is the default git author email for commitPush when not set in loom.yaml.
	GitEmail string
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
