# Generate Sources — Design Proposal (Draft)

Status: **draft / not implemented**. Extends [`specs/generate.md`](generate.md).

## Problem

`loom generate` accepts exactly one PR/MR. In practice the desired end state is often
reached through:

1. **Several PRs** — the first attempt plus follow-up fixes.
2. **Specific commits** — a single commit or a commit range, not tied to any PR.
3. **Current state of files** — the repo already looks right; no clean PR/commit
   history exists to point at.

The module builder should be able to consume any of these, alone or combined.

## Core insight

Everything downstream of `FetchDiff` — classification, SMP computation,
parameterization, emission — only consumes `PRInfo` (metadata + `[]FileChange`
with old/new content). The design therefore:

1. Generalizes the source abstraction: `DiffProvider` → `ChangeSource`, `PRInfo` → `ChangeSet`.
2. Adds two new source kinds (commits, local snapshot).
3. Adds one new step: **composition** of multiple `ChangeSet`s into a single net
   `ChangeSet`, which then flows through the existing pipeline unchanged.

```
refs ──► [ChangeSource]* ──► [ChangeSet]* ──► Compose ──► ChangeSet ──► buildModule (unchanged)
```

## Source abstraction

```go
// ChangeSet generalizes PRInfo. All fields except Files are optional;
// availability depends on the source kind.
type ChangeSet struct {
    Title, Body            string
    BaseBranch, HeadBranch string
    RepoURL                string
    Provider               string // "github" | "gitlab" | "" (local)
    Files                  []FileChange
}

type ChangeSource interface {
    Fetch(ctx context.Context, token string, logger *slog.Logger) (*ChangeSet, error)
}
```

`ParsePRRef` becomes `ParseSourceRef(ref) (provider string, ChangeSource, error)`
and recognizes the grammars below.

## Reference grammars

| Form | Kind | Example |
|------|------|---------|
| existing PR/MR forms | PR source | `github:owner/repo#123` |
| `<repo>@<sha>` | single commit | `git@bitbucket.org:o/r.git@abc1234` |
| `<repo>@<base>...<head>` | commit range | `github:o/r@abc1234...def5678` |
| commit URL (sugar) | single commit | `https://github.com/o/r/commit/abc1234` |
| compare URL (sugar) | commit range | `https://gitlab.com/g/r/-/compare/abc...def` |
| local path (`./…`, `/…`, or `file:` prefix) | snapshot | `./checkout-of-target-repo` |

`<repo>` in commit refs is any of:
- a short-form repo (`github:o/r`, `gitlab:g/r`) — sugar that expands to the
  canonical SSH URL for that host;
- **any git URL** (`https://…`, `git@host:…`, `ssh://…`) — this is what makes
  the commit source platform-agnostic;
- a local path to an existing checkout (no clone needed).

The rev part is split on the **last** `@` in the ref (SSH URLs contain an
earlier `@`) and must match `<rev>` or `<rev>...<rev>` where `<rev>` is a hex
SHA (7–40 chars) or a tag name. Commit/compare URLs from known hosts are
accepted as sugar and normalized to `<repo>@<range>` — they are parsed, not
fetched via API.

Detection order: `file:` prefix / path-like without `@rev` → snapshot;
trailing `@<rev>` → commit; short-form with `#`/`!` → PR (existing); then
PR/MR URL patterns. Malformed refs are rejected, never misclassified (same
philosophy as PD2).

### 1. PR source (existing, unchanged)

Current `GitHubDiffProvider` / `GitLabDiffProvider`, renamed to implement
`ChangeSource`.

### 2. Commit source (git-native, platform-agnostic)

The commit source uses **git only** — no provider API clients. This works
against any git host (GitHub, GitLab, Bitbucket, Gitea, bare SSH remotes) and
authenticates with the user's existing git credentials (SSH keys, credential
helpers) instead of API tokens. `--token-env` does not apply to commit sources.

**Obtaining the objects:**

1. If `<repo>` is a local path, open it directly (`git.Open`); fetch missing
   SHAs from `origin` if needed.
2. Otherwise clone `--bare --filter=blob:none` into a temp directory (blob
   contents are fetched lazily, so cost scales with changed files, not repo
   size), then `git fetch origin <base> <head>`. Direct SHA fetch requires the
   server to allow reachable-SHA-in-want (GitHub, GitLab, and Gitea all do);
   if the server refuses, fall back to a full fetch with a warning. Servers
   without partial-clone support fall back to a plain bare clone.
3. **Single `sha`** is treated as range `sha^...sha` (first parent for merge
   commits — the diff is "what this merge brought in"); the parent is covered
   by fetching with `--depth=2`.

Builds on `pkg/git`'s existing go-git-with-CLI-fallback pattern.

**Computing the changeset:**

- File statuses: `git diff -M --name-status <base> <head>` (rename detection
  included — better than most provider compare APIs).
- Old/new content: `git show <rev>:<path>` per changed file.

**Metadata (all from git, no API):**

| Field | Source |
|-------|--------|
| `Title` / `Body` | Subject / remaining body of the head commit (`git log -1`) |
| `BaseBranch` | Default branch via `git ls-remote --symref origin HEAD` |
| `HeadBranch` | Empty (synthesized later, see GO below) |
| `RepoURL` | The `<repo>` part of the ref (or the checkout's `origin`) |
| `Provider` | Inferred from the host, only for the generated `pr` op (empty → degraded path below) |

### 3. Snapshot source (current state of files)

Points at a **local checkout** of the target repo. Selection and baseline come
from flags (snapshot refs get no inline grammar for these):

- `--include <glob>` (repeatable, required for snapshot refs): files to capture,
  relative to the checkout root. `--exclude <glob>` optional.
- `--base <git-ref>` (optional): baseline to diff against.

Two modes:

- **No `--base`** — every matched file becomes `ChangeAdded` with its current
  working-tree content. Use when the module should *stamp out* these files.
- **With `--base`** — run `git diff --name-status <base> -- <paths>` in the
  checkout; old content via `git show <base>:<path>`, new content from the
  working tree (captures uncommitted state too). Produces real
  added/modified/deleted/renamed classification, so modified YAML becomes SMP
  patches. Use when the module should *transform* an existing repo.

Metadata: `RepoURL` from `git remote get-url origin` (empty if none);
`BaseBranch` = `--base` if it names a branch, else the current branch;
`Provider` inferred from the remote host (`github.com` → github, `gitlab` in
host → gitlab, else empty). `Title`/`Body` empty — `--name` is required when
the only sources are snapshots.

## Composition

```
loom generate <ref> [<ref>...]
```

Refs are fetched independently and composed **in the order given** (oldest →
newest; order is authoritative and never inferred).

### CS1: Per-file squash

Composition folds changesets left to right into a map keyed by *current path*,
tracking rename chains (a rename `a→b` re-keys the entry; later changes to `b`
land on the same entry; the final entry records original path → final path).

For each file, the net change keeps the **old content from the first**
changeset that touched it and the **new content from the last**:

| earlier \ later | added | modified | deleted | renamed |
|---|---|---|---|---|
| **added**    | added*  | added    | *(dropped)* | added @ new path |
| **modified** | modified* | modified | deleted | renamed + content change |
| **deleted**  | modified¹ | modified* | deleted* | — |
| **renamed**  | renamed* | renamed + content change | deleted @ old path | renamed old→final |

¹ delete-then-re-add nets to *modified* (old = original content, new = re-added
content) — SMP-able for YAML.
\* same-cell pairs shouldn't occur from well-ordered sources; treated as
last-wins with a warning.

### CS2: Continuity check

Before folding changeset *N* onto the accumulated state: if *N*'s old content
for a file differs from the accumulated new content, log a warning
(`"file <path> changed between sources (source <i> and <j>); composing anyway (manual review recommended)"`).
This surfaces interleaved out-of-band commits without blocking.

### CS3: Repo consistency

All sources with a non-empty `RepoURL` must normalize to the same repository
(compare host + path, ignoring scheme/`.git`). Mismatch → error:
`sources reference different repositories: <a> vs <b>`. A snapshot source with
no remote is compatible with anything but contributes no `RepoURL`.

### CS4: Metadata merge

| Field | Rule |
|-------|------|
| Title/Body | From the **last** source that has one (it represents the desired state). |
| BaseBranch | From the last source that has one; else repo default branch. |
| HeadBranch | From the last PR source; if none, synthesized as `loom/<module-name>`. |
| RepoURL / Provider | From any source that has them (all agree per CS3). |

Module name derivation (G3) then applies to the merged Title as today.

### CS5: Empty net change

If composition cancels everything out (e.g. a PR and its revert), error:
`no net file changes after composing sources`.

## GitOps operations

Unchanged for PR-backed generation (GO1–GO4). New cases:

- **HeadBranch empty** (commit/snapshot-only): `target.featureBranch` =
  `loom/<module-name>` (parameterized).
- **Provider empty** (snapshot with no recognizable remote): the `pr` operation
  is **omitted** with a warning; `commitPush` is still emitted. If `RepoURL` is
  also empty, `target` is omitted entirely and the module is emitted as a
  local-run module (user fills in `target` by hand — warning logged).
- **Commit message** (GO3) falls back to `"apply <module-name>"` when Title is
  empty.

## CLI summary

```bash
# several PRs, oldest first — net effect of all three
loom generate github:org/gitops#42 github:org/gitops#47 github:org/gitops#51 \
  -p serviceName=payments -o ./my-module

# a PR plus the follow-up fix commit
loom generate github:org/gitops#42 github:org/gitops@9f3ab12 ...

# commit range (short-form sugar)
loom generate 'github:org/gitops@a1b2c3d...f6e5d4c' ...

# commit range on any git host — no API involved
loom generate 'git@bitbucket.org:org/gitops.git@a1b2c3d...f6e5d4c' ...

# current state of files in a local checkout (stamp-out module)
loom generate ./gitops-checkout --include 'services/payments/**' -n onboard-payments ...

# current state diffed against a baseline (transform module)
loom generate ./gitops-checkout --base main --include 'argocd/**' -n update-argo ...
```

New flags: `--include`, `--exclude`, `--base` (apply to snapshot refs; error if
given without one). Existing flags (`-p`, `-o`, `-n`, `--token-env`) unchanged;
`--token-env` applies to PR sources only — commit and snapshot sources are
git-native and use the user's git credentials.

## Error conditions (new)

| Condition | Error |
|-----------|-------|
| Sources resolve to different repos | `sources reference different repositories: <a> vs <b>` |
| Net changeset empty | `no net file changes after composing sources` |
| Snapshot ref without `--include` | `snapshot source <path> requires at least one --include` |
| `--include`/`--base` without a snapshot ref | `--include/--base require a local path source` |
| Snapshot path not a git repo with `--base` | `--base requires <path> to be a git repository` |
| Snapshot-only sources without `-n` | `--name is required when generating from local files` |
| Bad commit ref | `cannot resolve commit "<sha>" in <repo>: ...` |
| Clone/fetch failure (auth, unreachable host) | `fetching <repo>: ...` (surfaces the underlying git error) |

## Implementation phases

1. **Refactor (no behavior change)**: `PRInfo` → `ChangeSet`, `DiffProvider` →
   `ChangeSource`, `ParsePRRef` → `ParseSourceRef`. Existing tests must pass.
2. **Composition + multi-PR**: `Compose()` with CS1–CS5, accept multiple refs.
   This alone covers the most common "took a few PRs" scenario.
3. **Commit source**: git-native fetch/diff (bare partial clone or local
   checkout), single-commit sugar, commit/compare URL normalization.
4. **Snapshot source**: local git integration, `--include/--exclude/--base`,
   target-less emission path.

Each phase is independently shippable and spec-testable.
