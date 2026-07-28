# loom validate

Check that a `loom.yaml` is well-formed.

```
loom validate [path] [--recursive]
```

| Flag | Description |
|------|-------------|
| `--recursive`, `-r` | Also validate the modules referenced by `spec.modules`, and theirs in turn |

## Scope

By default only the module at `path` is checked, along with the files it
renders. A module named in `spec.modules` is a separate config with its own
params — a run validates it when it gets there, and fetching it may mean a
clone — so it is left alone.

`--recursive` walks the whole tree, validating each referenced module on its
own terms and naming the module each finding came from:

```
⚠ kid: param "svc" is declared but never referenced by any template
✖ kid: operation "create-files": template file "app.yaml": references undeclared param "typo"
```

A module reached twice is checked once, and a templated `source` is reported
and skipped — its value is only known at run time.

## What It Checks

- `apiVersion` is `loom.rickliujh.github.io/v1beta1`
- `kind` is `Loom`
- `metadata.name` is present
- Parameter names are non-empty and unique
- Operation names are non-empty and unique
- Each operation has exactly one action type
- Required fields per action type are present
- Enums are valid: patch engine (`smp` / `json6902`), PR provider, LLM provider and mode
- Durations parse: `shell.timeout`, `llm.retryDelay`
- Every templatable field parses as a Go template and only references declared params
- The same holds for the files a run renders — see [Params and templates](#params-and-templates) below
- Every declared param is actually referenced by some template (a warning, not an error)
- Exclude/include patterns are usable globs — see [File filtering](#file-filtering) below
- Destinations stay inside the target directory: `patch.target`, `newFiles.dest`, `llm.target`
- `newFiles.source` is an existing directory and `patch.path` an existing file
- No patch file is also rendered into the target as module output

Checks that depend on a value only known at run time are skipped when the field
is templated.

## Params and templates

The files a run renders are templates too: the bodies `newFiles` walks, their
path names (including `__param__` placeholders), and patch file bodies. A
reference to something that is not a declared param is not an error at run
time — params are a plain string map, so it renders as the literal
`<no value>` straight into the target:

```yaml
# loom.yaml declares serviceName
params:
  - name: serviceName
```

```yaml
# templates/app.yaml
name: {{ .servicename }}   # error: references undeclared param "servicename"
```

The reverse is checked too, as a **warning**: the config is still valid and the
run is still correct, it just never reads the value. Nothing else tells you
either — a param left behind by a rename looks the same as one that is
deliberately optional, and a value you pass on the command line is silently
dropped:

```
⚠ param "namespace" is declared but never referenced by any template
```

Warnings print alongside any violations and do not affect the exit status.

Params reach submodules only through `spec.modules[].params`, so forwarding one
to a child counts as using it. This check is skipped whenever a template's
references cannot all be seen — a templated `newFiles.source` or `patch.path`,
a body that cannot be read, or a template that rebinds dot (`range`, `with`) or
addresses it whole (`{{ index . "my-param" }}`, the only way to reference a
param whose name is not a valid template identifier).

## File filtering

Exclude and include patterns are matched against **base names** with
`filepath.Match`, and a pattern that fails to compile silently matches nothing.
Both are easy to get wrong in a way a run never reports, so `validate` rejects
them up front:

```yaml
excludes:
  - "__functions/patches/*.yaml"  # error: matches base names only
  - "__functions["                # error: invalid glob pattern
  - "__functions"                 # correct
```

The same silent failure mode is why a patch file that survives a `newFiles`
walk is an error — it would be rendered into the target as output instead of
being applied as a patch. Fix it by excluding the directory the patch lives in.

## Example

```bash
loom validate ./onboard-service
```

All violations are reported together, one per line, so a config can be fixed in
a single pass. On success, prints a confirmation and exits 0; on failure, prints
the violations and exits non-zero.
