# Structured Parameters

Most parameters are a single value: a service name, a namespace, an image tag. Some are not. An Argo CD `Application` with several sources — a Helm chart plus a git repo holding its values, a kustomize path, a plain directory — needs a **list of maps**, and each client wants a different list. This page walks through that case end to end.

A parameter declares its shape with `type`:

```yaml
params:
  - name: appName          # type: string, the default
    required: true
  - name: sources
    type: list             # a YAML list of anything
    required: true
  - name: helmValues
    type: map              # a YAML map of anything
    default: {}
```

A `list` or `map` param holds real YAML. Templates `range` over it, index into it, and write it back out with `toYaml`.

## The module

An Argo CD multi-source `Application` lists its sources under `spec.sources`. The module below renders one `Application` per run into `apps/<appName>.yaml` of the GitOps repo:

```
argocd-app/
├── loom.yaml
└── templates/
    └── apps/
        └── {{ .appName }}.yaml
```

```yaml
# argocd-app/loom.yaml
apiVersion: loom.rickliujh.github.io/v1beta1
kind: Loom
metadata:
  name: argocd-app
spec:
  params:
    - name: appName
      required: true
    - name: namespace
      default: default
    - name: sources
      type: list
      required: true
  target:
    url: "https://github.com/myorg/gitops.git"
    branch: main
    featureBranch: "loom/app-{{ .appName }}"
  operations:
    - name: render
      newFiles:
        source: templates
```

There are two ways to write the template, depending on who owns the shape of a source.

### Pass-through: the client owns the shape

When clients know Argo CD and should be free to use any source field — `helm`, `kustomize`, `directory`, `plugin` — write the list out as given:

```yaml
# templates/apps/{{ .appName }}.yaml
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: {{ .appName }}
  namespace: argocd
spec:
  project: default
  sources: {{- .sources | toYaml | nindent 4 }}
  destination:
    server: https://kubernetes.default.svc
    namespace: {{ .namespace }}
```

`toYaml` re-serializes the list and `nindent 4` places it under `sources:`. Every field comes through, however deeply nested.

### Shaped: the platform team owns the shape

When the platform team wants to enforce fields and fill in defaults, `range` over the list and write each source field by field:

```yaml
# templates/apps/{{ .appName }}.yaml
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: {{ .appName }}
  namespace: argocd
spec:
  project: default
  sources:
  {{- range $i, $s := .sources }}
    - repoURL: {{ required (printf "sources[%d].repoURL is required" $i) $s.repoURL }}
      targetRevision: {{ default "HEAD" $s.targetRevision }}
      {{- with $s.chart }}
      chart: {{ . }}
      {{- end }}
      {{- with $s.path }}
      path: {{ . }}
      {{- end }}
      {{- with $s.ref }}
      ref: {{ . }}
      {{- end }}
      {{- with $s.helm }}
      helm: {{- toYaml . | nindent 8 }}
      {{- end }}
  {{- end }}
  destination:
    server: https://kubernetes.default.svc
    namespace: {{ .namespace }}
```

- `required` stops the run with your message when a source has no `repoURL`; the message names the item.
- `default "HEAD"` fills in a missing `targetRevision`.
- `with` writes an optional field only when the source sets it.
- Inside the `range` body, dot is the current source. A top-level param is still reachable through `$`: <code v-pre>{{ $.namespace }}</code>.

## Supplying the sources

### A params file

```yaml
# guestbook.yaml
appName: guestbook
namespace: guestbook
sources:
  - repoURL: https://charts.example.com
    chart: guestbook
    targetRevision: 1.10
    helm:
      releaseName: guestbook
      valueFiles:
        - $values/guestbook/values-prod.yaml
      parameters:
        - name: replicaCount
          value: "3"
          forceString: true
  - repoURL: https://git.example.com/guestbook-values.git
    ref: values
```

```bash
loom run ./argocd-app --params-file guestbook.yaml
```

With the shaped template this renders:

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: guestbook
  namespace: argocd
spec:
  project: default
  sources:
    - repoURL: https://charts.example.com
      targetRevision: 1.10
      chart: guestbook
      helm:
        parameters:
          - forceString: true
            name: replicaCount
            value: "3"
        releaseName: guestbook
        valueFiles:
          - $values/guestbook/values-prod.yaml
    - repoURL: https://git.example.com/guestbook-values.git
      targetRevision: HEAD
      ref: values
  destination:
    server: https://kubernetes.default.svc
    namespace: guestbook
```

Values arrive exactly as written: the chart version `1.10` is not turned into `1.1`, `"3"` stays a quoted string, `true` stays a boolean. The one thing that changes is key order — `toYaml` writes a map's keys sorted, because a parsed map does not remember the order they were written in. (The shaped template controls the order of the fields it writes itself.)

### The command line

`-p` takes YAML for a list param — flow style inline, or a file through the shell:

```bash
loom run ./argocd-app -p appName=guestbook \
  -p sources='[{repoURL: https://charts.example.com, chart: guestbook, targetRevision: 1.10}]'

loom run ./argocd-app -p appName=guestbook -p sources="$(cat sources.yaml)"
```

A value that is not a list fails before anything runs:

```
✖ resolving params for argocd-app: param "sources" is declared list, but received a string that parses as a map, not a list: "{repoURL: https://x}"
```

### A parent module

A platform module can compose `argocd-app` once per application, passing each a literal list — its strings rendered with the parent's params — or forwarding a list it was given:

```yaml
# platform/loom.yaml
apiVersion: loom.rickliujh.github.io/v1beta1
kind: Loom
metadata:
  name: platform
spec:
  params:
    - name: chartRepo
      default: https://charts.example.com
    - name: redisSources
      type: list
      required: true
  modules:
    - name: guestbook
      source: ../argocd-app
      params:
        appName: guestbook
        sources:
          - repoURL: "{{ .chartRepo }}"
            chart: guestbook
            targetRevision: 1.10
          - repoURL: https://git.example.com/guestbook-values.git
            ref: values
    - name: redis
      source: ../argocd-app
      params:
        appName: redis
        sources: "{{ .redisSources | toYaml }}"
```

The `redis` entry passes a string; because the child declares `sources` as a list, the string is parsed back into one. [`loom.jsonnet`](/reference/loom-yaml#loom-jsonnet) parents can pass real arrays and objects the same way.

## Mistakes caught for you

| Mistake | What happens |
|---------|--------------|
| A source without `repoURL` (shaped template) | The run fails: `sources[1].repoURL is required` |
| A template prints a field a source does not set, unguarded | The run fails instead of writing `<no value>` into the repo — guard it with `default`, `required`, `if` or `with` |
| `-p sources=` something that is not a list | The run fails naming the param and what it received |
| A `list` param with a string `default` | `loom validate` reports it |
| <code v-pre>{{ .sources }}</code> printed bare, without `toYaml` | `loom validate` warns: Go's own formatting (`[map[chart:guestbook]]`) is not YAML |
| <code v-pre>{{ .sources \| nindent 4 }}</code> — a list handed to a text function | The run fails (`nindent: value is a list, not text`), and `loom validate` warns ahead of it |

## Still supported: YAML in a string

Before typed params, the way in was a string parameter holding YAML, parsed in the template with `fromYaml`. Modules written that way keep working unchanged:

```yaml
# params.yaml
sources: |
  - repoURL: https://charts.example.com
    chart: guestbook
    targetRevision: 1.10
```

```yaml
spec:
  sources: {{- .sources | fromYaml | toYaml | nindent 4 }}
```

`fromYaml` keeps scalars' written form too, so `1.10` survives here as well. For new modules, prefer `type: list`: the value is checked when the run starts rather than when the template renders, `inspect` shows it as a list, and the params file reads as plain YAML.

A runnable copy of this example — the shaped module, the `guestbook.yaml` params file, and the `platform` parent — ships in the repository under `example-structured/`.

See also: [Templates](/guide/templates) for every function, and the [`loom.yaml` reference](/reference/loom-yaml#types) for the typing rules.
