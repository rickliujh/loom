# loom run

Execute a module.

```
loom run [path] [flags]
```

## Flags

| Flag | Description |
|------|-------------|
| `-p, --param key=value` | Set a parameter (repeatable). For a `list` or `map` param the value is parsed as YAML |
| `--params-file file.yaml` | Load parameters from a YAML file; values may be nested lists and maps |
| `--target-path /path` | Directory for target files. With `--local-run`, each target repo is cloned into a numbered subdirectory (`00-<name>/`, `01-<name>/`, …). Modules without a `target` spec use it directly as the target directory. Ignored otherwise. |
| `--author name` | Default git author name for `commitPush` (used when not set in `loom.yaml`) |
| `--email email` | Default git author email for `commitPush` (used when not set in `loom.yaml`) |
| `--summary` | Print a list of PRs/MRs created during the run at the end |
| `--dry-run` | Show what would happen without writing anything |
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

### See the diffs

To see exactly what files would be created and changed, use [`loom diff`](/reference/cli-diff).

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

The YAML file maps parameter names to values:

```yaml
serviceName: payments
namespace: fintech
env: prod
```

A value may be a real list or map when the module declares that param with `type: list` or `type: map` (see [Types](/reference/loom-yaml#types)):

```yaml
appName: guestbook
sources:
  - repoURL: https://charts.example.com
    chart: guestbook
    targetRevision: 1.10      # stays 1.10
  - repoURL: https://git.example.com/guestbook-values.git
    ref: values
```

Top-level scalars are read as the exact text written, as a flat file always was: `replicas: 3` gives a string param `3`, `version: 1.10` gives `1.10`.

`-p` values are loaded after the file and replace a file value of the same name whole — they never merge into a list or map.

### Structured values on the command line

`-p` keeps its `key=value` form. For a `list` or `map` param, the value is parsed as YAML — flow style inline, or a whole file through the shell:

```bash
loom run ./argocd-app -p appName=guestbook \
  -p sources='[{repoURL: https://charts.example.com, chart: guestbook, targetRevision: 1.10}]'

loom run ./argocd-app -p appName=guestbook -p sources="$(cat sources.yaml)"
```

A value that is not YAML of the declared kind stops the run before anything happens, naming the param:

```
✖ resolving params for argocd-app: param "sources" is declared list, but received a string that parses as a map, not a list: "{repoURL: https://x}"
```

A `string` param takes the text as is — <code v-pre>-p tags='[a, b]'</code> for a string param is the text `[a, b]`.
