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
- Patch engines are valid (`smp` or `json6902`)

## Example

```bash
loom validate ./onboard-service
```

On success, exits with code 0 and prints nothing. On failure, prints validation errors and exits with a non-zero code.
