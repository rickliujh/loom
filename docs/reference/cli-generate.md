# loom generate

Generate a reusable loom module from an existing GitHub PR or GitLab MR. This is the fastest way to turn a manual change you've already made into repeatable automation.

```
loom generate <pr-url> [flags]
```

## Flags

| Flag | Description |
|------|-------------|
| `-p, --param key=value` | Concrete value to parameterize (repeatable). Every occurrence of the literal value is replaced with a template expression. |
| `-o, --output dir` | Output directory for the generated module (default: current directory) |
| `-n, --name name` | Module name (default: derived from PR title) |
| `--token-env VAR` | Env var holding the API token (default: `GITHUB_TOKEN` or `GITLAB_TOKEN`) |
| `--exclude-git-ops` | Skip generating `target`, `commitPush`, and `pr` operations |

## Supported References

```
https://github.com/owner/repo/pull/123
https://gitlab.com/group/repo/-/merge_requests/123
github:owner/repo#123
gitlab:group/repo!123
```

## Output

By default, the generated module is written to the current directory. Use `-o` to write to a different directory:

```bash
loom generate <pr-url> -o ./my-module
```

## How It Works

Loom fetches the PR diff and produces a ready-to-use module:

- **Added files** become templates with `{{ .paramName }}` replacing the concrete values, including in file and folder names.
- **Modified YAML files** become strategic merge patches under `__functions/patches/`.
- **Deleted files** become `shell` operations with `rm`.
- **Renamed files** become `shell` operations with `mv`.

The generated `loom.yaml` declares all parameters as required and wires up the operations in order.

## Example

You onboarded a service called "payments" via a PR. Now you want to make that repeatable:

```bash
loom generate https://github.com/myorg/gitops-repo/pull/42 \
  -p serviceName=payments \
  -p namespace=fintech
```

Then run the generated module with different parameters:

```bash
loom run ./onboard-service -p serviceName=billing -p namespace=platform
```

## Authentication

Uses `GITHUB_TOKEN` / `GITLAB_TOKEN` environment variables by default. If the token is not set but the `gh` or `glab` CLI is installed and authenticated, Loom falls back to it automatically.
