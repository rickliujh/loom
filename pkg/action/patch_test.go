package action

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rickliujh/loom/pkg/config"
)

// helpers -----------------------------------------------------------

func setupPatch(t *testing.T, patchContent, targetContent string) (moduleDir, targetDir, targetFile string) {
	t.Helper()
	moduleDir = t.TempDir()
	patchDir := filepath.Join(moduleDir, "__functions", "patches")
	os.MkdirAll(patchDir, 0o755)
	os.WriteFile(filepath.Join(patchDir, "patch.yaml"), []byte(patchContent), 0o644)

	targetDir = t.TempDir()
	targetFile = "target.yaml"
	os.WriteFile(filepath.Join(targetDir, targetFile), []byte(targetContent), 0o644)
	return
}

func readTarget(t *testing.T, targetDir, targetFile string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(targetDir, targetFile))
	if err != nil {
		t.Fatalf("reading target: %v", err)
	}
	return string(data)
}

func runPatch(t *testing.T, engine string, params map[string]string, patchContent, targetContent string) string {
	t.Helper()
	moduleDir, targetDir, targetFile := setupPatch(t, patchContent, targetContent)

	a := &PatchAction{Config: config.Patch{
		Engine: engine,
		Path:   "__functions/patches/patch.yaml",
		Target: targetFile,
	}}

	execCtx := testExecCtx(t, moduleDir, targetDir)
	if params != nil {
		execCtx.Params = params
	}

	if err := a.Execute(context.Background(), execCtx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	return readTarget(t, targetDir, targetFile)
}

func runPatchErr(t *testing.T, engine string, params map[string]string, patchContent, targetContent string) error {
	t.Helper()
	moduleDir, targetDir, targetFile := setupPatch(t, patchContent, targetContent)

	a := &PatchAction{Config: config.Patch{
		Engine: engine,
		Path:   "__functions/patches/patch.yaml",
		Target: targetFile,
	}}

	execCtx := testExecCtx(t, moduleDir, targetDir)
	if params != nil {
		execCtx.Params = params
	}

	return a.Execute(context.Background(), execCtx)
}

// -------------------------------------------------------------------
// B1: Scalar field set/overwrite
// -------------------------------------------------------------------

func TestSMP_B1_ScalarFieldOverwrite(t *testing.T) {
	target := `apiVersion: v1
metadata:
  name: original
  namespace: default
`
	patch := `metadata:
  name: patched
`
	result := runPatch(t, "smp", nil, patch, target)

	if !strings.Contains(result, "name: patched") {
		t.Errorf("expected name overwritten to 'patched', got:\n%s", result)
	}
}

// -------------------------------------------------------------------
// B2: Absent fields preserved
// -------------------------------------------------------------------

func TestSMP_B2_AbsentFieldsPreserved(t *testing.T) {
	target := `apiVersion: v1
metadata:
  name: original
  namespace: default
  labels:
    app: myapp
`
	patch := `metadata:
  name: patched
`
	result := runPatch(t, "smp", nil, patch, target)

	if !strings.Contains(result, "name: patched") {
		t.Errorf("expected name overwritten, got:\n%s", result)
	}
	if !strings.Contains(result, "namespace: default") {
		t.Errorf("expected namespace preserved, got:\n%s", result)
	}
	if !strings.Contains(result, "app: myapp") {
		t.Errorf("expected labels preserved, got:\n%s", result)
	}
	if !strings.Contains(result, "apiVersion: v1") {
		t.Errorf("expected apiVersion preserved, got:\n%s", result)
	}
}

// -------------------------------------------------------------------
// B3: Nested map deep-merge
// -------------------------------------------------------------------

func TestSMP_B3_NestedMapDeepMerge(t *testing.T) {
	target := `spec:
  source:
    repoURL: https://example.com
    targetRevision: v1
    path: apps
`
	patch := `spec:
  source:
    targetRevision: HEAD
`
	result := runPatch(t, "smp", nil, patch, target)

	if !strings.Contains(result, "targetRevision: HEAD") {
		t.Errorf("expected targetRevision overwritten, got:\n%s", result)
	}
	if !strings.Contains(result, "repoURL: https://example.com") {
		t.Errorf("expected repoURL preserved, got:\n%s", result)
	}
	if !strings.Contains(result, "path: apps") {
		t.Errorf("expected path preserved, got:\n%s", result)
	}
}

func TestSMP_B3_DeeplyNestedMerge(t *testing.T) {
	target := `a:
  b:
    c:
      d: original
      e: keep
`
	patch := `a:
  b:
    c:
      d: changed
`
	result := runPatch(t, "smp", nil, patch, target)

	if !strings.Contains(result, "d: changed") {
		t.Errorf("expected deeply nested field changed, got:\n%s", result)
	}
	if !strings.Contains(result, "e: keep") {
		t.Errorf("expected sibling field preserved, got:\n%s", result)
	}
}

// -------------------------------------------------------------------
// B4: Scalar list append-unique (dedup)
// -------------------------------------------------------------------

func TestSMP_B4_ScalarListAppendUnique(t *testing.T) {
	target := `spec:
  namespaces:
    - alpha
    - beta
`
	patch := `spec:
  namespaces:
    - gamma
`
	result := runPatch(t, "smp", nil, patch, target)

	if !strings.Contains(result, "alpha") {
		t.Errorf("expected existing 'alpha' preserved, got:\n%s", result)
	}
	if !strings.Contains(result, "beta") {
		t.Errorf("expected existing 'beta' preserved, got:\n%s", result)
	}
	if !strings.Contains(result, "gamma") {
		t.Errorf("expected new 'gamma' appended, got:\n%s", result)
	}
}

func TestSMP_B4_ScalarListDedup(t *testing.T) {
	target := `spec:
  namespaces:
    - alpha
    - beta
`
	patch := `spec:
  namespaces:
    - beta
    - gamma
`
	result := runPatch(t, "smp", nil, patch, target)

	// beta should appear exactly once
	count := strings.Count(result, "beta")
	if count != 1 {
		t.Errorf("expected 'beta' exactly once (dedup), found %d times in:\n%s", count, result)
	}
	if !strings.Contains(result, "alpha") {
		t.Errorf("expected 'alpha' preserved, got:\n%s", result)
	}
	if !strings.Contains(result, "gamma") {
		t.Errorf("expected 'gamma' appended, got:\n%s", result)
	}
}

func TestSMP_B4_ScalarListAllDuplicates(t *testing.T) {
	target := `items:
  - a
  - b
`
	patch := `items:
  - a
  - b
`
	result := runPatch(t, "smp", nil, patch, target)

	if strings.Count(result, "- a") != 1 {
		t.Errorf("expected 'a' exactly once, got:\n%s", result)
	}
	if strings.Count(result, "- b") != 1 {
		t.Errorf("expected 'b' exactly once, got:\n%s", result)
	}
}

func TestSMP_B4_ScalarListOrderTargetFirst(t *testing.T) {
	target := `items:
  - first
  - second
`
	patch := `items:
  - third
`
	result := runPatch(t, "smp", nil, patch, target)

	firstIdx := strings.Index(result, "first")
	secondIdx := strings.Index(result, "second")
	thirdIdx := strings.Index(result, "third")

	if firstIdx < 0 || secondIdx < 0 || thirdIdx < 0 {
		t.Fatalf("expected all items present, got:\n%s", result)
	}
	if !(firstIdx < secondIdx && secondIdx < thirdIdx) {
		t.Errorf("expected order: first, second, third; got:\n%s", result)
	}
}

// -------------------------------------------------------------------
// B5: Map-list merge by inferred key
// -------------------------------------------------------------------

func TestSMP_B5_MapListMergeByKey(t *testing.T) {
	target := `parameters:
  ClusterSecretStore:
    - name: vault-1
      allowednamespace:
        - istio-system
        - argocd
    - name: vault-2
      allowednamespace:
        - mysoftware
        - argocd
    - name: vault-3
      allowednamespace:
        - generalservice
`
	patch := `parameters:
  ClusterSecretStore:
    - name: vault-2
      allowednamespace:
        - loom
    - name: vault-3
      allowednamespace:
        - loom-3
`
	result := runPatch(t, "smp", nil, patch, target)

	// vault-1 not in patch — must be preserved unchanged
	if !strings.Contains(result, "vault-1") {
		t.Errorf("expected vault-1 preserved, got:\n%s", result)
	}
	if !strings.Contains(result, "istio-system") {
		t.Errorf("expected vault-1's istio-system preserved, got:\n%s", result)
	}

	// vault-2 matched — loom appended to existing list
	if !strings.Contains(result, "mysoftware") {
		t.Errorf("expected vault-2's existing mysoftware preserved, got:\n%s", result)
	}
	if !strings.Contains(result, "loom") {
		t.Errorf("expected 'loom' appended to vault-2, got:\n%s", result)
	}

	// vault-3 matched — loom-3 appended
	if !strings.Contains(result, "generalservice") {
		t.Errorf("expected vault-3's existing generalservice preserved, got:\n%s", result)
	}
	if !strings.Contains(result, "loom-3") {
		t.Errorf("expected 'loom-3' appended to vault-3, got:\n%s", result)
	}
}

func TestSMP_B5_UnmatchedTargetItemsPreserved(t *testing.T) {
	target := `items:
  - name: keep-me
    value: important
  - name: also-keep
    value: data
`
	patch := `items:
  - name: also-keep
    extra: added
`
	result := runPatch(t, "smp", nil, patch, target)

	if !strings.Contains(result, "keep-me") {
		t.Errorf("expected unmatched 'keep-me' preserved, got:\n%s", result)
	}
	if !strings.Contains(result, "important") {
		t.Errorf("expected 'keep-me' value preserved, got:\n%s", result)
	}
	if !strings.Contains(result, "extra: added") {
		t.Errorf("expected 'extra' field added to 'also-keep', got:\n%s", result)
	}
}

func TestSMP_B5_NewPatchItemAppended(t *testing.T) {
	target := `items:
  - name: existing
    val: a
`
	patch := `items:
  - name: brand-new
    val: b
`
	result := runPatch(t, "smp", nil, patch, target)

	if !strings.Contains(result, "existing") {
		t.Errorf("expected existing item preserved, got:\n%s", result)
	}
	if !strings.Contains(result, "brand-new") {
		t.Errorf("expected new item appended, got:\n%s", result)
	}
}

// -------------------------------------------------------------------
// B6: New fields added
// -------------------------------------------------------------------

func TestSMP_B6_NewFieldsAdded(t *testing.T) {
	target := `metadata:
  name: app
`
	patch := `metadata:
  labels:
    managed-by: loom
`
	result := runPatch(t, "smp", nil, patch, target)

	if !strings.Contains(result, "name: app") {
		t.Errorf("expected existing name preserved, got:\n%s", result)
	}
	if !strings.Contains(result, "managed-by: loom") {
		t.Errorf("expected new label added, got:\n%s", result)
	}
}

func TestSMP_B6_NewTopLevelField(t *testing.T) {
	target := `apiVersion: v1
kind: Config
`
	patch := `spec:
  replicas: 3
`
	result := runPatch(t, "smp", nil, patch, target)

	if !strings.Contains(result, "apiVersion: v1") {
		t.Errorf("expected apiVersion preserved, got:\n%s", result)
	}
	if !strings.Contains(result, "kind: Config") {
		t.Errorf("expected kind preserved, got:\n%s", result)
	}
	if !strings.Contains(result, "replicas:") {
		t.Errorf("expected new spec.replicas added, got:\n%s", result)
	}
}

// -------------------------------------------------------------------
// B7: Template rendering before merge
// -------------------------------------------------------------------

func TestSMP_B7_TemplateRendering(t *testing.T) {
	target := `metadata:
  name: placeholder
  namespace: placeholder
`
	patch := `metadata:
  name: "{{ .serviceName }}"
  namespace: "{{ .namespace }}"
`
	params := map[string]string{
		"serviceName": "payments",
		"namespace":   "production",
	}
	result := runPatch(t, "smp", params, patch, target)

	if !strings.Contains(result, "name: payments") {
		t.Errorf("expected templated name 'payments', got:\n%s", result)
	}
	if !strings.Contains(result, "namespace: production") {
		t.Errorf("expected templated namespace 'production', got:\n%s", result)
	}
}

func TestSMP_B7_TemplateFuncUpper(t *testing.T) {
	target := `metadata:
  env: placeholder
`
	patch := `metadata:
  env: '{{ .env | upper }}'
`
	params := map[string]string{"env": "prod"}
	result := runPatch(t, "smp", params, patch, target)

	if !strings.Contains(result, "PROD") {
		t.Errorf("expected upper-cased 'PROD', got:\n%s", result)
	}
}

func TestSMP_B7_TemplateFuncLower(t *testing.T) {
	target := `metadata:
  env: PLACEHOLDER
`
	patch := `metadata:
  env: '{{ .env | lower }}'
`
	params := map[string]string{"env": "STAGING"}
	result := runPatch(t, "smp", params, patch, target)

	if !strings.Contains(result, "staging") {
		t.Errorf("expected lower-cased 'staging', got:\n%s", result)
	}
}

func TestSMP_B7_TemplateFuncDefault(t *testing.T) {
	target := `metadata:
  team: unknown
`
	patch := `metadata:
  team: '{{ default "platform" .team }}'
`
	// team param is empty string — should use default
	params := map[string]string{"team": ""}
	result := runPatch(t, "smp", params, patch, target)

	if !strings.Contains(result, "platform") {
		t.Errorf("expected default 'platform', got:\n%s", result)
	}
}

// -------------------------------------------------------------------
// B8: Expand scalar lists fallback
// -------------------------------------------------------------------

func TestSMP_B8_ExpandFailureReturnsError(t *testing.T) {
	// If the patch or target contains malformed YAML that expandScalarLists
	// cannot unmarshal, the error is propagated — no silent fallback.
	moduleDir, targetDir, targetFile := setupPatch(t,
		"valid: yaml\n",
		":\ninvalid yaml\n\t- broken",
	)

	a := &PatchAction{Config: config.Patch{
		Path:   "__functions/patches/patch.yaml",
		Target: targetFile,
	}}

	execCtx := testExecCtx(t, moduleDir, targetDir)
	err := a.Execute(context.Background(), execCtx)
	if err == nil {
		t.Fatal("expected error for malformed YAML in target")
	}
	if !strings.Contains(err.Error(), "expanding scalar lists") {
		t.Errorf("expected 'expanding scalar lists' error, got: %v", err)
	}
}

// -------------------------------------------------------------------
// Dry-run mode
// -------------------------------------------------------------------

func TestSMP_DryRun_DoesNotModifyTarget(t *testing.T) {
	moduleDir, targetDir, targetFile := setupPatch(t,
		"metadata:\n  name: patched\n",
		"metadata:\n  name: original\n",
	)

	a := &PatchAction{Config: config.Patch{
		Path:   "__functions/patches/patch.yaml",
		Target: targetFile,
	}}

	execCtx := testExecCtx(t, moduleDir, targetDir)
	execCtx.DryRun = true

	if err := a.Execute(context.Background(), execCtx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	result := readTarget(t, targetDir, targetFile)
	if !strings.Contains(result, "name: original") {
		t.Errorf("dry-run should not modify target, got:\n%s", result)
	}
}

// -------------------------------------------------------------------
// Error conditions
// -------------------------------------------------------------------

func TestSMP_Error_PatchFileNotFound(t *testing.T) {
	moduleDir := t.TempDir()
	targetDir := t.TempDir()
	os.WriteFile(filepath.Join(targetDir, "target.yaml"), []byte("key: val"), 0o644)

	a := &PatchAction{Config: config.Patch{
		Path:   "nonexistent/patch.yaml",
		Target: "target.yaml",
	}}

	execCtx := testExecCtx(t, moduleDir, targetDir)
	err := a.Execute(context.Background(), execCtx)
	if err == nil {
		t.Fatal("expected error for missing patch file")
	}
	if !strings.Contains(err.Error(), "reading patch file") {
		t.Errorf("expected 'reading patch file' error, got: %v", err)
	}
}

func TestSMP_Error_TargetFileNotFound(t *testing.T) {
	moduleDir := t.TempDir()
	patchDir := filepath.Join(moduleDir, "__functions", "patches")
	os.MkdirAll(patchDir, 0o755)
	os.WriteFile(filepath.Join(patchDir, "patch.yaml"), []byte("key: val"), 0o644)

	targetDir := t.TempDir()

	a := &PatchAction{Config: config.Patch{
		Path:   "__functions/patches/patch.yaml",
		Target: "nonexistent.yaml",
	}}

	execCtx := testExecCtx(t, moduleDir, targetDir)
	err := a.Execute(context.Background(), execCtx)
	if err == nil {
		t.Fatal("expected error for missing target file")
	}
	if !strings.Contains(err.Error(), "reading target file") {
		t.Errorf("expected 'reading target file' error, got: %v", err)
	}
}

func TestSMP_Error_InvalidTemplateSyntax(t *testing.T) {
	err := runPatchErr(t, "smp", nil,
		"metadata:\n  name: {{ .broken",
		"metadata:\n  name: original\n",
	)
	if err == nil {
		t.Fatal("expected error for invalid template")
	}
	if !strings.Contains(err.Error(), "rendering patch file") {
		t.Errorf("expected 'rendering patch file' error, got: %v", err)
	}
}

func TestSMP_Error_UnknownEngine(t *testing.T) {
	err := runPatchErr(t, "unknown-engine", nil,
		"key: val",
		"key: original\n",
	)
	if err == nil {
		t.Fatal("expected error for unknown engine")
	}
	if !strings.Contains(err.Error(), "unknown patch engine") {
		t.Errorf("expected 'unknown patch engine' error, got: %v", err)
	}
}

// -------------------------------------------------------------------
// Default engine is SMP
// -------------------------------------------------------------------

func TestPatch_DefaultEngineIsSMP(t *testing.T) {
	target := `metadata:
  name: original
`
	patch := `metadata:
  name: patched
`
	// Empty engine string should default to SMP
	result := runPatch(t, "", nil, patch, target)

	if !strings.Contains(result, "name: patched") {
		t.Errorf("expected default engine (smp) to apply patch, got:\n%s", result)
	}
}

// -------------------------------------------------------------------
// JSON 6902 engine
// -------------------------------------------------------------------

func TestJSON6902_ReplaceField(t *testing.T) {
	target := `apiVersion: v1
metadata:
  name: original
`
	patch := `- op: replace
  path: /metadata/name
  value: replaced
`
	result := runPatch(t, "json6902", nil, patch, target)

	if !strings.Contains(result, "name: replaced") {
		t.Errorf("expected json6902 replace, got:\n%s", result)
	}
}

func TestJSON6902_AddField(t *testing.T) {
	target := `apiVersion: v1
metadata:
  name: app
`
	patch := `- op: add
  path: /metadata/labels
  value:
    managed-by: loom
`
	result := runPatch(t, "json6902", nil, patch, target)

	if !strings.Contains(result, "managed-by: loom") {
		t.Errorf("expected json6902 add label, got:\n%s", result)
	}
}

func TestJSON6902_RemoveField(t *testing.T) {
	target := `apiVersion: v1
metadata:
  name: app
  namespace: default
`
	patch := `- op: remove
  path: /metadata/namespace
`
	result := runPatch(t, "json6902", nil, patch, target)

	if strings.Contains(result, "namespace") {
		t.Errorf("expected namespace removed, got:\n%s", result)
	}
	if !strings.Contains(result, "name: app") {
		t.Errorf("expected name preserved, got:\n%s", result)
	}
}

func TestJSON6902_TemplateRendering(t *testing.T) {
	target := `apiVersion: v1
metadata:
  name: original
`
	patch := `- op: replace
  path: /metadata/name
  value: "{{ .serviceName }}"
`
	params := map[string]string{"serviceName": "payments"}
	result := runPatch(t, "json6902", params, patch, target)

	if !strings.Contains(result, "name: payments") {
		t.Errorf("expected templated json6902 value, got:\n%s", result)
	}
}

func TestJSON6902_AppendToArray(t *testing.T) {
	target := `spec:
  parameters:
    ClusterSecretStore:
      - name: vault-1
        allowednamespace:
          - istio-system
`
	patch := `- op: add
  path: /spec/parameters/ClusterSecretStore/0/allowednamespace/-
  value: loom
`
	result := runPatch(t, "json6902", nil, patch, target)

	if !strings.Contains(result, "istio-system") {
		t.Errorf("expected existing value preserved, got:\n%s", result)
	}
	if !strings.Contains(result, "loom") {
		t.Errorf("expected 'loom' appended, got:\n%s", result)
	}
}

// -------------------------------------------------------------------
// End-to-end: the example from the spec / test branch
// -------------------------------------------------------------------

func TestSMP_E2E_ClusterSecretStorePatch(t *testing.T) {
	target := `apiVersion: constraints.gatekeeper.sh/v1beta1
kind: ClusterSecretStoreControl
metadata:
  name: clustersecretstorecontrol
spec:
  match:
    kinds:
      - apiGroups: ["external-secrets.io"]
        kinds: ["ExternalSecret"]
  parameters:
    ClusterSecretStore:
      - name: vault-example-1
        allowednamespace:
          - istio-system
          - istio-ingressgateway
          - argocd
      - name: vault-example-2
        allowednamespace:
          - istio-system
          - istio-ingressgateway
          - mysoftware
          - argocd
      - name: vault-example-3
        allowednamespace:
          - generalservice
          - mappingservice
`

	patch := `apiVersion: constraints.gatekeeper.sh/v1beta1
kind: ClusterSecretStoreControl
metadata:
  name: clustersecretstorecontrol
spec:
  match:
    kinds:
      - apiGroups: ["external-secrets.io"]
        kinds: ["ExternalSecret"]
  parameters:
    ClusterSecretStore:
      - name: vault-example-2
        allowednamespace:
          - loom
      - name: vault-example-3
        allowednamespace:
          - loom-3
`

	result := runPatch(t, "smp", nil, patch, target)

	// vault-example-1: not in patch, preserved as-is
	if !strings.Contains(result, "vault-example-1") {
		t.Errorf("expected vault-example-1 preserved, got:\n%s", result)
	}
	for _, ns := range []string{"istio-system", "istio-ingressgateway", "argocd"} {
		if !strings.Contains(result, ns) {
			t.Errorf("expected vault-example-1 namespace %q preserved, got:\n%s", ns, result)
		}
	}

	// vault-example-2: existing namespaces preserved, "loom" appended
	if !strings.Contains(result, "mysoftware") {
		t.Errorf("expected vault-example-2 existing 'mysoftware' preserved, got:\n%s", result)
	}
	if !strings.Contains(result, "loom") {
		t.Errorf("expected 'loom' appended to vault-example-2, got:\n%s", result)
	}

	// vault-example-3: existing namespaces preserved, "loom-3" appended
	if !strings.Contains(result, "generalservice") {
		t.Errorf("expected vault-example-3 existing 'generalservice' preserved, got:\n%s", result)
	}
	if !strings.Contains(result, "mappingservice") {
		t.Errorf("expected vault-example-3 existing 'mappingservice' preserved, got:\n%s", result)
	}
	if !strings.Contains(result, "loom-3") {
		t.Errorf("expected 'loom-3' appended to vault-example-3, got:\n%s", result)
	}

	// "loom" should not leak into vault-example-1 or vault-example-3
	// Split by vault-example entries and check isolation
	parts := strings.SplitAfter(result, "vault-example-1")
	if len(parts) > 1 {
		vault1Section := parts[0]
		if strings.Contains(vault1Section, "- loom\n") {
			t.Errorf("'loom' should not appear in vault-example-1 section")
		}
	}
}

// -------------------------------------------------------------------
// Helpers unit tests
// -------------------------------------------------------------------

func TestExpandScalarLists(t *testing.T) {
	target := `items:
  - a
  - b
`
	patch := `items:
  - b
  - c
`
	expanded, err := expandScalarLists(target, patch)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(expanded, "- a") {
		t.Errorf("expected target 'a' prepended, got:\n%s", expanded)
	}
	if !strings.Contains(expanded, "- c") {
		t.Errorf("expected patch 'c' present, got:\n%s", expanded)
	}
	// b should only appear once (dedup)
	if strings.Count(expanded, "- b") != 1 {
		t.Errorf("expected 'b' once (dedup), got:\n%s", expanded)
	}
}

func TestExpandWalkMapSlices(t *testing.T) {
	target := `stores:
  - name: store-1
    namespaces:
      - ns-a
  - name: store-2
    namespaces:
      - ns-b
`
	patch := `stores:
  - name: store-2
    namespaces:
      - ns-c
`
	expanded, err := expandScalarLists(target, patch)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// store-2's namespaces should have ns-b prepended
	if !strings.Contains(expanded, "ns-b") {
		t.Errorf("expected 'ns-b' from target prepended, got:\n%s", expanded)
	}
	if !strings.Contains(expanded, "ns-c") {
		t.Errorf("expected 'ns-c' from patch present, got:\n%s", expanded)
	}
}

func TestIsScalarSlice(t *testing.T) {
	tests := []struct {
		name string
		in   []any
		want bool
	}{
		{"strings", []any{"a", "b"}, true},
		{"ints", []any{1, 2}, true},
		{"mixed scalars", []any{"a", 1}, true},
		{"empty", []any{}, true},
		{"contains map", []any{map[string]any{"k": "v"}}, false},
		{"contains slice", []any{[]any{"a"}}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isScalarSlice(tt.in)
			if got != tt.want {
				t.Errorf("isScalarSlice(%v) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

func TestAppendUniqueScalars(t *testing.T) {
	tests := []struct {
		name   string
		target []any
		patch  []any
		want   int // expected length
	}{
		{"no overlap", []any{"a", "b"}, []any{"c", "d"}, 4},
		{"full overlap", []any{"a", "b"}, []any{"a", "b"}, 2},
		{"partial overlap", []any{"a", "b"}, []any{"b", "c"}, 3},
		{"empty target", []any{}, []any{"a"}, 1},
		{"empty patch", []any{"a"}, []any{}, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := appendUniqueScalars(tt.target, tt.patch)
			if len(got) != tt.want {
				t.Errorf("appendUniqueScalars() len = %d, want %d; result = %v", len(got), tt.want, got)
			}
		})
	}
}

func TestInferMapSliceKey(t *testing.T) {
	tests := []struct {
		name   string
		target []any
		patch  []any
		want   string
	}{
		{
			"common name key",
			[]any{map[string]any{"name": "a", "val": 1}},
			[]any{map[string]any{"name": "b", "val": 2}},
			"name",
		},
		{
			"empty slices",
			[]any{},
			[]any{},
			"",
		},
		{
			"non-map slices",
			[]any{"a"},
			[]any{"b"},
			"",
		},
		{
			"no common string key",
			[]any{map[string]any{"x": 1}},
			[]any{map[string]any{"y": "b"}},
			"",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := inferMapSliceKey(tt.target, tt.patch)
			if got != tt.want {
				t.Errorf("inferMapSliceKey() = %q, want %q", got, tt.want)
			}
		})
	}
}
