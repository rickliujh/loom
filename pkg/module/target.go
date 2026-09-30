package module

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/rickliujh/loom/pkg/git"
	tmpl "github.com/rickliujh/loom/pkg/template"
)

// ResolveTarget prepares the directory a module runs against. A module with a
// target spec gets a fresh clone of its target repo, on its feature branch when
// it names one: in local-run mode (LocalRun with a TargetPath) into the next
// numbered subdirectory of TargetPath, kept afterwards and recorded in
// opts.DirLabels under breadcrumb; otherwise into a temporary directory that
// cleanup removes. A module without a target spec runs in fallbackDir, and
// cleanup is nil.
//
// It is the single place a target spec is rendered and cloned, for the run's
// root and for every child alike. logger receives the clone and branch lines:
// a child passes its own instance logger, the root a plain one, so the root's
// lines carry no module chip.
func ResolveTarget(ctx context.Context, mod *Module, fallbackDir string, opts *RunOptions, breadcrumb []string, logger *slog.Logger) (dir string, cleanup func(), err error) {
	target := mod.Config.Spec.Target
	if target == nil {
		return fallbackDir, nil, nil
	}

	// Determine clone destination.
	var cloneDir string
	if opts.LocalRun && opts.TargetPath != "" {
		// In --local-run mode, clone into a numbered subdirectory — no cleanup.
		cloneDir = opts.NextLocalDir(mod.Config.Metadata.Name)
		if err := os.MkdirAll(cloneDir, 0o755); err != nil {
			return "", nil, fmt.Errorf("creating local target dir: %w", err)
		}
		opts.registerDirLabel(cloneDir, breadcrumb)
	} else {
		tmpDir, err := os.MkdirTemp("", "loom-target-*")
		if err != nil {
			return "", nil, fmt.Errorf("creating temp dir: %w", err)
		}
		cloneDir = tmpDir
		cleanup = func() { os.RemoveAll(tmpDir) }
	}
	// Past this point every failure removes the temp clone before returning.
	fail := func(err error) (string, func(), error) {
		if cleanup != nil {
			cleanup()
		}
		return "", nil, err
	}

	targetURL, err := tmpl.RenderString(target.URL, mod.Params)
	if err != nil {
		return fail(fmt.Errorf("rendering target URL: %w", err))
	}
	targetBranch, err := tmpl.RenderString(target.Branch, mod.Params)
	if err != nil {
		return fail(fmt.Errorf("rendering target branch: %w", err))
	}

	repo, err := git.Clone(ctx, targetURL, cloneDir, targetBranch, logger)
	if err != nil {
		return fail(err)
	}

	if target.FeatureBranch != "" {
		branchName, err := tmpl.RenderString(target.FeatureBranch, mod.Params)
		if err != nil {
			return fail(fmt.Errorf("rendering featureBranch: %w", err))
		}
		logger.Info("creating feature branch", "branch", branchName)
		if err := repo.CreateBranchContext(ctx, branchName); err != nil {
			return fail(fmt.Errorf("creating feature branch %q: %w", branchName, err))
		}
	}

	return cloneDir, cleanup, nil
}
