package module

import (
	"context"
	"fmt"

	"github.com/rickliujh/loom/pkg/action"
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

		if err := Execute(ctx, childMod, targetDir, dryRun); err != nil {
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
