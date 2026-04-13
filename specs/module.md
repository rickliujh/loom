# Loom Module — Behavioral Specification

> Version: `loom.rickliujh.github.io/v1beta1`

This document describes the expected behavior of a loom module — the core unit of execution in loom. A module is a directory containing a `loom.yaml` file that declares parameters, an optional git target, child modules, and a sequence of operations.

## loom.yaml Structure

```yaml
apiVersion: loom.rickliujh.github.io/v1beta1  # required, exact match
kind: Loom                                      # required, exact match
metadata:
  name: <string>                                # required, unique identifier
spec:
  params: [...]
  dynamicParams: [...]
  excludes: [...]
  includes: [...]
  target: {...}
  modules: [...]
  operations: [...]
```

---

## Parameters

### Inputs

| Input | Description |
|-------|-------------|
| `spec.params[]` | Static parameter definitions. The only field in `loom.yaml` that is **not** templatable — these are the source of template values. |
| `spec.dynamicParams[]` | Parameters whose values come from shell commands. |
| `--param` / `-p` | CLI flag, repeatable. Format: `key=value`. |
| `--params-file` | Path to a YAML file containing a `map[string]string`. |

### Behaviors

#### P1: Static param resolution priority

CLI `--param` / `--params-file` values take priority over `default`. If neither is provided and `required: true`, execution fails.

```yaml
# definition                     # CLI: -p env=staging
params:
  - name: env
    default: "production"
# resolved: env = "staging"      (CLI wins over default)
```

#### P2: Params-file merged with CLI params

When both `--params-file` and `--param` are provided, file values are loaded first, then CLI values override.

```yaml
# params.yaml                   # CLI: -p env=staging
region: us-east-1
env: production
# resolved: region = "us-east-1", env = "staging"
```

#### P3: Undeclared params rejected

Extra parameters provided via CLI that are not declared in `spec.params` or `spec.dynamicParams` cause an error. Every parameter must be declared before use.

#### P4: Dynamic params evaluated after static params

Dynamic params run after all static params are resolved. The `command` string is itself templatable with already-resolved params.

```yaml
params:
  - name: env
    default: "prod"
dynamicParams:
  - name: commitHash
    command: "git rev-parse --short HEAD"   # executed via sh -c
    default: "unknown"                       # fallback if command fails
```

#### P5: Dynamic params can chain

Later dynamic params can reference earlier ones via template, since they are evaluated in declaration order.

```yaml
dynamicParams:
  - name: tag
    command: "git describe --tags"
  - name: label
    command: "echo {{ .tag }}-release"    # references 'tag' resolved above
```

#### P6: CLI overrides skip dynamic evaluation (with warning)

If a dynamic param's name is provided via CLI, the command is never executed. A warning is logged to inform the user that the dynamic command was skipped due to the CLI override.

### Error Conditions

| Condition | Error |
|-----------|-------|
| Required param not provided and no default | `required parameter "<name>" not provided` |
| Empty param name | `param name cannot be empty` |
| Duplicate name across `params` and `dynamicParams` | `duplicate param name "<name>"` |
| Dynamic param missing `command` | `dynamicParam "<name>": command is required` |
| Dynamic param command fails and no default | `dynamic parameter "<name>" command failed: ...` |
| Undeclared param provided via CLI | `undeclared parameter "<name>"` |

---

## Templating

Loom uses Go's `text/template`. Params are accessed via dot notation on a `map[string]string`.

### Inputs

| Input | Description |
|-------|-------------|
| Template string | Any templatable field in `loom.yaml`, file content, or patch content. |
| Params | The resolved `map[string]string` from static + dynamic param resolution. |

### Behaviors

#### T1: Go template syntax

All templatable fields use `{{ .paramName }}` syntax.

```yaml
# params: serviceName=payments
target:
  featureBranch: "loom/onboard-{{ .serviceName }}"
# rendered: "loom/onboard-payments"
```

#### T2: Template functions

| Function  | Signature                    | Description                              |
|-----------|------------------------------|------------------------------------------|
| `default` | `default <fallback> <value>` | Returns `value` if non-empty, else `fallback` |
| `upper`   | `upper <string>`             | Converts to uppercase                    |
| `lower`   | `lower <string>`             | Converts to lowercase                    |

```yaml
# params: env="" (empty)
command: "deploy to {{ default \"production\" .env }}"
# rendered: "deploy to production"
```

#### T3: Path templates for file/directory names

File and directory names use `{{ }}` syntax by default, the same as all other templatable fields. When `{{ }}` is not supported by the filesystem or tooling, use double-underscore placeholders as a fallback:

```
__paramName__  →  {{ .paramName }}
```

Fallback pattern: `__([a-zA-Z_][a-zA-Z0-9_]*)__`

```
# params: serviceName=payments
# preferred (when filesystem supports it):
app/{{ .serviceName }}/deploy.yaml

# fallback (double-underscore):
app/__serviceName__/deploy.yaml

# both render to: app/payments/deploy.yaml
```

#### T4: What is templatable

Every string field in `loom.yaml` is templatable **except** `spec.params` definitions (names, defaults, required — these are the source of template values and cannot reference themselves). Boolean fields (`shell.pure`) are not strings and therefore not templatable.

Exhaustive list of templatable fields:

- `spec.excludes[]`, `spec.includes[]`
- `spec.dynamicParams[].command`, `spec.dynamicParams[].default`
- `spec.target.url`, `spec.target.branch`, `spec.target.featureBranch`
- `spec.modules[].name`, `spec.modules[].source`, `spec.modules[].params` values
- `newFiles.source`, `newFiles.dest`
- `patch.engine`, `patch.path`, `patch.target`
- `shell.command`, `shell.timeout`
- `commitPush.message`, `commitPush.author`, `commitPush.email`
- `pr.provider`, `pr.title`, `pr.body`, `pr.baseBranch`, `pr.labels[]`, `pr.tokenEnv`
- File contents processed by `newFiles`
- Patch file contents processed by `patch`

---

## File Filtering (Excludes / Includes)

Controls which files are walked during `newFiles` template rendering.

### Inputs

| Input | Description |
|-------|-------------|
| `spec.excludes[]` | Glob patterns for files/dirs to exclude. Matched via `filepath.Match`. |
| `spec.includes[]` | Glob patterns that override excludes (both implicit and user-defined). |

### Behaviors

#### F1: Implicit excludes

These are always applied without user configuration:

| Type       | Excluded             | Can be overridden by `includes`? |
|------------|----------------------|----------------------------------|
| Directories | `.git`              | Yes                              |
| Files       | `README.md` (case-insensitive) | Yes                  |
| Config      | `loom.yaml`, `loom.jsonnet`    | **No** (always excluded) |

#### F2: Includes override excludes

An `includes` pattern overrides both implicit and user-defined `excludes`.

```yaml
excludes:
  - "*.md"
includes:
  - "CHANGELOG.md"    # this file is included despite *.md exclude
```

#### F3: Utility directories (convention)

Loom does not reserve any directory name. By convention, `__functions` is commonly used for patch files and module utilities, but any name can be used. Utility directories are **not** auto-excluded — they must be explicitly listed in `spec.excludes` if present in the module directory and not intended as template output.

```yaml
excludes:
  - __functions       # or any name you chose for utility files
```

---

## Target (`spec.target`)

Specifies the git repository that operations act upon.

### Inputs

| Input | Description |
|-------|-------------|
| `target.url` | Git clone URL. Templatable. |
| `target.branch` | Branch to clone. Templatable. Optional (uses repo default if omitted). |
| `target.featureBranch` | New branch created on top of `branch`. Templatable. Optional. |

### Behaviors

#### TG1: Module with target spec — has git context

When `spec.target` is present, loom clones the repo and runs operations against the cloned working directory. This is a module that participates in git operations (newFiles, patch, commitPush, pr).

#### TG2: Module without target spec — no git context

When `spec.target` is omitted, the module has no git-related operations from loom's perspective. Operations run against the module directory itself (or `--target-path` if provided).

#### TG3: Clone and branch creation

1. Clone `url` at `branch` (default branch if omitted) into a working directory.
2. If `featureBranch` is set, create and checkout a new branch with that name.
3. Run operations against the cloned working directory.

#### TG4: Normal mode — temp dir with cleanup

In normal mode, the clone target is a temp directory (`loom-target-*`). It is cleaned up after execution completes (or fails).

#### TG5: Local mode — numbered subdirectory, no cleanup

In `--local-run` mode, the clone target is `<target-path>/NN-<moduleName>/`. It is **not** cleaned up — the user inspects it after the run. See Local Mode.

---

## Child Modules (`spec.modules`)

### Inputs

| Input | Description |
|-------|-------------|
| `modules[].name` | Required. Unique identifier for the child. |
| `modules[].source` | Required. Local path or git URL to the child module directory. |
| `modules[].params` | Optional. `map[string]string` of params to pass to the child. Values are templatable with the parent's params. |

### Behaviors

#### M1: Source resolution

| Source pattern | Resolution |
|---|---|
| Starts with `.` | Relative to parent module directory |
| Starts with `/` | Absolute path |
| Git URL without `//` | Cloned to temp directory, `loom.yaml` expected at repo root |
| Git URL with `//path` | Cloned to temp directory, `loom.yaml` expected at `path` within the clone |

The `source` field is templatable (see T4). Templates are rendered **before** source resolution, so `//` parsing operates on the rendered string.

The `//` separator (Terraform convention) splits a git URL into the repository URL and a subdirectory path within the cloned repo. This allows a single git repository to host multiple loom modules in different directories.

```yaml
modules:
  - name: local-child
    source: ./infra               # resolved: <parentDir>/infra
  - name: remote-child
    source: https://github.com/org/module.git  # cloned to temp dir, loom.yaml at root
  - name: remote-subdir
    source: https://github.com/org/modules.git//networking  # cloned to temp dir, loom.yaml at networking/
  - name: templated-source
    source: "https://github.com/{{ .org }}/modules.git//{{ .modulePath }}"  # templated, then // parsed
    params:
      env: "{{ .environment }}"
```

##### M1a: Subdirectory modules share clone context

When a git URL contains `//`, the entire repository is cloned (not sparse checkout). This means modules within the same repo can reference sibling modules using relative local paths (e.g., `source: "../other-module"`), and those paths resolve correctly because the full repo is on disk.

```yaml
# repo layout:
#   modules-repo/
#     networking/loom.yaml    ← source: https://github.com/org/modules-repo.git//networking
#     monitoring/loom.yaml
#
# inside networking/loom.yaml:
modules:
  - name: monitoring
    source: ../monitoring       # resolves to <cloneDir>/monitoring — works because full repo cloned
```

#### M2: Execution order — declaration order, children before parent operations

Child modules execute before the parent's operations, in the order they are written in `loom.yaml`. The full execution sequence for a module is:

1. For each child module (in declaration order):
   a. Resolve child source.
   b. Render child params through parent template context.
   c. Load child module.
   d. Resolve child target.
   e. Execute child module recursively.
2. For each parent operation (in declaration order):
   a. Execute operation against the parent's target directory.

#### M3: Target independence

Each child module respects its own `spec.target`. A child **never** inherits the parent's target. If a child has no target spec, it falls back to the parent's target directory (for non-git operations).

```yaml
# parent loom.yaml
spec:
  target:
    url: https://github.com/org/parent-repo.git   # parent's target
  modules:
    - name: child-a
      source: ./child-a
      # child-a's own loom.yaml has target: url: https://github.com/org/other-repo.git
      # child-a operates on other-repo, NOT parent-repo
```

#### M4: Param passing through parent template context

Child `params` values are rendered through the parent's resolved params before being passed to the child.

```yaml
# parent params: environment=staging
modules:
  - name: deploy
    source: ./deploy
    params:
      env: "{{ .environment }}"    # rendered to "staging" before passing to child
      region: us-east-1             # literal value
```

---

## Operations (`spec.operations`)

Operations execute sequentially in declaration order. Each operation has exactly **one** action type.

### `newFiles` — Render and Write Template Files

#### Inputs

| Input | Description |
|-------|-------------|
| `newFiles.source` | Directory path relative to module dir. Templatable. |
| `newFiles.dest` | Directory path relative to target dir. Templatable. Optional (defaults to target root). |

#### Behaviors

##### NF1: Template file walking

Walks the `source` directory recursively, applying exclude/include filters (see File Filtering). For each non-excluded file:

1. Read file content.
2. Render content through Go templates with params (both `spec.params` and `spec.dynamicParams` after resolution).
3. Render the file/directory path through Go templates with params. Paths use `{{ }}` syntax by default; double-underscore placeholders (`__paramName__`) are converted to `{{ .paramName }}` as a fallback (see T3).
4. Write to `<targetDir>/<dest>/<renderedPath>`.

##### NF2: Fails on existing destination

If a destination file already exists, the operation fails immediately. No overwrite.

```
# target dir already has: services/payments/deploy.yaml
# newFiles tries to write: services/payments/deploy.yaml
# result: error — "destination file already exists: services/payments/deploy.yaml"
```

##### NF3: Directory merge behavior

If a destination directory already exists, files are merged into it — new files are written alongside existing ones. NF2 still applies: if any individual file within the directory already exists, the operation fails.

##### NF4: Directory creation

If a destination directory does not exist, it is created automatically (including intermediate directories).

##### NF5: Dry-run behavior

Logs what would be written. Warns (but does not fail) if destination file already exists.

##### NF6: Diff behavior

Shows unified diff of the rendered content vs empty (new file) for each file.

#### Error Conditions

| Condition | Error |
|-----------|-------|
| Source directory does not exist | walk error |
| Template syntax error in file content | `rendering patch file: parsing template: ...` |
| Destination file already exists | `destination file already exists: <path>` |

---

### `patch` — Patch YAML Files

#### Inputs

| Input | Description |
|-------|-------------|
| `patch.engine` | `"smp"` (default) or `"json6902"`. |
| `patch.path` | Patch file path, relative to module dir. |
| `patch.target` | Target file path, relative to target dir. |

Patch file contents are templated with params before application.

#### Strategic Merge Patch (`smp`, default)

Deep-merges a partial YAML document into the target using kustomize's `merge2` library. For full behavioral specification including merge semantics, scalar list handling, and end-to-end examples, see [`specs/smp.md`](smp.md).

Summary of merge behaviors (B1–B8 in smp.md):
- **B1**: Scalar fields set/overwritten.
- **B2**: Absent fields preserved.
- **B3**: Nested maps deep-merged recursively.
- **B4**: Scalar lists append-unique (deduped).
- **B5**: Map-lists merged by inferred key.
- **B6**: New fields added.
- **B7**: Template rendering before merge.
- **B8**: Expand scalar lists error propagation (no silent fallback).

#### JSON 6902 Patch (`json6902`)

Applies RFC 6902 JSON Patch operations using kustomize's `patchjson6902` filter.

##### J1: Operation list

Patch file is a YAML list of operations: `add`, `remove`, `replace`, `move`, `copy`, `test`.

```yaml
- op: add
  path: /metadata/labels/managed-by
  value: loom
- op: replace
  path: /spec/replicas
  value: 3
```

##### J2: Template rendering before application

Same as B7 — patch content is templated before being applied.

#### Error Conditions

| Condition | Error |
|-----------|-------|
| Patch file does not exist | `reading patch file "<path>": ...` |
| Invalid Go template syntax in patch | `rendering patch file "<path>": ...` |
| Target file does not exist | `reading target file "<path>": ...` |
| Malformed YAML in patch or target (expand phase) | `expanding scalar lists: ...` |
| merge2 fails (e.g., type conflict) | `strategic merge patch failed: ...` |
| json6902 produces no output | `json6902 patch produced no output` |
| Writing result fails | `writing patched file "<path>": ...` |
| Unknown engine value | `unknown patch engine "<engine>" (supported: smp, json6902)` |

#### Dry-run and Diff Behavior

- **Dry-run**: logs what would be patched but does not read or modify the target file.
- **Diff**: computes the patched result in-memory and shows a unified diff against the original target.

---

### `shell` — Run Shell Command

#### Inputs

| Input | Description |
|-------|-------------|
| `shell.command` | Shell command string. Templatable. Executed via `sh -c`. |
| `shell.timeout` | Optional. Go duration format (e.g. `"30s"`, `"5m"`). Creates a context deadline. |
| `shell.local` | Optional. Default `false`. When `true`, this command runs even in `--local-run` mode. |

#### Behaviors

##### S1: Executed in target directory

The command runs with its working directory set to the target directory.

##### S2: Templated before execution

The `command` string is rendered with params before execution.

```yaml
# params: serviceName=payments
shell:
  command: "echo validating {{ .serviceName }}"
# executed: sh -c "echo validating payments"
```

##### S3: Local mode — skipped unless marked pure

In `--local-run` mode, shell commands are skipped by default. Only commands with `pure: true` execute. A pure command has no external side effects — it only reads or modifies local files (e.g., linting, formatting). This prevents remote-only commands (deploy scripts, API calls) from running during local preview.

```yaml
operations:
  - name: validate
    shell:
      command: "yamllint ."
      pure: true               # no side effects, runs in --local-run mode
  - name: deploy
    shell:
      command: "kubectl apply -f ."
                                # skipped in --local-run mode
```

##### S4: Dry-run — logged, not executed

##### S5: Timeout creates context deadline

If `timeout` is set and the command exceeds the duration, it is killed via context cancellation.

#### Error Conditions

| Condition | Error |
|-----------|-------|
| Command exits non-zero | `command failed: ... output: ...` |
| Invalid timeout format | `invalid timeout "<value>": ...` |

---

### `commitPush` — Commit and Push Changes

#### Inputs

| Input | Description |
|-------|-------------|
| `commitPush.message` | Commit message. Templatable. |
| `commitPush.author` | Optional. Git author name. |
| `commitPush.email` | Optional. Git author email. |
| `--author` | Optional. CLI flag. Default git author name when `commitPush.author` is not set. |
| `--email` | Optional. CLI flag. Default git author email when `commitPush.email` is not set. |

#### Behaviors

##### CP1: Stage and commit

Stages all changes (`git add -A`) and creates a commit with the rendered message, author, and email.

##### CP1a: Author/email resolution priority

Author and email are resolved independently, each following this priority:

1. `commitPush.author` / `commitPush.email` in `loom.yaml` (highest).
2. `--author` / `--email` CLI flags.
3. System git config (`user.name` / `user.email`) (lowest).

```yaml
# loom.yaml has author but no email:
commitPush:
  message: "update"
  author: "yaml-bot"
# CLI: --email bot@example.com
# resolved: author = "yaml-bot" (from loom.yaml), email = "bot@example.com" (from CLI)
```

##### CP2: Push to remote

Pushes to the remote. Uses the `LOOM_GIT_TOKEN` environment variable for authentication.

##### CP3: Local mode — commit only, no push

In `--local-run` mode, changes are committed but not pushed. The commit is visible in the local clone for inspection.

##### CP4: Dry-run — logged, not executed

#### Error Conditions

| Condition | Error |
|-----------|-------|
| Target dir is not a git repo | `opening repo at <path>: repository does not exist` |
| Commit fails | `commitPush: ...` |
| Push fails | `commitPush: ...` |

---

### `pr` — Open Pull Request

#### Inputs

| Input | Description |
|-------|-------------|
| `pr.provider` | Required. `"github"` (more providers planned). |
| `pr.title` | PR title. Templatable. |
| `pr.body` | PR body. Templatable. Optional. |
| `pr.baseBranch` | Target branch for the PR. Optional, defaults to `"main"`. |
| `pr.labels` | List of label strings. Optional. |
| `pr.tokenEnv` | Name of the environment variable containing the auth token. |

#### Behaviors

##### PR1: Creates PR via provider API

Reads the current branch and remote URL from the target repo, then creates a PR via the provider API.

##### PR2: Local mode — entirely skipped

In `--local-run` mode, the PR action is skipped entirely. Logged but no API call is made.

##### PR3: Dry-run — logged, not executed

---

## CLI Flags

### Global Flags (available on all commands)

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--verbose`, `-v` | bool | `false` | Enable verbose output (sets log level to debug) |
| `--dry-run` | bool | `false` | Simulate operations without making changes |
| `--local-run` | bool | `false` | Run locally: skip push and PR, clone into `--target-path` |
| `--diff` | bool | `false` | Show file diffs during dry-run (implies `--dry-run`) |
| `--log-level` | string | `"info"` | Log level: `debug`, `info`, `warn`, `error` |
| `--log-format` | string | `"pretty"` | Log format: `pretty`, `text`, `json` |

### `run` Command Flags

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--param`, `-p` | string[] | nil | Parameter in `key=value` format (repeatable) |
| `--params-file` | string | `""` | YAML file with parameters |
| `--target-path` | string | `""` | Base directory for local mode output |
| `--author` | string | `""` | Default git author name for `commitPush` operations |
| `--email` | string | `""` | Default git author email for `commitPush` operations |

---

## Local Mode (`--local-run`)

Local mode lets users preview the results of a loom run without pushing to any remote.

### Inputs

| Input | Description |
|-------|-------------|
| `--local-run` | Enables local mode. |
| `--target-path` | Required when `--local-run` is set. Base directory for cloned repos. |

### Behaviors

#### L1: Requires `--target-path`

`--local-run` without `--target-path` fails immediately.

```
Error: --local-run requires --target-path: provide a local directory to write results into
```

#### L2: Modules with target spec — clone into numbered subdirectory

Each module that has a `spec.target` clones its target repo into a numbered subdirectory under `--target-path`:

```
target-path/
  00-parent-module/     # cloned from parent's spec.target.url
  01-child-module-a/    # cloned from child A's spec.target.url
  02-child-module-b/    # cloned from child B's spec.target.url
```

The numbered prefix (`NN-`) reflects **execution order** (declaration order in `loom.yaml`). Directories are **not cleaned up** — the user inspects them after the run.

#### L3: Modules without target spec — unaffected

Modules without a `spec.target` are not git-related. They run against the module directory (or `--target-path` directly if provided). No numbered subdirectory is created.

#### L4: Operation behavior in local mode

| Operation | Behavior |
|-----------|----------|
| `newFiles` | Writes to local clone as normal |
| `patch` | Patches local clone as normal |
| `shell` | **Skipped** unless `pure: true` |
| `commitPush` | Commits locally, **no push** |
| `pr` | **Skipped entirely** |

#### L5: Inspecting results

Each numbered subdirectory is a full git repo clone. Users can:
- `git diff` / `git log` to see changes made by operations
- Inspect files directly
- Verify the commit that would be pushed
- Compare against the base branch

---

## Dry-Run Mode (`--dry-run`)

### Behaviors

#### DR1: Simulates all operations

No files are written, no commits are made, no PRs are opened. Each operation logs what it would do.

| Operation | Dry-run behavior |
|-----------|-----------------|
| `newFiles` | Logs files that would be written. Warns on existing destinations. |
| `patch` | Logs patches that would be applied. |
| `shell` | Logs commands that would be executed. |
| `commitPush` | Logs the commit that would be made. |
| `pr` | Logs the PR that would be opened. |

#### DR2: `--diff` implies `--dry-run`

When `--diff` is set, `--dry-run` is automatically enabled.

#### DR3: Diff shows unified diffs

In addition to dry-run logging:
- `newFiles`: unified diff of rendered content vs empty (new file).
- `patch`: unified diff of patched result vs original target file.

---

## Validation Rules

The following constraints are enforced when loading a module:

| Rule | Error |
|------|-------|
| `apiVersion` must be `loom.rickliujh.github.io/v1beta1` | `unsupported apiVersion "<value>"` |
| `kind` must be `Loom` | `unsupported kind "<value>"` |
| `metadata.name` required | `metadata.name is required` |
| Param names non-empty | `param name cannot be empty` |
| Param names unique across `params` and `dynamicParams` | `duplicate param name "<name>"` |
| Dynamic param `command` required | `dynamicParam "<name>": command is required` |
| Operation names non-empty | `operation name cannot be empty` |
| Operation names unique | `duplicate operation name "<name>"` |
| Each operation has exactly one action type | `operation "<name>" must have exactly one action type, got <N>` |
| Patch engine is `smp` or `json6902` | `unknown patch engine "<engine>"` |
| Undeclared CLI param not in `params` or `dynamicParams` | `undeclared parameter "<name>"` |

---

## Execution Flow

```
loom run <moduleSource> [flags]

 1. Parse CLI flags and parameters.
 1a. If moduleSource contains "//", split into git URL and subdirectory.
 2. Load loom.yaml from moduleDir (or cloneDir/subdir if git source with //).
 3. Validate config structure.
 4. Resolve static params (CLI > params-file > default > required error).
 5. Resolve dynamic params in declaration order
    (CLI override > command eval > default fallback).
 6. Resolve target directory:
    a. Module has target spec, normal mode  → clone to temp dir.
    b. Module has target spec, local mode   → clone to <target-path>/NN-<name>/.
    c. No target spec, --target-path given  → use target-path directly.
    d. No target spec, no --target-path     → use moduleDir.
 7. For each child module (in declaration order):
    a. Resolve child source (local path, git clone, or git clone with // subdir).
    b. Render child params through parent template context.
    c. Load child module (validate, resolve params).
    d. Resolve child target (same logic as step 6).
    e. Execute child module recursively (from step 7).
 8. For each operation (in declaration order):
    a. Create action from operation config.
    b. Execute action against the resolved target directory.
 9. Cleanup:
    - Normal mode: remove temp dirs.
    - Local mode: no cleanup (dirs persist for inspection).
```

---

## End-to-End Example

Given this module structure:

```
my-module/
  loom.yaml
  templates/
    __serviceName__/
      deployment.yaml
  __functions/
    patches/
      add-app.yaml
```

**loom.yaml**:
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
  excludes:
    - __functions
  target:
    url: "https://github.com/myorg/gitops-repo.git"
    branch: "main"
    featureBranch: "loom/onboard-{{ .serviceName }}"
  operations:
    - name: create-files
      newFiles:
        source: "templates"
        dest: ""
    - name: patch-app
      patch:
        path: "__functions/patches/add-app.yaml"
        target: "argocd/application.yaml"
    - name: validate
      shell:
        command: "yamllint ."
        pure: true
    - name: commit
      commitPush:
        message: "feat: onboard {{ .serviceName }} ({{ .commitHash }})"
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

### Normal mode

```bash
loom run my-module -p serviceName=payments
```

**What happens:**
1. Params resolved: `serviceName=payments`, `namespace=default`, `commitHash=abc1234`.
2. Clone `https://github.com/myorg/gitops-repo.git` at `main` to temp dir.
3. Create branch `loom/onboard-payments`.
4. `create-files`: render `templates/__serviceName__/deployment.yaml` → write `payments/deployment.yaml` in target.
5. `patch-app`: apply SMP patch from `__functions/patches/add-app.yaml` to `argocd/application.yaml`.
6. `validate`: run `yamllint .` in target dir.
7. `commit`: stage all, commit `"feat: onboard payments (abc1234)"`, push.
8. `open-pr`: create GitHub PR "Onboard payments" against `main`.
9. Cleanup temp dir.

### Local mode

```bash
loom run my-module -p serviceName=payments --local-run --target-path ./preview
```

**What happens:**
1. Same param resolution.
2. Clone to `./preview/00-onboard-service/` (not temp dir, not cleaned up).
3. Create branch `loom/onboard-payments`.
4. `create-files`: writes to `./preview/00-onboard-service/`.
5. `patch-app`: patches in `./preview/00-onboard-service/`.
6. `validate`: runs `yamllint .` (marked `pure: true`).
7. `commit`: commits locally, **no push**.
8. `open-pr`: **skipped**.
9. User inspects `./preview/00-onboard-service/` with `git log`, `git diff`, etc.
