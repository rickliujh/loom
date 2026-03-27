# Local Run

With `--local-run`, Loom runs all local operations (rendering templates, writing files, committing) but skips anything that touches a remote -- no push, no PR creation.

```bash
loom run ./onboard-service \
  -p serviceName=payments \
  --local-run \
  --target-path ./preview
```

`--local-run` requires `--target-path` so you have a persistent directory to inspect the results.

## Operation Behavior

| Operation | Behavior |
|-----------|----------|
| `newFiles` | Writes to local clone as normal |
| `patch` | Patches local clone as normal |
| `shell` | **Skipped** unless `pure: true` |
| `commitPush` | Commits locally, **no push** |
| `pr` | **Skipped entirely** |

## Pure Shell Commands

Shell commands are skipped by default in `--local-run` mode because they may create remote resources (deploy, notify, etc.). Mark a command with `pure: true` to indicate it has no external side effects:

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

A pure command only reads or modifies local files -- for example, linting, formatting, or validation commands.

## Inspecting Results

After a `--local-run`, you can inspect the result directory:

```bash
cd ./preview/00-onboard-service/
git log --oneline
git diff HEAD~1
```

Each module with a target spec is cloned into a numbered subdirectory (e.g., `00-onboard-service`, `01-base-infra`), preserving execution order.

::: tip
`--dry-run` takes precedence over `--local-run`. If both are set, dry-run wins and nothing is written.
:::
