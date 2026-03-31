# Generate

`loom generate` reverse-engineers a reusable module from an existing GitHub PR or GitLab MR. This is the fastest way to turn a manual change you've already made into repeatable automation.

## How It Works

You've onboarded a service by hand — created files, edited YAML, opened a PR. Now you want to make it repeatable:

```bash
loom generate https://github.com/myorg/gitops-repo/pull/42 \
  -p serviceName=payments \
  -p namespace=fintech
```

Loom fetches the PR diff and produces a ready-to-use module:

- **Added files** become templates with <code v-pre>{{ .paramName }}</code> replacing the concrete values, including in file and folder names.
- **Modified YAML files** become strategic merge patches under `__functions/patches/`.
- **Deleted files** become `shell` operations with `rm`.
- **Renamed files** become `shell` operations with `mv`.

The generated `loom.yaml` declares all parameters as required and wires up the operations in order.

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

## Supported Providers

Both GitHub and GitLab are supported:

```
https://github.com/owner/repo/pull/123
https://gitlab.com/group/repo/-/merge_requests/123
github:owner/repo#123
gitlab:group/repo!123
```

## Authentication

Uses `GITHUB_TOKEN` / `GITLAB_TOKEN` environment variables by default. If the token is not set but the `gh` or `glab` CLI is installed and authenticated, Loom falls back to it automatically.

## Options

| Flag | Description |
|------|-------------|
| `-p, --param key=value` | Concrete value to parameterize (repeatable) |
| `-o, --output dir` | Output directory (default: current directory) |
| `-n, --name name` | Module name (default: derived from PR title) |
| `--token-env VAR` | Env var holding the API token |
| `--exclude-git-ops` | Skip generating `target`, `commitPush`, and `pr` operations |

By default, the generated module is written to the current directory. Use `-o` to write to a different directory:

```bash
loom generate <pr-url> -o ./my-module
```

Use `--exclude-git-ops` when you only want the template/patch operations and will handle git operations separately.
