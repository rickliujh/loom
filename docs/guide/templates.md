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
| From YAML | <code v-pre>{{ fromYaml .items }}</code> | String parsed into a value (list, map, or scalar) |
| Split | <code v-pre>{{ .regions | split "," }}</code> | String divided around a separator, empty elements dropped |

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

## List Parameters

Parameters are always strings, but a string can contain YAML. `fromYaml` parses it into a real value so templates can range over it or re-serialize it.

Range over a list parameter (works with flow style like `regions: "[us-east-1, eu-west-1]"` or a block scalar):

```yaml
regions:
{{- range .regions | fromYaml }}
  - {{ . }}
{{- end }}
```

Or merge a list parameter into an existing list by round-tripping through `toYaml`, which validates and re-indents the fragment:

```yaml
env:
  - name: LOG_LEVEL
    value: info
  {{- .extraEnv | fromYaml | toYaml | nindent 2 }}
```

Malformed YAML in the parameter fails at render time instead of producing a broken file.

For simple comma-separated values, `split` avoids YAML syntax in the parameter entirely (with `regions: "us-east-1,eu-west-1"`):

```yaml
regions:
{{- range .regions | split "," }}
  - {{ . }}
{{- end }}
```

`split` drops empty elements, so an empty parameter or a trailing separator yields no blank list items.

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
