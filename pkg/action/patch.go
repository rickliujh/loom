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
	engine, err := tmpl.RenderString(a.Config.Engine, execCtx.Params)
	if err != nil {
		return actionError("patch", err)
	}
	if engine == "" {
		engine = "smp"
	}

	path, err := tmpl.RenderString(a.Config.Path, execCtx.Params)
	if err != nil {
		return actionError("patch", fmt.Errorf("rendering path: %w", err))
	}
	target, err := tmpl.RenderString(a.Config.Target, execCtx.Params)
	if err != nil {
		return actionError("patch", fmt.Errorf("rendering target: %w", err))
	}

	patchPath := util.ExpandPath(execCtx.ModuleDir, path)
	targetPath := filepath.Join(execCtx.TargetDir, target)

	execCtx.Logger.Info("applying patch", "engine", engine, "patch", patchPath, "target", targetPath)
	if execCtx.DryRun {
		execCtx.Logger.Info("dry-run: would apply patch", "engine", engine, "patch", patchPath, "target", targetPath)
		if execCtx.ShowDiff {
			return a.showPatchDiff(execCtx, engine, patchPath, targetPath, target)
		}
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
		return actionError("patch", fmt.Errorf("expanding scalar lists: %w", err))
	}

	normalized, err := normalizeEmptyTargetLists(string(targetRaw))
	if err != nil {
		return actionError("patch", fmt.Errorf("normalizing empty lists: %w", err))
	}

	merged, err := merge2.MergeStrings(expanded, normalized, true, kyaml.MergeOptions{
		ListIncreaseDirection: kyaml.MergeOptionsListAppend,
	})
	if err != nil {
		return actionError("patch", fmt.Errorf("strategic merge patch failed: %w", err))
	}

	result, err := restoreEmptyLists(merged, string(targetRaw))
	if err != nil {
		return actionError("patch", fmt.Errorf("restoring empty lists: %w", err))
	}

	if err := os.WriteFile(targetPath, []byte(result), 0o644); err != nil {
		return actionError("patch", fmt.Errorf("writing patched file %q: %w", targetPath, err))
	}
	return nil
}

// normalizeEmptyTargetLists rewrites every empty sequence in the target to
// null before merging. kyaml infers an empty sequence as an associative
// list (its merge-key check is vacuously true over zero elements), which
// sends merge2 down the merge-by-key path and fails with "no merge key
// found" — even for fields the patch never touches. Null values merge
// cleanly and keep each field's position; restoreEmptyLists turns unfilled
// nulls back into empty sequences afterwards.
func normalizeEmptyTargetLists(targetStr string) (string, error) {
	target, err := kyaml.Parse(targetStr)
	if err != nil {
		return "", err
	}
	normalizeEmptyWalk(target.YNode())
	return target.String()
}

func normalizeEmptyWalk(n *kyaml.Node) {
	switch n.Kind {
	case kyaml.MappingNode:
		for i := 0; i+1 < len(n.Content); i += 2 {
			v := n.Content[i+1]
			if v.Kind == kyaml.SequenceNode && len(v.Content) == 0 {
				// Value must be non-empty ("null"), or merge2 drops the
				// field instead of preserving the explicit null.
				*v = kyaml.Node{Kind: kyaml.ScalarNode, Tag: kyaml.NodeTagNull, Value: "null"}
				continue
			}
			normalizeEmptyWalk(v)
		}
	case kyaml.SequenceNode, kyaml.DocumentNode:
		for _, c := range n.Content {
			normalizeEmptyWalk(c)
		}
	}
}

// restoreEmptyLists walks the merge result alongside the original target
// and turns null values back into the original empty sequences wherever
// the patch left them unfilled.
func restoreEmptyLists(resultStr, targetStr string) (string, error) {
	result, err := kyaml.Parse(resultStr)
	if err != nil {
		return "", err
	}
	target, err := kyaml.Parse(targetStr)
	if err != nil {
		return "", err
	}
	restoreWalk(result.YNode(), target.YNode())
	return result.String()
}

func restoreWalk(res, orig *kyaml.Node) {
	if res == nil || orig == nil {
		return
	}
	switch {
	case res.Kind == kyaml.DocumentNode && orig.Kind == kyaml.DocumentNode:
		if len(res.Content) > 0 && len(orig.Content) > 0 {
			restoreWalk(res.Content[0], orig.Content[0])
		}
	case res.Kind == kyaml.MappingNode && orig.Kind == kyaml.MappingNode:
		for i := 0; i+1 < len(orig.Content); i += 2 {
			ov := orig.Content[i+1]
			rf := kyaml.NewRNode(res).Field(orig.Content[i].Value)
			if rf == nil {
				continue
			}
			rv := rf.Value.YNode()
			if ov.Kind == kyaml.SequenceNode && len(ov.Content) == 0 && rv.Tag == kyaml.NodeTagNull {
				*rv = *ov
				continue
			}
			restoreWalk(rv, ov)
		}
	case res.Kind == kyaml.SequenceNode && orig.Kind == kyaml.SequenceNode:
		restoreWalkSeq(res, orig)
	}
}

// restoreWalkSeq matches map items in the result and original sequences by
// a common scalar key (e.g. "name") and recurses into matched pairs, so
// empty lists nested inside list items are restored too.
func restoreWalkSeq(res, orig *kyaml.Node) {
	rElems := wrapRNodes(res.Content)
	oElems := wrapRNodes(orig.Content)
	key := inferRNodeSliceKey(rElems, oElems)
	if key == "" {
		return
	}
	for _, oe := range oElems {
		ov := fieldScalarValue(oe, key)
		if ov == "" {
			continue
		}
		for _, re := range rElems {
			if fieldScalarValue(re, key) == ov {
				restoreWalk(re.YNode(), oe.YNode())
				break
			}
		}
	}
}

func wrapRNodes(nodes []*kyaml.Node) []*kyaml.RNode {
	out := make([]*kyaml.RNode, len(nodes))
	for i, n := range nodes {
		out[i] = kyaml.NewRNode(n)
	}
	return out
}

// inferRNodeSliceKey finds a common scalar-valued key across the first
// map elements of both sequences, mirroring inferMapSliceKey.
func inferRNodeSliceKey(target, patch []*kyaml.RNode) string {
	if len(target) == 0 || len(patch) == 0 {
		return ""
	}
	pe, te := patch[0], target[0]
	if pe.YNode().Kind != kyaml.MappingNode || te.YNode().Kind != kyaml.MappingNode {
		return ""
	}
	fields, err := pe.Fields()
	if err != nil {
		return ""
	}
	for _, name := range fields {
		if fieldScalarValue(pe, name) == "" {
			continue
		}
		if te.Field(name) != nil {
			return name
		}
	}
	return ""
}

// fieldScalarValue returns the scalar value of a mapping field, or "" if
// the node is not a mapping, the field is absent, or its value is not scalar.
func fieldScalarValue(n *kyaml.RNode, field string) string {
	if n.YNode().Kind != kyaml.MappingNode {
		return ""
	}
	f := n.Field(field)
	if f == nil || f.Value.YNode().Kind != kyaml.ScalarNode {
		return ""
	}
	return f.Value.YNode().Value
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

		if pIsList && tIsList && len(pSlice) == 0 {
			// Append-nothing is a no-op; drop the field so merge2 never
			// sees an empty patch list (kyaml misinfers it as associative
			// and fails with "no merge key found").
			delete(pm, k)
			continue
		}

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

// showPatchDiff renders the patch and displays a diff without writing.
func (a *PatchAction) showPatchDiff(execCtx *ExecutionContext, engine, patchPath, targetPath, target string) error {
	patchRaw, err := os.ReadFile(patchPath)
	if err != nil {
		return actionError("patch", fmt.Errorf("reading patch file %q: %w", patchPath, err))
	}

	rendered, err := tmpl.RenderFile(patchRaw, execCtx.Params)
	if err != nil {
		return actionError("patch", fmt.Errorf("rendering patch file %q: %w", patchPath, err))
	}

	targetRaw, err := os.ReadFile(targetPath)
	if err != nil {
		return actionError("patch", fmt.Errorf("reading target file %q: %w", targetPath, err))
	}

	var result string
	switch engine {
	case "smp":
		expanded, err := expandScalarLists(string(targetRaw), string(rendered))
		if err != nil {
			return actionError("patch", fmt.Errorf("expanding scalar lists: %w", err))
		}
		normalized, err := normalizeEmptyTargetLists(string(targetRaw))
		if err != nil {
			return actionError("patch", fmt.Errorf("normalizing empty lists: %w", err))
		}
		merged, err := merge2.MergeStrings(expanded, normalized, true, kyaml.MergeOptions{
			ListIncreaseDirection: kyaml.MergeOptionsListAppend,
		})
		if err != nil {
			return actionError("patch", fmt.Errorf("strategic merge patch failed: %w", err))
		}
		result, err = restoreEmptyLists(merged, string(targetRaw))
		if err != nil {
			return actionError("patch", fmt.Errorf("restoring empty lists: %w", err))
		}
	case "json6902":
		node, err := kyaml.Parse(string(targetRaw))
		if err != nil {
			return actionError("patch", fmt.Errorf("parsing target file %q: %w", targetPath, err))
		}
		filter := patchjson6902.Filter{Patch: string(rendered)}
		nodes, err := filter.Filter([]*kyaml.RNode{node})
		if err != nil {
			return actionError("patch", fmt.Errorf("json6902 patch failed: %w", err))
		}
		if len(nodes) == 0 {
			return actionError("patch", fmt.Errorf("json6902 patch produced no output"))
		}
		result, err = nodes[0].String()
		if err != nil {
			return actionError("patch", fmt.Errorf("serializing patched document: %w", err))
		}
	default:
		return actionError("patch", fmt.Errorf("unknown patch engine %q", engine))
	}

	printDiff(execCtx, target, string(targetRaw), result)
	return nil
}
