# Module Composition

Modules can reference other modules. This is Loom's answer to the question: "how do I reuse automation across teams?"

## Example

```yaml
# root module
spec:
  params:
    - name: serviceName
      required: true
    - name: namespace
      default: "default"

  modules:
    - name: base-infra
      source: "./modules/base-infra"
      params:
        namespace: "{{ .namespace }}"

    - name: argocd-app
      source: "https://github.com/myorg/loom-modules.git"
      params:
        serviceName: "{{ .serviceName }}"
        namespace: "{{ .namespace }}"

  operations:
    - name: commit-all
      commitPush:
        message: "feat: full onboard for {{ .serviceName }}"
        author: "loom-bot"
        email: "loom@example.com"
```

## Execution Order

1. Child modules run first, in the order they're listed
2. Each child module can have its own child modules (recursive)
3. Then the parent's operations run
4. All modules write into the same target directory

A child (or an individual operation) can be gated with an [`if`](#conditional-modules) predicate — a shell expression that skips the step when it exits non-zero.

## Module Sources

Sources can be:

- **Local paths** (`./relative/path` or `/absolute/path`) — resolved relative to the parent module
- **Git URLs** — cloned to a temporary directory automatically
- **Git URLs with `//` separator** — `repo.git//subdir` clones the repo and uses `subdir/` as the module root

The `//` convention (borrowed from Terraform) lets a single Git repository host multiple modules in different directories:

```yaml
modules:
  - name: networking
    source: https://github.com/myorg/loom-modules.git//networking
  - name: monitoring
    source: https://github.com/myorg/loom-modules.git//monitoring
```

The `source` field is templatable, so the subdirectory can be dynamic:

```yaml
modules:
  - name: env-module
    source: "https://github.com/myorg/loom-modules.git//{{ .environment }}"
    params:
      env: "{{ .environment }}"
```

### Sibling modules within the same repo

When a git URL contains `//`, the **entire repository** is cloned (not a sparse checkout). Modules within the same repo can reference each other using relative local paths:

```yaml
# repo layout:
#   loom-modules/
#     networking/loom.yaml
#     monitoring/loom.yaml
#
# inside networking/loom.yaml:
modules:
  - name: monitoring
    source: ../monitoring    # resolves to <cloneDir>/monitoring
```

This means you can publish reusable modules as Git repositories. A platform team maintains the standard modules; product teams compose them.

## Parameter Flow

Parameters are passed from parent to child through the `params` field. Child params are rendered through the parent's template context, so you can reference any parent parameter:

```yaml
modules:
  - name: child-module
    source: "./child"
    params:
      env: "{{ .env }}"
      label: "{{ .serviceName }}-{{ .namespace }}"
```

Each child module resolves its own `spec.params` independently -- only the values passed in `params` are available. There is no implicit inheritance of parent parameters.

Because of that, a deeply composed tree can require a value you never see at the top level. [`loom inspect`](/reference/cli-inspect) walks the whole tree and reports every parameter each module declares, where its value comes from, and which required ones nothing supplies -- without running anything.

## Conditional Modules

A child reference accepts an optional `if` predicate that decides whether the module runs. The string is rendered with the **parent's** params (like `name`, `source`, and `params`), then executed via `sh -c`: exit `0` runs the child, any non-zero exit skips it — along with all of its operations and sub-modules.

```yaml
modules:
  - name: argocd-app
    source: "./modules/argocd-app"
    if: "test -d argocd"          # skip unless the target has an argocd/ dir
  - name: prod-only
    source: "./modules/prod-only"
    if: '[ {{ .env }} = prod ]'   # params are templated before the shell runs
```

The predicate runs in the child's **resolved target directory** — its own `spec.target` clone if it has one, otherwise the parent target it inherits — so it can inspect the very repository the child would change. (A child with its own `target` is therefore cloned before its `if` is evaluated.)

The same `if` field works on individual operations; see the [operations reference](/reference/loom-yaml#conditional-execution-if). Predicates are evaluated in every mode, including `--dry-run`, so keep them side-effect-free (`test`, `grep`, file checks).

## Bulk Runs

Repeating the same child module with different params is how you run one module in bulk — and with [`loom.jsonnet`](/reference/loom-yaml#loom-jsonnet) the entries can be generated from a list instead of written by hand. See [Bulk Runs](/guide/bulk-runs) for the full patterns, including how to ship a batch as one PR or one PR per item.
