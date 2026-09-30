# The Web UI

`loom serve` runs a small web server on your machine and gives you a browser UI for everything the CLI does: browse your modules, fill in their parameters, preview the changes, run them, and follow each run live. It is the same engine as `loom run`, `loom diff` and friends, and every action can be copied as the equivalent command line.

```bash
loom serve --root ~/gitops
```

It prints one URL:

```
http://127.0.0.1:7788/#token=3f9c…
```

Open it in your browser (`--open` does that for you). That is all the setup there is.

## What It Gives You

- **A catalogue** of every module under your roots, with a fuzzy filter.
- **A form for each module**, generated from its declared parameters, that checks your values as you type and shows the target repository and feature branch they resolve to.
- **Previews before runs**: a quick preview that executes nothing, and a full diff that runs the module locally and diffs every target.
- **Live runs**: the module and operation tree, the log in the CLI's colours, the diffs and the pull requests, as they happen.
- **History**: every job with its log, diff and result, ready to re-run.
- **Editing**: the module's files, with template actions highlighted.
- **Wizards** for [generating a module from a PR](/reference/cli-generate) and [scaffolding a bulk run](/reference/cli-bulk).
- **Help**: this documentation, from the binary you are running.

## Pointing It at Your Modules

A module is any directory holding a `loom.yaml` or `loom.jsonnet`. `loom serve` looks for them under its **roots**:

```bash
loom serve --root ~/gitops --root ~/platform-modules
```

Without `--root`, the current directory is the root. The search follows no symlinks and skips `.git`, other dot-directories, `node_modules`, `vendor` and `__functions`. Nested modules — a wrapper and the children it composes — are all found.

The list is cached. After adding a module on disk, press **Refresh**.

A module outside the list can still be opened with **Open by path or git URL** on the catalogue: an absolute path inside a root, or a git URL such as `https://github.com/org/loom-modules.git//onboard`. A relative path is not accepted, because the CLI would read a bare name as a git URL.

**New module** creates a directory with a minimal `loom.yaml` and opens it in the Files tab.

## Screens

### Modules

The catalogue groups modules by root and folder. Each card shows the module's name and path, how many of its parameters are required, whether it has a target repository, how many submodules it composes, and the state of its last job. A module whose config does not load is still listed, with the error, so you can open it and fix it.

### A Module

A module page has five tabs.

**Params & Run** is where you spend most of your time.

- Each parameter gets a field. Required ones are marked with an asterisk; a default is shown as the placeholder and applies when you leave the field empty.
- A `list` or `map` parameter is edited as YAML, in a monospace box where Tab indents. The text is checked as you type and an error shows its line and column. The value is sent exactly as you wrote it, so a version like `1.10` stays `1.10` — see [Structured Parameters](/guide/structured-params).
- A dynamic parameter shows the command that computes it. You can override it; the command then does not run, as with `-p` on the command line.
- The side panel shows the target repository and feature branch as your values resolve them, and lists what is still missing or wrong.
- **Presets** save the current values under a name, per module, on the server — outside the module directory, where a file would be rendered as a template. **Import** reads a params file; **Export** writes one.

The action bar offers, from safest to most consequential:

| Action | What it does | CLI |
|--------|--------------|-----|
| Quick preview | Simulates the run and shows the `newFiles` and `patch` diffs. Executes nothing. | `loom diff --quick` |
| Full diff | Runs the module locally — shell steps included — and diffs every target. Pushes nothing. | `loom diff` |
| Run › Dry run | Walks every step, writes and pushes nothing. | `loom run --dry-run` |
| Run › Local run | Renders and commits into a local workspace. No push, no pull request. | `loom run --local-run` |
| Run › Execute | The real run: pushes branches and opens pull requests. | `loom run` |

**Overview** is [`loom inspect`](/reference/cli-inspect): the parameters and where each value comes from, the target, the operations in order, and the submodules. A submodule that was only listed can be expanded in place. Nothing runs to build this view.

**Files** browses and edits the module directory. Template actions such as <code v-pre>{{ .serviceName }}</code> are highlighted in file contents and names, and files are labelled as config, template, patch or excluded. You can create, rename and delete files and folders. `loom.yaml` and `loom.jsonnet` can be edited but not deleted.

**Validate** is [`loom validate`](/reference/cli-validate), with a switch for `--recursive`. Findings are grouped by module.

**History** lists this module's jobs.

### A Job

Every preview, diff, run, generate and bulk scaffold is a **job**. Its page shows:

- the state, elapsed time and the request that started it;
- **Steps**: the module and operation tree, with each step's status and duration. Click a module or operation to show only its log lines;
- **Log**: the run log as the CLI prints it — the module chip, the reserved `≡ root ≡` chip for the orchestrator, the `▸ parent › child` hand-off. Debug lines are hidden until you turn them on; shell output is folded. The newest line stays in view until you scroll up;
- **Changes**: the diffs, grouped by module and target, unified or side by side. A diff from a run that failed is shown with an *incomplete* banner, as `loom diff --partial` would;
- **Result**: the pull requests opened, with the module that opened each, and for a local run the workspace path.

A job can be cancelled while it runs, re-run as it was, or opened back in the module form with **Re-run with edits**. **Download log** gives the uncoloured text log.

Jobs that execute anything run one at a time, in order; a quick preview never waits. Jobs are kept after the server stops, and one that was running at the time is marked *interrupted*.

### Generate and Bulk

**Generate** walks through [`loom generate`](/reference/cli-generate): the pull or merge requests to learn from, which token reads them (the UI shows only whether each variable is set, never its value), the literal values to turn into parameters, and where to write the module. It ends on the new module's page.

**Bulk** walks through [`loom bulk`](/reference/cli-bulk): pick a module, fill a grid with one row per run (paste rows from a spreadsheet), choose the parameter that names each run, and where to write the wrapper. It ends with the wrapper ready for a quick preview.

## Safety

`loom serve` can run shell commands with your credentials, so it is locked down by default.

- **Local only.** It listens on `127.0.0.1`. Listening elsewhere needs `--allow-remote`.
- **A token for your browser.** The URL it prints carries a random token after the `#`, a part of the address the browser never sends over the network. The page swaps it for a cookie and removes it from the address bar. Without it, the page tells you to open the printed link, and no request is served. Each start makes a new token.
- **Other sites cannot use it.** Requests from another origin, or under another host name, are refused.
- **Nothing runs by looking.** Browsing, the overview, validation and parameter checks execute no operation, dynamic-param command or condition.
- **You confirm before anything pushes.** Execute first shows the target repositories and feature branches, the shell commands that will run, and the operations that will push and open pull requests. Nothing starts until you confirm.
- **Files stay in their module.** Paths are confined to the module directory, and a save is refused if the file changed since you opened it, so an edit made elsewhere is not overwritten silently.

The API behind the UI is documented in [the serve API reference](/reference/serve-api).

## The UI and the CLI

The UI adds no behaviour of its own. Every action has **Copy as CLI**, which gives the equivalent `loom` command; when a value is a list or a map, it also gives the params file the command reads. Use it to move something you tried in the browser into a script or a CI job:

```bash
loom run /home/u/gitops/modules/argocd-app -p appName=guestbook --params-file params.yaml --summary
```

A few things are more convenient in the UI than on the command line: the target resolves as you type, a failed diff always shows the partial result, the pull requests of a run are always listed, and a submodule can be expanded without re-running inspect with `--module`.
