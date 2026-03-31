# newFiles

Copies template files from the module directory into the target repository, rendering Go template expressions along the way.

## Usage

```yaml
- name: create-files
  newFiles:
    source: "."    # relative to module directory
    dest: ""       # relative to target repository root
```

| Field | Description |
|-------|-------------|
| `source` | Source directory relative to the module directory |
| `dest` | Destination directory relative to the target repository root. Empty string means root. |

## Behavior

Every file in the source directory is treated as a Go template, subject to the [exclude/include rules](/reference/loom-yaml#spec-excludes-and-spec-includes). The directory structure is preserved. File and folder names can also contain Go template expressions (see [Path Templating](/guide/templates#path-templating)).

**Default excludes**: `.git`, `README.md`, `loom.yaml`, and `loom.jsonnet`. Directories like `__functions/` must be explicitly listed in `excludes`.

## File Conflict Rules

- If a destination file **already exists**, the operation **fails** with an "already exists" error. Loom does not overwrite existing files.
- If a destination **directory** already exists, files are **merged** into it. New files are written; existing files in the directory are left untouched.
- If a destination directory does **not** exist, it is created.

## Dry Run

In dry-run mode, `newFiles` logs what would be written but does not create any files. If `--diff` is enabled, a unified diff is displayed showing the rendered content.

## Examples

Basic file copy:

```yaml
- name: create-manifests
  newFiles:
    source: "templates"
    dest: ""
```

With a destination subdirectory:

```yaml
- name: create-configs
  newFiles:
    source: "configs"
    dest: "environments/prod"
```
