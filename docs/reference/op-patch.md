# patch

Modifies YAML files already in the target repository. Loom supports two patch engines, selected with the `engine` field.

## Usage

```yaml
- name: patch-app
  patch:
    engine: smp          # or "json6902"
    path: "__functions/patches/overlay.yaml"
    target: "argocd/application.yaml"
```

| Field | Description |
|-------|-------------|
| `engine` | Patch engine: `smp` (default) or `json6902` |
| `path` | Path to the patch file, relative to module directory |
| `target` | Path to the target file, relative to target repository root |
| `preserveComments` | `"true"` (default) or `"false"`. Keep target comments through an `smp` merge. |

All fields are rendered as Go templates over the resolved params before use.

Both engines are powered by the [kustomize](https://github.com/kubernetes-sigs/kustomize) library and built into Loom -- no external tools required. Patch files are rendered as Go templates before being applied.

## Strategic Merge Patch (SMP) {#smp}

The default engine. The patch file is a partial YAML document that is deep-merged into the target.

```yaml
- name: overlay-app
  patch:
    path: "__functions/patches/smp-overlay.yaml"
    target: "argocd/application.yaml"
```

### Merge Rules

- **Maps** are merged recursively -- fields present in the patch overwrite the target; fields absent from the patch are left untouched.
- **Lists of maps** are merged by an inferred key. Loom detects a common string field (e.g. `name`) across list items and uses it to match entries. Matched items are deep-merged; unmatched patch items are appended.
- **Lists of scalars** (strings, numbers) append unique values from the patch. Existing values are preserved, and duplicates are skipped.
- **Scalars** are replaced by the patch value.

Original file formatting (indentation, key order, flow style) is preserved.

### Comments

By default, comments in the target file survive the merge — including comments on values the patch changes and on scalar list items. Set `preserveComments: "false"` to skip the restoration pass and keep only the comments the merge engine retains on its own (untouched fields). The field is templatable and must render to `true` or `false`. It only affects the `smp` engine; `json6902` preserves comments natively.

```yaml
- name: overlay-app
  patch:
    path: "__functions/patches/smp-overlay.yaml"
    target: "argocd/application.yaml"
    preserveComments: "false"
```

### Example: Adding Labels

```yaml
# __functions/patches/smp-overlay.yaml
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: "{{ .serviceName }}"
  namespace: "{{ .namespace }}"
  labels:
    managed-by: loom
    team: platform
spec:
  source:
    targetRevision: HEAD
```

### Example: Appending to a Scalar List

Given a target with a list of entries keyed by `name`, this patch appends `loom` to the `allowednamespace` list of `vault-example-2`:

```yaml
# __functions/patches/clustersecretstore-patch.yaml
apiVersion: constraints.gatekeeper.sh/v1beta1
kind: ClusterSecretStoreControl
metadata:
  name: clustersecretstorecontrol
spec:
  parameters:
    ClusterSecretStore:
      - name: vault-example-2
        allowednamespace:
          - loom
```

If `vault-example-2` already has `allowednamespace: [istio-system, argocd]`, the result will be `[istio-system, argocd, loom]`.

### Patch Directives

Strategic merge also supports Kubernetes patch directives:
- `$patch: delete` -- remove a field or list element
- `$patch: replace` -- replace an entire subtree instead of merging

## JSON6902 {#json6902}

An explicit list of [RFC 6902](https://datatracker.ietf.org/doc/html/rfc6902) operations.

```yaml
- name: patch-app
  patch:
    engine: json6902
    path: "__functions/patches/add-app.yaml"
    target: "argocd/application.yaml"
```

### Patch File Format

```yaml
- op: replace
  path: /metadata/name
  value: "{{ .serviceName }}"

- op: replace
  path: /metadata/namespace
  value: "{{ .namespace }}"

- op: add
  path: /metadata/labels/managed-by
  value: loom

- op: remove
  path: /metadata/annotations/deprecated
```

### Supported Operations

| Operation | Description |
|-----------|-------------|
| `add` | Add a value. Creates intermediate maps if needed. Use `-` to append to arrays. |
| `remove` | Remove a value at the path. |
| `replace` | Replace the value at the path. |
| `move` | Move a value from one path to another. |
| `copy` | Copy a value from one path to another. |
| `test` | Assert a value equals the expected value. Fails the operation if not. |

Paths follow [RFC 6901 JSON Pointer](https://datatracker.ietf.org/doc/html/rfc6901) syntax -- `/` separated segments, with `~0` for `~` and `~1` for `/` in key names.

## Dry Run

In dry-run mode, patches are computed but not written to disk. Run [`loom diff`](/reference/cli-diff) to see a colored unified diff of the before/after.
