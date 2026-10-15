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

Both fields are rendered as Go templates over the resolved params before use.

## Behavior

Every file in the source directory is treated as a Go template, subject to the [exclude/include rules](/reference/loom-yaml#spec-excludes-and-spec-includes). The directory structure is preserved. File and folder names can also contain Go template expressions (see [Path Templating](/guide/templates#path-templating)).

**Default excludes**: `.git`, `README.md`, `loom.yaml`, and `loom.jsonnet`. Directories like `__functions/` must be explicitly listed in `excludes`.

## File Conflict Rules

- If a destination file **already exists**, the operation **fails** with an "already exists" error. Loom does not overwrite existing files.
- If a destination **directory** already exists, files are **merged** into it. New files are written; existing files in the directory are left untouched.
- If a destination directory does **not** exist, it is created.

## Staying Inside the Target

Every file is written inside the target repository. If `dest`, or a rendered file or directory name, resolves to a path outside it, the operation fails and nothing is written:

```
newFiles dest "../elsewhere" escapes the target directory
```

This matters most when `dest` is built from a param, because `loom validate` can only check a value written literally in the config. The run checks the rendered value, in every mode including `--dry-run` and `loom diff`. A symlink in the target that points outside it is refused the same way.

## Dry Run

In dry-run mode, `newFiles` logs what would be written but does not create any files. Run [`loom diff`](/reference/cli-diff) to see a unified diff of the rendered content.

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

With a destination chosen by a param (`.` means the target root):

```yaml
- name: create-configs
  newFiles:
    source: "configs"
    dest: '{{ if eq .anthos "true" }}ACM{{ else }}.{{ end }}'
```
