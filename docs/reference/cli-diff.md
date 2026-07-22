# loom diff

Show the changes a module run would produce.

```
loom diff [path] [flags]
```

`loom diff` has two fidelity levels.

- **Full** (default): runs the module in local mode — clones each target, executes every operation (including `pure: true` shell commands), commits locally, and skips push and PR creation — then prints a `git diff` of each target against its base branch. Because operations actually run, this is the complete change a run would make, **including files rewritten by shell commands** (formatters, codegen).
- **Quick** (`--quick`): simulates the run (dry-run). It prints unified diffs for `newFiles` and `patch` operations only and executes nothing — fast and side-effect-free, but it cannot show changes made by shell commands.

## Flags

| Flag | Description |
|------|-------------|
| `--quick` | Simulate the run (dry-run); show `newFiles`/`patch` diffs only, execute nothing |
| `-p, --param key=value` | Set a parameter (repeatable) |
| `--params-file file.yaml` | Load parameters from a YAML file |
| `--target-path /path` | Keep target clones here for inspection instead of a cleaned-up temp dir |
| `--author name` | Default git author name for `commitPush` |
| `--email email` | Default git author email for `commitPush` |
| `-v, --verbose` | Enable debug logging |
| `--log-level level` | Set log level: `debug`, `info`, `warn`, `error` |
| `--log-format format` | Set log format: `pretty` (default), `text`, `json` |

The `[path]` argument accepts the same forms as [`loom run`](/reference/cli-run#source-argument): a local directory or a git URL (with an optional `//subdir`).

## Examples

### Full diff

The complete change, including shell-op effects:

```bash
loom diff ./onboard-service -p serviceName=payments
```

Each target is cloned into a temporary directory that is removed once the diff is printed. Pass `--target-path` to keep the numbered clones for inspection:

```bash
loom diff ./onboard-service -p serviceName=payments --target-path ./preview
```

### Quick diff

A fast, no-execution preview of `newFiles`/`patch` changes:

```bash
loom diff ./onboard-service -p serviceName=payments --quick
```

::: tip
Use full mode when your module has `shell` operations that rewrite files (formatters, linters, codegen) — quick mode never runs them, so their effects are invisible to it. Use `--quick` when you only need to preview templated files and patches and want to avoid executing anything.
:::
