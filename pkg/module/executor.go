package module

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	prettylog "github.com/rickliujh/loom/internal/log"
	"github.com/rickliujh/loom/pkg/action"
	"github.com/rickliujh/loom/pkg/event"
	"github.com/rickliujh/loom/pkg/params"
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
	// Events receives the run's progress as structured events, shared by
	// parent and child executions. Nil (the CLI) discards them.
	Events event.Sink
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

// Execute runs all operations in a module sequentially.
func Execute(ctx context.Context, mod *Module, targetDir string, opts RunOptions) (err error) {
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

	// Copied: the events outlive this call, and opts.ModulePath is extended
	// for each child.
	path := append([]string(nil), opts.ModulePath...)
	name := mod.Config.Metadata.Name
	start := time.Now()
	opts.Events.Emit(event.Event{Type: event.ModuleStart, Path: path, Module: name})
	defer func() {
		opts.Events.Emit(event.Event{Type: event.ModuleEnd, Path: path, Module: name,
			DurationMS: event.Since(start), Error: event.ErrorText(err)})
	}()

	execCtx := mod.NewExecutionContext(targetDir, opts)

	// Execute child modules first.
	for i, childRef := range children {
		// A cancelled run stops at the next boundary rather than dispatching
		// more work; whatever was already running has been stopped by ctx.
		if err := context.Cause(ctx); err != nil {
			return fmt.Errorf("stopped before child module %q: %w", childRef.Name, err)
		}
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

		// Extend the breadcrumb with this child's instance name. ResolveTarget
		// records it against the clone dir (for full-mode diff), and the recursive
		// Execute carries it into the child's own diffs and setup.
		childPath := append(append([]string{}, opts.ModulePath...), childName)

		renderedSource, err := tmpl.RenderString(childRef.Source, mod.Params)
		if err != nil {
			return fmt.Errorf("rendering source for child %q: %w", childName, err)
		}

		childDir, sourceCleanup, err := ResolveSourceContext(ctx, renderedSource, mod.Dir, childLogger)
		if err != nil {
			return fmt.Errorf("resolving child module %q: %w", childName, err)
		}
		if sourceCleanup != nil {
			defer sourceCleanup()
		}

		// Render child params through parent's template context: every
		// string leaf, at any depth, of a string, list or map value.
		childParams := make(map[string]any, len(childRef.Params))
		for k, v := range childRef.Params {
			rendered, err := params.RenderLeaves(v, mod.Params)
			if err != nil {
				return fmt.Errorf("rendering param %q for child %q: %w", k, childName, err)
			}
			childParams[k] = rendered
		}

		childMod, err := LoadContext(ctx, childDir, childParams, childLogger)
		if err != nil {
			return fmt.Errorf("loading child module %q: %w", childName, err)
		}
		// Load re-labels the logger with the child's metadata name; keep the
		// instance identity for everything downstream.
		childMod.Logger = childLogger

		childTargetDir, cleanup, err := ResolveTarget(ctx, childMod, targetDir, &opts, childPath, childMod.Logger)
		if err != nil {
			return fmt.Errorf("resolving target for child module %q: %w", childName, err)
		}
		if cleanup != nil {
			defer cleanup()
		}

		// The if predicate is authored in the parent's loom.yaml, so it renders
		// with the parent's params — but it runs against the child's resolved
		// target dir, so it can inspect the repo the child would operate on.
		run, err := evalCondition(ctx, childRef.If, mod.Params, childTargetDir)
		if err != nil {
			return fmt.Errorf("evaluating condition for child module %q: %w", childName, err)
		}
		if !run {
			childLogger.Info("skipping module (if condition false)")
			opts.Events.Emit(event.Event{Type: event.ModuleSkip, Path: childPath,
				Module: childMod.Config.Metadata.Name, Reason: event.ReasonIfFalse})
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
		if err := context.Cause(ctx); err != nil {
			return fmt.Errorf("stopped before operation %q: %w", op.Name, err)
		}
		mod.Logger.Info(fmt.Sprintf("operation %s (%d/%d)", op.Name, i+1, len(ops)), prettylog.KeySection, true)

		run, err := evalCondition(ctx, op.If, mod.Params, targetDir)
		if err != nil {
			return fmt.Errorf("operation %q: evaluating condition: %w", op.Name, err)
		}

		kind, _, _ := action.DescribeOperation(op)
		opEvent := func(t event.Type) event.Event {
			return event.Event{Type: t, Path: path, Op: op.Name, Kind: kind, Index: i + 1, Total: len(ops)}
		}
		if !run {
			mod.Logger.Info("skipping operation (if condition false)")
			skip := opEvent(event.OpSkip)
			skip.Reason = event.ReasonIfFalse
			opts.Events.Emit(skip)
			continue
		}

		opts.Events.Emit(opEvent(event.OpStart))
		opStart := time.Now()
		act, err := action.FromOperation(op)
		if err == nil {
			err = act.Execute(ctx, execCtx)
		}
		end := opEvent(event.OpEnd)
		end.DurationMS, end.Error = event.Since(opStart), event.ErrorText(err)
		opts.Events.Emit(end)
		if err != nil {
			if act == nil {
				return err
			}
			return fmt.Errorf("operation %q failed: %w", op.Name, err)
		}
	}

	return nil
}
