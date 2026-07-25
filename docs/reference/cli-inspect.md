# loom inspect

Show what a module is made of, without running any of it.

```
loom inspect [path] [flags]
```

By default inspect describes **one** module — its parameters and where their
values come from, its target repository, and its operations in execution order —
and lists the submodules it composes by name, without opening them.

That is the usual question ("what is this module, and what does it need?"), and
it stays fast: a listed submodule is never fetched, so a shallow look at a module
composing half a dozen remote ones does no network I/O at all.

It is the command to reach for before a first run of an unfamiliar module — to
see what it will do, and to find out what you have to pass it.

## Flags

| Flag | Description |
|------|-------------|
| `-p, --param key=value` | Parameter for the root module. Repeatable. |
| `--params-file <file>` | YAML file with parameters. |
| `--full` | Describe every module in the tree. Same as `--depth 0`. |
| `--depth <n>` | Levels of module to describe. `1` (default) is the subject alone; `0` means all of them. |
| `-m, --module <name>` | Describe this submodule instead of the root, by instance name or `parent/child` path. Repeatable. |
| `--no-fetch` | Do not clone modules sourced from a git URL. They are listed, but not described. |
| `-o, --output <format>` | `tree` (default) or `json`. |

`--full` and an explicit `--depth` set the same limit two ways; passing both is
an error rather than a silent winner.

## Going Deeper

A module reported with a trailing `…` was listed but not read — inspect knows
only what its parent declares about it. Three ways to open one up:

```bash
loom inspect ./platform-rollout --full          # describe the whole tree
loom inspect ./platform-rollout --depth 2       # one more level
loom inspect ./platform-rollout -m svc-a-prod   # make that submodule the subject
```

`--module` matches the **tail** of a module's breadcrumb, so a bare name finds a
module wherever it sits, and `svc-b/docs` distinguishes two that share a name.
Matching several is an error listing the candidates, rather than a silent pick of
the wrong one. The summary's hint always suggests a query that resolves.

Repeat it to describe several modules at once — comparing two siblings, for
instance:

```bash
loom inspect ./platform-rollout -p env=prod -m svc-a-prod -m svc-b
```

Each is reported with its own breadcrumb, in the order you asked for them, under
**one** summary: what you have to supply is a single list however many modules
you looked at. Naming the same module twice describes it once, and two subjects
may overlap — a module and one it composes — without either affecting how the
other is reported.

A focused module is reported with its breadcrumb from the root, and its
parameters carry the values its parents actually hand it — not the defaults it
would show if you inspected its directory directly:

```
[≡ docs ≡] ../grandchild
in platform-rollout › svc-b › docs
├─ params
│    title  provided  = "svc-b docs" ← {{ .service }} docs
```

Finding a module means reading the tree that holds it, so `--module` walks in
full regardless of `--depth`; the limit then applies below the subject.

## Nothing Runs

Inspect executes none of the module's behavior:

- No operation runs.
- No `dynamicParams` command runs — the command is shown, the value is not.
- No `if` condition is evaluated — the condition is shown, and the module or
  operation it gates is described either way.
- No target repository is cloned.

The one thing it does fetch is a module `source` that is a git URL, and only when
that module is actually being described, since its contents cannot be read
otherwise. Those clones are temporary and removed before inspect exits;
`--no-fetch` skips them entirely, listing remote modules instead.

## Example

```bash
loom inspect ./platform-rollout -p env=prod
```

```
[≡ platform-rollout ≡] ./platform-rollout
├─ params
│    env         provided  = "prod"
│    owner       default   = "platform-team"
│    note        optional  = ""
│    commitHash  dynamic   $ git rev-parse --short HEAD (falls back to "unknown")
├─ operations (1, in order)
│    announce  shell  echo rolling out to {{ .env }}
├─ ▸ svc-a-prod  ../child  if: test -d ./svc-a  …
└─ ▸ svc-b  ../child  …

✔ every required parameter of the module(s) shown is satisfied

2 submodule(s) not expanded — they may need parameters of their own:
  platform-rollout › svc-a-prod
  platform-rollout › svc-b

  --full describes all of them; --module svc-a-prod describes one
```

Note the scoped wording. The two submodules were never read, so nothing is known
about what *they* require — and inspect will not claim a tree is fully
parameterized on the strength of modules it did not open.

Expanding one shows the rest:

```bash
loom inspect ./platform-rollout -p env=prod -m svc-a-prod
```

```
[≡ svc-a-prod ≡] ../child
in platform-rollout › svc-a-prod
├─ params
│    service    provided    = "svc-a"
│    namespace  provided    = "prod-apps" ← {{ .env }}-apps
│    region     required    must be supplied
│    stamp      unresolved  resolved at run time ← {{ .commitHash }}
├─ target  https://github.com/acme/svc-a-gitops.git (main) → loom/onboard-svc-a
├─ operations (3, in order)
│    render   newFiles  templates → manifests
│    gate     shell     kubeconform manifests  if: test -f manifests/app.yaml
│    open-pr  pr        github: Onboard {{ .service }}
└─ ▸ docs  ../grandchild  …

⚠ 1 required parameter(s) not supplied — a run would fail:
  region  platform-rollout › svc-a-prod

  supply them with -p name=value
```

## Reading the Tree

Each module hangs off its parent under a `▸` marker, labelled with the **instance
name** — the parent's `modules[].name`, rendered — which is the same identity the
run log and `loom diff` use. Its own `metadata.name` follows in parentheses when
the two differ, as they do whenever one module is composed under several names.

A trailing `…` marks a module that was listed but not read. Only what its parent
declares about it is known: its name, source, and `if` condition. It has no
`metadata.name` shown because that lives in a config inspect never opened.

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

The summary collects every required parameter that is missing **from the modules
it described**, each located by the breadcrumb of the module that declares it.
Anything listed but not read is reported separately, because its requirements are
unknown rather than absent. Use `--full` for the complete answer:

```bash
loom inspect ./platform-rollout --full -p env=prod
```

Note that `-p` supplies the **root** module only. A submodule gets its values
from its parent's `modules[].params`, so a requirement reported deep in the tree
is satisfied by the parent forwarding a value, not by another `-p` — unless the
parent declares the parameter and passes it down.

Missing parameters do not make inspect fail. Reporting them is the point.

## JSON Output

`-o json` emits the described module plus three roll-ups, for scripting and CI
checks:

```bash
loom inspect ./platform-rollout --full -p env=prod -o json | jq '.missingParams'
```

```json
[
  { "path": ["platform-rollout", "svc-a-prod"], "name": "region" }
]
```

| Field | Description |
|-------|-------------|
| `modules` | The described modules, each `{ path, module }`. Always a list — of one, unless `--module` was repeated — so it is indexed the same way either way. `path` is the breadcrumb from the root. |
| `modules[].module` | Each node carries `instance`, `name`, `source`, `params`, `operations`, and nested `modules`; a node with `"listed": true` was named but not read. |
| `missingParams` | Unsatisfied required parameters, with the breadcrumb of the module declaring each. Spans every described module, deduplicated. |
| `problems` | Modules that could not be described. |
| `unexpanded` | Breadcrumbs of the modules listed but not read. While this is non-empty, `missingParams` covers part of the tree, not all of it. |

A CI check that a module is fully parameterized should assert on `--full`, and
treat a non-empty `unexpanded` as a reason not to trust `missingParams`:

```bash
loom inspect ./mod --full -p env=prod -o json \
  | jq -e '.missingParams == [] and .unexpanded == []'
```

## Exit Status

Inspect exits non-zero when a module it tried to describe could not be — an
unresolvable source, or a config that fails validation. It exits zero otherwise,
including when required parameters are missing.

A module that was only listed is never resolved, so it cannot fail. A broken
submodule therefore surfaces at `--full` (or whatever depth reaches it), not at
the default depth — where the summary instead reports it as unexpanded.

A module whose config is structurally invalid is reported as an error and its
contents are not described. A module that merely refers to files that are not
there (a `newFiles.source` directory, a `patch.path` file) is described in full,
with those findings as warnings.

## See Also

- [`loom validate`](/reference/cli-validate) — check a single module's config.
- [`loom diff`](/reference/cli-diff) — see the changes a run would actually make.
- [Module Composition](/guide/module-composition) — how submodules and parameter
  hand-off work.
