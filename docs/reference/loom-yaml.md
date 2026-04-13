# loom.yaml

Every module starts with a `loom.yaml`. It follows a Kubernetes-style schema.

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
- `spec.modules[].name`, `spec.modules[].source`, `spec.modules[].params` values
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
| `default` | Fallback value if the command fails |

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
| `source` | Path to the child module. Accepts local paths, git URLs, or git URLs with `//subdir` separator. Templatable — rendered before source resolution. |
| `params` | Parameters to pass down, rendered through the parent's context |

Source formats:

| Format | Behavior |
|--------|----------|
| `./relative` or `/absolute` | Local path, resolved relative to parent module directory |
| `https://github.com/org/repo.git` | Git URL — `loom.yaml` expected at repo root |
| `https://github.com/org/repo.git//path` | Git URL — `loom.yaml` expected at `path/` within the clone |

## `spec.operations`

The ordered list of steps. Each operation has a `name` and exactly one action type:

- [`newFiles`](/reference/op-newfiles) -- render and write templates
- [`patch`](/reference/op-patch) -- patch existing YAML files
- [`shell`](/reference/op-shell) -- run a command
- [`commitPush`](/reference/op-commitpush) -- commit and push
- [`pr`](/reference/op-pr) -- open a pull request
