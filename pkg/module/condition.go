package module

import (
	"fmt"
	"os/exec"
	"strings"

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
func evalCondition(raw string, params map[string]string, workDir string) (bool, error) {
	if strings.TrimSpace(raw) == "" {
		return true, nil
	}

	rendered, err := tmpl.RenderString(raw, params)
	if err != nil {
		return false, err
	}

	cmd := exec.Command("sh", "-c", rendered)
	cmd.Dir = workDir
	if err := cmd.Run(); err != nil {
		if _, ok := err.(*exec.ExitError); ok {
			// Non-zero exit: the predicate is false, skip the step.
			return false, nil
		}
		return false, fmt.Errorf("running condition %q: %w", rendered, err)
	}
	return true, nil
}
