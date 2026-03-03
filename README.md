# Loom

Loom automates the last mile of your GitOps.

You've adopted GitOps. Your applications deploy through Git. But every time you onboard a new service, add a new environment, or wire up a new team, you find yourself doing the same thing: copying YAML files, editing five fields, opening a PR, and moving on. It's not hard work. It's just tedious, error-prone, and never worth building a whole internal tool for.

Loom sits in that gap. You describe the repetitive part once as a **module** — a folder of templates and a `loom.yaml` file that declares what to do with them — and then you run it whenever you need it. Loom renders your templates, writes them into a target Git repository, commits, pushes, and opens a pull request. One command, done.

```
loom run ./onboard-service -p serviceName=payments -p namespace=fintech
```

No scripting. No custom CI jobs. No imperative glue code. Just a declarative workflow that turns parameters into a pull request.

## The Problem Loom Solves

GitOps repositories accumulate operational patterns. Onboarding a service means creating an ArgoCD Application, an AppProject, a Gatekeeper constraint, and a kustomization entry — every time. Teams handle this in different ways:

- **Copy-paste**: fast, but drifts. Someone forgets a label, uses the wrong namespace, or misses the constraint file entirely.
- **Shell scripts**: better, but fragile. They grow organically, have poor error handling, and nobody wants to maintain them.
- **Internal platforms**: correct, but expensive. Most teams can't justify building and maintaining a self-service portal for what amounts to templating and a `git push`.

Loom gives you the structure of a platform without the cost. Your automation is version-controlled, composable, and runs from a single binary with no runtime dependencies.

## How It Works

A Loom module is a directory. Inside it, you put:

1. **`loom.yaml`** — the workflow definition. It declares parameters, operations, and optionally references child modules.
2. **Template files** — any files alongside `loom.yaml` are treated as Go templates. Their directory structure mirrors where they'll land in the target repository.
3. **`__functions/`** — a reserved directory for patches, configs, and supporting files that should _not_ be copied to the target.

```
onboard-service/
├── loom.yaml
├── argocd/
│   ├── application.yaml      <- template, rendered with params
│   └── project.yaml          <- template, rendered with params
├── cluster/
│   └── constraints/
│       └── pod-must-have-label.yaml
└── __functions/
    └── patches/
        └── add-app.yaml      <- used by patch operations, not copied
```

When you run `loom run ./onboard-service -p serviceName=payments`, Loom:

1. Loads `loom.yaml` and resolves parameters
2. Clones the target Git repository (or uses a local path you specify)
3. Creates a feature branch if `featureBranch` is configured
4. Walks through operations in order — rendering templates, running shell commands, committing, pushing, opening a PR
5. Reports what it did

With `--dry-run`, nothing is written, committed, or pushed. Loom just shows you what _would_ happen.

## The `loom.yaml` File

Every module starts with a `loom.yaml`. It follows a Kubernetes-style schema:

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

    - name: patch-kustomize
      patch:
        engine: kustomize
        path: "__functions/patches/add-app.yaml"
        target: "kustomization.yaml"

    - name: validate
      shell:
        command: "kubeval --strict argocd/{{ .serviceName }}.yaml"
        timeout: "30s"

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

### `spec.params`

Parameters are the inputs to your module. They're injected into every template — file contents, file paths, shell commands, commit messages, PR titles. Everything is templatable.

| Field | Description |
|-------|-------------|
| `name` | Parameter name, referenced as `{{ .name }}` in templates |
| `required` | If `true`, the run fails when this param is not provided |
| `default` | Fallback value when the param is not provided |

### `spec.target`

Where the rendered files go. Loom clones this repository, creates a feature branch, writes into it, and pushes.

| Field | Description |
|-------|-------------|
| `url` | Git repository URL (HTTPS or SSH) |
| `branch` | Base branch to clone from |
| `featureBranch` | Branch to create for changes (templated). If omitted, changes are made directly on the base branch. |

The `featureBranch` field supports templates, so you can generate unique branch names per run:

```yaml
target:
  url: "https://github.com/myorg/gitops-repo.git"
  branch: "main"
  featureBranch: "loom/onboard-{{ .serviceName }}"
```

This is the intended workflow: Loom clones the base branch, creates `loom/onboard-payments` from it, runs all operations on the feature branch, commits, pushes, then opens a PR from the feature branch back to the base branch. Without `featureBranch`, the head and base of a PR would be the same branch — so if your pipeline includes a `pr` operation, you should always set `featureBranch`.

You can skip the clone entirely with `--target-path /some/local/repo` on the CLI.

### `spec.modules`

Child modules to execute before this module's operations. This is how you compose workflows — a parent module orchestrates several smaller ones.

| Field | Description |
|-------|-------------|
| `name` | Identifier for the child module |
| `source` | Path to the child module — local (`./sub-module`) or a Git URL |
| `params` | Parameters to pass down, rendered through the parent's context |

Child modules execute first, in order. Then the parent's operations run. This lets you build layered workflows: a base module that creates the namespace, a service module that creates the ArgoCD app, a policy module that adds the Gatekeeper constraint — all composed from a single root module.

### `spec.operations`

The ordered list of steps. Each operation has a `name` and exactly one action type.

## Operations Reference

### `newFiles` — Render and Write Templates

Copies template files from the module directory into the target repository, rendering Go template expressions along the way.

```yaml
- name: create-files
  newFiles:
    source: "."    # relative to module directory
    dest: ""       # relative to target repository root
```

Every file in the source directory (except `loom.yaml`, `loom.jsonnet`, and anything under `__functions/`) is treated as a Go template. The directory structure is preserved. File paths themselves can also contain template expressions.

### `patch` — Patch Existing Files

Applies a kustomize patch to a file already in the target repository.

```yaml
- name: patch-kustomize
  patch:
    engine: kustomize
    path: "__functions/patches/add-app.yaml"
    target: "kustomization.yaml"
```

This shells out to the `kustomize` binary, so it must be installed on the machine running Loom. The `__functions/` directory is the conventional place for patch files.

### `shell` — Run a Command

Runs an arbitrary shell command in the target repository directory.

```yaml
- name: validate
  shell:
    command: "kubeval --strict argocd/{{ .serviceName }}.yaml"
    timeout: "30s"
```

The command is rendered as a template, so you can inject parameters. The working directory is the target repository. If the command fails, Loom stops.

### `commitPush` — Commit and Push

Stages all changes, creates a commit, and pushes to the remote.

```yaml
- name: commit
  commitPush:
    message: "feat: onboard {{ .serviceName }}"
    author: "loom-bot"
    email: "loom@example.com"
```

Push authentication uses the `LOOM_GIT_TOKEN` environment variable when using the Go library. If the library push fails, Loom falls back to the system `git` binary, which uses your existing credential helpers and SSH configuration.

### `pr` — Open a Pull Request

Opens a pull request on the target repository.

```yaml
- name: open-pr
  pr:
    provider: github
    title: "Onboard {{ .serviceName }}"
    body: "Automated onboarding for {{ .serviceName }}"
    baseBranch: main
    labels: [automated]
    tokenEnv: GITHUB_TOKEN
```

| Field | Description |
|-------|-------------|
| `provider` | `github` or `gitlab` |
| `title` | PR/MR title, templated |
| `body` | PR/MR description, templated |
| `baseBranch` | Branch to merge into (default: `main`) |
| `labels` | Labels to apply |
| `tokenEnv` | Name of the environment variable holding the API token |

Both GitHub and GitLab are fully supported. For GitLab, the same schema applies — Loom creates a merge request instead of a pull request:

```yaml
- name: open-mr
  pr:
    provider: gitlab
    title: "Onboard {{ .serviceName }}"
    baseBranch: main
    labels: [automated]
    tokenEnv: GITLAB_TOKEN
```

GitLab URLs are parsed automatically, including self-hosted instances and SSH URLs (`git@gitlab.example.com:group/repo.git`).

## Templates

Loom uses Go's `text/template` syntax. Inside any templatable string, you can reference parameters with `{{ .paramName }}`.

Available functions:

| Function | Example | Result |
|----------|---------|--------|
| Parameter access | `{{ .serviceName }}` | `payments` |
| Default value | `{{ default "prod" .env }}` | `prod` if `.env` is empty |
| Uppercase | `{{ upper .serviceName }}` | `PAYMENTS` |
| Lowercase | `{{ lower .serviceName }}` | `payments` |

Templates work in:
- File contents (newFiles)
- File paths (newFiles)
- Shell commands
- Commit messages
- PR/MR titles and bodies
- Feature branch names
- Child module parameters

## Module Composition

Modules can reference other modules. This is Loom's answer to the question: "how do I reuse automation across teams?"

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

Execution order:
1. Child modules run first, in the order they're listed
2. Each child module can have its own child modules (recursive)
3. Then the parent's operations run
4. All modules write into the same target directory

Sources can be:
- **Local paths** (`./relative/path` or `/absolute/path`) — resolved relative to the parent module
- **Git URLs** — cloned to a temporary directory automatically

This means you can publish reusable modules as Git repositories. A platform team maintains the standard modules; product teams compose them.

## CLI

### `loom run`

Execute a module.

```
loom run [path] [flags]
```

| Flag | Description |
|------|-------------|
| `-p, --param key=value` | Set a parameter (repeatable) |
| `--params-file file.yaml` | Load parameters from a YAML file |
| `--target-path /path` | Use a local directory as the target (skip git clone) |
| `--dry-run` | Show what would happen without writing anything |
| `-v, --verbose` | Enable debug logging |
| `--log-level level` | Set log level: `debug`, `info`, `warn`, `error` |

```bash
# Full run against a remote repo
loom run ./onboard-service -p serviceName=payments

# Dry run against a local checkout
loom run ./onboard-service \
  -p serviceName=payments \
  --target-path ~/repos/gitops \
  --dry-run

# Parameters from file
loom run ./onboard-service --params-file params.yaml
```

### `loom validate`

Check that a `loom.yaml` is well-formed.

```
loom validate [path]
```

Validates: `apiVersion`, `kind`, required metadata, unique parameter names, unique operation names, and that each operation has exactly one action type.

### `loom version`

Print the version.

```
loom version
```

## Installation

```bash
go install github.com/rickliujh/loom@latest
```

Or build from source:

```bash
git clone https://github.com/rickliujh/loom.git
cd loom
go build -o loom .
```

Loom is a single static binary. It embeds Go libraries for Git and PR/MR creation so it can run with zero external dependencies. When those libraries hit an edge case — an SSH agent configuration go-git doesn't handle, or a credential helper it can't talk to — Loom automatically falls back to the CLI tools already on your machine (`git`, `gh`, `glab`, `kustomize`).

On a DevOps laptop where these tools are already authenticated and configured, Loom just works.

## Design Philosophy

**Declarative over imperative.** You describe _what_ should happen, not _how_. Loom handles the mechanics of cloning, rendering, committing, and pushing. Your module is a specification, not a script.

**Files as the interface.** A module is a folder. Templates are just files with Go template syntax. The directory structure _is_ the destination structure. There's no abstraction layer between what you write and what lands in the repository.

**Composable by default.** Modules reference other modules. Parameters flow down. You build small, focused modules and combine them. The same module that onboards one service onboards a hundred — you just change the parameters.

**Ordered operations, flat list.** Operations execute top to bottom. There's no DAG, no dependency graph, no parallel execution. This is intentional. GitOps workflows are inherently sequential: render files, then validate, then commit, then open a PR. A flat list is easy to read, easy to debug, and hard to get wrong.

**Template everywhere.** Every string in operations — commands, commit messages, PR titles, file paths — is a Go template. You never have to switch between "static" and "dynamic" configuration. It's all dynamic, all the time.

**Library first, CLI fallback.** Loom embeds [go-git](https://github.com/go-git/go-git) for Git operations and uses the GitHub/GitLab APIs for PR creation — no external binaries needed in CI or containers. But on a developer's laptop, the system `git`, `gh`, and `glab` are already authenticated and configured. When the library path fails (auth issues, unsupported SSH configs, token not set), Loom detects the local binary and retries through it. You get the portability of a self-contained binary and the compatibility of native tooling, without choosing between them.

| Operation | Library (primary) | CLI fallback |
|-----------|------------------|-------------|
| Clone, push, branch, commit | go-git | `git` |
| GitHub PR | go-github API | `gh` |
| GitLab MR | go-gitlab API | `glab` |
| Kustomize patch | — | `kustomize` |

## Architecture

```
loom run ./module -p key=val
        │
        ▼
┌─────────────────┐
│   Config Loader  │  Parse loom.yaml, validate schema
└────────┬────────┘
         ▼
┌─────────────────┐
│  Module Loader   │  Resolve params (provided + defaults)
└────────┬────────┘
         ▼
┌─────────────────┐
│  Clone + Branch  │  Clone target repo, create feature branch
└────────┬────────┘
         ▼
┌─────────────────┐
│    Executor      │  Walk child modules (recursive), then operations
└────────┬────────┘
         ▼
┌─────────────────┐
│  Action Dispatch │  Route each operation to its handler
└────────┬────────┘
         ▼
┌────┬────┬────┬────┬────┐
│ New │Pch │Shl │ CP │ PR │  Action implementations
│Files│    │    │    │    │
└────┴────┴────┴────┴────┘
         │
         ▼
┌─────────────────┐
│  Git / Provider  │  Library-first, CLI fallback
│  go-git → git    │
│  go-github → gh  │
│  go-gitlab → glab│
└─────────────────┘
```

Each action implements a single interface:

```go
type Action interface {
    Execute(ctx context.Context, execCtx *ExecutionContext) error
}
```

Adding a new operation type means implementing this interface and registering it — nothing else changes.

## License

[Apache 2.0](LICENSE)
