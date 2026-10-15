# loom serve API

The HTTP API behind the `loom serve` web UI. The UI is its only intended client, but the API is stable within a Loom version and can be scripted.

```
loom serve [--listen 127.0.0.1:7788] [--root DIR]... [--open]
```

All endpoints live under `/api/v1`. Requests and responses are JSON unless noted. The server binds `127.0.0.1` by default and every API call needs the per-process session token (see [Authentication](#authentication)).

## Conventions

**Errors.** The HTTP status is always set and the body is:

```json
{"error": {"code": "invalid_request", "message": "source must be an absolute path or a git URL", "details": {"field": "source"}}}
```

| Code | Status | Meaning |
|------|--------|---------|
| `unauthorized` | 401 | No or wrong session token. |
| `forbidden` | 403 | Foreign `Origin`, a non-JSON mutating request, or a path outside the allowed roots. |
| `misdirected` | 421 | `Host` is not an allowed host. |
| `not_found` | 404 | No such job, topic, file or module. |
| `conflict` | 409 | The job cannot be cancelled in its state, a path is held by a running job, or a file changed since it was read. |
| `invalid_request` | 400 | Malformed body or parameters. |
| `unprocessable` | 422 | The module could not be loaded or validated; `details` carries the messages. |
| `internal` | 500 | Anything else. |

**Sources.** A `source` names a module the way `loom run` does, with two restrictions: a local path must be **absolute** and lie inside a configured root, and a relative path is rejected, because a bare name would be read as a git URL. A git URL may carry `//subdir`. `file://` URLs count as local paths.

**Params.** A `params` object maps names to values. A value is a JSON string, array or object. A string arriving for a `list` or `map` param is parsed as YAML, exactly as `-p` does, so a YAML textarea can be sent as is. Where the server returns a structured value it returns it twice: `value` as JSON and `valueYaml` as text, because a number written `1.10` survives only as text.

**Paths.** A `path` inside a module is relative, uses `/`, and may not contain `..`. The server resolves symlinks and refuses anything that leaves the module directory. `.git` is never exposed.

## Authentication

The server prints one URL at startup:

```
http://127.0.0.1:7788/#token=<token>
```

The token is in the fragment, which browsers never send to the server. The UI exchanges it for a cookie and removes it from the address bar.

| Method | Path | Body | Result |
|--------|------|------|--------|
| POST | `/session` | `{"token": "…"}` | Sets an `HttpOnly; SameSite=Strict` cookie named `loom_session_<port>`. 204. |
| DELETE | `/session` | | Clears the cookie. 204. |

Scripts may send `Authorization: Bearer <token>` instead. `--token-file PATH` writes the token to a `0600` file instead of printing it.

Every `/api` request with an `Origin` header must match the server's own origin, mutating requests must carry `Content-Type: application/json`, and `Host` must be an allowed host. These checks defeat cross-site requests and DNS rebinding; see [Security](#security).

## Info and discovery

### `GET /info`

```json
{
  "version": "v0.4.0",
  "roots": ["/home/u/gitops"],
  "workspace": "/home/u/.local/state/loom/serve",
  "maxConcurrentJobs": 1,
  "capabilities": {"git": true, "gh": true, "glab": false},
  "env": {"GITHUB_TOKEN": true, "GITLAB_TOKEN": false, "LOOM_GIT_TOKEN": false}
}
```

`env` reports only whether each well-known variable is set. Values are never exposed.

### `GET /modules[?refresh=1]`

Every module found under the roots: a directory holding `loom.yaml` or `loom.jsonnet`. The walk skips `.git`, other dot-directories, `node_modules`, `vendor` and `__functions`, follows no symlinks, and keeps descending below a module, since child modules nest. Results are cached until `refresh=1`.

```json
{"modules": [{
  "dir": "/home/u/gitops/modules/onboard",
  "rel": "modules/onboard",
  "root": "/home/u/gitops",
  "name": "onboard-service",
  "format": "yaml",
  "params": {"required": 1, "total": 3},
  "hasTarget": true,
  "children": 2,
  "loadError": ""
}]}
```

A module whose config fails to load is still listed, with `loadError` set and `name` empty.

### `POST /modules`

Creates a new module: a directory with a minimal `loom.yaml`.

```json
{"dir": "/home/u/gitops/modules/new-service", "name": "new-service"}
```

`dir` must be inside a root and must not exist. Returns the discovery entry with 201.

## Describing a module

These endpoints execute nothing: no operation, no dynamic-param command, no `if` predicate. Cloning a remote module source to read its config is the only side effect.

### `POST /inspect`

The `loom inspect` report.

```json
{"source": "/home/u/gitops/modules/onboard", "params": {"env": "prod"}, "depth": 1, "modules": [], "noFetch": false}
```

`depth` 1 describes the root alone, 0 means every level. `modules` selects submodules by instance name or `parent/child` path, as `-m` does.

The response is the `loom inspect -o json` document plus a `cli` string. Every param carries its type and, for structured values, the YAML form:

```json
{"name": "sources", "type": "list", "state": "provided", "required": true,
 "value": [{"repoURL": "https://charts.example.com", "targetRevision": 1.10}],
 "valueYaml": "- repoURL: https://charts.example.com\n  targetRevision: 1.10\n"}
```

`state` is passed through from `loom inspect` unchanged: `provided`, `default`, `dynamic`, `missing`, `unset` or `unresolved`.

### `POST /validate`

```json
{"source": "/home/u/gitops/modules/onboard", "recursive": false}
```

```json
{"valid": true, "count": 3,
 "warnings": [{"module": "", "message": "param \"region\" is declared but never referenced by any template"}],
 "errors": []}
```

`module` is `""` for the root and the `parent › child` label otherwise. Validation problems come back with status 200 and `valid: false`; only a source that cannot be read at all is a 422.

### `POST /params/check`

Checks supplied values against the declarations without loading the module, so that no dynamic-param command runs while someone is typing.

```json
{"source": "/home/u/gitops/modules/onboard", "params": {"sources": "- repoURL: x\n"}}
```

```json
{"fields": {"sources": {"ok": true}, "replicas": {"ok": false, "error": "param \"replicas\" is declared string, but received a list"}},
 "undeclared": ["typo"],
 "missing": ["serviceName"]}
```

### `POST /yaml/parse` and `POST /yaml/format`

Helpers for the UI's YAML fields, since the browser has no YAML parser.

```json
{"text": "- a\n- b\n"}      →  {"value": ["a", "b"]}
{"text": "- a\n b"}          →  {"error": {"line": 2, "col": 2, "message": "…"}}
{"value": {"a": 1}}          →  {"text": "a: 1\n"}
```

Text that does not parse is the helper's answer, not a failed request: the status is 200 and the body is `{"error": {"line", "col", "message"}}` as shown, not the error envelope. `col` is 0 when the parser does not report one.

## Module files

Local sources only. A git-URL source is not browsable or editable.

### `GET /files?source=…&path=…`

A directory (`path` empty or a directory) lists entries; a file returns its content.

```json
{"kind": "dir", "entries": [{"name": "argocd", "kind": "dir"}, {"name": "loom.yaml", "kind": "file", "size": 812}]}
{"kind": "file", "content": "apiVersion: …", "etag": "sha256:…", "truncated": false, "binary": false}
```

Content is capped at 1 MiB; larger files come back `truncated: true` and cannot be edited through the API. Binary files return `binary: true` and no content.

### `PUT /files?source=…&path=…`

Writes a file. Creates it and any missing parent directories when it does not exist.

```json
{"content": "…", "ifMatch": "sha256:…"}
```

`ifMatch` is the `etag` from the last read; when the file changed since, the write is refused with 409 so an edit made elsewhere is not overwritten. Omit it to create a file. Returns the new etag: `{"etag": "sha256:…"}`.

### `POST /files`

Creates an empty directory or file.

```json
{"source": "…", "path": "argocd/prod", "kind": "dir"}
```

Returns 201 with the new entry as a directory listing shows it: `{"name": "prod", "kind": "dir"}`, with `"size": 0` for a file.

### `DELETE /files?source=…&path=…`

Deletes a file or an empty directory. `loom.yaml` and `loom.jsonnet` cannot be deleted through the API; edit them instead.

### `POST /files/move`

```json
{"source": "…", "from": "argocd/app.yaml", "to": "argocd/{{ .serviceName }}.yaml"}
```

Both paths are confined to the module. Fails with 409 if `to` exists. Returns 200 with the new path: <code v-pre>{"path": "argocd/{{ .serviceName }}.yaml"}</code>.

## Jobs

A job is one execution: a run, a diff, a generate or a bulk scaffold. Jobs that execute anything — `run` in any mode, a full `diff`, `generate`, `bulk` — are queued and run `maxConcurrentJobs` at a time (default 1). A quick diff executes nothing and is still a job, so its output streams the same way, but it is never queued.

### `POST /jobs`

Returns 202 with `{"id": "…", "state": "queued"}`.

**Run:**

```json
{"kind": "run", "source": "/home/u/gitops/modules/onboard",
 "params": {"serviceName": "payments", "sources": "- repoURL: x\n"},
 "mode": "execute", "targetPath": "", "author": "", "email": ""}
```

`mode` is `execute`, `dry-run` or `local`. In `local` mode the server allocates a workspace under its state directory unless `targetPath` names a directory inside a root. A local run creates a `targetPath` that does not exist yet; the other modes use it as given and create nothing. `author` and `email` are the `commitPush` defaults.

**Diff:**

```json
{"kind": "diff", "source": "…", "params": {}, "quick": false, "keepWorkspace": false, "author": "", "email": ""}
```

A failed diff still returns the diffs of what ran before the failure, flagged `incomplete` — the UI behaves as `--partial` always.

**Generate:**

```json
{"kind": "generate", "refs": ["https://github.com/o/r/pull/12"], "values": {"svc": "payments"},
 "name": "", "output": "/home/u/gitops/modules/new", "tokenEnv": "", "overwrite": false}
```

`output` must be inside a root. An existing non-empty directory is refused unless `overwrite` is true, because generate overwrites files silently.

**Bulk:**

```json
{"kind": "bulk", "module": "/home/u/gitops/modules/onboard", "name": "", "nameParam": "serviceName",
 "items": [{"serviceName": "a"}, {"serviceName": "b"}], "output": "/home/u/gitops/batches/q3"}
```

### `GET /jobs?module=&kind=&state=&limit=`

Returns `{"jobs": [...]}`, newest first. Each entry is the job record without `request` details beyond `source` and `mode`. `module` is a module's source path; it matches jobs whose `request.source` or `module.dir` equals it.

### `GET /jobs/{id}`

```json
{"id": "j_20260930T101500_7f3a", "kind": "run", "state": "succeeded",
 "request": {"…": "…"},
 "createdAt": "…", "startedAt": "…", "endedAt": "…",
 "module": {"name": "onboard-service", "dir": "/home/u/gitops/modules/onboard"},
 "result": {
   "prs": [{"path": ["bulk-onboard", "onboard-a"], "module": "onboard-service", "title": "Onboard a", "url": "https://github.com/o/r/pull/12"}],
   "workspace": "/home/u/.local/state/loom/serve/workspaces/j_…",
   "diff": {"incomplete": false, "targets": 2, "files": 7}
 },
 "error": "",
 "cli": "loom run /home/u/gitops/modules/onboard -p serviceName=payments --params-file …"}
```

States: `queued → running → (cancelling) → succeeded | failed | cancelled`. A job found running when the server starts is `interrupted`. Terminal states are final.

### `GET /jobs/{id}/events`

Server-Sent Events. Every event has an `id` (its sequence number) and a JSON `data`:

```
id: 42
event: op.start
data: {"seq":42,"type":"op.start","time":"…","path":["onboard-service"],"op":"render","kind":"newFiles","index":1,"total":4}

id: 43
event: log
data: {"seq":43,"type":"log","time":"…","path":["onboard-service"],"log":{"level":"INFO","msg":"writing file","attrs":[["path","argocd/payments.yaml"]]}}
```

| Event | Fields |
|-------|--------|
| `job.state` | `state`, and `error` when failed |
| `module.start`, `module.end`, `module.skip` | `path`, `module`, `reason` (skip), `durationMs` (end) |
| `op.start`, `op.end`, `op.skip` | `path`, `op`, `kind`, `index`, `total`, `error` and `durationMs` (end), `reason` (skip) |
| `log` | `path`, `log: {level, msg, attrs, section, dispatch, root}` |
| `diff.file` | `path`, `target`, `diff: {path, oldPath, status, binary, unified}` |
| `pr.created` | `path`, `pr: {module, title, url}` |
| `end` | sent after the terminal state, with `data: {}` and the next sequence number as its `id`; the server then closes the stream |

`path` is the module breadcrumb, root first. A `log` event's `level` is the slog level name: `DEBUG`, `INFO`, `WARN` or `ERROR`. A client reconnecting with `Last-Event-ID` (or `?after=<seq>`) receives every event after that sequence number, with no gap and no duplicate. A comment line `: ping` is sent every 15 seconds. URL userinfo (`https://user:token@…`) is redacted from log attributes before they are stored or sent.

### `GET /jobs/{id}/diff`

The structured diffs of a diff job, or of a local run.

```json
{"mode": "full", "incomplete": false,
 "targets": [{"path": ["bulk-onboard", "onboard-a"], "repo": "git@github.com:o/r.git", "branch": "main",
   "files": [{"path": "apps/a/deploy.yaml", "oldPath": "", "status": "added", "binary": false,
              "unified": "@@ -0,0 +1,12 @@\n+apiVersion: …"}]}]}
```

`status` is `added`, `modified`, `deleted` or `renamed`. Quick and full mode share this shape. A local run's diff covers the clones the run made, never the rest of its target path; when it made none — a module without a target spec runs directly in the target path — `targets` is empty and a `note` says why.

### `GET /jobs/{id}/log.txt`

The job's log as plain text, uncoloured, in the CLI's pretty format.

### `POST /jobs/{id}/cancel`

Cancels a queued or running job. Running subprocesses — shell steps, git, dynamic-param commands — are terminated as a tree. 202, or 409 if the job is already terminal.

### `POST /jobs/{id}/rerun`

Creates a new job from the same request, with optional overrides: `{"overrides": {"params": {…}, "mode": "dry-run"}}`. Returns 202 with the new id.

### `DELETE /jobs/{id}`

Removes the job from history along with its managed workspace. 409 while it is running.

## Presets

A preset is a saved params file for one module, kept in the server's state directory, never in the module (files there would be rendered as templates).

| Method | Path | Body / Result |
|--------|------|---------------|
| GET | `/presets?source=…` | `{"presets": [{"name": "prod", "updatedAt": "…"}]}` |
| GET | `/presets/{name}?source=…` | `{"name": "prod", "params": {…}, "yaml": "…"}` |
| PUT | `/presets/{name}?source=…` | `{"params": {…}}` or `{"yaml": "…"}` |

The stored file is the preset: `yaml` is its text verbatim, and is what keeps a value like `1.10` exact. `PUT` with `yaml` stores the text as given once it parses as a mapping of param names; `PUT` with `params` formats them as YAML and stores that.
| DELETE | `/presets/{name}?source=…` | 204 |

## CLI and docs

### `POST /cli`

Takes any job request and returns the equivalent command line, so an action in the UI can be repeated in a terminal or CI:

```json
{"command": "loom run /home/u/gitops/modules/onboard -p serviceName=payments --params-file params.yaml --local-run --target-path ./out"}
```

When a param value is structured, the command references a params file and the response carries its content in `paramsFile`.

### `GET /docs` and `GET /docs/{topic}`

The documentation `loom skill` prints, for the UI's help panel. `GET /docs` returns the index as JSON, `{"topics": [{"name": "guide/templates", "title": "Templates"}, …]}`. `GET /docs/{topic}` returns one topic as Markdown (`text/markdown; charset=utf-8`); the topic's `/` may be sent as is or escaped (`reference%2Fcli-run`).

## Security

| Threat | Mitigation |
|--------|------------|
| Anyone who can reach the port can run shell commands with your credentials | Loopback bind by default; 256-bit random token per process, compared in constant time, required on every API call. |
| Cross-site request forgery | Cookie is `HttpOnly; SameSite=Strict`; `Origin` must match; mutating requests need a JSON content type, which forces a CORS preflight that is never granted. |
| DNS rebinding | `Host` must be the listen address or a `--allowed-host`, else 421. |
| Token leakage | Delivered in the URL fragment only; `Referrer-Policy: no-referrer`; never logged. |
| Script injection in the UI | `Content-Security-Policy: default-src 'self'; frame-ancestors 'none'`; `X-Content-Type-Options: nosniff`; the UI builds DOM from text only. |
| Path traversal | Every file, output and target path is resolved with symlinks and must stay inside a root, the module, or the server workspace. |
| Executing an untrusted module | Sources are confined to the roots unless `--allow-outside-roots`; `--no-remote-modules` refuses git URLs. Describing a module never executes it. |
| Secrets in logs | Environment is reported as booleans; URL credentials are redacted from events; history files are `0600`. |
| A remote listen address | Requires `--allow-remote`; the token is still enforced and a plaintext warning is printed. |
