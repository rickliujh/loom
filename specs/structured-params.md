# Structured Parameters — Behavioral Specification

This document describes how loom parameters carry structured values — lists and maps as well as strings — and the template behavior that goes with them.

A module's resolved params are a map from name to value. A value is a string, a list, or a map; lists and maps nest, and their leaves are strings, numbers, booleans, or null. Scalars stay text at heart: loom emits files, so a scalar is always printed exactly as it was written.

Modules described here conform to the module specification defined in [`specs/module.md`](module.md).

---

## Value Model

#### SP1: Scalars keep their written form

Every YAML value that becomes part of a param — a params file, a structured default, a child's `params` entry, a string parsed as YAML, and the result of `fromYaml` — is decoded the same way:

| Written as | Decoded as | Prints as | `toYaml` emits |
|---|---|---|---|
| `plain`, `"quoted"`, `'quoted'` | string | the text | the text, quoted where YAML requires it (`"3"`, `"true"`) |
| `3`, `-7`, `1.5` (text survives a round trip) | int / float | `3`, `-7`, `1.5` | `3`, `-7`, `1.5` |
| `1.10`, `010`, `1e3`, `0x1F`, `+3`, very large ints (text would change) | number kept as its text | the text | the text, unquoted |
| `true`, `false` | bool | `true`, `false` | `true`, `false` |
| `null`, `~`, empty | null | — (a missing value, see SP5) | `null` |

A number whose native form would re-serialize differently keeps its source text: a chart version written `1.10` is never turned into `1.1`. Numbers that round-trip unchanged stay native, so `{{ if eq .replicas 3 }}` works; a kept-as-text number compares equal to its text (`{{ eq .version "1.10" }}`).

```yaml
# params.yaml
sources:
  - chart: nginx
    targetRevision: 1.10     # stays 1.10
    value: "3"               # stays the string "3"
    enabled: true            # stays a bool
```

```yaml
# template: {{ .sources | toYaml }}
- chart: nginx
  enabled: true
  targetRevision: 1.10
  value: "3"
```

Anchors, aliases, and merge keys (`<<`) resolve as YAML defines them. A mapping that repeats a key is an error — including a param definition in `spec.params`.

`toYaml` and `toJson` emit map keys in sorted order: a map carries no key order once decoded.

---

## Template Functions

These extend the function table of module spec T2. Functions that take text (`upper`, `lower`, `quote`, `indent`, `nindent`, `split`, and the elements `join` joins) accept any scalar and use its printed form. Handed a missing value (nil) they fail the render instead of printing nothing; handed a list or map they fail too, rather than write Go's own formatting (`[a b]`, `map[k:v]`) — render those with `toYaml`, `toJson`, or `join`.

| Function | Signature | Description |
|---|---|---|
| `default` | `default <fallback> <value>` | `value`, unless it is empty (SP2) — then `fallback` |
| `required` | `required <message> <value>` | `value`, or fail the render with `message` when it is empty (SP3) |
| `hasKey` | `hasKey <map> <key>` | Whether the map holds the key (even one set to null); false for anything that is not a map |
| `toJson` | `toJson <value>` | Compact JSON |
| `join` | `join <separator> <list>` | The list's elements as text, separated; pipe-friendly like `split`: `{{ .regions \| join "," }}` |

#### SP2: `default` works on any value

A value is **empty** when it is null (including a missing map key), the empty string, an empty list, or an empty map. `0` and `false` are values, not absences, and are never replaced.

```yaml
# sources: [{repoURL: a}, {repoURL: b, targetRevision: 1.10}]
{{ range .sources }}{{ default "HEAD" .targetRevision }};{{ end }}
# rendered: HEAD;1.10;
```

For strings the behavior is unchanged: `""` is replaced, anything else is kept.

#### SP3: `required` fails the render with the author's message

`required` returns its value when it is not empty (SP2's rule), and otherwise fails the render with exactly the message given — so the message can name the item that is wrong.

```yaml
{{ range $i, $s := .sources }}
- repoURL: {{ required (printf "sources[%d].repoURL is required" $i) $s.repoURL }}
{{ end }}
# sources[1] has no repoURL → error: ... sources[1].repoURL is required
```

#### SP4: `hasKey`, `toJson`, `join`

Per the table above. `join` of null is `""`; `join` of anything that is not a list is an error.

---

## Missing Values

#### SP5: A render that prints a missing value fails

A missing map key evaluates to null: it is falsy, and works with `default`, `required`, `if`, and `with`. Printed unguarded, Go templates write it as the literal `<no value>`. Writing that into a target repo is never intended, so a render whose output contains `<no value>` fails — unless the literal could have come from somewhere else. The rule is decided per render:

| The literal `<no value>` appears in… | Result |
|---|---|
| the template text itself | the guard is off for this render |
| any string in the params, at any depth, or a map key (a PR body quoting the message, say) | the guard is off for this render |
| neither | any `<no value>` in the output is a printed missing value: the render fails |

```yaml
# -p body='fix: stop printing <no value> in manifests'
pr:
  body: "{{ .body }}"          # renders as written
```

```yaml
# sources: [{repoURL: a}]
targetRevision: {{ (index .sources 0).targetRevision }}
# error: template printed a missing value: "<no value>" appears on output line 1;
#        guard the field with default, required, if or with
```

This applies to every render: file contents, patch contents, and every templatable `loom.yaml` field.

#### SP6: Every declared param is present

A declared param that is optional, has no default, and is not supplied resolves to the empty value, not to "missing". Templates that print it print nothing, exactly as before structured params.

```yaml
params:
  - name: suffix        # optional, no default, not supplied
# {{ .suffix }} → ""    {{ if .suffix }} → false    {{ default "x" .suffix }} → x
```

---

## Declaring Types

#### SP7: A param declares its type

`spec.params[]` and `spec.dynamicParams[]` take an optional `type`:

| `type` | Value | Unset, no default (SP6) |
|---|---|---|
| `string` (default when omitted) | a scalar, used as its text | `""` |
| `list` | a YAML sequence of any values | `[]` |
| `map` | a YAML mapping of any values | `{}` |

There are no number or boolean types: a scalar param is text, because loom emits text.

`type` is part of the param definition and so, like the rest of `spec.params`, is a literal — never a template (module spec T4).

A `list` or `map` param's `default` is written as YAML of that shape. A `string` param's scalar default is the exact text written: `default: 1.10` is `"1.10"`.

```yaml
params:
  - name: sources
    type: list
    default:
      - repoURL: https://charts.example.com
        chart: nginx
        targetRevision: 1.10
  - name: values
    type: map
    default: {replicas: 3}
  - name: env                  # type: string
    default: prod
dynamicParams:
  - name: regions
    type: list
    command: "cat regions.yaml"   # stdout parsed as YAML (SP14)
```

#### SP8: Type and default shape are validated

`type` must be one of `string`, `list`, `map`. A `default` whose shape is not the declared type — a list default on a string param, a string on a list param — is a validation error: a run could never use it. An empty default (`""`, `[]`, `{}`) is no default, as an empty string always was.

---

## Validation

#### SP9: Reference checking reads structured access

`loom validate` checks that templates reference only declared params (module spec, Validation Rules). With structured params:

- `{{ .name }}`, `{{ .name.sub }}`, `{{ index .name "k" }}`, and `{{ index . "name" }}` all reference `name`.
- Inside a `range` or `with` body, dot is the item — a list element, a nested map — so `{{ .repoURL }}` there is a field of the item, never a param.
- `$` is the param map everywhere: `{{ $.env }}` inside a `range` body is a reference to `env` and is checked like any other.
- The `else` branch of `range` and `with` runs with dot unchanged and is checked as top-level text.
- A bare `$` or dot handed to a function (`{{ toYaml $ }}`) reads the param map opaquely; as before, such a template is exempt from reference checking and disables the unused-param warning.
- Every string at any depth of a structured `spec.modules[].params` value is a template: it is syntax-checked, its references are checked, and it counts as a use. The field is named by its path: `module "child" param "sources[0].repoURL"`.

Printing a `list` or `map` param bare — `{{ .sources }}`, or `{{ $.sources }}` in a body — writes Go's own formatting (`[map[chart:nginx]]`) into the target. The run does exactly what the template says, so this is a **warning**, not a violation: render it with `toYaml`, `toJson`, `join`, or `range`.

Handing one to a function that takes text — as an argument (`{{ nindent 2 .cfg }}`) or through a pipe (`{{ .cfg | nindent 2 }}`), to `upper`, `lower`, `quote`, `indent`, `nindent`, `split`, `print`, `printf`, or `println` — is warned about the same way. The loom functions fail such a render (SP4) and the print builtins write Go's formatting; whether the template reaches that line is not known statically, so it stays a warning.

```yaml
# sources is a list param
a: {{ .sources }}                     # warning: prints list param "sources" directly
b: {{ .sources | toYaml | nindent 2 }}   # fine
{{ range .sources }}- {{ .repoURL }} in {{ $.env }}{{ end }}   # env is checked; repoURL is an item field
```

---

## Supplying Values

#### SP10: The declared type drives parsing

Wherever a value arrives for a param — a params file, `-p`, a parent's `spec.modules[].params`, a dynamic param's output — one rule turns it into the param's value:

| Declared | Received | Result |
|---|---|---|
| `list` / `map` | a list / map | used as is |
| `list` / `map` | a string | parsed as YAML (flow or block); must yield that kind. An empty string is the empty list / map |
| `list` / `map` | null | the empty list / map |
| `list` / `map` | anything else | error |
| `string` | a scalar (string, number, bool) | its text, exactly as written |
| `string` | null | `""` |
| `string` | a list / map | error |

A string param never parses its value: `-p tags='[a, b]'` for a string param is the text `[a, b]`.

```bash
loom run . -p sources='[{repoURL: https://charts.example.com, chart: nginx, targetRevision: 1.10}]'
loom run . -p sources="$(cat sources.yaml)"
loom run . -p sources='{repoURL: x}'
# error: param "sources" is declared list, but received a string that parses as a map, not a list: "{repoURL: x}"
```

#### SP11: A params file may nest

`--params-file` is a YAML mapping of param name to value. A value may be a list or a map, nested to any depth; its scalars keep their written form (SP1). A top-level scalar is kept as the exact text written — as a flat params file always read — so `replicas: 3`, `version: 1.10`, and `enabled: True` reach a string param as `3`, `1.10`, and `True`. The same holds for a top-level scalar merged in with `<<: *anchor`.

```yaml
# params.yaml
appName: guestbook
sources:
  - repoURL: https://charts.example.com
    chart: guestbook
    targetRevision: 1.10
  - repoURL: https://git.example.com/guestbook-values.git
    targetRevision: main
    ref: values
```

File values are loaded first and `-p` values override them (module spec P2), whole: a `-p` value replaces the file's value for that param, never merges into it.

#### SP12: `-p` values are text until typed

`-p key=value` keeps its syntax. The value is everything after the first `=`, and reaches the param as a string, which SP10 parses for a `list` or `map` param.

#### SP13: A parent passes structured values to a child

A `spec.modules[].params` value may be a string, list, or map. Every string in it, at any depth, is rendered with the parent's params (module spec T4); the result is then typed against the child's declaration (SP10). A parent can therefore pass a literal list, or forward one of its own:

```yaml
modules:
  - name: literal
    source: ./argocd-app
    params:
      sources:
        - repoURL: "{{ .chartRepo }}"     # rendered with the parent's params
          chart: nginx
          targetRevision: 1.10           # stays 1.10
  - name: forwarded
    source: ./argocd-app
    params:
      sources: "{{ .sources | toYaml }}" # a string; the child's list type parses it
```

A top-level scalar in `params` is the exact text written, as before. `loom.jsonnet` evaluates to the same schema, so a jsonnet parent passes real arrays and objects the same way. (A jsonnet number is evaluated before loom sees it, so write a version like `'1.10'` as a string in jsonnet.)

#### SP14: A dynamic param's output is typed

A `dynamicParams[]` entry with `type: list` or `type: map` has its command's stdout — or, if the command fails, its rendered `default` — parsed per SP10. A value supplied with `-p` overrides the command (module spec P6) and is typed the same way.

```yaml
dynamicParams:
  - name: regions
    type: list
    command: "yq '.regions' clusters.yaml"   # prints a YAML list
    default: "[us-east-1]"                   # used if the command fails
```

---

## Commands

#### SP15: `inspect` reports each param's type and structured values

Every inspected parameter carries its declared `type` (`string` when omitted). A supplied value is typed exactly as a run would type it (SP10), so `-p sources='[...]'` is reported as the list it becomes; one that the declared type rejects is reported as supplied, with a warning that a run would fail. `value`, `default`, and `from` of a `list` or `map` param are real JSON arrays and objects in `--output json`:

```json
{"name": "appName", "type": "string", "state": "default", "value": "guestbook", "default": "guestbook"}
{"name": "sources", "type": "list", "state": "provided", "required": true,
 "value": [{"chart": "guestbook", "repoURL": "https://charts.example.com", "targetRevision": 1.10}]}
{"name": "labels", "type": "map", "state": "default", "value": {"team": "platform"}, "default": {"team": "platform"}}
{"name": "extra", "type": "list", "state": "unset"}
```

The tree output shows a structured value on one line — its size, then compact JSON cut short when long — and string params exactly as before:

```
appName   default   = "guestbook"
sources   provided  = [1 item] [{"chart":"guestbook","repoURL":"https://charts.example.com"…
labels    default   = {1 key} {"team":"platform"}
extra     optional  = []
```

Whether a template resolves is decided from what it references, not from what it happens to print (inspect spec IN8):

- A template that references a param inspect cannot know — dynamic, missing, or unresolved — anywhere, in any position, stays unresolved: behind `default`, `if`, `with`, `not`, or `eq`, under any `printf` verb, indexed, ranged over, reached through `$`, or in a branch the render would not take. `{{ default "main" .commitHash }}` and `{{ if .commitHash }}a{{ else }}b{{ end }}` are shown as authored, never as `main` or `b`.
- While any param is unknown, a template that reaches the param map in a way whose keys cannot be read (`{{ index . .key }}`, `{{ template "x" . }}`, `{{ toYaml $ }}`) stays unresolved too.
- A child param its parent could not render — the whole value, or any leaf of a structured one — is `unresolved` with no value, and the child's own templates treat it as unknown. Its template text never stands in for a value.
- An unset optional param is known: it is `""`, `[]`, or `{}` (SP6), so `{{ default "x" .suffix }}` shows `x`.

#### SP16: `bulk` items may be structured

A `--items` entry may give a `list` or `map` param a real list or map. Items decode like a params file (SP11), each value is checked against the child's declared type (SP10) before anything is written, and structured values are emitted as Jsonnet arrays and objects. A number kept as its written text (SP1) is emitted as a Jsonnet string — as a Jsonnet number, `1.10` would evaluate to `1.1`. Placeholder items give an optional `list`/`map` param without a default `[]`/`{}`, and a required one `'CHANGEME'`, which a run rejects until it is edited. `--name-param` must name a `string` param.

```jsonnet
local items = [
  {
    serviceName: 'payments',
    sources: [
      {
        chart: 'payments',
        repoURL: 'https://charts.example.com',
        targetRevision: '1.10',
      },
    ],
  },
];
```

---

## Error Conditions

| Condition | Error |
|-----------|-------|
| Output contains `<no value>` not written by the template | `template printed a missing value: "<no value>" appears on output line <n>; guard the field with default, required, if or with` |
| `required` on an empty value | `<message>` (as given) |
| Text function given a missing value | `<fn>: value is missing; guard it with default, required, if or with` |
| Text function given a list or map | `<fn>: value is a list, not text; render it with toYaml, toJson or join` / `<fn>: value is a map, not text; render it with toYaml or toJson` |
| `join` given a non-list | `join: expected a list, got <type>` |
| YAML mapping repeats a key (in a value, a params file, or a param definition) | `line <n>: mapping key "<key>" already defined at line <m>` |
| Supplied value does not have the declared type | `param "<name>" is declared <type>, but received <what>` |
| String for a list/map param is not YAML of that kind | `param "<name>" is declared <type>, but received a string that parses as <what>, not a <type>: "<value>"` / `... a string that is not valid YAML (<err>): "<value>"` |
| Dynamic param output does not have the declared type | `dynamic param "<name>": param "<name>" is declared <type>, ...` |
| Params file is not a mapping | `parsing params file: line <n>: expected a mapping of param names to values, got <kind>` |
| `bulk` item value does not have the declared type | `item <N>: param "<name>" is declared <type>, but received ...` |
| `bulk --name-param` names a list or map param | `--name-param "<name>" is a <type> parameter; it must be a string` |
| Unknown param `type` | `param "<name>": unknown type "<type>" (supported: string, list, map)` |
| Unknown dynamic param `type` | `dynamicParam "<name>": unknown type "<type>" (supported: string, list, map)` |
| `default` shape differs from `type` | `param "<name>": default is a <shape>, but the param's type is <type>` |
| Unknown field in a param definition | `line <n>: field <field> not found in type config.ParamDef` |
| *(warning)* List or map param printed bare | `<field>: prints <type> param "<name>" directly, which writes Go's formatting (like [map[k:v]]); render it with toYaml, toJson, join or range` |
| *(warning)* List or map param handed to a text function | `<field>: passes <type> param "<name>" to <fn>, which takes text; render it with toYaml, toJson, join or range` |
