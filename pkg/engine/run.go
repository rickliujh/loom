package engine

import (
	"context"
	"errors"
	"log/slog"

	"github.com/rickliujh/loom/pkg/action"
	"github.com/rickliujh/loom/pkg/event"
	"github.com/rickliujh/loom/pkg/module"
)

// ErrLocalRunWithoutTargetPath is returned by Run for a local run with no
// TargetPath: local mode keeps its results, so they need somewhere to go.
var ErrLocalRunWithoutTargetPath = errors.New("--local-run requires --target-path: provide a local directory to write results into")

// RunRequest describes one `loom run`.
type RunRequest struct {
	// ModuleDir is the root module's local directory.
	ModuleDir string
	// Params are the values supplied for the root module.
	Params map[string]any
	// DryRun simulates every operation; LocalRun runs them but skips push and
	// PR creation, keeping clones under TargetPath. DryRun wins when both are
	// set.
	DryRun, LocalRun bool
	// TargetPath is where local mode clones targets, into numbered
	// subdirectories. A module without a target spec runs in it directly.
	TargetPath string
	// GitAuthor and GitEmail are the commitPush defaults.
	GitAuthor, GitEmail string
	// Logger receives the run's logs. Required.
	Logger *slog.Logger
	// Events receives the run's progress. Nil discards it.
	Events event.Sink
}

// RunResult is what a run leaves behind.
type RunResult struct {
	// ModuleName is the root module's metadata name; empty when it did not
	// load.
	ModuleName string
	// TargetDir is the directory the root ran against. A temporary clone is
	// already removed when Run returns.
	TargetDir string
	// PRs are the pull/merge requests opened, in order — including those
	// opened before a failure, which are what someone has to track down.
	PRs []action.PRResult
	// DirLabels maps each clone a local run made under TargetPath to the
	// breadcrumb of the module that made it — including those made before a
	// failure. They are the directories whose changes are the run's result,
	// and the breadcrumb tells bulk items that share a module apart. Nil
	// outside local mode.
	DirLabels map[string][]string
}

// Run loads the module, resolves its target and executes it. The result is
// never nil, even with an error.
func Run(ctx context.Context, req RunRequest) (*RunResult, error) {
	res := &RunResult{}
	mod, err := module.LoadContext(ctx, req.ModuleDir, req.Params, req.Logger)
	if err != nil {
		return res, err
	}
	res.ModuleName = mod.Config.Metadata.Name

	// Checked after loading, where `loom run` has always checked it.
	if req.LocalRun && req.TargetPath == "" {
		return res, ErrLocalRunWithoutTargetPath
	}

	summary := &action.RunSummary{}
	opts := module.RunOptions{
		DryRun:     req.DryRun,
		LocalRun:   req.LocalRun,
		TargetPath: req.TargetPath,
		GitAuthor:  req.GitAuthor,
		GitEmail:   req.GitEmail,
		Summary:    summary,
		Events:     req.Events,
	}
	if req.LocalRun {
		opts.DirLabels = map[string][]string{}
		res.DirLabels = opts.DirLabels
	}
	targetDir, cleanup, err := resolveRootTarget(ctx, mod, req.ModuleDir, &opts, req.Logger)
	if err != nil {
		return res, err
	}
	defer cleanup()
	res.TargetDir = targetDir

	err = module.Execute(ctx, mod, targetDir, opts)
	res.PRs = summary.PRs
	return res, err
}
