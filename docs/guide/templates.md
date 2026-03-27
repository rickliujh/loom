# Templates

Loom uses Go's [`text/template`](https://pkg.go.dev/text/template) syntax. Inside any templatable string, you can reference parameters with `{{ .paramName }}`.

## Functions

| Function | Example | Result |
|----------|---------|--------|
| Parameter access | `{{ .serviceName }}` | `payments` |
| Default value | `{{ default "prod" .env }}` | `prod` if `.env` is empty |
| Uppercase | `{{ upper .serviceName }}` | `PAYMENTS` |
| Lowercase | `{{ lower .serviceName }}` | `payments` |

## Where Templates Work

Templates are evaluated in:

- File contents (`newFiles`)
- File and folder paths (`newFiles`) -- see [Path Templating](#path-templating)
- Shell commands
- Commit messages
- PR/MR titles and bodies
- Feature branch names
- Child module parameters
- Dynamic param commands

## Path Templating

File and folder names are rendered as Go templates, just like file contents. You can use `{{ .paramName }}` directly in file and directory names.

| Source path | With `serviceName=payments`, `env=prod` | Result |
|-------------|------------------------------------------|--------|
| `{{ .env }}/config.yaml` | | `prod/config.yaml` |
| `application-{{ .serviceName }}.yaml` | | `application-payments.yaml` |
| `{{ .env }}/{{ .serviceName }}-deploy.yaml` | | `prod/payments-deploy.yaml` |

This means your module directory can look like:

```
onboard-service/
├── loom.yaml
├── {{ .env }}/
│   └── {{ .serviceName }}-app.yaml
└── shared/
    └── config.yaml
```

Running with `-p serviceName=payments -p env=prod` produces:

```
prod/
└── payments-app.yaml
shared/
└── config.yaml
```

### Double-Underscore Syntax

For convenience, Loom also supports a filesystem-friendly `__paramName__` placeholder syntax. This is useful when your filesystem, shell, or editor has trouble with curly braces in filenames. Loom converts `__paramName__` to `{{ .paramName }}` before rendering. Both syntaxes can be mixed freely.

| `__paramName__` syntax | Equivalent Go template |
|------------------------|----------------------|
| `__env__/config.yaml` | `{{ .env }}/config.yaml` |
| `application-__serviceName__.yaml` | `application-{{ .serviceName }}.yaml` |
