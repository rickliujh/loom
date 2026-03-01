package action

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"

	"github.com/rickliujh/loom/internal/util"
	"github.com/rickliujh/loom/pkg/config"
)

// PatchAction applies a kustomize patch to a target file.
type PatchAction struct {
	Config config.Patch
}

func (a *PatchAction) Execute(ctx context.Context, execCtx *ExecutionContext) error {
	if a.Config.Engine != "kustomize" {
		return actionError("patch", fmt.Errorf("unsupported patch engine %q, only \"kustomize\" is supported", a.Config.Engine))
	}

	patchPath := filepath.Join(util.ExpandPath(execCtx.ModuleDir, a.Config.Path))
	targetPath := filepath.Join(execCtx.TargetDir, a.Config.Target)

	execCtx.Logger.Info("applying kustomize patch", "patch", patchPath, "target", targetPath)
	if execCtx.DryRun {
		execCtx.Logger.Info("dry-run: would apply kustomize patch", "patch", patchPath, "target", targetPath)
		return nil
	}

	// Shell out to kustomize build on the target directory.
	cmd := exec.CommandContext(ctx, "kustomize", "build", filepath.Dir(targetPath))
	output, err := cmd.CombinedOutput()
	if err != nil {
		return actionError("patch", fmt.Errorf("kustomize failed: %w\noutput: %s", err, output))
	}

	if err := util.WriteFile(targetPath, output, 0o644); err != nil {
		return actionError("patch", err)
	}

	return nil
}
