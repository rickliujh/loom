package action

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"
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

// diffEntry is one captured file diff plus the context needed to read it once
// diffs are printed together at the end: which module produced it and which
// target (repo) it applies to.
type diffEntry struct {
	module string
	target string
	text   string // uncolored unified diff
}

// DiffCollector accumulates file diffs across a whole run, shared by parent
// and child module executions. Execution is sequential, so no locking is
// needed. Diffs are held rather than written inline as each file is merged,
// so the whole set can be printed once at the very end of the run — after the
// per-operation logs. Each diff carries a module/target header so it stays
// legible out of the surrounding log context.
type DiffCollector struct {
	entries []diffEntry
}

// Add records one file diff along with the module and target it belongs to.
// Safe to call on a nil collector.
func (c *DiffCollector) Add(module, target, diff string) {
	if c == nil {
		return
	}
	c.entries = append(c.entries, diffEntry{module: module, target: target, text: diff})
}

// Print writes all collected diffs to w, colorized when w is a terminal.
// A module/target header is written before each diff (deduplicated across a
// run of diffs from the same module and target). Nothing is written when the
// collector is nil or empty.
func (c *DiffCollector) Print(w io.Writer) {
	if c == nil || len(c.entries) == 0 {
		return
	}
	color := isTerminalWriter(w)
	var lastModule, lastTarget string
	for i, e := range c.entries {
		if i == 0 || e.module != lastModule || e.target != lastTarget {
			fmt.Fprint(w, diffHeader(e.module, e.target, color))
			lastModule, lastTarget = e.module, e.target
		}
		fmt.Fprint(w, colorizeDiff(e.text, color))
	}
}

// diffHeader renders the "which module / which repo" banner shown above a diff.
// Returns just a leading blank line when there is no context to show.
func diffHeader(module, target string, color bool) string {
	if module == "" && target == "" {
		return "\n"
	}
	var b strings.Builder
	b.WriteByte('\n')
	switch {
	case !color:
		if module != "" {
			b.WriteString("[" + module + "]")
		}
		if target != "" {
			if module != "" {
				b.WriteByte(' ')
			}
			b.WriteString(target)
		}
	default:
		if module != "" {
			b.WriteString(diffColorInvert + " " + module + " " + diffColorReset)
		}
		if target != "" {
			if module != "" {
				b.WriteByte(' ')
			}
			b.WriteString(diffColorMuted + target + diffColorReset)
		}
	}
	b.WriteByte('\n')
	return b.String()
}

// ExecutionContext holds runtime state shared across actions.
type ExecutionContext struct {
	// ModuleName is the metadata.name of the executing module.
	ModuleName string
	// ModuleDir is the path to the module directory containing loom.yaml.
	ModuleDir string
	// TargetDir is the path to the target repository working directory.
	TargetDir string
	// TargetLabel is a human-readable identity for the target (repo URL and
	// branch, or the target dir), shown as a header above collected diffs so
	// they stay legible out of the surrounding log context. May be empty.
	TargetLabel string
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
	// Diffs collects file diffs across the run for printing at the very end,
	// shared by parent and child executions. When nil, diff output is suppressed.
	Diffs *DiffCollector
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
