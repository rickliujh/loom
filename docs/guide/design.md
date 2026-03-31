# Design Philosophy

**Declarative over imperative.** You describe _what_ should happen, not _how_. Loom handles the mechanics of cloning, rendering, committing, and pushing. Your module is a specification, not a script.

**Files as the interface.** A module is a folder. Templates are just files with Go template syntax. The directory structure _is_ the destination structure. There's no abstraction layer between what you write and what lands in the repository.

**Composable by default.** Modules reference other modules. Parameters flow down. You build small, focused modules and combine them. The same module that onboards one service onboards a hundred -- you just change the parameters.

**Ordered operations, flat list.** Operations execute top to bottom. There's no DAG, no dependency graph, no parallel execution. This is intentional. GitOps workflows are inherently sequential: render files, then validate, then commit, then open a PR. A flat list is easy to read, easy to debug, and hard to get wrong.

**Template everywhere.** Every string in operations -- commands, commit messages, PR titles, file paths -- is a Go template. You never have to switch between "static" and "dynamic" configuration. It's all dynamic, all the time.

**Library first, CLI fallback.** Loom embeds [go-git](https://github.com/go-git/go-git) for Git operations, uses the GitHub/GitLab APIs for PR creation, and implements RFC 6902 patching natively -- no external binaries needed in CI or containers. But on a developer's laptop, the system `git`, `gh`, and `glab` are already authenticated and configured. When the library path fails (auth issues, unsupported SSH configs, token not set), Loom detects the local binary and retries through it. You get the portability of a self-contained binary and the compatibility of native tooling, without choosing between them.

## Architecture

Each action implements a single interface:

```go
type Action interface {
    Execute(ctx context.Context, execCtx *ExecutionContext) error
}
```

Adding a new operation type means implementing this interface and registering it -- nothing else changes.

Loom is a single static binary. It embeds Go libraries for Git, PR/MR creation, and YAML patching, so it can run with zero external dependencies.
