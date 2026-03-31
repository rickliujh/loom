# pr

Opens a pull request (or merge request) on the target repository.

## Usage

```yaml
- name: open-pr
  pr:
    provider: github
    title: "Onboard {{ .serviceName }}"
    body: "Automated onboarding for {{ .serviceName }}"
    baseBranch: main
    labels: [automated]
    tokenEnv: GITHUB_TOKEN
```

| Field | Description |
|-------|-------------|
| `provider` | `github` or `gitlab` |
| `title` | PR/MR title, templated |
| `body` | PR/MR description, templated |
| `baseBranch` | Branch to merge into (default: `main`) |
| `labels` | Labels to apply |
| `tokenEnv` | Name of the environment variable holding the API token |

## Providers

### GitHub

Uses the GitHub API (go-github library) with fallback to the `gh` CLI.

```yaml
- name: open-pr
  pr:
    provider: github
    title: "Onboard {{ .serviceName }}"
    baseBranch: main
    tokenEnv: GITHUB_TOKEN
```

### GitLab

Creates a merge request instead of a pull request. Same schema applies.

```yaml
- name: open-mr
  pr:
    provider: gitlab
    title: "Onboard {{ .serviceName }}"
    baseBranch: main
    labels: [automated]
    tokenEnv: GITLAB_TOKEN
```

GitLab URLs are parsed automatically, including self-hosted instances and SSH URLs (`git@gitlab.example.com:group/repo.git`).

## Local Run

In `--local-run` mode, PR creation is **skipped entirely**.

## Dry Run

In dry-run mode, no PR is created. The PR title and body are logged.
