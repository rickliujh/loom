package engine

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"slices"

	"github.com/rickliujh/loom/pkg/action"
	"github.com/rickliujh/loom/pkg/event"
	"github.com/rickliujh/loom/pkg/module"
)

// Diff modes, as DiffResult.Mode reports them.
const (
	ModeQuick = "quick"
	ModeFull  = "full"
)

// FileDiff is one changed file of a target; quick and full mode share it.
type FileDiff = event.FileDiff

// ErrNoGit is returned by a full-mode Diff when the git CLI is missing.
var ErrNoGit = errors.New("loom diff needs the git CLI to compute diffs; install git or use --quick for a dry-run preview")

// RequireGit reports ErrNoGit when full mode cannot run, so a caller can check
// before doing anything slow.
func RequireGit() error {
	if _, err := exec.LookPath("git"); err != nil {
		return ErrNoGit
	}
	return nil
}

// DiffRequest describes one `loom diff`.
type DiffRequest struct {
	// ModuleDir is the root module's local directory.
	ModuleDir string
	// Params are the values supplied for the root module.
	Params map[string]any
	// Quick simulates the run (dry-run) and diffs newFiles/patch results in
	// memory; otherwise the module runs in local mode and each clone is diffed
	// with git against its base branch.
	Quick bool
	// Workspace is where full mode clones targets, kept afterwards. Empty: a
	// temporary directory, removed before Diff returns. In quick mode it is
	// only the directory a module without a target spec runs against.
	Workspace string
	// GitAuthor and GitEmail are the commitPush defaults; full mode falls
	// back to "loom-diff" / "loom-diff@localhost" so a commit needs no
	// configured identity.
	GitAuthor, GitEmail string
	// CollectOnFailure still collects the diffs of what ran when the run
	// fails. Full mode has to stage a workspace to read it, so a caller that
	// will not show a failed run's diffs leaves it off.
	CollectOnFailure bool
	// Raw records git's own output per target in TargetDiff.Raw — what
	// `loom diff` prints — coloured by git when Color is set; Files parses
	// each target's changes into TargetDiff.Files and emits them as diff.file
	// events. Full mode reads back only what is asked for, and asking for
	// neither reads Files; quick mode always has Files and never Raw.
	Raw, Color, Files bool
	// Logger receives the run's logs. Required.
	Logger *slog.Logger
	// Events receives the run's progress, including a diff.file event per
	// changed file: in quick mode as each is computed, in full mode once the
	// clones are read back. Nil discards it.
	Events event.Sink
}

// DiffResult holds the diffs a module run would produce.
type DiffResult struct {
	// ModuleName is the root module's metadata name; empty when it did not
	// load.
	ModuleName string
	// Mode is ModeQuick or ModeFull.
	Mode string
	// Targets are the changed targets: in full mode one per changed clone,
	// in directory order; in quick mode one per run of consecutive diffs
	// sharing a module and target, in the order they were made.
	Targets []TargetDiff
	// Entries are quick mode's diffs as collected, for
	// action.PrintDiffEntries. Nil in full mode.
	Entries []action.DiffEntry
	// Incomplete reports that the run failed: the diffs, if any were
	// collected, cover only what ran before the error Diff returned.
	Incomplete bool
}

// TargetDiff is the change to one target.
type TargetDiff struct {
	// Path is the breadcrumb of the module that produced the change.
	Path []string `json:"path"`
	// Dir is the target directory: the clone in full mode.
	Dir string `json:"-"`
	// Repo and Branch name the target repository; Branch is the base the
	// diff is against. Either may be empty (no remote, or no target spec).
	Repo   string `json:"repo"`
	Branch string `json:"branch"`
	// Base is the git ref full mode diffed against; empty in quick mode.
	Base string `json:"base,omitempty"`
	// Label is the target line `loom diff` heads the change with: repo and
	// branch as "repo (branch)", or the target directory.
	Label string `json:"label"`
	// Raw is git's output for the target when DiffRequest.Raw is set.
	Raw   string     `json:"-"`
	Files []FileDiff `json:"files"`
}

// Diff runs the module in quick or full mode and collects the resulting diffs.
//
// The result is never nil. When the run itself fails, Incomplete is set and
// the error is the run's; the diffs made before it are collected only with
// CollectOnFailure. Any other error — loading, cloning, reading the diffs
// back — leaves Incomplete unset, with whatever targets were read before it.
func Diff(ctx context.Context, req DiffRequest) (*DiffResult, error) {
	if req.Quick {
		return diffQuick(ctx, req)
	}
	return diffFull(ctx, req)
}

func diffQuick(ctx context.Context, req DiffRequest) (*DiffResult, error) {
	res := &DiffResult{Mode: ModeQuick}
	diffs := &action.DiffCollector{}
	opts := module.RunOptions{
		DryRun:     true,
		ShowDiff:   true,
		TargetPath: req.Workspace,
		Diffs:      diffs,
		Events:     req.Events,
	}

	loaded, err := LoadAndResolve(ctx, req.ModuleDir, req.Params, &opts, req.Logger)
	if err != nil {
		return res, err
	}
	defer loaded.Cleanup()
	res.ModuleName = loaded.Module.Config.Metadata.Name

	execErr := module.Execute(ctx, loaded.Module, loaded.TargetDir, opts)
	if execErr != nil {
		res.Incomplete = true
		if !req.CollectOnFailure {
			return res, execErr
		}
	}
	res.Entries = diffs.Entries()
	res.Targets = groupEntries(res.Entries)
	return res, execErr
}

// groupEntries folds consecutive entries of one module and target into one
// TargetDiff — the grouping `loom diff --quick` prints under one header.
func groupEntries(entries []action.DiffEntry) []TargetDiff {
	var out []TargetDiff
	for _, e := range entries {
		if n := len(out); n > 0 && out[n-1].Label == e.Target && slices.Equal(out[n-1].Path, e.Breadcrumb) {
			out[n-1].Files = append(out[n-1].Files, e.File)
			continue
		}
		out = append(out, TargetDiff{
			Path:   e.Breadcrumb,
			Dir:    e.Dir,
			Repo:   e.Repo,
			Branch: e.Branch,
			Label:  e.Target,
			Files:  []FileDiff{e.File},
		})
	}
	return out
}

func diffFull(ctx context.Context, req DiffRequest) (*DiffResult, error) {
	res := &DiffResult{Mode: ModeFull}
	if err := RequireGit(); err != nil {
		return res, err
	}

	// Workspace: a caller-supplied one is kept for inspection; otherwise a
	// temp dir that is removed once the diffs are read.
	workspace := req.Workspace
	if workspace == "" {
		tmp, err := os.MkdirTemp("", "loom-diff-*")
		if err != nil {
			return res, fmt.Errorf("creating diff workspace: %w", err)
		}
		workspace = tmp
		defer os.RemoveAll(tmp)
	}

	// Local mode gives us exactly what a diff wants: pure shell runs,
	// remote-only commands and PRs are skipped, and commits stay local.
	// Default a git identity so a module's commitPush can commit without a
	// configured user.
	author := req.GitAuthor
	if author == "" {
		author = "loom-diff"
	}
	email := req.GitEmail
	if email == "" {
		email = "loom-diff@localhost"
	}
	opts := module.RunOptions{
		LocalRun:   true,
		TargetPath: workspace,
		GitAuthor:  author,
		GitEmail:   email,
		// Full mode reads changes back from the numbered clone dirs, so it
		// needs the executor to record which module (and bulk item) each dir
		// belongs to.
		DirLabels: map[string][]string{},
		Events:    req.Events,
	}

	loaded, err := LoadAndResolve(ctx, req.ModuleDir, req.Params, &opts, req.Logger)
	if err != nil {
		return res, err
	}
	defer loaded.Cleanup()
	res.ModuleName = loaded.Module.Config.Metadata.Name

	collect := CollectOptions{Labels: opts.DirLabels, Raw: req.Raw, Color: req.Color, Files: req.Files}
	execErr := module.Execute(ctx, loaded.Module, loaded.TargetDir, opts)
	if execErr != nil {
		res.Incomplete = true
		if req.CollectOnFailure {
			// The run's error is the one that matters; a failure reading
			// back what it left is secondary, and the targets read before it
			// are still worth showing.
			res.Targets, _ = CollectTargetDiffs(ctx, workspace, collect)
			emitTargets(req.Events, res.Targets)
		}
		return res, execErr
	}

	res.Targets, err = CollectTargetDiffs(ctx, workspace, collect)
	emitTargets(req.Events, res.Targets)
	return res, err
}

// emitTargets reports full mode's files as diff.file events, the same events
// quick mode emits as it goes.
func emitTargets(sink event.Sink, targets []TargetDiff) {
	for _, t := range targets {
		for i := range t.Files {
			sink.Emit(event.Event{Type: event.DiffFile, Path: t.Path, Target: t.Label, Diff: &t.Files[i]})
		}
	}
}
