package module

import (
	"context"
	"fmt"
	"os"

	"github.com/rickliujh/loom/pkg/action"
	"github.com/rickliujh/loom/pkg/git"
	tmpl "github.com/rickliujh/loom/pkg/template"
)

// Execute runs all operations in a module sequentially.
func Execute(ctx context.Context, mod *Module, targetDir string, dryRun bool) error {
	execCtx := mod.NewExecutionContext(targetDir, dryRun)

	// Execute child modules first.
	for _, childRef := range mod.Config.Spec.Modules {
		childDir, err := ResolveSource(childRef.Source, mod.Dir)
		if err != nil {
			return fmt.Errorf("resolving child module %q: %w", childRef.Name, err)
		}

		// Render child params through parent's template context.
		childParams := make(map[string]string)
		for k, v := range childRef.Params {
			rendered, err := tmpl.RenderString(v, mod.Params)
			if err != nil {
				return fmt.Errorf("rendering param %q for child %q: %w", k, childRef.Name, err)
			}
			childParams[k] = rendered
		}

		childMod, err := Load(childDir, childParams, mod.Logger)
		if err != nil {
			return fmt.Errorf("loading child module %q: %w", childRef.Name, err)
		}

		childTargetDir, cleanup, err := resolveChildTarget(ctx, childMod, childParams, targetDir)
		if err != nil {
			return fmt.Errorf("resolving target for child module %q: %w", childRef.Name, err)
		}
		if cleanup != nil {
			defer cleanup()
		}

		if err := Execute(ctx, childMod, childTargetDir, dryRun); err != nil {
			return fmt.Errorf("executing child module %q: %w", childRef.Name, err)
		}
	}

	// Execute operations.
	for _, op := range mod.Config.Spec.Operations {
		mod.Logger.Info("executing operation", "name", op.Name)

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
// If the child module has its own target spec, it clones the target repo
// and returns the temp directory along with a cleanup function.
// Otherwise, it falls back to the parent's targetDir.
func resolveChildTarget(ctx context.Context, childMod *Module, childParams map[string]string, parentTargetDir string) (string, func(), error) {
	target := childMod.Config.Spec.Target
	if target == nil {
		return parentTargetDir, nil, nil
	}

	tmpDir, err := os.MkdirTemp("", "loom-target-*")
	if err != nil {
		return "", nil, fmt.Errorf("creating temp dir: %w", err)
	}
	cleanup := func() { os.RemoveAll(tmpDir) }

	repo, err := git.Clone(ctx, target.URL, tmpDir, target.Branch, childMod.Logger)
	if err != nil {
		cleanup()
		return "", nil, err
	}

	if target.FeatureBranch != "" {
		branchName, err := tmpl.RenderString(target.FeatureBranch, childParams)
		if err != nil {
			cleanup()
			return "", nil, fmt.Errorf("rendering featureBranch: %w", err)
		}
		childMod.Logger.Info("creating feature branch", "branch", branchName)
		if err := repo.CreateBranch(branchName); err != nil {
			cleanup()
			return "", nil, fmt.Errorf("creating feature branch %q: %w", branchName, err)
		}
	}

	return tmpDir, cleanup, nil
}
