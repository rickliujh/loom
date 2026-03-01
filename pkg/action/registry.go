package action

import (
	"fmt"

	"github.com/rickliujh/loom/pkg/config"
)

// FromOperation returns the appropriate Action for an Operation.
func FromOperation(op config.Operation) (Action, error) {
	switch {
	case op.NewFiles != nil:
		return &NewFilesAction{Config: *op.NewFiles}, nil
	case op.Shell != nil:
		return &ShellAction{Config: *op.Shell}, nil
	case op.CommitPush != nil:
		return &CommitPushAction{Config: *op.CommitPush}, nil
	case op.PR != nil:
		return &PRAction{Config: *op.PR}, nil
	case op.Patch != nil:
		return &PatchAction{Config: *op.Patch}, nil
	default:
		return nil, fmt.Errorf("operation %q has no recognized action type", op.Name)
	}
}
