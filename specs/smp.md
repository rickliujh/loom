# Strategic Merge Patch (SMP) — Behavioral Specification

This document describes the expected behavior of the **`patch`** operation when using the **`smp`** (Strategic Merge Patch) engine, which is the default patch engine in loom.

## Overview

The SMP engine deep-merges a partial YAML document (the **patch**) into an existing YAML document (the **target**) in the target repository. Fields present in the patch are set or overwritten; fields absent from the patch are left untouched. This mirrors the Kubernetes strategic merge patch semantics.

## Inputs

| Input         | Description |
|---------------|-------------|
| `patch.path`  | Relative path (from module root) to the patch YAML file. Typically under `__functions/patches/`. |
| `patch.target`| Relative path (from target repo root) to the YAML file to patch. |
| `patch.engine`| Optional. Defaults to `"smp"`. Set to `"json6902"` for RFC 6902 mode. |
| `patch.preserveComments`| Optional. `"true"` (default) or `"false"`. When true, comments in the target survive the merge (see B9). |

## Execution Flow

1. **Read** the patch file from `<moduleDir>/<patch.path>`.
2. **Template-render** the patch contents using the module's resolved params (Go `text/template` with custom funcs: `default`, `upper`, `lower`).
3. **Read** the target file from `<targetDir>/<patch.target>`.
4. **Expand scalar lists** — pre-process the patch so that scalar lists include the target's existing values (deduped) before merge (see below).
5. **Merge** the expanded patch into the target using `kustomize/kyaml/yaml/merge2.MergeStrings`.
6. **Restore comments** — unless `preserveComments` is `"false"`, copy target comments lost during the merge back onto the result (see B9).
7. **Write** the merged result back to the target file path, overwriting the original.

## Merge Behaviors

### B1: Scalar field set/overwrite

A scalar field in the patch **replaces** the corresponding field in the target.

```yaml
# target                    # patch                     # result
metadata:                   metadata:                   metadata:
  name: original              name: patched               name: patched
```

### B2: Absent fields preserved

Fields in the target that are **not mentioned** in the patch remain unchanged.

```yaml
# target                    # patch                     # result
metadata:                   metadata:                   metadata:
  name: original              name: patched               name: patched
  namespace: default                                      namespace: default
```

### B3: Nested map deep-merge

Maps are merged recursively, not replaced wholesale. Only the leaf fields specified in the patch are changed.

```yaml
# target                    # patch                     # result
spec:                       spec:                       spec:
  source:                     source:                     source:
    repoURL: a                  targetRevision: HEAD        repoURL: a
    targetRevision: v1                                      targetRevision: HEAD
```

### B4: Scalar list append-unique (dedup)

When both the target and patch contain a **scalar list** (list of strings/numbers) at the same path, the result is the **union** — all existing target values plus any new values from the patch that aren't already present. Order: target values first, then new patch values appended.

```yaml
# target                              # patch                         # result
parameters:                           parameters:                     parameters:
  ClusterSecretStore:                   ClusterSecretStore:             ClusterSecretStore:
    - name: vault-example-2               - name: vault-example-2         - name: vault-example-2
      allowednamespace:                     allowednamespace:               allowednamespace:
        - istio-system                        - loom                          - istio-system
        - argocd                                                              - argocd
                                                                              - loom
```

Duplicates are suppressed: if the patch value already exists in the target list, it is **not** added again.

### B5: Map-list merge by inferred key

When both the target and patch contain a **list of maps**, the engine infers a merge key by finding a common string-valued key in the first elements of both lists (e.g., `"name"`). Items are matched by this key and merged pairwise:

- **Matched items** (same key value in both target and patch): deep-merged recursively.
- **Unmatched target items** (present in target but not in patch): preserved as-is.
- **New patch items** (present in patch but not in target): appended.

```yaml
# target                              # patch                           # result
ClusterSecretStore:                   ClusterSecretStore:               ClusterSecretStore:
  - name: vault-example-1               - name: vault-example-2           - name: vault-example-1
    allowednamespace:                      allowednamespace:                 allowednamespace:
      - istio-system                         - loom                            - istio-system
  - name: vault-example-2               - name: vault-example-3           - name: vault-example-2
    allowednamespace:                      allowednamespace:                 allowednamespace:
      - argocd                               - loom-3                          - argocd
  - name: vault-example-3                                                    - loom
    allowednamespace:                                                      - name: vault-example-3
      - generalservice                                                       allowednamespace:
                                                                               - generalservice
                                                                               - loom-3
```

### B6: New fields added

Fields in the patch that do **not exist** in the target are added.

```yaml
# target                    # patch                     # result
metadata:                   metadata:                   metadata:
  name: app                   labels:                     name: app
                                managed-by: loom            labels:
                                                              managed-by: loom
```

### B7: Template rendering before merge

All Go template expressions in the patch file are resolved **before** the merge. Custom template functions (`default`, `upper`, `lower`) are available.

```yaml
# patch file (raw)                      # after rendering (params: serviceName=payments)
metadata:                               metadata:
  name: "{{ .serviceName }}"              name: "payments"
```

### B8: Expand scalar lists error propagation

If the expand-scalar-lists pre-processing fails to unmarshal either document (e.g., malformed YAML), the error is propagated immediately. There is no silent fallback — since expand and merge2 share the same YAML parser, any document that fails expand would also fail merge2.

### B9: Comment preservation

By default (`preserveComments: "true"` or unset), comments in the target document survive the merge. The merge itself drops target comments wherever the patch wins a node — changed scalar values, matched map-list items, and rebuilt scalar lists — so after merging, the engine walks the result alongside the original target and copies head/line/foot comments back onto matching nodes (mapping fields matched by key name, map-list items by inferred merge key, scalar list items by value). A comment already present on the result node is never overwritten.

Note that a comment attached to a value the patch changed is kept next to the new value.

```yaml
# target                        # patch                     # result (preserveComments: true)
replicas: 1 # scale with care   replicas: 2                 replicas: 2 # scale with care
list:                           list:                       list:
  - a # keep first                - c                         - a # keep first
                                                              - c
```

With `preserveComments: "false"`, no restoration pass runs and only the comments merge2 itself keeps (untouched fields, head comments of surviving map entries) remain.

The value must render to `"true"`, `"false"`, or empty; anything else fails the operation. The setting only affects the `smp` engine — `json6902` edits the target document in place and preserves comments natively.

## Dry-run Mode

When `dryRun` is `true`, the patch action logs what it **would** do but does not read or modify the target file.

## Error Conditions

| Condition | Error |
|-----------|-------|
| Patch file does not exist | `reading patch file "<path>": ...` |
| Patch file contains invalid Go template syntax | `rendering patch file "<path>": ...` |
| Target file does not exist | `reading target file "<path>": ...` |
| Malformed YAML in patch or target (expand phase) | `expanding scalar lists: ...` |
| merge2 fails (e.g., type conflict) | `strategic merge patch failed: ...` |
| Writing result fails | `writing patched file "<path>": ...` |
| Unknown engine value | `unknown patch engine "<engine>" (supported: smp, json6902)` |
| Invalid preserveComments value | `invalid patch preserveComments "<value>" (supported: true, false)` |

## End-to-End Example

Given the example from `test/stratigic-merge-patch`:

**Patch file** (`__functions/patches/clustersecretstore-patch.yaml`):
```yaml
apiVersion: constraints.gatekeeper.sh/v1beta1
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
```

**Target file** (`cluster/constraints/cluster-secret-store-control.yaml` in target repo):
```yaml
apiVersion: constraints.gatekeeper.sh/v1beta1
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
```

**Expected result** (as seen in [rickliujh/loom-test#7](https://github.com/rickliujh/loom-test/pull/7)):
```yaml
apiVersion: constraints.gatekeeper.sh/v1beta1
kind: ClusterSecretStoreControl
metadata:
  name: clustersecretstorecontrol
spec:
  match:
    kinds:
    - apiGroups:
      - external-secrets.io
      kinds:
      - ExternalSecret
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
      - loom
    - name: vault-example-3
      allowednamespace:
      - generalservice
      - mappingservice
      - loom-3
```

**What happened:**
1. `vault-example-1` — not in patch → preserved unchanged (B2, B5).
2. `vault-example-2` — matched by `name` key (B5), `allowednamespace` scalar list merged with append-unique → `loom` appended (B4).
3. `vault-example-3` — matched by `name` key (B5), `allowednamespace` scalar list merged → `loom-3` appended (B4).
4. YAML formatting is normalized by the kustomize merge2 library (flow sequences → block sequences).
