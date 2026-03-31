# How It Works

A Loom module is a directory. Inside it, you put:

1. **`loom.yaml`** — the workflow definition. It declares parameters, operations, and optionally references child modules.
2. **Template files** — any files alongside `loom.yaml` are treated as Go templates. Their directory structure mirrors where they'll land in the target repository.
3. **`__functions/`** — a conventional directory for patches, configs, and supporting files that should _not_ be copied to the target. Add it to `excludes` in your `loom.yaml` to prevent it from being rendered as template files.

```
onboard-service/
├── loom.yaml
├── argocd/
│   ├── application-{{ .serviceName }}.yaml   ← file name is templated
│   └── project.yaml                          ← template, rendered with params
├── {{ .namespace }}/                         ← folder name is templated
│   └── constraints/
│       └── pod-must-have-label.yaml
└── __functions/
    └── patches/
        └── add-app.yaml                      ← used by patch operations, not copied
```

## Execution Flow

When you run `loom run ./onboard-service -p serviceName=payments`, Loom:

1. Loads `loom.yaml` and resolves parameters
2. Clones the target Git repository (or uses a local path you specify)
3. Creates a feature branch if `featureBranch` is configured
4. Walks through operations in order — rendering templates, running shell commands, committing, pushing, opening a PR
5. Reports what it did

```
loom run ./module -p key=val
        │
        ▼
┌─────────────────┐
│   Config Loader  │  Parse loom.yaml, validate schema
└────────┬────────┘
         ▼
┌─────────────────┐
│  Module Loader   │  Resolve params (provided + dynamic + defaults)
└────────┬────────┘
         ▼
┌─────────────────┐
│  Clone + Branch  │  Clone target repo, create feature branch
└────────┬────────┘
         ▼
┌─────────────────┐
│    Executor      │  Walk child modules (recursive), then operations
└────────┬────────┘
         ▼
┌─────────────────┐
│  Action Dispatch │  Route each operation to its handler
└────────┬────────┘
         ▼
┌────┬────┬────┬────┬────┐
│ New │Pch │Shl │ CP │ PR │  Action implementations
│Files│    │    │    │    │
└────┴────┴────┴────┴────┘
         │
         ▼
┌─────────────────┐
│  Git / Provider  │  Library-first, CLI fallback
│  go-git → git    │
│  go-github → gh  │
│  go-gitlab → glab│
└─────────────────┘
```

## Library First, CLI Fallback

Loom embeds Go libraries for all core operations. When the library path fails (auth issues, unsupported SSH configs, token not set), Loom detects the local binary and retries through it.

| Operation | Library (primary) | CLI fallback |
|-----------|------------------|-------------|
| Clone, push, branch, commit | go-git | `git` |
| GitHub PR | go-github API | `gh` |
| GitLab MR | go-gitlab API | `glab` |
| YAML patch (SMP, JSON6902) | kustomize library | -- |

On a DevOps laptop where these tools are already authenticated and configured, Loom just works.
