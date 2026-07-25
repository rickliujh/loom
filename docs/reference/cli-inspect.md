# loom inspect

Show what a module is made of, without running any of it.

```
loom inspect [path] [flags]
```

Inspect reads a module and every module it composes, then prints the tree: each
module's parameters and where their values come from, its target repository, and
its operations in execution order.

It is the command to reach for before a first run of an unfamiliar module — to
see what it will do, and to find out what you have to pass it.

## Flags

| Flag | Description |
|------|-------------|
| `-p, --param key=value` | Parameter for the root module. Repeatable. |
| `--params-file <file>` | YAML file with parameters. |
| `--depth <n>` | Maximum module depth to walk. `1` is the root alone; `0` (default) means unlimited. |
| `--no-fetch` | Do not clone modules sourced from a git URL. They are listed, but not described. |
| `-o, --output <format>` | `tree` (default) or `json`. |

## Nothing Runs

Inspect executes none of the module's behavior:

- No operation runs.
- No `dynamicParams` command runs — the command is shown, the value is not.
- No `if` condition is evaluated — the condition is shown, and the module or
  operation it gates is described either way.
- No target repository is cloned.

The one thing it does fetch is a module `source` that is a git URL, since a
module's contents cannot be described without reading them. Those clones are
temporary and removed before inspect exits; `--no-fetch` skips them entirely.

## Example

```bash
loom inspect ./platform-rollout -p env=prod
```

```
[≡ platform-rollout ≡] ./platform-rollout
├─ params
│    env         provided  = "prod"
│    owner       default   = "platform-team"
│    commitHash  dynamic   $ git rev-parse --short HEAD
├─ operations (1, in order)
│    announce  shell  echo rolling out to {{ .env }}
├─ ▸ svc-a-prod (service-onboard)  ../child  if: test -d ./svc-a
│  ├─ params
│  │    service    provided    = "svc-a"
│  │    namespace  provided    = "prod-apps" ← {{ .env }}-apps
│  │    region     required    must be supplied
│  │    stamp      unresolved  resolved at run time ← {{ .commitHash }}
│  ├─ target  https://github.com/acme/svc-a-gitops.git (main) → loom/onboard-svc-a
│  └─ operations (3, in order)
│       render   newFiles  templates → manifests
│       gate     shell     kubeconform manifests  if: test -f manifests/app.yaml
│       open-pr  pr        github: Onboard {{ .service }}
└─ ▸ svc-b (service-onboard)  ../child
   └─ …

⚠ 1 required parameter(s) not supplied — a run would fail:
  region  platform-rollout › svc-a-prod

  supply them with -p name=value
```

## Reading the Tree

Each module hangs off its parent under a `▸` marker, labelled with the **instance
name** — the parent's `modules[].name`, rendered — which is the same identity the
run log and `loom diff` use. Its own `metadata.name` follows in parentheses when
the two differ, as they do whenever one module is composed under several names.

## Parameter States

| State | Meaning |
|-------|---------|
| `provided` | Supplied — from `-p` at the root, or by the parent for a submodule. |
| `default` | Nothing supplied it, so the declared `default` applies. |
| `dynamic` | Comes from a `dynamicParams` command at run time. The command is shown; inspect never runs it. |
| `required` | Required, with no default and nothing supplying it. **A run would fail.** |
| `optional` | Optional, with no default and nothing supplying it. Renders as the empty string. |
| `unresolved` | Supplied by the parent through an expression that depends on a run-time value, typically a dynamic param. |

A value the parent passed is shown with the expression that produced it after a
`←`, so a templated hand-off stays traceable:

```
namespace  provided  = "prod-apps" ← {{ .env }}-apps
```

Templates that inspect cannot resolve are printed as authored rather than as a
placeholder. `{{ .service }}` in an operation stays `{{ .service }}`.

## Parameter Requirements

The summary collects every required parameter the tree is missing, each located
by the breadcrumb of the module that declares it.

Note that `-p` supplies the **root** module only. A submodule gets its values
from its parent's `modules[].params`, so a requirement reported deep in the tree
is satisfied by the parent forwarding a value, not by another `-p` — unless the
parent declares the parameter and passes it down.

Missing parameters do not make inspect fail. Reporting them is the point.

## JSON Output

`-o json` emits the whole tree plus two roll-ups, for scripting and CI checks:

```bash
loom inspect ./platform-rollout -p env=prod -o json | jq '.missingParams'
```

```json
[
  { "path": ["platform-rollout", "svc-a-prod"], "name": "region" }
]
```

| Field | Description |
|-------|-------------|
| `module` | The root of the inspected tree. Each node carries `instance`, `name`, `source`, `params`, `operations`, and nested `modules`. |
| `missingParams` | Every unsatisfied required parameter, with the breadcrumb of the module declaring it. |
| `problems` | Modules that could not be described. |

## Exit Status

Inspect exits non-zero when a module in the tree could not be described — an
unresolvable source, or a config that fails validation. It exits zero otherwise,
including when required parameters are missing.

A module whose config is structurally invalid is reported as an error and its
contents are not described. A module that merely refers to files that are not
there (a `newFiles.source` directory, a `patch.path` file) is described in full,
with those findings as warnings.

## See Also

- [`loom validate`](/reference/cli-validate) — check a single module's config.
- [`loom diff`](/reference/cli-diff) — see the changes a run would actually make.
- [Module Composition](/guide/module-composition) — how submodules and parameter
  hand-off work.
