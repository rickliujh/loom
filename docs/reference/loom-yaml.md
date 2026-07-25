# loom.yaml

Every module starts with a `loom.yaml`. It follows a Kubernetes-style schema. Modules that need programmatic config can use [`loom.jsonnet`](#loom-jsonnet) instead.

## Full Example

```yaml
apiVersion: loom.rickliujh.github.io/v1beta1
kind: Loom
metadata:
  name: onboard-service
spec:
  params:
    - name: serviceName
      required: true
    - name: namespace
      default: "default"

  dynamicParams:
    - name: commitHash
      command: "git rev-parse --short HEAD"
    - name: branchLabel
      command: "echo {{ .serviceName }}-{{ .namespace }}"

  excludes:
    - __functions

  target:
    url: "https://github.com/myorg/gitops-repo.git"
    branch: "main"
    featureBranch: "loom/onboard-{{ .serviceName }}"

  modules:
    - name: base-setup
      source: "./base-module"
      params:
        namespace: "{{ .namespace }}"

  operations:
    - name: create-files
      newFiles:
        source: "."
        dest: ""

    - name: patch-app
      patch:
        engine: json6902
        path: "__functions/patches/add-app.yaml"
        target: "argocd/application.yaml"

    - name: validate
      shell:
        command: "kubeval --strict argocd/{{ .serviceName }}.yaml"
        timeout: "30s"
        pure: true

    - name: commit
      commitPush:
        message: "feat: onboard {{ .serviceName }}"
        author: "loom-bot"
        email: "loom@example.com"

    - name: open-pr
      pr:
        provider: github
        title: "Onboard {{ .serviceName }}"
        baseBranch: main
        labels: [automated]
        tokenEnv: GITHUB_TOKEN
```

## `spec.params`

Parameters are the inputs to your module. They're injected into every template — file contents, file paths, shell commands, commit messages, PR titles, and module sources.

**What is templatable:** every string field in `loom.yaml` except `spec.params` definitions (names, defaults, required — these are the source of template values). Boolean fields (e.g. `shell.pure`) are not strings and are not templatable. Specifically:

- `spec.excludes[]`, `spec.includes[]`
- `spec.dynamicParams[].command`, `spec.dynamicParams[].default`
- `spec.target.url`, `spec.target.branch`, `spec.target.featureBranch`
- `spec.modules[].name`, `spec.modules[].source`, `spec.modules[].version`, `spec.modules[].params` values
- `newFiles.source`, `newFiles.dest`
- `patch.engine`, `patch.path`, `patch.target`
- `shell.command`, `shell.timeout`
- `commitPush.message`, `commitPush.author`, `commitPush.email`
- `pr.provider`, `pr.title`, `pr.body`, `pr.baseBranch`, `pr.labels[]`, `pr.tokenEnv`
- File contents and paths processed by `newFiles`
- Patch file contents processed by `patch`

| Field | Description |
|-------|-------------|
| `name` | Parameter name, referenced as <code v-pre>{{ .name }}</code> in templates |
| `required` | If `true`, the run fails when this param is not provided |
| `default` | Fallback value when the param is not provided |

Resolution priority: **provided (`-p`) > default > required error**.

Undeclared parameters (not listed in `params` or `dynamicParams`) are rejected.

## `spec.dynamicParams`

Dynamic parameters are evaluated via shell commands **after** all regular `params` are resolved. Their commands are Go templates -- they can reference any regular param or any previously declared dynamic param.

| Field | Description |
|-------|-------------|
| `name` | Parameter name, referenced as <code v-pre>{{ .name }}</code> in templates |
| `command` | Shell command (`sh -c`) whose stdout becomes the value. Supports Go template syntax. |
| `default` | Fallback value used only if the command exits non-zero. Supports Go template syntax (rendered with already-resolved params). A successful command with empty output yields an empty value, not the default. |

```yaml
dynamicParams:
  - name: commitHash
    command: "git rev-parse --short HEAD"
  - name: timestamp
    command: "date +%s"
  - name: configPath
    command: "echo /configs/{{ .namespace }}/app.yaml"    # references a regular param
  - name: clusterRegion
    command: "kubectl config view --minify -o jsonpath='{.clusters[0].name}'"
    default: "us-east-1"   # fallback if command fails
```

Dynamic params are evaluated in declaration order, so later entries can reference earlier ones:

```yaml
dynamicParams:
  - name: branch
    command: "git rev-parse --abbrev-ref HEAD"
  - name: branchLabel
    command: "echo {{ .branch }}-{{ .namespace }}"    # references the dynamic param above
```

If a value is explicitly passed via `-p` or `--params-file`, the command is skipped entirely and a warning is logged. This means you can always override a dynamic parameter from the CLI.

## `spec.excludes` and `spec.includes`

Control which files and directories are picked up during template walking (e.g. by `newFiles`).

**Implicit excludes** -- these are always excluded by default:
- `.git` directory
- `README.md` file (case-insensitive)
- `loom.yaml` and `loom.jsonnet` config files (always excluded, cannot be overridden)

Directories that contain shell scripts or supporting files -- such as `__functions` -- are **not** implicitly excluded. You must list them explicitly in `excludes` if you don't want them copied to the target.

**`excludes`** adds additional glob patterns to skip. **`includes`** overrides any exclusion (both implicit and user-defined), letting you bring back files that would otherwise be filtered out.

Both fields accept [glob patterns](https://pkg.go.dev/path/filepath#Match) matched against file or directory names.

```yaml
spec:
  excludes:
    - __functions
    - "*.env"
  includes:
    - README.md
    - .git
```

| Field | Description |
|-------|-------------|
| `excludes` | Glob patterns for files/directories to skip during template walking |
| `includes` | Glob patterns that override excludes (including implicit ones) |

**Precedence**: `includes` > `excludes` > implicit excludes. If a file matches both an exclude and an include pattern, it is included.

Both `excludes` and `includes` are templatable.

## `spec.target`

Where the rendered files go. This field is **optional**. Loom clones this repository, creates a feature branch, writes into it, and pushes.

If omitted (and no `--target-path` is provided), Loom uses the module directory itself as the target.

::: warning
When no `target` is configured, operations like `newFiles` will write directly into the module directory, and `shell` commands will execute there. Git operations (`commitPush`, `pr`) will fail if the directory is not a Git repository. Only omit `target` for modules that don't need Git operations.
:::

| Field | Description |
|-------|-------------|
| `url` | Git repository URL (HTTPS or SSH) |
| `branch` | Base branch to clone from |
| `featureBranch` | Branch to create for changes (templated). If omitted, changes are made directly on the base branch. |

The `featureBranch` field supports templates:

```yaml
target:
  url: "https://github.com/myorg/gitops-repo.git"
  branch: "main"
  featureBranch: "loom/onboard-{{ .serviceName }}"
```

Without `featureBranch`, the head and base of a PR would be the same branch -- so if your pipeline includes a `pr` operation, you should always set `featureBranch`.

You can skip the clone entirely with `--target-path /some/local/repo` on the CLI.

## `spec.modules`

Child modules to execute before this module's operations. See [Module Composition](/guide/module-composition) for details.

| Field | Description |
|-------|-------------|
| `name` | Identifier for the child module. Templatable. |
| `source` | Path to the child module. Accepts local paths, git URLs, and git URLs with a `//subdir` separator. Templatable — rendered before source resolution. |
| `version` | Optional. Pins a git `source` to a branch, tag, or commit. Templatable. Ignored for local sources (an error to combine). Equivalent to a `?ref=` suffix on the source, which it overrides. |
| `params` | Parameters to pass down, rendered through the parent's context |
| `if` | Optional shell predicate gating the child. Templatable (parent's params). Runs via `sh -c` in the child's resolved target dir — exit `0` runs the child, non-zero skips it. See [`if`](#conditional-execution-if). |

Source formats:

| Format | Behavior |
|--------|----------|
| `./relative` or `/absolute` | Local path, resolved relative to parent module directory |
| `https://github.com/org/repo.git` | Git URL — `loom.yaml` expected at repo root |
| `https://github.com/org/repo.git//path` | Git URL — `loom.yaml` expected at `path/` within the clone |
| `https://github.com/org/repo.git//path?ref=v1.4.0` | Git URL pinned to a branch, tag, or commit — `?ref=` comes last, after any `//path`. For child modules, prefer the `version` field above. |

## `spec.operations`

The ordered list of steps. Each operation has a `name` and exactly one action type:

- [`newFiles`](/reference/op-newfiles) -- render and write templates
- [`patch`](/reference/op-patch) -- patch existing YAML files
- [`shell`](/reference/op-shell) -- run a command
- [`llm`](/reference/op-llm) -- generate or modify a file using LLM inference
- [`commitPush`](/reference/op-commitpush) -- commit and push
- [`pr`](/reference/op-pr) -- open a pull request

An operation may also carry an optional [`if`](#conditional-execution-if) predicate.

## Conditional execution (`if`)

Both operations and child modules (`spec.modules[]`) accept an optional `if` field: a shell expression that decides whether the step runs.

- **Templated first.** The string is rendered with the resolved params (an operation's own params; a child module's *parent* params), then executed via `sh -c`.
- **Shell exit code decides.** Exit `0` runs the step; any non-zero exit skips it. A non-zero exit is a normal "no", not an error — only a template error or a shell that fails to launch fails the run.
- **Omitted means always run**, so existing configs are unaffected.
- **Working directory:** an operation's `if` runs in the target dir; a child module's `if` runs in the child's resolved target dir, so it can inspect the repo the child would change.
- **Always evaluated**, including under `--dry-run` and `--local-run`, since it is control flow. Keep predicates side-effect-free (`test`, `grep`, file checks), the same expectation as `dynamicParams` commands.

```yaml
spec:
  modules:
    - name: argocd-app
      source: "./modules/argocd-app"
      if: "test -d argocd"                 # skip unless the target has argocd/
  operations:
    - name: deploy
      if: '[ {{ .env }} = prod ]'          # only in prod
      shell:
        command: "kubectl apply -f ."
```

::: warning
Param values are templated into the shell string, exactly like `shell` and `dynamicParams`. Treat untrusted params (e.g. from `bulk` provider data) with care — they are an injection vector.
:::

## `loom.jsonnet`

A module may define its config as [Jsonnet](https://jsonnet.org/) instead of YAML. Loom evaluates `loom.jsonnet` and expects the result to be a single object with the exact same schema as `loom.yaml`. A module must have exactly one config file — if both `loom.yaml` and `loom.jsonnet` are present, loading fails.

Jsonnet is useful when the config itself needs logic — the most common case is generating `spec.modules` entries from a list to run the same module in bulk:

```jsonnet
// loom.jsonnet
local services = ['payments', 'billing', 'auth'];

{
  apiVersion: 'loom.rickliujh.github.io/v1beta1',
  kind: 'Loom',
  metadata: { name: 'bulk-onboard' },
  spec: {
    modules: [
      {
        name: 'onboard-' + svc,
        source: '../onboard-service',
        params: { serviceName: svc },
      }
      for svc in services
    ],
  },
}
```

A few rules:

- **Evaluation happens before parameter resolution.** Jsonnet produces the config; params are resolved from it afterward. Jsonnet cannot see CLI params — runtime values still flow through Go template placeholders in the evaluated strings.
- **Imports work.** `import` and `importstr` resolve relative to the module directory, so shared logic can live in `.libsonnet` files next to `loom.jsonnet`. Remember to add them to `spec.excludes` if the module also uses `newFiles`.
- **Same validation.** The evaluated object passes through the same validation as a `loom.yaml`.
- `loom.jsonnet` is always excluded from template walking, same as `loom.yaml`.
