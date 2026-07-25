# loom alias

Give a module source a short name, so a long invocation:

```bash
loom run git@github.com:foo/bar.git//modules/svc -p foo=bar -p something=anotherthing
```

becomes:

```bash
loom :bar
```

An alias stores a module source plus a set of default parameters. Any argument
beginning with `:` is an alias reference; everything else is a path or git URL
as before.

## Quick start

```bash
loom alias add bar git@github.com:foo/bar.git//modules/svc -p foo=bar
loom :bar                       # run it
loom :bar -p foo=other          # override a param
loom diff :bar                  # preview it
```

`loom :bar` is shorthand for `loom run :bar`, so every `loom run` flag works
unchanged. Other commands take the reference in the source position.

## Subcommands

| Command | Description |
|---------|-------------|
| `loom alias add <name> <source>` | Create an alias |
| `loom alias list` (`ls`) | List defined aliases |
| `loom alias remove <name>` (`rm`) | Delete an alias |

### `loom alias add`

| Flag | Description |
|------|-------------|
| `-p, --param key=value` | Default parameter (can be repeated) |
| `--force` | Replace an existing alias of the same name |

The source accepts the same forms as `loom run`: a local path, a git URL, or a
git URL with the `//subdir` suffix. Adding a name that already exists is an
error unless `--force` is given.

Alias names may contain letters, digits, `.`, `_`, and `-`, and must start with
a letter or digit. They may not contain `:`, `/`, `=`, or whitespace — that is
what keeps a name unambiguous against a path, a git URL, and a `key=value`
argument.

## Where aliases live

Aliases are **user-level** configuration, stored in a single file:

```
$LOOM_CONFIG_DIR/aliases.yaml      # if LOOM_CONFIG_DIR is set
<user config dir>/loom/aliases.yaml  # otherwise (e.g. ~/.config/loom/aliases.yaml)
```

There is no repository-local alias file and no search up the directory tree, so
changing branch or directory can never change what an alias resolves to.

The file is managed by `loom alias`, but it is plain YAML and can be edited by
hand:

```yaml
aliases:
  bar:
    source: git@github.com:foo/bar.git//modules/svc
    params:
      foo: bar
      something: anotherthing
```

Define one alias per parameter set to get named variants:

```yaml
aliases:
  deploy-prod:
    source: git@github.com:foo/deploy.git
    params: {env: prod, region: us-east-1}
  deploy-dev:
    source: git@github.com:foo/deploy.git
    params: {env: dev, region: us-west-2}
```

```bash
loom :deploy-prod
```

## Parameter precedence

An alias's params are the lowest-precedence source, so anything you pass at run
time wins:

| Precedence | Source |
|-----------|--------|
| 1 (lowest) | Alias `params` |
| 2 | `--params-file` |
| 3 (highest) | `-p key=value` |

Module-level `spec.params` defaults still apply beneath all three — an alias
param supplies a value, so it suppresses the module's default exactly as `-p`
would.

## Aliases are not module configuration

Aliases are resolved by the CLI only. A module's own `spec.modules[].source` is
never resolved against the alias file:

```yaml
spec:
  modules:
    - name: child
      source: ":bar"   # not an alias — fails to resolve
```

This is deliberate. A module whose child sources depended on whichever aliases
the operator happened to have defined would not be portable between machines.

## Which commands accept a reference

`loom run` and `loom diff` accept `:<name>` in the module-source position.
`loom validate` and `loom bulk` do not — `bulk` embeds the source it is given
into the module it generates, where an alias reference would not resolve for
anyone else.

## Errors

| Condition | Error |
|-----------|-------|
| Bare `:` | `empty alias name: expected :<name>` |
| Alias not defined | `unknown alias "<name>" (looked in <path>)` |
| Invalid name | `invalid alias name "<name>": must match [a-zA-Z0-9][a-zA-Z0-9._-]*` |
| `add` over an existing name | `alias "<name>" already exists (use --force to replace)` |

An unknown alias is never retried as a git URL, so a mistyped alias reports a
mistyped alias rather than a failed clone.

## See also

- [Alias specification](https://github.com/rickliujh/loom/blob/main/specs/alias.md)
- [loom run](/reference/cli-run)
