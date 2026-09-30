// Package engine runs Loom's commands — run, diff, inspect and validate — as
// library calls: a request struct in, a result struct out. It is what the
// `loom` CLI and `loom serve` share.
//
// It never writes to stdout or stderr. Logs go to the request's logger, and
// presentation — status lines, the inspect tree, coloured diffs — stays with
// the caller, so each caller can render the same result its own way.
//
// Requests name a module by its local directory. A module source (a git URL,
// "repo//subdir") is resolved to one first with module.ResolveSourceContext,
// which lets a caller order that step against its own checks as it sees fit.
package engine

import (
	"context"
	"log/slog"

	"github.com/rickliujh/loom/pkg/module"
)

// Loaded is a root module loaded with its params and ready to execute: its
// target directory is resolved, cloned when the module names a target repo.
type Loaded struct {
	Module    *module.Module
	TargetDir string
	// Cleanup removes a temporary target clone. Never nil.
	Cleanup func()
}

// LoadAndResolve loads the module in moduleDir and resolves the directory it
// runs against per opts: a clone of its target repo when it has a target spec,
// otherwise opts.TargetPath when set, otherwise moduleDir itself — so a module
// with only local operations (shell, newFiles) needs no clone.
func LoadAndResolve(ctx context.Context, moduleDir string, params map[string]any, opts *module.RunOptions, logger *slog.Logger) (*Loaded, error) {
	mod, err := module.LoadContext(ctx, moduleDir, params, logger)
	if err != nil {
		return nil, err
	}
	targetDir, cleanup, err := resolveRootTarget(ctx, mod, moduleDir, opts, logger)
	if err != nil {
		return nil, err
	}
	return &Loaded{Module: mod, TargetDir: targetDir, Cleanup: cleanup}, nil
}

// resolveRootTarget is module.ResolveTarget for the run's root. The root has
// no parent to name it, so its breadcrumb is its own name (Execute has not yet
// seeded opts.ModulePath), and it logs through the plain logger: its clone
// lines carry no module chip. The returned cleanup is never nil.
func resolveRootTarget(ctx context.Context, mod *module.Module, moduleDir string, opts *module.RunOptions, logger *slog.Logger) (string, func(), error) {
	fallback := moduleDir
	if opts.TargetPath != "" {
		fallback = opts.TargetPath
	}
	dir, cleanup, err := module.ResolveTarget(ctx, mod, fallback, opts, []string{mod.Config.Metadata.Name}, logger)
	if err != nil {
		return "", nil, err
	}
	if cleanup == nil {
		cleanup = func() {}
	}
	return dir, cleanup, nil
}
