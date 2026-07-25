package action

import (
	"fmt"
	"strings"

	"github.com/rickliujh/loom/pkg/config"
)

// opInfo is everything callers derive from an operation's shape: the Action a
// run executes, plus the kind and one-line detail `loom inspect` prints. Both
// come out of the single switch in classify, so a new action type is wired up
// in exactly one place and inspect can never drift out of step with run.
type opInfo struct {
	action Action
	kind   string
	detail string
}

// FromOperation returns the appropriate Action for an Operation.
func FromOperation(op config.Operation) (Action, error) {
	info, err := classify(op)
	if err != nil {
		return nil, err
	}
	return info.action, nil
}

// DescribeOperation reports an operation's action kind (e.g. "shell") and a
// short summary of what it does (e.g. the command). Templates are left as
// authored — describing a module must not require resolved params. It rejects
// the same malformed operations FromOperation does, so inspecting a module
// surfaces an unrecognized action rather than silently skipping it.
func DescribeOperation(op config.Operation) (kind, detail string, err error) {
	info, err := classify(op)
	if err != nil {
		return "", "", err
	}
	return info.kind, info.detail, nil
}

func classify(op config.Operation) (opInfo, error) {
	switch {
	case op.NewFiles != nil:
		c := *op.NewFiles
		return opInfo{
			action: &NewFilesAction{Config: c},
			kind:   "newFiles",
			detail: arrow(orDot(c.Source), orDot(c.Dest)),
		}, nil
	case op.Shell != nil:
		c := *op.Shell
		return opInfo{
			action: &ShellAction{Config: c},
			kind:   "shell",
			detail: c.Command,
		}, nil
	case op.CommitPush != nil:
		c := *op.CommitPush
		return opInfo{
			action: &CommitPushAction{Config: c},
			kind:   "commitPush",
			detail: c.Message,
		}, nil
	case op.PR != nil:
		c := *op.PR
		return opInfo{
			action: &PRAction{Config: c},
			kind:   "pr",
			detail: strings.TrimPrefix(strings.TrimSpace(c.Provider+": "+c.Title), ": "),
		}, nil
	case op.Patch != nil:
		c := *op.Patch
		engine := c.Engine
		if engine == "" {
			engine = "smp"
		}
		return opInfo{
			action: &PatchAction{Config: c},
			kind:   "patch",
			detail: engine + " " + arrow(c.Path, c.Target),
		}, nil
	case op.LLM != nil:
		c := *op.LLM
		mode := c.Mode
		if mode == "" {
			mode = "generate"
		}
		return opInfo{
			action: &LLMAction{Config: c},
			kind:   "llm",
			detail: strings.TrimSpace(c.Provider + "/" + c.Model + " " + mode + " " + arrow("", c.Target)),
		}, nil
	default:
		return opInfo{}, fmt.Errorf("operation %q has no recognized action type", op.Name)
	}
}

// arrow joins a source and destination as "src → dst", dropping either side
// when it is empty so a one-sided detail reads as "→ dst" rather than " → dst".
func arrow(from, to string) string {
	switch {
	case from == "" && to == "":
		return ""
	case from == "":
		return "→ " + to
	case to == "":
		return from
	}
	return from + " → " + to
}

// orDot renders an empty newFiles source/dest as ".", the module or target root
// it actually means, so the detail is not a blank.
func orDot(s string) string {
	if s == "" {
		return "."
	}
	return s
}
