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
| `author` | Git author name |
| `email` | Git author email |

## Authentication

Push authentication uses the `LOOM_GIT_TOKEN` environment variable when using the Go library. If the library push fails, Loom falls back to the system `git` binary, which uses your existing credential helpers and SSH configuration.

## Local Run

In `--local-run` mode, the commit is created locally but the push is **skipped**.

## Dry Run

In dry-run mode, no commit is created. The commit message is logged.
