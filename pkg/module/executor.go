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
	DryRun   bool
	LocalRun bool
	ShowDiff bool
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
	// A module with children is the run's orchestrator: mark its logger so its
	// own lines (the batch headers below, plus any operations of its own) render
	// with the reserved "≡ … ≡" root chip. Marking it structurally — rather than
	// inferring nesting from log order — keeps the orchestrator visible even when
	// it only fans work out and never runs an operation itself. Children derive
	// their loggers from this one and inherit the attr harmlessly; the handler
	// only treats a depth-1 module as root. Must precede NewExecutionContext so
	// the operations' action logs carry the marker too, not just the headers.
	children := mod.Config.Spec.Modules
	if len(children) > 0 {
		mod.Logger = mod.Logger.With(prettylog.KeyRoot, true)
	}

	execCtx := mod.NewExecutionContext(targetDir, opts)

	// Execute child modules first.
	for i, childRef := range children {
		childName, err := tmpl.RenderString(childRef.Name, mod.Params)
		if err != nil {
			return fmt.Errorf("rendering name for child module %q: %w", childRef.Name, err)
		}

		// Label every log line from this invocation with the instance name
		// (childRef.Name), not the child's metadata name. In a bulk run all
		// items share one source — and thus one metadata name — so only the
		// instance name distinguishes item 0's output from item 1's. Deriving
		// from mod.Logger keeps the breadcrumb one level deep instead of
		// stacking the redundant metadata-name label. Threading it through
		// source resolution and Load attributes the child's setup logs (dynamic
		// params, target clone) to the instance too, not to the parent.
		childLogger := mod.Logger.With(prettylog.KeyModule, childName)

		// The reference's optional description renders with the parent's params
		// (M4), same context as name/source/params. Documentary — surfaced at
		// debug level so a `--verbose` run shows why the child is composed in.
		if childRef.Description != "" {
			desc, err := tmpl.RenderString(childRef.Description, mod.Params)
			if err != nil {
				return fmt.Errorf("rendering description for child module %q: %w", childName, err)
			}
			childLogger.Debug("module description", "description", desc)
		}
		// The orchestrator announces each dispatch, so the batch header carries
		// the root chip and names the item it is handing off to; the child's own
		// lines that follow carry the child chip.
		mod.Logger.Info(fmt.Sprintf("%s (%d/%d)", childName, i+1, len(children)), prettylog.KeySection, true)

		renderedSource, err := tmpl.RenderString(childRef.Source, mod.Params)
		if err != nil {
			return fmt.Errorf("rendering source for child %q: %w", childName, err)
		}

		childDir, sourceCleanup, err := ResolveSource(renderedSource, mod.Dir, childLogger)
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

		childMod, err := Load(childDir, childParams, childLogger)
		if err != nil {
			return fmt.Errorf("loading child module %q: %w", childName, err)
		}
		// Load re-labels the logger with the child's metadata name; keep the
		// instance identity for everything downstream.
		childMod.Logger = childLogger

		childTargetDir, cleanup, err := resolveChildTarget(ctx, childMod, targetDir, &opts)
		if err != nil {
			return fmt.Errorf("resolving target for child module %q: %w", childName, err)
		}
		if cleanup != nil {
			defer cleanup()
		}

		// The if predicate is authored in the parent's loom.yaml, so it renders
		// with the parent's params — but it runs against the child's resolved
		// target dir, so it can inspect the repo the child would operate on.
		run, err := evalCondition(childRef.If, mod.Params, childTargetDir)
		if err != nil {
			return fmt.Errorf("evaluating condition for child module %q: %w", childName, err)
		}
		if !run {
			childLogger.Info("skipping module (if condition false)")
			continue
		}

		if err := Execute(ctx, childMod, childTargetDir, opts); err != nil {
			return fmt.Errorf("executing child module %q: %w", childName, err)
		}
	}

	// Execute operations.
	ops := mod.Config.Spec.Operations
	for i, op := range ops {
		mod.Logger.Info(fmt.Sprintf("operation %s (%d/%d)", op.Name, i+1, len(ops)), prettylog.KeySection, true)

		run, err := evalCondition(op.If, mod.Params, targetDir)
		if err != nil {
			return fmt.Errorf("operation %q: evaluating condition: %w", op.Name, err)
		}
		if !run {
			mod.Logger.Info("skipping operation (if condition false)")
			continue
		}

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
