# Serve — Behavioral Specification

`loom serve` runs a local HTTP server: a JSON API under `/api/v1` and the web UI that uses it. It browses, edits, describes and runs modules through the same engine as the CLI, so everything below is about what the server adds — authentication, confinement, scheduling, the event stream and history — not about what a run does.

```
loom serve [--listen 127.0.0.1:7788] [--root DIR]... [--open]
```

The API itself — every endpoint, body and status — is specified by the [API reference](/reference/serve-api); the flags by the [command reference](/reference/cli-serve).

## Inputs

| Input | Description |
|-------|-------------|
| `--listen` | Address to bind. Default `127.0.0.1:7788`; port 0 picks a free port. |
| `--root` | Directories the server works in (repeatable). Default: the current directory. |
| `--allow-outside-roots` | Accept local module sources outside the roots. |
| `--no-remote-modules` | Refuse git-URL module sources. |
| `--allow-remote` | Permit a non-loopback `--listen`. |
| `--allowed-host` | Extra accepted `Host` values (repeatable). |
| `--token-file` | Write the session token to a `0600` file instead of printing it. |
| `--state-dir` | Job history, presets and workspaces. Default `$XDG_STATE_HOME/loom/serve`. |
| `--history` | `disk` (default) or `memory`. |
| `--max-concurrent-jobs` | Executing jobs that may run at once. Default 1. |

## Access

#### SV1: The server binds loopback unless told otherwise

The default listen address is `127.0.0.1:7788`. An address with no host (`:0`) binds `127.0.0.1`, not every interface. Any address that is not loopback — `0.0.0.0`, a LAN address, a host name — is refused unless `--allow-remote` is given, and then a warning that the connection is plain HTTP is printed. The token is enforced either way.

#### SV2: Every API call needs the per-process token

Each server generates a 256-bit random token at startup. Every `/api/v1` request except the session exchange must carry it, as `Authorization: Bearer <token>` or in the session cookie; without it the response is `401 unauthorized`. The comparison runs in constant time. Unknown API paths are behind the token too.

#### SV3: The token is exchanged for a port-scoped, HttpOnly, SameSite=Strict cookie

The startup URL carries the token in its fragment (`http://host:port/#token=…`), which a browser never sends to a server. `POST /api/v1/session` with the token sets a cookie named `loom_session_<port>` with `HttpOnly`, `SameSite=Strict` and `Path=/`; a wrong token gets `401` and no cookie. The port in the name keeps two servers on one host from sharing a session, since cookies ignore ports. `DELETE /api/v1/session` clears it.

#### SV4: Requests for a host the server is not are refused

The `Host` header must be the listen address, a loopback spelling of it (`127.0.0.1`, `localhost`, `[::1]` with the server's port) when bound to loopback, or an `--allowed-host` value (`name` for any port, `name:port` for one). Anything else is `421 misdirected`, for the UI as well as the API. This is what defeats DNS rebinding.

#### SV5: Cross-origin and non-JSON mutating requests are refused

An API request with an `Origin` header that is not the server's own origin (`http://<Host>`) is `403 forbidden`, reads included. A `POST`, `PUT`, `PATCH` or `DELETE` without `Content-Type: application/json` is `403 forbidden`, whatever it carries: a form cannot send that type without a CORS preflight, and the server never grants one.

#### SV6: Every response carries the security headers

Every response — the UI, static files, API results, errors and 404s — has `Content-Security-Policy: default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'`, `X-Frame-Options: DENY`, `X-Content-Type-Options: nosniff` and `Referrer-Policy: no-referrer`.

## Confinement

#### SV7: Module sources follow the root rules

A local source must be an absolute path, or a `file://` URL, that — with symlinks resolved — lies inside a root; outside the roots it is `403` unless `--allow-outside-roots`. A relative path is `400`, because the CLI would read a bare name as a git URL. A missing directory is `404`. A git URL (`scheme://…` or `user@host:path`, optionally with `//subdir`) is accepted, and refused with `403` under `--no-remote-modules`.

#### SV8: File access stays inside the module and never shows .git

A file path is relative, uses `/`, and may not contain `..` or be absolute (`400`). Resolved with symlinks, it must stay inside the module directory (`403` otherwise); a directory listing leaves out entries whose links lead outside it. Nothing named `.git`, and nothing a symlink leads to inside one, is listed, read or written (`404`). Files over 1 MiB are returned truncated and cannot be written; a write to an existing file needs the etag it was read with (`409` on a mismatch or when it is missing); `loom.yaml` and `loom.jsonnet` cannot be deleted or moved; a move never replaces an existing path; deleting a symlink removes the link. A git-URL source is not browsable.

#### SV9: Every path a job or request writes to is inside a root or the workspace

A run's `targetPath`, a generate or bulk `output`, and a new module's `dir` must be absolute (`400`) and, with the symlinks of their existing part resolved, inside a root or the server's workspace directory; otherwise `403` and nothing is written. Generate refuses an existing non-empty `output` with `409` unless `overwrite` is set, and checks again when the job starts: one whose output filled up while it waited fails without writing.

#### SV10: Describing a module executes nothing

Discovery, inspect, validate, params check and file browsing never run a module's operations, dynamic-param commands or `if` predicates, at any depth. They read configs with the config loader — never by loading the module, which is where dynamic-param commands run. Cloning a git-URL source to read it is their only side effect.

## Jobs

#### SV11: Jobs run through the CLI's engine

A `run` job calls the same engine entry point as `loom run`, a `diff` job the one `loom diff` uses, and `generate` and `bulk` jobs the packages behind those commands. A local run job leaves the same target changes as the equivalent `loom run --local-run`. Its result carries the diff of each clone the run made against its base branch, read back after the run under the breadcrumb of the module that made it. Nothing else is read back: reading stages changes, so a user-supplied target path — and a module without a target spec, which runs directly in it — is never staged or diffed; the diff then carries a `note` saying so.

#### SV12: A job's command line parses back to the job

The `cli` string of a job — and `POST /api/v1/cli` for any job request — is a `loom` command that, parsed by the real flag set, yields the same request: source, mode, params, paths and identities. Single-line text params become `-p name=value`; structured and multi-line values go to a `--params-file params.yaml` whose content is returned alongside, as is the `--items items.yaml` content of a bulk job. Arguments are quoted for a POSIX shell.

#### SV13: Job states only move forward

A job is `queued`, then `running`, then — after `cancelling`, when cancelled while running — `succeeded`, `failed` or `cancelled`. A queued job that is cancelled goes straight to `cancelled`; a job the server stopped before it ended is `interrupted`. Terminal states are final: cancelling one is `409`. Each change is a `job.state` event, with `error` when the job failed or was interrupted. A running job cannot be deleted (`409`); a finished one is deleted with its managed workspace. A rerun is a new job from the stored request, with any field named in `overrides` replaced whole; the kind cannot change.

#### SV14: Cancelling a job stops its whole process tree

`loom serve` starts every subprocess in its own session, so cancelling a running job terminates the command and everything it started — a shell step's backgrounded children included — and no step after it runs. The job ends `cancelled` within seconds.

#### SV15: Executing jobs are queued; a quick diff is not

Runs in every mode, full diffs, generate and bulk start in submission order, at most `--max-concurrent-jobs` at a time (default 1). A quick diff starts at once and never waits behind them. Requests that are not jobs are never queued.

#### SV16: The event stream replays without gaps and ends

`GET /jobs/{id}/events` sends every event after the sequence number in `Last-Event-ID` (or `?after=`), numbered 1, 2, 3… with no gap and no duplicate, then new events as they happen, a `: ping` comment when idle (every 15 seconds), and after the terminal `job.state` an `end` event with `data: {}` and the next number as its id; then the server closes the stream. Log events past 50,000 per job are dropped from the stream (after one warning event) without taking a number; lifecycle events are always kept. URL credentials are redacted from every event before it is stored or sent.

#### SV17: Events carry the module breadcrumb

Every module, operation, diff and PR event carries the instance breadcrumb of the module it belongs to, root first — the executor's module path. A log event carries the breadcrumb of the module that logged it; the root module's setup logs, before its `module.start` (cloning a source or target, resolving params), may carry an empty path.

#### SV18: Diffs are structured, and a failed diff keeps what it found

`GET /jobs/{id}/diff` returns, for a diff job or a local run, one entry per changed target — its breadcrumb, repository, base branch and label — each with per-file entries (`path`, `oldPath`, `status`, `binary`, `unified`). A diff that fails still returns what was collected before the failure, marked `incomplete`. Before the job ends the diff is `409`; a job that has none is `404`.

#### SV19: Every PR a job opens is in its result

Each pull or merge request a run opens appears in the job's `result.prs` with its breadcrumb, module, title and URL, and as a `pr.created` event — including PRs opened before a later failure.

#### SV20: Workspaces are per job, and paths are held while a job is active

Each local run without a `targetPath`, and each full diff, gets its own directory under the state directory's `workspaces/`. A `targetPath` or `output` held by a queued or running job — the same path, or one inside the other — is refused to another job with `409` until the holder ends.

#### SV22: Job history survives a restart and is pruned

With `--history disk`, each job's record, events, plain-text log and diff are kept under the state directory's `jobs/<id>/` (files `0600`; directories the server creates `0700`, while an existing state directory keeps its mode) and reloaded at startup; a job that was queued or running when the server stopped comes back `interrupted`, its stream closed with that state. History keeps at most 200 jobs and none older than 30 days, dropping the oldest first. With `--history memory` nothing is written and nothing survives a restart.

## Environment

#### SV21: The environment is reported only as presence

`GET /info` reports whether each of `GITHUB_TOKEN`, `GITLAB_TOKEN` and `LOOM_GIT_TOKEN` is set, as booleans. No environment value is ever returned.
