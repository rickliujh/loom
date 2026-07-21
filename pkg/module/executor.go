package module

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	prettylog "github.com/rickliujh/loom/internal/log"
	"github.com/rickliujh/loom/pkg/action"
	"github.com/rickliujh/loom/pkg/git"
	tmpl "github.com/rickliujh/loom/pkg/template"
)

// RunOptions holds runtime flags for module execution.
type RunOptions struct {
	DryRun    bool
	LocalRun bool
	ShowDiff  bool
	// DiffWriter is the destination for diff output. Defaults to os.Stdout.
	DiffWriter io.Writer
	// TargetPath is the base directory for --local-run mode.
	// Each module with a target spec clones into a numbered subdirectory.
	TargetPath string
	// GitAuthor is the default git author name for commitPush operations
	// when not specified in loom.yaml.
	GitAuthor string
	// GitEmail is the default git author email for commitPush operations
	// when not specified in loom.yaml.
	GitEmail string
	// Summary collects created PRs/MRs across the run, shared by parent
	// and child executions. May be nil.
	Summary *action.RunSummary
	// localSeq tracks the execution order for numbered subdirectories.
	localSeq *int
}

// NextLocalDir returns the next numbered subdirectory under TargetPath
// for a module with the given name, e.g. "00-parent-module".
func (o *RunOptions) NextLocalDir(name string) string {
	if o.localSeq == nil {
		seq := 0
		o.localSeq = &seq
	}
	dir := fmt.Sprintf("%02d-%s", *o.localSeq, name)
	*o.localSeq++
	return filepath.Join(o.TargetPath, dir)
}

// Execute runs all operations in a module sequentially.
func Execute(ctx context.Context, mod *Module, targetDir string, opts RunOptions) error {
	execCtx := mod.NewExecutionContext(targetDir, opts)

	// Execute child modules first.
	for _, childRef := range mod.Config.Spec.Modules {
		childName, err := tmpl.RenderString(childRef.Name, mod.Params)
		if err != nil {
			return fmt.Errorf("rendering name for child module %q: %w", childRef.Name, err)
		}

		renderedSource, err := tmpl.RenderString(childRef.Source, mod.Params)
		if err != nil {
			return fmt.Errorf("rendering source for child %q: %w", childName, err)
		}

		childDir, sourceCleanup, err := ResolveSource(renderedSource, mod.Dir, mod.Logger)
		if err != nil {
			return fmt.Errorf("resolving child module %q: %w", childName, err)
		}
		if sourceCleanup != nil {
			defer sourceCleanup()
		}

		// Render child params through parent's template context.
		childParams := make(map[string]string)
		for k, v := range childRef.Params {
			rendered, err := tmpl.RenderString(v, mod.Params)
			if err != nil {
				return fmt.Errorf("rendering param %q for child %q: %w", k, childName, err)
			}
			childParams[k] = rendered
		}

		childMod, err := Load(childDir, childParams, mod.Logger)
		if err != nil {
			return fmt.Errorf("loading child module %q: %w", childName, err)
		}

		childTargetDir, cleanup, err := resolveChildTarget(ctx, childMod, targetDir, &opts)
		if err != nil {
			return fmt.Errorf("resolving target for child module %q: %w", childName, err)
		}
		if cleanup != nil {
			defer cleanup()
		}

		if err := Execute(ctx, childMod, childTargetDir, opts); err != nil {
			return fmt.Errorf("executing child module %q: %w", childName, err)
		}
	}

	// Execute operations.
	ops := mod.Config.Spec.Operations
	for i, op := range ops {
		mod.Logger.Info(fmt.Sprintf("operation %s (%d/%d)", op.Name, i+1, len(ops)), prettylog.KeySection, true)

		act, err := action.FromOperation(op)
		if err != nil {
			return err
		}

		if err := act.Execute(ctx, execCtx); err != nil {
			return fmt.Errorf("operation %q failed: %w", op.Name, err)
		}
	}

	return nil
}

// resolveChildTarget resolves the target directory for a child module.
// If the child module has its own target spec, it clones the target repo.
// In --local-run mode, it clones into a numbered subdirectory of TargetPath.
// Otherwise, it clones into a temp directory with a cleanup function.
// If the child has no target spec, it falls back to the parent's targetDir.
func resolveChildTarget(ctx context.Context, childMod *Module, parentTargetDir string, opts *RunOptions) (string, func(), error) {
	target := childMod.Config.Spec.Target
	if target == nil {
		return parentTargetDir, nil, nil
	}

	// Determine clone destination.
	var cloneDir string
	var cleanup func()
	if opts.LocalRun && opts.TargetPath != "" {
		// In --local-run mode, clone into a numbered subdirectory — no cleanup.
		cloneDir = opts.NextLocalDir(childMod.Config.Metadata.Name)
		if err := os.MkdirAll(cloneDir, 0o755); err != nil {
			return "", nil, fmt.Errorf("creating local target dir: %w", err)
		}
	} else {
		tmpDir, err := os.MkdirTemp("", "loom-target-*")
		if err != nil {
			return "", nil, fmt.Errorf("creating temp dir: %w", err)
		}
		cloneDir = tmpDir
		cleanup = func() { os.RemoveAll(tmpDir) }
	}

	targetURL, err := tmpl.RenderString(target.URL, childMod.Params)
	if err != nil {
		if cleanup != nil {
			cleanup()
		}
		return "", nil, fmt.Errorf("rendering target URL: %w", err)
	}
	targetBranch, err := tmpl.RenderString(target.Branch, childMod.Params)
	if err != nil {
		if cleanup != nil {
			cleanup()
		}
		return "", nil, fmt.Errorf("rendering target branch: %w", err)
	}

	repo, err := git.Clone(ctx, targetURL, cloneDir, targetBranch, childMod.Logger)
	if err != nil {
		if cleanup != nil {
			cleanup()
		}
		return "", nil, err
	}

	if target.FeatureBranch != "" {
		branchName, err := tmpl.RenderString(target.FeatureBranch, childMod.Params)
		if err != nil {
			if cleanup != nil {
				cleanup()
			}
			return "", nil, fmt.Errorf("rendering featureBranch: %w", err)
		}
		childMod.Logger.Info("creating feature branch", "branch", branchName)
		if err := repo.CreateBranch(branchName); err != nil {
			if cleanup != nil {
				cleanup()
			}
			return "", nil, fmt.Errorf("creating feature branch %q: %w", branchName, err)
		}
	}

	return cloneDir, cleanup, nil
}
