# loom generate

Generate a reusable loom module from existing changes: GitHub PRs / GitLab MRs, commits or commit ranges, or a snapshot of files in a repository. Multiple references compose into one net changeset.

```
loom generate <ref> [<ref>...] [flags]
```

## Flags

| Flag | Description |
|------|-------------|
| `-p, --param key=value` | Concrete value to parameterize (repeatable). Every occurrence of the literal value is replaced with a template expression. |
| `-o, --output dir` | Output directory for the generated module (default: current directory) |
| `-n, --name name` | Module name (default: derived from PR title or commit subject; required for snapshot-only sources) |
| `--token-env VAR` | Env var holding the API token for PR/MR sources (default: `GITHUB_TOKEN` or `GITLAB_TOKEN`) |
| `--include glob` | Files to capture from a snapshot source (repeatable; `**` matches any number of directories). Required with a snapshot ref. |
| `--exclude glob` | Files to skip from a snapshot source (repeatable) |
| `--base git-ref` | Baseline to diff a snapshot source against (default: capture matched files as-is) |

## Supported References

### PR / MR (provider API)

```
https://github.com/owner/repo/pull/123
https://gitlab.com/group/repo/-/merge_requests/123
github:owner/repo#123
gitlab:group/repo!123
```

Self-hosted instances work with full URLs; use the `github:` / `gitlab:` prefix when the URL pattern is ambiguous.

### Commit or commit range (git-native, any host)

```
github:owner/repo@abc1234                          # one commit (diff vs. its first parent)
github:owner/repo@abc1234...def5678                # range: base (exclusive) to head (inclusive)
git@bitbucket.org:owner/repo.git@abc1234...def5678 # any git URL as the repo part
./checkout@abc1234...def5678                       # local checkout, no clone
https://github.com/owner/repo/commit/abc1234       # commit URL shorthand
https://gitlab.com/group/repo/-/compare/a...b      # compare URL shorthand
```

The rev suffix is split on the **last** `@` and must be a hex SHA (7–40 chars) or tag name, or a `...` range of them. No API or token involved: remote repos are cloned bare with lazy blob fetching and your git credentials.

### Snapshot (state of files)

```
./checkout                          # working tree, incl. uncommitted/untracked files
/abs/path    file:relative/path    # same, alternative spellings
snapshot:github:owner/repo@v1.2.3   # committed tree of a remote repo at a ref
snapshot:git@host:owner/repo.git    # committed tree at the default branch
snapshot:./checkout@release-2024    # committed tree of a local repo at a ref
```

Requires at least one `--include`. At most one snapshot ref per invocation. Without `--base` every matched file becomes a template; with `--base` the captured state is diffed against that ref, producing patches for modified YAML. Local paths with `@<ref>` (and `--base`) must point at the repository root. Symlinks and submodules are skipped with a warning.

## Multiple Sources

Pass references **oldest first**; they are composed in the order given into a single net changeset:

```bash
loom generate github:org/gitops#42 github:org/gitops#47 github:org/gitops@9f3ab12
```

- Per file: old content from the first source touching it, new content from the last. Add-then-delete drops out; delete-then-re-add nets to a modification; rename chains collapse.
- All sources must reference the same repository (HTTPS/SSH spellings are equivalent).
- Metadata (title, body, branches) comes from the last source that has it.
- If everything cancels out (e.g. a PR and its revert), generation fails with `no net file changes after composing sources`.

## Generated Git Operations

| Source metadata | Result |
|-----------------|--------|
| Full PR metadata | `target` + `commitPush` + `pr` operations, as before |
| No head branch (commit/snapshot) | Feature branch synthesized as `loom/<module-name>` |
| No recognizable provider | `pr` operation omitted (warning); `commitPush` kept |
| No repository URL | `target` omitted (warning); fill in manually before running |

## Output

By default, the generated module is written to the current directory. Use `-o` to write to a different directory:

```bash
loom generate <ref> -o ./my-module
```

## How It Works

Loom fetches the changes from each source and produces a ready-to-use module:

- **Added files** become templates with <code v-pre>{{ .paramName }}</code> replacing the concrete values, including in file and folder names.
- **Modified YAML files** become strategic merge patches under `__functions/patches/`.
- **Deleted files** become `shell` operations with `rm`.
- **Renamed files** become `shell` operations with `mv`.

The generated `loom.yaml` declares all parameters as required and wires up the operations in order.

## Example

You onboarded a service called "payments" via a PR, then fixed it in a follow-up commit. Make the combined result repeatable:

```bash
loom generate https://github.com/myorg/gitops-repo/pull/42 github:myorg/gitops-repo@9f3ab12 \
  -p serviceName=payments \
  -p namespace=fintech
```

Then run the generated module with different parameters:

```bash
loom run ./onboard-service -p serviceName=billing -p namespace=platform
```

## Authentication

Only PR/MR sources use a provider API: `GITHUB_TOKEN` / `GITLAB_TOKEN` by default, with automatic fallback to an authenticated `gh` / `glab` CLI. Commit and snapshot sources are git-native and use your existing git credentials (SSH keys, credential helpers).
