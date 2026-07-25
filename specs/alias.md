# Loom Alias — Behavioral Specification

An alias is a short, user-defined name for a module source plus a set of default
parameters. It exists purely to shorten invocation: `loom :bar` in place of
`loom run git@github.com:foo/bar.git -p foo=bar`.

Aliases are **user-level configuration**, never module configuration. They are
resolved at the CLI boundary only, so a module's own `spec.modules[].source` is
never affected by the aliases a particular operator happens to have defined.

---

## Alias File

Aliases live in a single user-level YAML file:

```yaml
aliases:
  bar:
    source: git@github.com:foo/bar.git//modules/svc
    params:
      foo: bar
      something: anotherthing
```

The value of an entry is an `AliasDef`: a required `source` and an optional
`params` map. Unknown fields are rejected.

### Behaviors

#### AL1: Alias file location

The file path is `$LOOM_CONFIG_DIR/aliases.yaml` when `LOOM_CONFIG_DIR` is set,
otherwise `<os.UserConfigDir()>/loom/aliases.yaml`. There is no repository-local
alias file and no search up the directory tree: switching branches or changing
directory can never change what an alias resolves to.

#### AL2: Missing file is an empty alias set

A missing alias file is not an error. Commands that only list aliases report an
empty set; resolving a specific alias fails with the AL5 unknown-alias error.

#### AL3: Alias names

An alias name must match `[a-zA-Z0-9][a-zA-Z0-9._-]*` — it may not contain
`:`, `/`, `=`, or whitespace, so it can never be confused with a path, a git
URL, or a `key=value` argument. Names are validated when an alias is created;
an alias file containing an invalid name is rejected at load time.

---

## Reference Syntax

### Behaviors

#### AL4: A leading colon marks an alias reference

An argument of the form `:<name>` is an alias reference. Any other argument is
a module source and follows the existing local-path / git-URL rules unchanged.
A bare `:` with no name is an error.

#### AL5: Unknown aliases do not fall through

An alias reference that is not defined in the alias file is an error naming the
alias and the file it was looked up in. It is never retried as a git URL, so a
mistyped alias reports a mistyped alias rather than a failed clone.

#### AL6: Top-level alias dispatch defaults to `run`

`loom :bar [args...]` is exactly equivalent to `loom run :bar [args...]`. The
rewrite happens before command dispatch and only when the first argument begins
with `:`, so every other argument keeps normal subcommand dispatch — an unknown
subcommand still reports `unknown command "..." for "loom"` with its usual
suggestions.

#### AL7: Alias references are accepted by the commands that run a module

`loom run` and `loom diff` accept `:<name>` in place of a module source.

`loom validate` and `loom bulk` do not. Neither consumes an alias's params —
`validate` resolves no remote sources at all, and `bulk` embeds the source it
is given into the module it generates, where an alias reference would violate
AL8. Extending them is additive and deliberately out of scope here.

#### AL8: Aliases are never resolved inside a module

`spec.modules[].source` is resolved by the module executor, which does not
consult the alias file. A module referencing `:bar` fails to resolve it as a
source, keeping modules portable between operators.

---

## Parameter Merging

### Behaviors

#### AL9: Alias params are the lowest-precedence source

Parameters are merged in this order, each overriding the previous:

| Precedence | Source |
|-----------|--------|
| 1 (lowest) | Alias `params` |
| 2 | `--params-file` |
| 3 (highest) | `-p key=value` |

Module-level `spec.params` defaults still apply beneath all three: an alias
param supplies a value, so it suppresses the module's default exactly as a `-p`
would.

---

## Alias Management

The `loom alias` command group manages the alias file. All subcommands operate
on the AL1 path and create the parent directory as needed.

### Behaviors

#### AL10: `alias add` creates an entry

`loom alias add <name> <source> [-p key=value ...]` writes an entry. The name
may be given with or without a leading `:`. Adding a name that already exists
is an error unless `--force` is given, so an existing alias is never silently
replaced.

#### AL11: `alias remove` deletes an entry

`loom alias remove <name>` deletes the entry. Removing an alias that does not
exist is an error.

#### AL12: `alias list` reports every alias

`loom alias list` prints each alias name with its source and params, sorted by
name. With no aliases defined it reports that none are, and exits 0.

#### AL13: Writes preserve the rest of the file

Adding or removing an alias rewrites the file from the parsed alias set. A
write never partially updates the file: it is written to a temporary file in
the same directory and renamed into place.

### Error Conditions

| Condition | Error |
|-----------|-------|
| Bare `:` reference | `empty alias name: expected :<name>` |
| Alias not defined | `unknown alias "<name>" (looked in <path>)` |
| Invalid name at add time | `invalid alias name "<name>": must match [a-zA-Z0-9][a-zA-Z0-9._-]*` |
| Invalid name in file | `invalid alias name "<name>" in <path>: must match [a-zA-Z0-9][a-zA-Z0-9._-]*` |
| Entry missing a source | `alias "<name>" in <path> has no source` |
| `add` over an existing name | `alias "<name>" already exists (use --force to replace)` |
| `remove` of an unknown name | `unknown alias "<name>" (looked in <path>)` |
