# Using Loom with AI Agents

This page is written for AI coding agents (and the humans configuring them) that author, validate, and run Loom modules. It is tool-neutral: point any agent at this page — Claude Code, Codex, Cursor, Gemini CLI — via your repo's agent config (`AGENTS.md`, rules file, or a skill that references this URL).

Authoritative sources in the Loom repository: `specs/module.md` (behavior spec), `specs/smp.md` (patch merge semantics), and the [reference docs](/reference/loom-yaml).

## Golden Workflow

Never jump straight to a real run. Always:

```bash
loom validate ./my-module                                  # 1. schema + semantic checks
loom run ./my-module -p key=val --diff                     # 2. dry-run with file diffs
loom run ./my-module -p key=val --local-run --target-path ./preview   # 3. real files, local clone, no push/PR
loom run ./my-module -p key=val                            # 4. real run (only after the user confirms)
```

- `--diff` implies `--dry-run`: nothing is written, unified diffs are printed.
- `--local-run` **requires** `--target-path`. Each module with a `spec.target` clones into a numbered subdirectory (`00-<name>/`, `01-<name>/`, execution order, never cleaned up). Commits happen locally; push and PR are skipped; shell ops are skipped unless marked `pure: true`.
- A real run that includes `commitPush` or `pr` pushes branches and opens pull requests — an agent should treat this as an outward-facing action and confirm with the user first.

## Module Anatomy

```
my-module/
├── loom.yaml            # or loom.jsonnet — exactly ONE, never both
├── argocd/
│   └── app-{{ .serviceName }}.yaml    # any other file = template; path AND content are templated
└── __functions/         # convention for patch files etc. — NOT auto-excluded,
    └── patches/         # must be listed in spec.excludes
```

Minimal `loom.yaml`:

```yaml
apiVersion: loom.rickliujh.github.io/v1beta1   # exact string required
kind: Loom                                     # exact string required
metadata:
  name: my-module
spec:
  params:
    - name: serviceName
      required: true
    - name: namespace
      default: "default"
  excludes: [__functions]
  target:                                      # omit for local-only modules
    url: "https://github.com/org/gitops-repo.git"
    branch: main
    featureBranch: "loom/{{ .serviceName }}"
  operations:
    - name: create-files
      newFiles: { source: ".", dest: "" }
    - name: commit
      commitPush: { message: "feat: onboard {{ .serviceName }}" }
    - name: open-pr
      pr: { provider: github, title: "Onboard {{ .serviceName }}", tokenEnv: GITHUB_TOKEN }
```

## Rules That Are Easy to Get Wrong

### Params

- Every param passed via `-p`/`--params-file` must be declared in `spec.params` or `spec.dynamicParams` — undeclared params are a hard error.
- Priority: CLI `-p` overrides `--params-file` overrides `default`; a missing `required` param fails the run.
- `dynamicParams` run shell commands after static params resolve, in declaration order (later ones can template earlier ones). A CLI override skips the command.
- `spec.params` definitions are the **only** non-templatable strings in the file. Everything else — paths, commands, commit messages, PR fields, child params, file contents — is a Go template over the resolved params. Available template functions: `default`, `upper`, `lower` — nothing else (no sprig).

### Files and newFiles

- `newFiles` **never overwrites**: an existing destination file fails the run (directories are merged; only file collisions fail). Use `patch` to modify existing files.
- Auto-excluded from template walking: `.git`, `README.md`, `loom.yaml`, `loom.jsonnet`. Everything else in the module directory gets rendered and copied — so list `__functions` and any `.libsonnet` files in `spec.excludes`. `includes` wins over excludes (except the config files).
- Templated paths: prefer Go template syntax in file and directory names; a double-underscore placeholder (`__paramName__`) is an equivalent fallback when braces are awkward for the filesystem.

### Operations

- Each operation has exactly **one** action key (`newFiles` | `patch` | `shell` | `llm` | `commitPush` | `pr`) and a unique non-empty `name`. Execution is strictly declaration order.
- `shell.command` runs via `sh -c` in the **target** directory. Mark side-effect-free commands (lint, format) `pure: true` so they still run under `--local-run`.
- `patch` engines: `smp` (strategic merge, default; scalar lists append-unique, map-lists merge by key) or `json6902` (RFC 6902 op list). Patch file content is templated before applying; the target file must already exist.
- `llm` generates or modifies a file via LLM inference (`provider`, `model`, `prompt`, `target` required; `mode: generate` fails on an existing target, `mode: modify` prepends existing content). See the [llm reference](/reference/op-llm).
- Optional `if`: a shell predicate (templated, then `sh -c`) that gates the operation — exit `0` runs it, non-zero skips it. Runs in the target dir; evaluated in every mode including `--dry-run`, so keep it side-effect-free. Omitted = always runs.

### Composition (spec.modules)

- Children execute **before** parent operations, in declaration order, recursively.
- A child (like an operation) accepts an `if` shell predicate — rendered with the **parent's** params, run in the child's resolved target dir; a non-zero exit skips the whole child. See the [reference](/reference/loom-yaml#conditional-execution-if).
- A child **never inherits** the parent's `spec.target`. Child with its own target → own clone/branch/PR. Child without a target → writes into the parent's target directory (this is how you batch into one PR).
- Child `params` values are rendered with the parent's params before passing down; there is no implicit param inheritance.
- `source`: `./relative`, `/absolute`, git URL, or git URL with a `//subdir` suffix (Terraform convention; the full repo is cloned, so sibling relative paths work).

### Secrets

Never write credentials into config files. Git push auth comes from the `LOOM_GIT_TOKEN` environment variable. PR and LLM auth come from named env vars via `tokenEnv` / `providerConfig.tokenEnv`.

## loom.jsonnet (Programmatic Config)

A module may use [`loom.jsonnet`](/reference/loom-yaml#loom-jsonnet) instead of `loom.yaml` — it must evaluate to the same schema, and validation is identical. Use it when the config needs logic, typically generating `spec.modules` entries for bulk:

```jsonnet
local services = ['payments', 'billing', 'auth'];
{
  apiVersion: 'loom.rickliujh.github.io/v1beta1',
  kind: 'Loom',
  metadata: { name: 'bulk-onboard' },
  spec: {
    modules: [
      { name: 'onboard-' + svc, source: '../onboard-service', params: { serviceName: svc } }
      for svc in services
    ],
  },
}
```

- Jsonnet is evaluated **before** param resolution — it cannot see CLI params. Runtime values still go through Go template placeholders inside the evaluated strings.
- `import` resolves relative to the module directory; put shared code in `.libsonnet` files (and exclude them).
- Both `loom.yaml` and `loom.jsonnet` present → load error.

## Bulk Runs

Three patterns for running one module against many parameter sets — full guide in [Bulk Runs](/guide/bulk-runs):

1. **YAML wrapper**: repeat the child entry N times in `spec.modules`. Reviewable, best for small stable lists.
2. **Jsonnet wrapper**: generate the entries from a list (example above). Best for long or changing lists.
3. **Shell loop**: iterate `loom run` with per-item params files (`xargs -P` for parallel). Best for ad-hoc or dynamic lists; always one branch/PR per item.

One PR per item vs one PR for the batch is decided by where `spec.target` lives (child = per item; wrapper + target-less children = single batch PR).

## Other Commands

- `loom generate <pr-url> -p value=…` — reverse-engineer a module from an existing GitHub PR / GitLab MR; every occurrence of a `-p` value becomes a template expression. Flags: `-o` output dir, `-n` name, `--exclude-git-ops`.
- `loom bulk <module> [-o dir] [--items file.yaml] [--name-param param]` — scaffold a jsonnet bulk wrapper from a module's declared params; prefer this over hand-writing wrappers. It refuses to overwrite existing configs and self-verifies the output.
- Global flags on all commands: `--dry-run`, `--diff`, `--local-run`, `-v/--verbose`, `--log-level`, `--log-format`. On `loom run`, `--summary` prints the PRs/MRs created during the run as a list at the end — use it whenever the run opens PRs, especially bulk runs.

## Debugging Quick Map

| Symptom | Cause / fix |
|---|---|
| `undeclared parameter "x"` | Declare `x` in `spec.params` or `dynamicParams` |
| `required parameter "x" not provided` | Pass `-p x=…` or add a `default` |
| `destination file already exists` | `newFiles` never overwrites — use `patch` for existing files |
| `operation "x" must have exactly one action type` | One action key per operation |
| `both loom.yaml and loom.jsonnet found` | Keep exactly one config file |
| `--local-run requires --target-path` | Add `--target-path ./preview` |
| Patch error `reading target file` | `patch.target` is relative to the **target** dir and must exist |
| Literal `<no value>` in rendered output | Unresolved param renders silently (no error) — check the param name and that it resolved; catch it with `--diff` before a real run |
