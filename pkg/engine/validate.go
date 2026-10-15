package engine

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"

	"github.com/rickliujh/loom/pkg/config"
	"github.com/rickliujh/loom/pkg/module"
)

// Finding is one warning or error about one module config.
type Finding struct {
	// Module labels the config: "" for the module validated, "parent › child"
	// for one reached through spec.modules.
	Module  string `json:"module"`
	Message string `json:"message"`
}

// String is the finding as `loom validate` prints it.
func (f Finding) String() string {
	if f.Module == "" {
		return f.Message
	}
	return f.Module + ": " + f.Message
}

// ValidateRequest describes one `loom validate`.
type ValidateRequest struct {
	// Dir is the module's local directory.
	Dir string
	// Recursive also validates the modules spec.modules references, and
	// theirs in turn. Otherwise only Dir's own config is checked: a
	// referenced module is a separate config that a run resolves on its own.
	Recursive bool
	// Logger receives the logs of cloning a git-sourced child. Required when
	// Recursive is set.
	Logger *slog.Logger
	// OnWarning, if set, is called with each warning as the walk finds it,
	// before it moves on — so a caller can report warnings in step with the
	// walk's logs rather than after them.
	OnWarning func(Finding)
}

// ValidateResult is what validation found.
type ValidateResult struct {
	// Count is the number of module configs checked, valid or not.
	Count int
	// Warnings are in the order they were found. They are reported for an
	// invalid config too: both are findings about the same config, and
	// reporting them together keeps one pass enough to fix everything.
	Warnings []Finding
	// Errors holds one entry per module that failed: an invalid config, one
	// that cannot be read, or a child source that cannot be resolved.
	Errors []Finding
}

// Valid reports that no config failed.
func (r *ValidateResult) Valid() bool { return len(r.Errors) == 0 }

// Validate checks a module config and, when Recursive, the tree it composes.
// The result is never nil. The error is non-nil exactly when a config failed,
// and reads as `loom validate` reports it: a failure at Dir itself alone
// (validation stops there), otherwise every child failure joined, each named
// by its module label.
func Validate(ctx context.Context, req ValidateRequest) (*ValidateResult, error) {
	v := &validator{ctx: ctx, req: req, res: &ValidateResult{}, visited: map[string]bool{}}
	var err error
	if req.Recursive {
		err = v.tree(req.Dir, "")
	} else {
		err = v.one(req.Dir, "")
	}
	return v.res, err
}

type validator struct {
	ctx context.Context
	req ValidateRequest
	res *ValidateResult
	// visited is keyed by resolved directory, so a module reached twice — a
	// shared library, or a cycle — is checked once.
	visited map[string]bool
}

func (v *validator) warn(f Finding) {
	v.res.Warnings = append(v.res.Warnings, f)
	if v.req.OnWarning != nil {
		v.req.OnWarning(f)
	}
}

func (v *validator) fail(label string, err error) {
	v.res.Errors = append(v.res.Errors, Finding{Module: label, Message: err.Error()})
}

// one loads and validates a single module directory. label names the module
// when it was reached from a parent, and is empty for the one validated.
func (v *validator) one(moduleDir, label string) error {
	v.res.Count++
	lf, err := config.Load(moduleDir)
	if err != nil {
		v.fail(label, err)
		return prefixed(label, err)
	}

	warnings, err := config.ValidateInDirWithWarnings(lf, moduleDir)
	for _, w := range warnings {
		v.warn(Finding{Module: label, Message: w})
	}
	if err != nil {
		v.fail(label, err)
	}
	return prefixed(label, err)
}

// tree validates moduleDir and every module it references. A templated source
// is skipped with a warning rather than failing — it has no value until a run
// resolves its params, so there is nothing to fetch yet.
func (v *validator) tree(moduleDir, label string) error {
	abs, err := filepath.Abs(moduleDir)
	if err != nil {
		abs = moduleDir
	}
	if v.visited[abs] {
		return nil
	}
	v.visited[abs] = true

	if err := v.one(moduleDir, label); err != nil {
		return err
	}

	lf, err := config.Load(moduleDir)
	if err != nil {
		v.fail(label, err)
		return prefixed(label, err)
	}

	var errs []error
	for _, ref := range lf.Spec.Modules {
		childLabel := ref.Name
		if label != "" {
			childLabel = label + " › " + ref.Name
		}
		if strings.Contains(ref.Source, "{{") {
			v.warn(Finding{Module: childLabel, Message: fmt.Sprintf(
				"source %q is templated, not checked — its value is only known at run time", ref.Source)})
			continue
		}

		childDir, cleanup, err := module.ResolveSourceContext(v.ctx, ref.Source, moduleDir, v.req.Logger)
		if err != nil {
			v.fail(childLabel, err)
			errs = append(errs, fmt.Errorf("%s: %w", childLabel, err))
			continue
		}
		err = v.tree(childDir, childLabel)
		if cleanup != nil {
			cleanup()
		}
		if err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// prefixed names the module a nested failure came from, so a violation deep in
// a tree says which config to open.
func prefixed(label string, err error) error {
	if err == nil || label == "" {
		return err
	}
	return fmt.Errorf("%s: %w", label, err)
}
