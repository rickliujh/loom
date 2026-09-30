# Templates

Loom uses Go's [`text/template`](https://pkg.go.dev/text/template) syntax. Inside any templatable string, you can reference parameters with <code v-pre>{{ .paramName }}</code>.

## Functions

| Function | Example | Result |
|----------|---------|--------|
| Parameter access | <code v-pre>{{ .serviceName }}</code> | `payments` |
| Default value | <code v-pre>{{ default "prod" .env }}</code> | `prod` if `.env` is empty — `""`, missing, an empty list or map (not `0` or `false`) |
| Required value | <code v-pre>{{ required "repoURL is required" .repoURL }}</code> | The value, or the run fails with the message when it is empty |
| Uppercase | <code v-pre>{{ upper .serviceName }}</code> | `PAYMENTS` |
| Lowercase | <code v-pre>{{ lower .serviceName }}</code> | `payments` |
| Indent | <code v-pre>{{ indent 2 .config }}</code> | Every line prefixed with 2 spaces |
| Newline + indent | <code v-pre>{{ nindent 4 .config }}</code> | Leading newline, then every line indented 4 spaces |
| Quote | <code v-pre>{{ quote .value }}</code> | `"payments"` (escaped double-quoted string) |
| To YAML | <code v-pre>{{ toYaml .value }}</code> | Value marshaled as 2-space-indented YAML |
| To JSON | <code v-pre>{{ toJson .value }}</code> | Value marshaled as compact JSON |
| From YAML | <code v-pre>{{ fromYaml .items }}</code> | String parsed into a value (list, map, or scalar) |
| Split | <code v-pre>{{ .regions \| split "," }}</code> | String divided around a separator, empty elements dropped |
| Join | <code v-pre>{{ .regions \| join "," }}</code> | List elements joined by a separator |
| Has key | <code v-pre>{{ if hasKey .helm "values" }}</code> | Whether a map holds the key |

`toYaml` and `toJson` write map keys in sorted order: once parsed, a map no longer remembers the order its keys were written in.

The functions that take text — `upper`, `lower`, `quote`, `indent`, `nindent`, `split` — accept any single value. Given a list or a map they fail the render instead of writing Go's own formatting (`[a b]`, `map[k:v]`); turn it into text first with `toYaml`, `toJson` or `join`, as in <code v-pre>{{ .config | toYaml | nindent 4 }}</code>.

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

## Lists and Maps

A param declared `type: list` or `type: map` holds real structured YAML — the recommended way to pass anything that is not a single value. See [Structured Parameters](/guide/structured-params) for the full walk-through; in short:

```yaml
# loom.yaml
params:
  - name: sources
    type: list
    required: true
```

```yaml
# params.yaml
sources:
  - repoURL: https://charts.example.com
    chart: nginx
    targetRevision: 1.10
  - repoURL: https://git.example.com/values.git
    ref: values
```

Pass the whole value through, re-indented:

```yaml
spec:
  sources: {{- .sources | toYaml | nindent 4 }}
```

Or shape each item, filling in and enforcing fields:

```yaml
spec:
  sources:
  {{- range $i, $s := .sources }}
    - repoURL: {{ required (printf "sources[%d].repoURL is required" $i) $s.repoURL }}
      targetRevision: {{ default "HEAD" $s.targetRevision }}
  {{- end }}
```

Inside a `range` or `with` body, dot is the current item; reach a top-level param with `$`, as in <code v-pre>{{ $.namespace }}</code>.

Scalars keep the text they were written with: `targetRevision: 1.10` renders as `1.10`, never `1.1`; `value: "3"` stays a quoted string; `true` stays a boolean. Numbers that survive a round trip unchanged (`3`, `1.5`) are real numbers, so <code v-pre>{{ if eq .replicas 3 }}</code> works.

### Lists in string parameters

String parameters can still carry YAML, and `fromYaml` parses it into a value — the approach that predates typed params, and still fully supported. `fromYaml` keeps scalars' written form the same way.

Range over a list held in a string (flow style like `regions: "[us-east-1, eu-west-1]"`, or a block scalar):

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

`split` drops empty elements, so an empty parameter or a trailing separator yields no blank list items. `join` goes the other way: <code v-pre>{{ .regions | join "," }}</code> on a list param gives `us-east-1,eu-west-1`.

## Missing Values

A field that is not there — `targetRevision` on a source that does not set it — is empty (nil) inside a template. That makes it work with `default`, `required`, `if` and `with`:

```yaml
targetRevision: {{ default "HEAD" .targetRevision }}
{{- with .chart }}
chart: {{ . }}
{{- end }}
```

Printed unguarded, Go would write it as the literal text `<no value>`. Loom refuses to write that into your repo: the render fails, naming the output line.

```
✖ operation "render" failed: operation "newFiles": rendering template file "apps/{{ .appName }}.yaml":
  template printed a missing value: "<no value>" appears on output line 12;
  guard the field with default, required, if or with
```

The check stands down for a render whose template contains the literal text `<no value>` itself (documentation about Loom, say), or whose parameters carry it anywhere — a PR body passed as <code v-pre>-p body='stop printing &lt;no value&gt;'</code> renders as written.

A declared parameter that is optional, has no default, and is not supplied is never missing: it is `""`, `[]` or `{}` by its type.

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
