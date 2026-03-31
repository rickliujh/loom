# commitPush

Stages all changes, creates a commit, and pushes to the remote.

## Usage

```yaml
- name: commit
  commitPush:
    message: "feat: onboard {{ .serviceName }}"
    author: "loom-bot"
    email: "loom@example.com"
```

| Field | Description |
|-------|-------------|
| `message` | Commit message, templated |
| `author` | Git author name (optional) |
| `email` | Git author email (optional) |

## Author / Email Resolution

Author and email are resolved independently, each following this priority:

1. `commitPush.author` / `commitPush.email` in `loom.yaml` (highest)
2. `--author` / `--email` CLI flags
3. System git config — `user.name` / `user.email` (lowest)

This means generated modules that omit `author` and `email` will use CLI flags or your git config automatically.

## Authentication

Push authentication uses the `LOOM_GIT_TOKEN` environment variable when using the Go library. If the library push fails, Loom falls back to the system `git` binary, which uses your existing credential helpers and SSH configuration.

## Local Run

In `--local-run` mode, the commit is created locally but the push is **skipped**.

## Dry Run

In dry-run mode, no commit is created. The commit message is logged.
