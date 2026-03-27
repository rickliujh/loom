# shell

Runs an arbitrary shell command in the target repository directory.

## Usage

```yaml
- name: validate
  shell:
    command: "kubeval --strict argocd/{{ .serviceName }}.yaml"
    timeout: "30s"
    pure: true
```

| Field | Description |
|-------|-------------|
| `command` | Shell command to execute (`sh -c`), templated |
| `timeout` | Maximum duration (e.g. `30s`, `5m`). Operation fails if exceeded. |
| `pure` | If `true`, this command has no external side effects and runs even in `--local-run` mode. Default: `false`. |

## Behavior

The command is rendered as a Go template, so you can inject parameters. The working directory is the target repository. If the command exits non-zero, Loom stops with an error.

## Pure Commands

In `--local-run` mode, shell commands are **skipped by default** because they may create remote resources (deploy, notify, call external APIs, etc.).

Mark a command with `pure: true` to indicate it has no external side effects -- it only reads or modifies local files. Examples: linting, formatting, validation.

```yaml
- name: format
  shell:
    command: "gofmt -w ."
    pure: true      # no side effects, safe in --local-run mode

- name: deploy
  shell:
    command: "kubectl apply -f manifests/"
    # not marked pure -- skipped in --local-run mode
```

## Timeout

If `timeout` is set and the command exceeds it, the process is killed and the operation fails.

```yaml
- name: slow-check
  shell:
    command: "heavy-validation-script.sh"
    timeout: "5m"
```

## Dry Run

In dry-run mode, the command is logged but not executed.
