package action

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/rickliujh/loom/internal/util"
	"github.com/rickliujh/loom/pkg/config"
	tmpl "github.com/rickliujh/loom/pkg/template"
	"sigs.k8s.io/kustomize/api/filters/patchjson6902"
	kyaml "sigs.k8s.io/kustomize/kyaml/yaml"
	"sigs.k8s.io/kustomize/kyaml/yaml/merge2"
)

// PatchAction applies patch operations to a target YAML file using the
// kustomize library. Two engines are supported:
//   - "smp" (default): Strategic Merge Patch — a partial YAML document
//     deep-merged into the target.
//   - "json6902": RFC 6902 JSON Patch — an explicit list of
//     add/remove/replace/move/copy/test operations.
type PatchAction struct {
	Config config.Patch
}

func (a *PatchAction) Execute(ctx context.Context, execCtx *ExecutionContext) error {
	engine := a.Config.Engine
	if engine == "" {
		engine = "smp"
	}

	patchPath := util.ExpandPath(execCtx.ModuleDir, a.Config.Path)
	targetPath := filepath.Join(execCtx.TargetDir, a.Config.Target)

	execCtx.Logger.Info("applying patch", "engine", engine, "patch", patchPath, "target", targetPath)
	if execCtx.DryRun {
		execCtx.Logger.Info("dry-run: would apply patch", "engine", engine, "patch", patchPath, "target", targetPath)
		return nil
	}

	// Read and template-render the patch file.
	patchRaw, err := os.ReadFile(patchPath)
	if err != nil {
		return actionError("patch", fmt.Errorf("reading patch file %q: %w", patchPath, err))
	}

	rendered, err := tmpl.RenderFile(patchRaw, execCtx.Params)
	if err != nil {
		return actionError("patch", fmt.Errorf("rendering patch file %q: %w", patchPath, err))
	}

	switch engine {
	case "smp":
		return a.applySMP(string(rendered), targetPath)
	case "json6902":
		return a.applyJSON6902(string(rendered), targetPath)
	default:
		return actionError("patch", fmt.Errorf("unknown patch engine %q (supported: smp, json6902)", engine))
	}
}

// applySMP applies a Strategic Merge Patch using kustomize's merge2.
func (a *PatchAction) applySMP(patchContent, targetPath string) error {
	targetRaw, err := os.ReadFile(targetPath)
	if err != nil {
		return actionError("patch", fmt.Errorf("reading target file %q: %w", targetPath, err))
	}

	result, err := merge2.MergeStrings(patchContent, string(targetRaw), false, kyaml.MergeOptions{
		ListIncreaseDirection: kyaml.MergeOptionsListAppend,
	})
	if err != nil {
		return actionError("patch", fmt.Errorf("strategic merge patch failed: %w", err))
	}

	if err := os.WriteFile(targetPath, []byte(result), 0o644); err != nil {
		return actionError("patch", fmt.Errorf("writing patched file %q: %w", targetPath, err))
	}
	return nil
}

// applyJSON6902 applies an RFC 6902 JSON Patch using kustomize's patchjson6902 filter.
func (a *PatchAction) applyJSON6902(patchContent, targetPath string) error {
	targetRaw, err := os.ReadFile(targetPath)
	if err != nil {
		return actionError("patch", fmt.Errorf("reading target file %q: %w", targetPath, err))
	}

	node, err := kyaml.Parse(string(targetRaw))
	if err != nil {
		return actionError("patch", fmt.Errorf("parsing target file %q: %w", targetPath, err))
	}

	filter := patchjson6902.Filter{Patch: patchContent}
	result, err := filter.Filter([]*kyaml.RNode{node})
	if err != nil {
		return actionError("patch", fmt.Errorf("json6902 patch failed: %w", err))
	}

	if len(result) == 0 {
		return actionError("patch", fmt.Errorf("json6902 patch produced no output"))
	}

	out, err := result[0].String()
	if err != nil {
		return actionError("patch", fmt.Errorf("serializing patched document: %w", err))
	}

	if err := os.WriteFile(targetPath, []byte(out), 0o644); err != nil {
		return actionError("patch", fmt.Errorf("writing patched file %q: %w", targetPath, err))
	}
	return nil
}
