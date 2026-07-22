# Getting Started

## Installation

### Download a release binary

Pre-built binaries are available on the [Releases](https://github.com/rickliujh/loom/releases) page for Linux and macOS (amd64/arm64).

```bash
# Example: Linux amd64
curl -Lo loom https://github.com/rickliujh/loom/releases/latest/download/loom-linux-amd64
chmod +x loom
sudo mv loom /usr/local/bin/
```

### Build from source

Requires Go 1.25+.

```bash
go install github.com/rickliujh/loom@latest
```

Or clone and build:

```bash
git clone https://github.com/rickliujh/loom.git
cd loom
go build -o loom .
```

### Verify

```bash
loom version
```

## Quick Start

### 1. Create a module

A Loom module is a directory with a `loom.yaml` and template files:

```
onboard-service/
├── loom.yaml
├── argocd/
│   └── application-{{ .serviceName }}.yaml
└── __functions/
    └── patches/
        └── add-app.yaml
```

### 2. Write the `loom.yaml`

```yaml
apiVersion: loom.rickliujh.github.io/v1beta1
kind: Loom
metadata:
  name: onboard-service
spec:
  params:
    - name: serviceName
      required: true
      description: "Name of the service to onboard"
    - name: namespace
      default: "default"

  excludes:
    - __functions

  target:
    url: "https://github.com/myorg/gitops-repo.git"
    branch: "main"
    featureBranch: "loom/onboard-{{ .serviceName }}"

  operations:
    - name: create-files
      newFiles:
        source: "."
        dest: ""

    - name: commit
      commitPush:
        message: "feat: onboard {{ .serviceName }}"
        author: "loom-bot"
        email: "loom@example.com"

    - name: open-pr
      pr:
        provider: github
        title: "Onboard {{ .serviceName }}"
        baseBranch: main
        tokenEnv: GITHUB_TOKEN
```

### 3. Preview with dry-run

```bash
loom run ./onboard-service \
  -p serviceName=payments \
  --dry-run
```

Add `--diff` to see the rendered file contents:

```bash
loom run ./onboard-service \
  -p serviceName=payments \
  --diff
```

### 4. Run it for real

```bash
loom run ./onboard-service -p serviceName=payments
```

Loom clones the target repo, renders templates, creates a feature branch, commits, pushes, and opens a PR.

If you forget a required param, the run fails and names it, including the `description` you gave it:

```
required parameter "serviceName" not provided: Name of the service to onboard
```

To fill missing required params interactively instead of failing, add `-i` / `--interactive` — Loom prompts for each one using its description:

```bash
loom run ./onboard-service -i
# Enter value for serviceName — Name of the service to onboard: payments
```

Fail-fast is the default so CI runs stay deterministic; reach for `-i` when running by hand.

### 5. Or test locally first

```bash
loom run ./onboard-service \
  -p serviceName=payments \
  --local-run \
  --target-path ./preview
```

This writes files and commits locally but skips push and PR creation. You can inspect the result in `./preview/`.
