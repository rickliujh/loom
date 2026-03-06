package action

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/rickliujh/loom/internal/util"
	"github.com/rickliujh/loom/pkg/config"
	tmpl "github.com/rickliujh/loom/pkg/template"
	"gopkg.in/yaml.v3"
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

// applySMP applies a Strategic Merge Patch using kustomize's merge2 for
// format preservation. Scalar lists in the patch are pre-expanded with
// the target's existing values so that merge2's list replacement produces
// the correct append-unique result.
func (a *PatchAction) applySMP(patchContent, targetPath string) error {
	targetRaw, err := os.ReadFile(targetPath)
	if err != nil {
		return actionError("patch", fmt.Errorf("reading target file %q: %w", targetPath, err))
	}

	expanded, err := expandScalarLists(string(targetRaw), patchContent)
	if err != nil {
		expanded = patchContent
	}

	result, err := merge2.MergeStrings(expanded, string(targetRaw), true, kyaml.MergeOptions{
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

// expandScalarLists walks the patch and target as untyped Go values.
// For every scalar list in the patch, it prepends the target's existing
// values (deduped) so that when merge2 replaces the list the result
// contains both old and new entries.
func expandScalarLists(targetStr, patchStr string) (string, error) {
	var target, patch any
	if err := yaml.Unmarshal([]byte(targetStr), &target); err != nil {
		return "", err
	}
	if err := yaml.Unmarshal([]byte(patchStr), &patch); err != nil {
		return "", err
	}

	expandWalk(target, patch)

	out, err := yaml.Marshal(patch)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// expandWalk recursively walks target and patch in parallel. When it finds
// a scalar list in patch that also exists in target, it prepends the
// target values (skipping duplicates).
func expandWalk(target, patch any) {
	pm, pOk := patch.(map[string]any)
	tm, tOk := target.(map[string]any)
	if !pOk || !tOk {
		return
	}

	for k, pv := range pm {
		tv, exists := tm[k]
		if !exists {
			continue
		}

		pSlice, pIsList := pv.([]any)
		tSlice, tIsList := tv.([]any)

		if pIsList && tIsList && len(pSlice) > 0 {
			if isScalarSlice(pSlice) && isScalarSlice(tSlice) {
				pm[k] = appendUniqueScalars(tSlice, pSlice)
				continue
			}
			// Recurse into matched map-list items by merge key.
			expandWalkMapSlices(tSlice, pSlice)
			continue
		}

		expandWalk(tv, pv)
	}
}

// expandWalkMapSlices matches map items in target and patch by a common
// string key, then recurses into matched pairs.
func expandWalkMapSlices(target, patch []any) {
	key := inferMapSliceKey(target, patch)
	if key == "" {
		return
	}
	for _, pi := range patch {
		pm, ok := pi.(map[string]any)
		if !ok {
			continue
		}
		pv, ok := pm[key]
		if !ok {
			continue
		}
		for _, ti := range target {
			tm, ok := ti.(map[string]any)
			if !ok {
				continue
			}
			if tm[key] == pv {
				expandWalk(ti, pi)
				break
			}
		}
	}
}

// inferMapSliceKey finds a common string-valued key across the first
// map elements of both slices (e.g. "name").
func inferMapSliceKey(target, patch []any) string {
	if len(target) == 0 || len(patch) == 0 {
		return ""
	}
	tm, tOk := target[0].(map[string]any)
	pm, pOk := patch[0].(map[string]any)
	if !tOk || !pOk {
		return ""
	}
	for k, v := range pm {
		if _, isStr := v.(string); !isStr {
			continue
		}
		if _, exists := tm[k]; exists {
			return k
		}
	}
	return ""
}

func isScalarSlice(s []any) bool {
	for _, v := range s {
		switch v.(type) {
		case map[string]any, []any:
			return false
		}
	}
	return true
}

func appendUniqueScalars(target, patch []any) []any {
	seen := make(map[any]bool, len(target))
	for _, v := range target {
		seen[v] = true
	}
	result := make([]any, len(target))
	copy(result, target)
	for _, v := range patch {
		if !seen[v] {
			result = append(result, v)
		}
	}
	return result
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
