package module

import (
	"context"
	"fmt"
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
	// Diffs collects file diffs across the run, shared by parent and child
	// executions, for printing once at the end. May be nil.
	Diffs *action.DiffCollector
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
	// ModulePath is the instance breadcrumb of the executing module: the chain
	// of instance names from the run's root down to and including this module.
	// It identifies which module — and, in a bulk run, which item — a diff or
	// log line belongs to, where the shared metadata.name cannot. It is extended
	// as each child is dispatched.
	ModulePath []string
	// DirLabels maps a local-run clone directory to the breadcrumb of the module
	// that clones into it. Full-mode `loom diff` reads changes back from those
	// clone dirs rather than the in-memory collector, so this lets it head each
	// diff with the same module/item identity quick mode shows. The map is shared
	// across parent and child executions; nil outside full-mode diff.
	DirLabels map[string][]string
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

// registerDirLabel records the breadcrumb of the module cloning into dir, when a
// DirLabels registry is present. A copy is stored so later reuse of the caller's
// slice cannot mutate a recorded entry.
func (o *RunOptions) registerDirLabel(dir string, breadcrumb []string) {
	if o.DirLabels == nil || len(breadcrumb) == 0 {
		return
	}
	o.DirLabels[dir] = append([]string(nil), breadcrumb...)
}

// RegisterDirLabel records a single-name breadcrumb for a root module's clone
// dir. The diff command clones the root before Execute seeds opts.ModulePath, so
// it labels that dir with just the module's own name.
func (o *RunOptions) RegisterDirLabel(dir, name string) {
	o.registerDirLabel(dir, []string{name})
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

	// Initialize the shared numbered-clone counter once, at the run's root, so
	// its pointer propagates through every child's opts copy and the whole tree
	// numbers into one monotonic sequence. Doing it here (not lazily in
	// NextLocalDir) keeps sibling subtrees from each restarting at 00 once child
	// dispatch clones opts by value.
	if opts.localSeq == nil {
		seq := 0
		opts.localSeq = &seq
	}
	// The module's breadcrumb: the ancestry the parent handed down, or — at the
	// run's root, where no parent named this instance — the module's own name.
	if len(opts.ModulePath) == 0 {
		opts.ModulePath = []string{mod.Config.Metadata.Name}
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
		// The orchestrator announces each dispatch, so the batch header names the
		// item it is handing off to; the handler marks it (root chip at the top
		// level, a "▸ parent › child" hand-off when nested) and the child's own
		// lines that follow carry the child chip.
		mod.Logger.Info(fmt.Sprintf("%s (%d/%d)", childName, i+1, len(children)), prettylog.KeySection, true, prettylog.KeyDispatch, true)

		// Extend the breadcrumb with this child's instance name. resolveChildTarget
		// records it against the clone dir (for full-mode diff), and the recursive
		// Execute carries it into the child's own diffs and setup.
		childPath := append(append([]string{}, opts.ModulePath...), childName)

		renderedSource, err := tmpl.RenderString(childRef.Source, mod.Params)
		if err != nil {
			return fmt.Errorf("rendering source for child %q: %w", childName, err)
		}

		// The version field pins the child to a branch, tag, or commit. Like
		// name/source/params it is authored in the parent and renders with the
		// parent's params (M4/IF2).
		renderedVersion, err := tmpl.RenderString(childRef.Version, mod.Params)
		if err != nil {
			return fmt.Errorf("rendering version for child %q: %w", childName, err)
		}

		childDir, sourceCleanup, err := ResolveSource(renderedSource, renderedVersion, mod.Dir, childLogger)
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

		childTargetDir, cleanup, err := resolveChildTarget(ctx, childMod, targetDir, &opts, childPath)
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

		childOpts := opts
		childOpts.ModulePath = childPath
		if err := Execute(ctx, childMod, childTargetDir, childOpts); err != nil {
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
func resolveChildTarget(ctx context.Context, childMod *Module, parentTargetDir string, opts *RunOptions, breadcrumb []string) (string, func(), error) {
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
		opts.registerDirLabel(cloneDir, breadcrumb)
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
