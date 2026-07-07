# Generate — Behavioral Specification

This document describes the expected behavior of the **`generate`** command, which reverse-engineers a loom module from an existing Pull Request (GitHub) or Merge Request (GitLab). The generated module, when executed with loom, reproduces the changes from the original PR/MR in a parameterized, repeatable form.

The generated module conforms to the module specification defined in [`specs/module.yaml`](module.yaml).

## Overview

Generate takes a PR/MR reference, fetches the diff and file contents from the provider, classifies each changed file, applies user-supplied parameterization, and emits a complete loom module directory. The module contains a `loom.yaml`, template files for new content, and SMP patch files for modified or renamed YAML files with content changes.

## Inputs

| Input | Description |
|-------|-------------|
| `ref` | Required. PR/MR URL or short reference (see Provider Detection). |
| `params` | Optional. `map[string]string` of `name → value` pairs. Literal occurrences of each value in file contents, file paths, branch names, and PR metadata are replaced with Go template expressions `{{ .name }}`. |
| `outputDir` | Optional. Directory to write the generated module. Defaults to `.` (current directory). |
| `moduleName` | Optional. Overrides the auto-derived module name (see G3). |
| `tokenEnv` | Optional. Name of an environment variable containing the GitHub personal access token or GitLab private token used to authenticate API requests when fetching PR/MR data. Overrides default token resolution. |

---

## Provider Detection

### Supported Reference Formats

| Format | Provider | Example |
|--------|----------|---------|
| GitHub PR URL | `github` | `https://github.com/owner/repo/pull/123` |
| GitLab MR URL | `gitlab` | `https://gitlab.com/group/repo/-/merge_requests/123` |
| GitHub short-form | `github` | `github:owner/repo#123` |
| GitLab short-form | `gitlab` | `gitlab:group/repo!123` |

### Behaviors

#### PD1: Short-form detection (checked first)

Short-form references are unambiguous and checked first:
- Starts with `github:` → GitHub. Expected format: `github:owner/repo#number`.
- Starts with `gitlab:` → GitLab. Expected format: `gitlab:group/repo!number`.

For self-hosted instances where URL patterns may be ambiguous, use the short-form prefix to explicitly specify the provider.

#### PD2: URL-based detection with strict pattern matching

The reference is validated against strict regex patterns that check the full URL structure (scheme, host, path pattern, and numeric ID):
- `^https?://[^/]+/.+/pull/\d+/?$` → GitHub.
- `^https?://[^/]+/.+/-/merge_requests/\d+/?$` → GitLab.

This ensures malformed or ambiguous URLs are rejected rather than misclassified.

#### PD3: Self-hosted support

Both GitHub and GitLab URLs support any host (including GitHub Enterprise and self-hosted GitLab). The host is not checked against a hardcoded domain. For GitHub, owner and repo are extracted as the two path segments immediately before `/pull/`. The repository URL is derived from the original reference URL to preserve the correct host.

### Error Conditions

| Condition | Error |
|-----------|-------|
| Reference matches no known pattern | `cannot detect provider from reference "<ref>"; use a full URL or prefix with github: or gitlab:` |
| GitHub URL missing `/pull/` segment | `cannot parse GitHub PR URL "<ref>"` |
| GitHub short-form missing `#` | `expected github:owner/repo#number, got "<ref>"` |
| GitLab short-form missing `!` | `expected gitlab:group/repo!number, got "<ref>"` |
| Non-numeric PR/MR number | `invalid PR number in "<ref>": ...` / `invalid MR number in "<ref>": ...` |

---

## Authentication

### Behaviors

#### A1: Custom token env takes priority

If `tokenEnv` is provided, the value of that environment variable is used regardless of provider.

#### A2: Default token env by provider

When `tokenEnv` is not set, the default environment variable is used:

| Provider | Environment Variables (checked in order) |
|----------|------------------------------------------|
| GitHub | `GITHUB_TOKEN` |
| GitLab | `GITLAB_TOKEN`, `GITLAB_PRIVATE_TOKEN` |

#### A3: Token-less fallback to CLI

If no token is available, the provider falls back to CLI tools (`gh` for GitHub, `glab` for GitLab). If neither a token nor the CLI is available, an error is returned.

### Error Conditions

| Condition | Error |
|-----------|-------|
| No GitHub token and no `gh` CLI | `set GITHUB_TOKEN or install the gh CLI to fetch PR data` |
| No GitLab token and no `glab` CLI | `set GITLAB_TOKEN or install the glab CLI to fetch MR data` |
| API fails and CLI not available | `<Provider> API failed and <cli> CLI is not available` |

---

## Diff Fetching

### Behaviors

#### D1: API-first, CLI-fallback strategy

For both GitHub and GitLab, the provider first attempts the REST API (requires token). If the API call fails and the corresponding CLI tool is installed, it falls back to the CLI.

| Provider | API Client | CLI Fallback |
|----------|-----------|--------------|
| GitHub | `go-github/v60` with OAuth2 token | `gh pr view`, `gh pr diff`, `gh api` |
| GitLab | `gitlab-org/api/client-go` | `glab api` |

#### D2: File content fetching

For each file in the PR/MR:
- **Added / Modified / Renamed** files: the new (head-ref) content is fetched.
- **Modified / Renamed** files: the old (base-ref) content is also fetched (needed for SMP computation). For renamed files, the old content is fetched at the **previous path**.
- **Deleted** files: no content is fetched.

#### D3: Commit SHA resilience

Both providers prefer commit SHAs over branch names when fetching file content. Branches may be deleted after a PR/MR is merged, but the commit objects remain reachable. The provider extracts SHAs from:
- GitHub CLI: `headRefOid` and `baseRefOid` from `gh pr view --json`.
- GitLab API/CLI: `diff_refs.head_sha` and `diff_refs.base_sha`.

If SHAs are unavailable, the provider falls back to branch names.

#### D4: Pagination

GitHub file lists are fetched with pagination (100 files per page). All pages are consumed before returning.

#### D5: PR metadata extraction

The following metadata is extracted from the PR/MR and used in module generation:

| Field | Source (GitHub) | Source (GitLab) |
|-------|----------------|-----------------|
| `Title` | PR title | MR title |
| `Body` | PR body | MR description |
| `BaseBranch` | base ref name | target branch |
| `HeadBranch` | head ref name | source branch |
| `RepoURL` | Derived from the reference URL (preserves host for self-hosted) | `<baseURL>/project.git` |
| `Provider` | `"github"` | `"gitlab"` |

### Error Conditions

| Condition | Error |
|-----------|-------|
| PR/MR has no file changes | `PR/MR has no file changes` |
| API request fails (and no CLI fallback) | `fetching diff: ...` |
| File content fetch fails for a file | File is skipped with warning logged |

---

## Module Name Derivation

### Behaviors

#### G3: Name priority

1. If `moduleName` is provided via options, it is used as-is.
2. Otherwise, the PR/MR title is slugified.
3. If the title produces an empty slug, `"generated-module"` is used.

#### G4: Slugification rules

The title is converted to a URL/path-friendly slug:
1. Lowercased.
2. Characters `a-z`, `0-9`, `-` are kept.
3. Spaces, underscores, and slashes are replaced with `-`.
4. All other characters are removed.
5. Consecutive dashes are collapsed to a single dash.
6. Leading and trailing dashes are trimmed.
7. Truncated to 60 characters.

```
"Add Payment Service (v2)" → "add-payment-service-v2"
"fix/broken_deploy"        → "fix-broken-deploy"
```

---

## File Classification

Each file in the PR/MR diff is classified into one of four change types:

| Change Type | Criteria | Generated Operation |
|-------------|----------|---------------------|
| `added` | New file in the PR/MR | `newFiles` operation |
| `modified` | Existing file changed | `patch` (YAML with SMP) or skipped with warning |
| `deleted` | File removed | `shell` operation (`rm -f`) |
| `renamed` | File moved to a new path | `shell` (`mv`) + `patch` (if YAML content also changed) |

### Behaviors

#### FC1: Added files use a single newFiles operation from root

All added files produce a single `newFiles` operation with `source: "."` and `dest: ""`. Template files are written at their original paths relative to the module root.

```yaml
# Files: a/foo.yaml, a/bar.yaml, b/baz.yaml
# Produces:
operations:
  - name: create-files
    newFiles: { source: ".", dest: "" }
```

#### FC2: Modified YAML files produce SMP patches; others are skipped

When a modified file has a `.yaml` or `.yml` extension **and** both old and new content are available, the engine computes a strategic merge patch (the minimal diff). The patch is stored under `__functions/patches/`.

If the file is not YAML, it is skipped with a warning log (`"modified non-YAML file skipped (manual review needed)"`). If old content is unavailable, it is skipped with a warning. If SMP computation fails or detects no changes, it is skipped with a warning. The philosophy is: when something goes wrong, force the user to review instead of falling back silently.

#### FC3: Deleted files produce shell operations

Each deleted file produces a `shell` operation with `rm -f "<path>"`.

#### FC4: Renamed files — SMP patch first, then move

A renamed file produces operations in this order:

1. **Patch** (if content also changed): An SMP patch targeting the **old path** (the file is still at its original location). The built-in patch engine runs first because it is more stable than shell operations.
2. **Move**: A `shell` operation `mv "<oldPath>" "<newPath>"` relocates the (now patched) file.

Content change handling:
- **YAML files** with both old and new content: SMP patch computed and emitted before the `mv`.
- **Non-YAML files** with content changes: Skipped with a warning (`"renamed non-YAML file has content changes (manual review needed)"`).
- **Old content unavailable**: No patch produced (only the `mv`).
- **Identical content**: No patch produced (pure rename, only the `mv`).

---

## Parameterization

### Behaviors

#### PM1: Literal value replacement in content

For each `name → value` pair in `params`, every literal occurrence of `value` in file content is replaced with the Go template expression `{{ .name }}`.

```
# params: serviceName=payments
# input:  "namespace: payments-system"
# output: "namespace: {{ .serviceName }}-system"
```

#### PM2: Literal value replacement in paths

The same replacement is applied to file paths and patch target paths.

```
# params: serviceName=payments
# input:  "services/payments/deploy.yaml"
# output: "services/{{ .serviceName }}/deploy.yaml"
```

#### PM3: Longest-value-first replacement

Replacements are applied in descending order of value length. This prevents shorter values from partially matching within longer values.

```
# params: env=prod, fullEnv=prod-payments
# "prod-payments" is replaced first (longer), preventing "prod" from breaking it:
#   "deploy-prod-payments" → "deploy-{{ .fullEnv }}"
# NOT:
#   "deploy-prod-payments" → "deploy-{{ .env }}-payments"
```

#### PM4: Empty values are skipped

Parameters with empty string values are ignored during replacement.

#### PM5: Parameterization targets

Parameterization is applied to:
- Template file content (added files).
- SMP patch content (modified and renamed YAML files with content changes).
- File paths (template files and patch targets).
- PR/MR metadata used in gitops operations: feature branch name, commit message, PR title, and PR body.

#### PM6: Param definitions in generated module

Each parameter produces a `ParamDef` in the generated `loom.yaml` with `required: true` and no default value. The user must supply all parameters when running the generated module.

```yaml
# params: serviceName=payments, env=prod
# generates:
spec:
  params:
    - name: serviceName
      required: true
    - name: env
      required: true
```

---

## SMP Computation

### Behaviors

#### SMP1: Minimal diff computation

`ComputeSMP` takes old and new YAML content and produces the minimal YAML document that, when applied through the `expandScalarLists` + `merge2` pipeline in `pkg/action/patch.go`, reproduces the new content from the old content.

- **Maps**: recursively compared. Only keys with changed values or new keys are included in the patch.
- **Scalar lists**: only added items are included. On apply, `expandScalarLists` prepends old values back, so the patch only needs the additions. Removed scalar list items cannot be represented in SMP — `expandScalarLists` always preserves all existing target items. When removals are detected, `ComputeSMP` reports them as warnings so the caller can inform the user (manual patch needed). If a list has both additions and removals, the additions are still included but the removals are dropped with a warning.
- **Map-lists** (lists of maps with a common key like `name`): items are matched by an inferred merge key. Only changed or newly added items are included. Unchanged items are omitted. On apply, `merge2` matches by key and deep-merges, preserving unmatched old items.
- **Other lists/scalars**: if they differ, the entire new value is included.
- **Deleted keys**: not represented in the SMP output (SMP does not support key deletion; this would require `$patch: delete` directives).

#### SMP2: Nil patch on no changes

If old and new content are identical, `ComputeSMP` returns a nil `Patch`. The file is not included in the generated module.

#### SMP3: Nil patch on parse failure

If either document cannot be parsed as YAML, `ComputeSMP` returns a nil `Patch`. The caller logs a warning and skips the file (manual review needed).

#### SMP4: Patch file naming

The patch file name is derived from the patch target path by replacing `/` with `--` and appending `.patch.yaml`. For modified files this is the file's current path; for renamed files this is the **old path** (since the patch is applied before the `mv`).

```
cluster/apps/deployment.yaml -> cluster--apps--deployment.yaml.patch.yaml
```

#### SMP5: Patch file location

Patch files are written to `<outputDir>/__functions/patches/<patchName>`.

---

## GitOps Operations

The generated module always includes a `target` spec and two gitops operations (`commitPush` and `pr`).

### Behaviors

#### GO1: Target spec from PR metadata

The target is populated from the PR/MR metadata:

| Field | Value |
|-------|-------|
| `target.url` | Repository URL converted to SSH format (`git@host:owner/repo.git`) |
| `target.branch` | Base branch of the PR/MR |
| `target.featureBranch` | Head branch of the PR/MR (parameterized) |

#### GO2: SSH URL conversion

HTTPS repository URLs are converted to SSH format for the target spec:
- `https://github.com/myorg/myrepo.git` → `git@github.com:myorg/myrepo.git`
- `https://ghe.internal/myorg/myrepo.git` → `git@ghe.internal:myorg/myrepo.git`
- URLs that are already SSH or cannot be parsed are returned as-is.

#### GO3: Commit operation

A `commitPush` operation is appended with the PR/MR title (parameterized) as the commit message.

```yaml
- name: commit
  commitPush:
    message: "{{ .serviceName }}: onboard new service"
```

#### GO4: PR operation

A `pr` operation is appended with:
- `provider`: `"github"` or `"gitlab"` (from the source PR/MR).
- `title`: PR/MR title (parameterized).
- `body`: PR/MR body (parameterized).
- `baseBranch`: base branch of the PR/MR.

---

## Module Emission

### Behaviors

#### E1: Output directory structure

The generated module is written to `outputDir` with the following layout:

```
<outputDir>/
  loom.yaml                          # module manifest
  <path>/<to>/<template-files>       # added files (parameterized)
  __functions/
    patches/
      <sanitized-name>.patch.yaml    # SMP patch files
```

#### E2: loom.yaml structure

The emitted `loom.yaml` conforms to the module spec and uses 2-space indentation, consistent with the emitted patch files:

```yaml
apiVersion: loom.rickliujh.github.io/v1beta1
kind: Loom
metadata:
  name: <moduleName>
spec:
  params: [...]          # from PM6
  target: {...}          # from GO1
  operations: [...]      # from file classification + GO3/GO4
```

#### E3: Operation ordering

Operations are emitted in the following order:
1. `newFiles` operation (single operation from root for all added files).
2. `patch` operations (one per modified YAML file with successful SMP).
3. `shell` operations for deleted files.
4. For each renamed file: `patch` (if YAML content also changed), then `shell` (mv).
5. `commitPush` operation.
6. `pr` operation.

#### E4: File writing

All files are written with permission `0644`. Intermediate directories are created as needed.

---

## Execution Flow

```
generate <ref> [options]

 1. Parse the reference string to detect provider (GitHub/GitLab)
    and create the appropriate DiffProvider.
 2. Resolve authentication token:
    a. Custom tokenEnv → read env var.
    b. Default env var for provider (GITHUB_TOKEN / GITLAB_TOKEN).
    c. Empty string (CLI fallback will be attempted).
 3. Fetch PR/MR diff via DiffProvider:
    a. Try REST API with token.
    b. On failure or no token, try CLI tool (gh / glab).
    c. Fail if neither works.
 4. Validate that the PR/MR has at least one file change.
 5. Derive module name:
    a. Use explicit moduleName if provided.
    b. Slugify PR/MR title.
    c. Fall back to "generated-module".
 6. Build module structure:
    a. Create param definitions from params map.
    b. Classify each file change (added / modified / deleted / renamed).
    c. For added files: parameterize content and path, create a single
       newFiles operation from root.
    d. For modified YAML files: compute SMP patch, parameterize patch content,
       create patch operations. Skip with warning if SMP fails or not YAML.
    e. For non-YAML modified files: skip with warning (manual review needed).
    f. For deleted files: create shell rm operations.
    g. For renamed files: compute SMP patch first if YAML content also
       changed, then create shell mv operation. Warn and skip for non-YAML.
    h. Set target spec, append commitPush and pr operations.
 7. Emit module to outputDir:
    a. Marshal and write loom.yaml.
    b. Write template files to their parameterized paths.
    c. Write patch files under __functions/patches/.
```

---

## End-to-End Example

Given a GitHub PR at `https://github.com/myorg/gitops/pull/42` with:

**PR metadata:**
- Title: `onboard payments service`
- Base branch: `main`
- Head branch: `feat/onboard-payments`

**Changed files:**
- `services/payments/deployment.yaml` — **added**
- `services/payments/service.yaml` — **added**
- `argocd/applications.yaml` — **modified** (YAML, added a new app entry)
- `legacy/old-config.yaml` — **deleted**

**Command:**
```bash
loom generate https://github.com/myorg/gitops/pull/42 \
  --param serviceName=payments \
  --output ./my-module
```

**Generated module (`./my-module/`):**

```
my-module/
  loom.yaml
  services/{{ .serviceName }}/deployment.yaml
  services/{{ .serviceName }}/service.yaml
  __functions/
    patches/
      argocd--applications.yaml.patch.yaml
```

**loom.yaml:**
```yaml
apiVersion: loom.rickliujh.github.io/v1beta1
kind: Loom
metadata:
  name: onboard-payments-service
spec:
  params:
    - name: serviceName
      required: true
  target:
    url: git@github.com:myorg/gitops.git
    branch: main
    featureBranch: feat/onboard-{{ .serviceName }}
  operations:
    - name: create-files
      newFiles:
        source: "."
        dest: ""
    - name: patch-0
      patch:
        engine: smp
        path: __functions/patches/argocd--applications.yaml.patch.yaml
        target: argocd/applications.yaml
    - name: delete-0
      shell:
        command: rm -f "legacy/old-config.yaml"
    - name: commit
      commitPush:
        message: onboard {{ .serviceName }} service
    - name: open-pr
      pr:
        provider: github
        title: onboard {{ .serviceName }} service
        body: ""
        baseBranch: main
```

**What happened:**
1. Provider detected as GitHub from the URL via strict regex matching (PD2).
2. Token read from `GITHUB_TOKEN` (A2).
3. PR diff fetched via GitHub API (D1); 4 file changes found.
4. Module name slugified from title: `"onboard-payments-service"` (G3, G4).
5. Two added files → single `newFiles` operation from root (FC1). Content and paths parameterized: `payments` → `{{ .serviceName }}` (PM1, PM2).
6. Modified YAML file → SMP computed (SMP1), patch stored as `argocd--applications.yaml.patch.yaml` (SMP4, SMP5).
7. Deleted file → `shell` operation with `rm -f` (FC3).
8. Target spec with SSH URL (GO1, GO2), `commitPush` (GO3), and `pr` (GO4) appended. Feature branch parameterized (PM5).
9. Module emitted to `./my-module/` (E1–E4).

**Running the generated module:**
```bash
loom run ./my-module -p serviceName=billing
```

This reproduces the same structural changes but for a `billing` service instead of `payments`.
