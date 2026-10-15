package module

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"github.com/rickliujh/loom/internal/proc"
	tmpl "github.com/rickliujh/loom/pkg/template"
)

// evalCondition decides whether a step (operation or child module) guarded by
// an "if" predicate should run.
//
// An empty predicate always runs. Otherwise raw is rendered with params (so it
// supports the same Go templating as every other field), then executed via
// sh -c in workDir. Shell exit-code semantics apply: exit 0 runs the step, any
// non-zero exit skips it. A non-zero exit is a decision, not a failure — only a
// template render error or an inability to launch the shell is returned as err.
//
// The predicate is always evaluated, including under --dry-run and --local-run,
// because it is control flow: skipping it would misrepresent which steps a run
// would actually perform. Like dynamicParams commands, an if predicate is
// expected to be a side-effect-free check (test, grep, file existence).
//
// A predicate stopped by ctx is an error, not a false: a killed shell exits
// non-zero, and reading that as "skip" would quietly carry on with the run.
func evalCondition(ctx context.Context, raw string, params map[string]any, workDir string) (bool, error) {
	if strings.TrimSpace(raw) == "" {
		return true, nil
	}

	rendered, err := tmpl.RenderString(raw, params)
	if err != nil {
		return false, err
	}

	cmd := proc.Command(ctx, "sh", "-c", rendered)
	cmd.Dir = workDir
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return false, fmt.Errorf("condition %q stopped: %w", rendered, context.Cause(ctx))
		}
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			// Non-zero exit: the predicate is false, skip the step.
			return false, nil
		}
		return false, fmt.Errorf("running condition %q: %w", rendered, err)
	}
	return true, nil
}
