package action

import (
	"context"
	"fmt"
	"io"
	"log/slog"
)

// PRResult records one pull/merge request created during a run.
type PRResult struct {
	// Module is the name of the module whose pr operation created it.
	Module string
	// Title is the rendered PR title.
	Title string
	// URL is the web URL of the created PR/MR.
	URL string
}

// RunSummary collects outward-facing results across a whole run, shared by
// parent and child module executions. Execution is sequential, so no
// locking is needed.
type RunSummary struct {
	PRs []PRResult
}

// AddPR records a created PR/MR. Safe to call on a nil summary.
func (s *RunSummary) AddPR(module, title, url string) {
	if s == nil {
		return
	}
	s.PRs = append(s.PRs, PRResult{Module: module, Title: title, URL: url})
}

// Print writes the collected PR/MR list to w. Nothing is written when the
// summary is nil or empty.
func (s *RunSummary) Print(w io.Writer) {
	if s == nil || len(s.PRs) == 0 {
		return
	}
	fmt.Fprintf(w, "\nPull/merge requests created (%d):\n", len(s.PRs))
	for _, pr := range s.PRs {
		fmt.Fprintf(w, "  - %s (%s): %s\n", pr.Title, pr.Module, pr.URL)
	}
}

// ExecutionContext holds runtime state shared across actions.
type ExecutionContext struct {
	// ModuleName is the metadata.name of the executing module.
	ModuleName string
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
	// Summary collects created PRs/MRs across the run. May be nil.
	Summary *RunSummary
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
