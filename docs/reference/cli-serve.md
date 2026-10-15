# loom serve

Serve a web UI for browsing, editing, inspecting, diffing and running modules.

```
loom serve [flags]
```

`loom serve` starts a small HTTP server on your machine and prints one URL.
Open it and the UI lists every module under the directories you point it at:
you can read and edit their files, see what a module needs with the same report
as [`loom inspect`](/reference/cli-inspect), fill in its parameters in a form,
preview the changes with a diff, and run it — watching each operation's logs
as they happen.

Everything the UI does goes through the same code as the matching command. A
run in the UI is a `loom run`, a diff a `loom diff`, and every job shows the
command line that reproduces it in a terminal or CI.

## Flags

| Flag | Description |
|------|-------------|
| `--listen <addr>` | Address to listen on. Default `127.0.0.1:7788`. Port `0` picks a free port; an address with no host, such as `:0`, means `127.0.0.1`. |
| `--root <dir>` | A directory whose modules the UI works with. Repeatable. Default: the current directory. |
| `--allow-outside-roots` | Accept module paths outside the roots. |
| `--no-remote-modules` | Refuse module sources that are git URLs. |
| `--allow-remote` | Allow a `--listen` address that is not loopback. |
| `--allowed-host <host>` | Accept this `Host` header, as `name` (any port) or `name:port`. Repeatable. |
| `--token-file <path>` | Write the session token to this file, readable only by you, instead of printing it. |
| `--state-dir <dir>` | Where job history, presets and job workspaces live. Default `$XDG_STATE_HOME/loom/serve`, or `~/.local/state/loom/serve`. |
| `--history <disk\|memory>` | `disk` (default) keeps job history across restarts; `memory` keeps nothing. |
| `--max-concurrent-jobs <n>` | Jobs that execute anything which may run at once. Default `1`. |
| `--open` | Open the UI in your browser. |

## Starting It

```bash
cd ~/gitops
loom serve
```

```
loom serve listening; open this URL to sign in:

  http://127.0.0.1:7788/#token=3f9c…e1

state directory: /home/you/.local/state/loom/serve
```

The part after `#token=` is a secret that is new every time the server starts.
Opening the URL signs the browser in; the UI then removes the token from the
address bar. Scripts can send it as `Authorization: Bearer <token>` instead —
see the [API reference](/reference/serve-api).

With `--token-file`, the token goes to that file (mode `0600`) and is not
printed.

Stop the server with Ctrl-C. Jobs still running are cancelled and given ten
seconds to stop; any that have not are recorded as interrupted.

## What It Can Reach

The server is a way to run shell commands with your credentials, so it is
locked down by default:

- **It listens on `127.0.0.1` only.** Nothing else on the network can connect.
- **Every API call needs the token.** Without it the server answers `401`.
  Another web page open in your browser cannot use your session: the cookie is
  `SameSite=Strict`, requests from other origins are refused, and a page that
  tries to reach the server under another name (DNS rebinding) gets `421`.
- **Modules are read from the roots.** A module path outside every `--root` is
  refused unless you pass `--allow-outside-roots`. Git-URL sources are allowed;
  `--no-remote-modules` refuses them.
- **Writes stay inside the roots.** Files are edited only inside the module
  being browsed, and a run's target path or a scaffold's output directory must
  be inside a root or the server's own workspace.
- **Looking executes nothing.** Browsing, inspecting, validating and checking
  parameters never run a module's shell steps, dynamic parameters or `if`
  conditions. Only a job you start does.

Jobs run one at a time by default, in the order you start them; a quick diff,
which executes nothing that changes anything, runs at once. Cancelling a job
stops everything it started, child processes included.

## The State Directory

| Path | Holds |
|------|-------|
| `jobs/<id>/` | Each job's record, event stream, plain-text log and diff. The last 200 jobs are kept, none older than 30 days. |
| `workspaces/<id>/` | The clones of a local run, or a full diff you asked to keep, until the job is deleted. |
| `presets/` | Saved parameter sets, per module. They are kept here, never in the module, where a `newFiles` operation would copy them into the target. |

The directory is created `0700` and its files `0600`: job logs contain whatever
your modules' commands print. `--history memory` writes no job history at all.

## On a Remote Machine

Keep the default loopback address and reach it through SSH:

```bash
# on the server
loom serve --root ~/gitops --token-file ~/.loom-token

# on your machine
ssh -L 7788:127.0.0.1:7788 you@server
```

Then open `http://127.0.0.1:7788/#token=<token>` locally, with the token from
`~/.loom-token` on the server. The tunnel encrypts the traffic, and the server
still sees a loopback address.

Listening on another address instead needs `--allow-remote`:

```bash
loom serve --listen 0.0.0.0:7788 --allow-remote --allowed-host build-box.lan
```

The token is still required, but the connection is plain HTTP — anyone on the
path can read the token and everything the UI shows — so the server prints a
warning. `--allowed-host` names the host name you will browse to, since the
server refuses requests addressed to any name it does not know.

## Related

- [API reference](/reference/serve-api) — the endpoints the UI uses, for scripting.
- [`loom run`](/reference/cli-run), [`loom diff`](/reference/cli-diff) — what a
  run and a diff job do.
