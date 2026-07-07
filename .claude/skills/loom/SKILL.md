---
name: loom
description: Author, validate, and run Loom modules (loom.yaml / loom.jsonnet GitOps automation). Use when creating or editing a Loom module, writing loom.yaml or loom.jsonnet, running or debugging `loom run`, setting up bulk runs, or configuring newFiles/patch/shell/llm/commitPush/pr operations.
---

# Using Loom

**First, read the canonical agent guide: `docs/guide/ai-agents.md`** (published at `/guide/ai-agents` on the docs site). It is tool-neutral and holds the full rules: param resolution, templating limits, operation gotchas, composition/target semantics, loom.jsonnet, bulk patterns, secrets, and a debugging table. This file is only the trigger and the safety workflow.

## Golden workflow (always follow)

```bash
loom validate ./my-module                                  # 1. schema + semantic checks
loom run ./my-module -p key=val --diff                     # 2. dry-run with file diffs
loom run ./my-module -p key=val --local-run --target-path ./preview   # 3. real files locally, no push/PR
loom run ./my-module -p key=val                            # 4. real run — pushes branches / opens PRs,
                                                           #    confirm with the user first
```

## Deep references

- `docs/guide/ai-agents.md` — canonical agent guide (read this)
- `specs/module.md` — full behavioral spec
- `specs/smp.md` — strategic-merge-patch semantics
- `docs/guide/bulk-runs.md` — running one module against many param sets
