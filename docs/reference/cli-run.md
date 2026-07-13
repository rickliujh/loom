# loom run

Execute a module.

```
loom run [path] [flags]
```

## Flags

| Flag | Description |
|------|-------------|
| `-p, --param key=value` | Set a parameter (repeatable) |
| `--params-file file.yaml` | Load parameters from a YAML file |
| `--target-path /path` | Directory for target files. With `--local-run`, each target repo is cloned into a numbered subdirectory (`00-<name>/`, `01-<name>/`, …). Modules without a `target` spec use it directly as the target directory. Ignored otherwise. |
| `--author name` | Default git author name for `commitPush` (used when not set in `loom.yaml`) |
| `--email email` | Default git author email for `commitPush` (used when not set in `loom.yaml`) |
| `--summary` | Print a list of PRs/MRs created during the run at the end |
| `--dry-run` | Show what would happen without writing anything |
| `--diff` | Show file diffs during dry-run (implies `--dry-run`) |
| `--local-run` | Run all operations locally but skip remote push and PR creation |
| `-v, --verbose` | Enable debug logging |
| `--log-level level` | Set log level: `debug`, `info`, `warn`, `error` |
| `--log-format format` | Set log format: `pretty` (default), `text`, `json` |

## Source Argument

`[path]` accepts:

| Format | Behavior |
|--------|----------|
| `./relative` or `/absolute` | Local directory |
| `https://github.com/org/repo.git` | Git URL — clones to temp dir, `loom.yaml` at repo root |
| `https://github.com/org/repo.git//subdir` | Git URL with `//` separator — `loom.yaml` at `subdir/` within the clone |

## Examples

### Full run — local module

```bash
loom run ./onboard-service -p serviceName=payments
```

### Full run — remote module

```bash
loom run https://github.com/myorg/loom-modules.git//onboard-service \
  -p serviceName=payments
```

### Dry run

Modules with a `target` spec clone the target repo into a temporary
directory and preview against that fresh clone:

```bash
loom run ./onboard-service \
  -p serviceName=payments \
  --dry-run
```

### Dry run with diffs

See exactly what files would be created and changed:

```bash
loom run ./onboard-service \
  -p serviceName=payments \
  --diff
```

### Local mode

Render, write, and commit, but don't push or open a PR. `--target-path` is required so you can inspect the results:

```bash
loom run ./onboard-service \
  -p serviceName=payments \
  --target-path ~/repos/gitops \
  --local-run
```

### Parameters from file

```bash
loom run ./onboard-service --params-file params.yaml
```

The YAML file should be a flat key-value map:

```yaml
serviceName: payments
namespace: fintech
env: prod
```
