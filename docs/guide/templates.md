# Templates

Loom uses Go's [`text/template`](https://pkg.go.dev/text/template) syntax. Inside any templatable string, you can reference parameters with <code v-pre>{{ .paramName }}</code>.

## Functions

| Function | Example | Result |
|----------|---------|--------|
| Parameter access | <code v-pre>{{ .serviceName }}</code> | `payments` |
| Default value | <code v-pre>{{ default "prod" .env }}</code> | `prod` if `.env` is empty |
| Uppercase | <code v-pre>{{ upper .serviceName }}</code> | `PAYMENTS` |
| Lowercase | <code v-pre>{{ lower .serviceName }}</code> | `payments` |
| Indent | <code v-pre>{{ indent 2 .config }}</code> | Every line prefixed with 2 spaces |
| Newline + indent | <code v-pre>{{ nindent 4 .config }}</code> | Leading newline, then every line indented 4 spaces |
| Quote | <code v-pre>{{ quote .value }}</code> | `"payments"` (escaped double-quoted string) |
| To YAML | <code v-pre>{{ toYaml .value }}</code> | Value marshaled as 2-space-indented YAML |

## Multi-line Values

Go templates substitute text literally, so inserting a multi-line parameter into an indented YAML block would leave its continuation lines at column 0. Use `nindent` to re-indent the whole value:

```yaml
data:
  app-config: |{{ .appConfig | nindent 4 }}
```

With `appConfig` set to `key1: val1\nkey2: val2`, this renders:

```yaml
data:
  app-config: |
    key1: val1
    key2: val2
```

Note the `|` sits directly against the template expression — `nindent` supplies the newline.

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

File and folder names are rendered as Go templates, just like file contents. You can use <code v-pre>{{ .paramName }}</code> directly in file and directory names.

| Source path | With `serviceName=payments`, `env=prod` | Result |
|-------------|------------------------------------------|--------|
| <code v-pre>{{ .env }}/config.yaml</code> | | `prod/config.yaml` |
| <code v-pre>application-{{ .serviceName }}.yaml</code> | | `application-payments.yaml` |
| <code v-pre>{{ .env }}/{{ .serviceName }}-deploy.yaml</code> | | `prod/payments-deploy.yaml` |

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

For convenience, Loom also supports a filesystem-friendly `__paramName__` placeholder syntax. This is useful when your filesystem, shell, or editor has trouble with curly braces in filenames. Loom converts `__paramName__` to <code v-pre>{{ .paramName }}</code> before rendering. Both syntaxes can be mixed freely.

| `__paramName__` syntax | Equivalent Go template |
|------------------------|----------------------|
| `__env__/config.yaml` | <code v-pre>{{ .env }}/config.yaml</code> |
| `application-__serviceName__.yaml` | <code v-pre>application-{{ .serviceName }}.yaml</code> |
