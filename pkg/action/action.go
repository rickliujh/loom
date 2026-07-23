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
// diffs are printed together at the end: the instance breadcrumb of the module
// that produced it and the target (repo) it applies to.
type diffEntry struct {
	breadcrumb []string // root instance name … producing instance name
	target     string
	text       string // uncolored unified diff
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

// Add records one file diff along with the instance breadcrumb and target it
// belongs to. Safe to call on a nil collector.
func (c *DiffCollector) Add(breadcrumb []string, target, diff string) {
	if c == nil {
		return
	}
	c.entries = append(c.entries, diffEntry{
		breadcrumb: append([]string(nil), breadcrumb...),
		target:     target,
		text:       diff,
	})
}

// Print writes all collected diffs to w, colorized when w is a terminal.
//
// A fanned-out run (breadcrumb longer than one segment — a bulk item or a nested
// child) is grouped like the run log announces a dispatch: an inverted root chip
// banner naming the turn is written once per turn, a "▸ parent › child" hand-off
// once per submodule path beneath it, and the target line once per repo. So a
// batch reads as clearly-topped blocks instead of one flat wall, and the root is
// not repeated inside a turn. A single-module run keeps its plain chip + target
// header (deduplicated per target), unchanged. Nothing is written when the
// collector is nil or empty.
func (c *DiffCollector) Print(w io.Writer) {
	if c == nil || len(c.entries) == 0 {
		return
	}
	color := isTerminalWriter(w)
	var lastTurn, lastCrumb, lastKey string
	for i, e := range c.entries {
		segs := nonEmptySegs(e.breadcrumb)
		crumb := strings.Join(segs, "\x00")
		key := crumb + "\x00" + e.target

		if len(segs) > 1 {
			turn := segs[0] + "\x00" + segs[1]
			if i == 0 || turn != lastTurn {
				fmt.Fprint(w, diffTurnBanner(segs[0], segs[1], color))
			}
			if i == 0 || crumb != lastCrumb {
				fmt.Fprint(w, diffHandoffChain(segs, color))
			}
			if i == 0 || key != lastKey {
				fmt.Fprint(w, diffTargetLine(e.target, color))
			}
			lastTurn = turn
		} else {
			if i == 0 || key != lastKey {
				fmt.Fprint(w, DiffHeader(segs, e.target, color))
			}
			lastTurn = ""
		}

		lastCrumb, lastKey = crumb, key
		fmt.Fprint(w, colorizeDiff(e.text, color))
	}
}

// DiffHeader renders the whole "which module / which repo" banner above a diff.
// A single-module run (a one-segment breadcrumb) gets a plain inverted chip with
// the target on its own line beneath — unchanged. A fanned-out run gets the
// run-log dispatch header: a root-chip turn banner, a "▸ parent › child" hand-off
// for each submodule step, then the target. Returns just a leading blank line
// when there is no context to show. DiffCollector.Print composes the same pieces
// selectively so a shared banner is not repeated across a turn's diffs.
func DiffHeader(breadcrumb []string, target string, color bool) string {
	segs := nonEmptySegs(breadcrumb)
	if len(segs) > 1 {
		return diffTurnBanner(segs[0], segs[1], color) +
			diffHandoffChain(segs, color) +
			diffTargetLine(target, color)
	}
	if len(segs) == 0 && target == "" {
		return "\n"
	}

	var b strings.Builder
	b.WriteByte('\n')
	if len(segs) == 1 {
		if color {
			b.WriteString(diffColorInvert + " " + segs[0] + " " + diffColorReset)
		} else {
			b.WriteString("[" + segs[0] + "]")
		}
	}
	if target != "" {
		if len(segs) == 1 {
			b.WriteByte('\n')
		}
		if color {
			b.WriteString(diffColorMuted + target + diffColorReset)
		} else {
			b.WriteString(target)
		}
	}
	b.WriteByte('\n')
	return b.String()
}

// diffTurnBanner heads one bulk turn: a leading blank line, then the root module
// as an inverted "≡ … ≡" chip (mirroring the log's root section header) followed
// by the turn's instance name in bold. This is the anchor the eye lands on, so
// turn boundaries stand out in a long batch.
func diffTurnBanner(root, turn string, color bool) string {
	if color {
		return "\n" +
			diffColorInvert + diffColorRoot + diffColorBold + " ≡ " + root + " ≡ " + diffColorReset +
			" " + diffColorBold + turn + diffColorReset + "\n"
	}
	return "\n[≡ " + root + " ≡] " + turn + "\n"
}

// diffHandoffChain renders the submodule hand-off lines beneath a turn banner:
// one "▸ parent › child" per descent step from the turn down to the leaf, the
// run log's nested-dispatch form — no root chip, so headers inside a turn read
// lighter. Empty when the breadcrumb stops at the turn (no submodule).
func diffHandoffChain(segs []string, color bool) string {
	var b strings.Builder
	for i := 1; i+1 < len(segs); i++ {
		parent, child := segs[i], segs[i+1]
		if color {
			b.WriteString(diffColorWorker + "▸ " + diffColorReset +
				diffColorMuted + parent + " › " + diffColorReset +
				diffColorBold + child + diffColorReset + "\n")
		} else {
			b.WriteString("▸ " + parent + " › " + child + "\n")
		}
	}
	return b.String()
}

// diffTargetLine renders the target (repo URL and branch) on its own muted line
// beneath the header, so a long breadcrumb never crowds it. Empty when there is
// no target to name.
func diffTargetLine(target string, color bool) string {
	if target == "" {
		return ""
	}
	if color {
		return diffColorMuted + target + diffColorReset + "\n"
	}
	return target + "\n"
}

// nonEmptySegs drops empty breadcrumb segments so a defensive blank never
// produces a stray "› " step or an empty chip.
func nonEmptySegs(breadcrumb []string) []string {
	segs := make([]string, 0, len(breadcrumb))
	for _, s := range breadcrumb {
		if s != "" {
			segs = append(segs, s)
		}
	}
	return segs
}

// ExecutionContext holds runtime state shared across actions.
type ExecutionContext struct {
	// ModuleName is the metadata.name of the executing module.
	ModuleName string
	// ModulePath is the instance breadcrumb from the run's root down to this
	// module (root instance name … this instance name). In a bulk run every item
	// shares one ModuleName, so this is what tells their diffs apart. It heads
	// each collected diff; may be empty for a directly-constructed context.
	ModulePath []string
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

// diffBreadcrumb is the instance breadcrumb heading this context's diffs. It
// falls back to the bare module name for contexts built directly (e.g. in
// tests) without a threaded ModulePath.
func (e *ExecutionContext) diffBreadcrumb() []string {
	if len(e.ModulePath) > 0 {
		return e.ModulePath
	}
	if e.ModuleName != "" {
		return []string{e.ModuleName}
	}
	return nil
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
