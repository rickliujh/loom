# loom validate

Check that a `loom.yaml` is well-formed.

```
loom validate [path]
```

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
- Exclude/include patterns are usable globs — see [File filtering](#file-filtering) below
- Destinations stay inside the target directory: `patch.target`, `newFiles.dest`, `llm.target`
- `newFiles.source` is an existing directory and `patch.path` an existing file
- No patch file is also rendered into the target as module output

Checks that depend on a value only known at run time are skipped when the field
is templated.

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
