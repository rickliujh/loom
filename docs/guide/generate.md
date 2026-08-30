# Generate

`loom generate` reverse-engineers a reusable module from changes that already exist — a GitHub PR or GitLab MR, specific commits, or the current state of files in a repository. This is the fastest way to turn a manual change you've already made into repeatable automation.

## How It Works

You've onboarded a service by hand — created files, edited YAML, opened a PR. Now you want to make it repeatable:

```bash
loom generate https://github.com/myorg/gitops-repo/pull/42 \
  -p serviceName=payments \
  -p namespace=fintech
```

Loom fetches the changes and produces a ready-to-use module:

- **Added files** become templates with <code v-pre>{{ .paramName }}</code> replacing the concrete values, including in file and folder names.
- **Modified YAML files** become strategic merge patches under `__functions/patches/`.
- **Deleted files** become `shell` operations with `rm`.
- **Renamed files** become `shell` operations with `mv`.

The generated `loom.yaml` declares all parameters as required and wires up the operations in order.

## Sources

Real changes don't always live in a single tidy PR. Generate accepts three kinds of sources, and they can be combined:

### Pull Requests / Merge Requests

```bash
loom generate https://github.com/myorg/gitops-repo/pull/42 ...
loom generate github:myorg/gitops-repo#42 ...
loom generate gitlab:mygroup/gitops-repo!42 ...
```

Fetched via the provider API (see [Authentication](#authentication)). PR title, body, and branches carry over into the generated `commitPush` and `pr` operations.

### Commits and Commit Ranges

Sometimes the change is a commit or a range of commits, not a PR. Commit sources are **git-native** — no provider API, no token. They work against any git host (GitHub, GitLab, Bitbucket, Gitea, bare SSH remotes) using your existing git credentials:

```bash
# one commit
loom generate github:myorg/gitops-repo@9f3ab12 ...

# a range: everything from base (exclusive) to head (inclusive)
loom generate 'github:myorg/gitops-repo@a1b2c3d...f6e5d4c' ...

# any git host — the repo part is just a git URL
loom generate 'git@bitbucket.org:myorg/gitops-repo.git@a1b2c3d...f6e5d4c' ...

# a checkout you already have (no clone)
loom generate './gitops-checkout@a1b2c3d...f6e5d4c' ...
```

Commit and compare URLs from GitHub/GitLab also work as shorthand (`https://github.com/o/r/commit/abc1234`). Remote repos are cloned bare with lazy blob fetching, so cost scales with the changed files, not the repository size.

### Snapshots (current state of files)

Sometimes there's no clean history at all — the files just *are* the desired state. A snapshot source captures files matched by `--include` globs:

```bash
# working tree of a local checkout, including uncommitted files
loom generate ./gitops-checkout --include 'services/payments/**' -n onboard-payments ...

# committed tree of a remote repo at a tag — no local checkout needed
loom generate 'snapshot:github:myorg/gitops-repo@v1.2.3' --include 'services/payments/**' -n onboard-payments ...

# diff the current state against a baseline instead of capturing everything
loom generate ./gitops-checkout --base main --include 'argocd/**' -n update-argo ...
```

Two orthogonal choices:

- **What is captured.** A bare local path captures the **working tree** — including uncommitted and untracked files (the only form that can). `snapshot:<repo>[@<ref>]` captures the **committed tree** at a ref (default branch if omitted), locally or remotely.
- **How it becomes a module.** Without `--base`, every matched file becomes a template — a *stamp-out* module. With `--base <git-ref>`, the captured state is diffed against that baseline, so modified YAML becomes strategic merge patches — a *transform* module.

Snapshots have no title to derive a name from, so `-n` is required. One snapshot source per invocation; use repeated `--include` flags to capture multiple areas.

## Combining Sources

If the desired state took a few attempts to reach, pass all of them **oldest first** — Loom composes them into one net changeset:

```bash
# the original PR plus two follow-up fixes
loom generate github:myorg/gitops#42 github:myorg/gitops#47 github:myorg/gitops#51 ...

# a PR plus the fix commit that landed after it
loom generate github:myorg/gitops#42 github:myorg/gitops@9f3ab12 ...
```

Composition works per file: a file added in one PR and modified in the next becomes a single template with the final content; add-then-delete disappears; a PR and its revert cancel out (and error, since nothing is left). All sources must reference the same repository. Metadata (title, branches) comes from the last source that has it — the one closest to the desired state.

## Example

Given a PR that added these files for the "payments" service in the "fintech" namespace:

```
src/fintech/payments-app.yaml
argocd/application-payments.yaml
```

Running:

```bash
loom generate https://github.com/myorg/gitops-repo/pull/42 \
  -p serviceName=payments \
  -p namespace=fintech
```

Produces a module like:

```
onboard-service/
├── loom.yaml
├── src/
│   └── {{ .namespace }}/
│       └── {{ .serviceName }}-app.yaml
└── argocd/
    └── application-{{ .serviceName }}.yaml
```

Every occurrence of `payments` in file contents and paths is replaced with <code v-pre>{{ .serviceName }}</code>, and every occurrence of `fintech` with <code v-pre>{{ .namespace }}</code>.

## Running the Generated Module

```bash
loom run ./onboard-service -p serviceName=billing -p namespace=platform
```

Same structure, different parameters. What took a PR now takes a command.

## Git Operations in the Generated Module

PR sources carry full metadata, so the module gets a `target`, a `commitPush`, and a `pr` operation. Sources with less metadata degrade gracefully:

- No head branch (commits, snapshots) → the feature branch is synthesized as `loom/<module-name>`.
- No recognizable provider (e.g. Bitbucket) → the `pr` operation is omitted with a warning; `commitPush` remains.
- No repository URL at all (a directory with no git remote) → `target` is omitted; fill it in before running the module.

## Authentication

Only PR/MR sources talk to a provider API. They use `GITHUB_TOKEN` / `GITLAB_TOKEN` environment variables by default; if the token is not set but the `gh` or `glab` CLI is installed and authenticated, Loom falls back to it automatically.

Commit and snapshot sources go through git directly and use whatever credentials git already has (SSH keys, credential helpers).

## Options

| Flag | Description |
|------|-------------|
| `-p, --param key=value` | Concrete value to parameterize (repeatable) |
| `-o, --output dir` | Output directory (default: current directory) |
| `-n, --name name` | Module name (default: derived from PR title or commit subject) |
| `--token-env VAR` | Env var holding the API token (PR/MR sources only) |
| `--include glob` | Files to capture from a snapshot source (repeatable; `**` matches directories) |
| `--exclude glob` | Files to skip from a snapshot source (repeatable) |
| `--base git-ref` | Baseline to diff a snapshot source against |

By default, the generated module is written to the current directory. Use `-o` to write to a different directory:

```bash
loom generate <ref> -o ./my-module
```
