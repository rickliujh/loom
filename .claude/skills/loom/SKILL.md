---
name: loom
description: Author, validate, and run Loom modules (loom.yaml / loom.jsonnet GitOps automation). Use when creating or editing a Loom module, writing loom.yaml or loom.jsonnet, running or debugging `loom run`, setting up bulk runs, or configuring newFiles/patch/shell/llm/commitPush/pr operations.
---

# Using Loom

**First, run `loom skill` and read what it prints.** That is the canonical agent guide, served from the binary, so it matches the version of Loom installed here. It holds the full rules: param resolution, templating, operation gotchas, composition and target semantics, loom.jsonnet, bulk patterns, secrets, and a debugging table. This file is only the trigger and the safety workflow.

If `loom skill` is not available (an older Loom), read `docs/guide/ai-agents.md` in the Loom repository instead.

## Golden workflow (always follow)

```bash
loom validate ./my-module                                  # 1. schema + semantic checks
loom diff ./my-module -p key=val                           # 2. the diff a run would produce
loom run ./my-module -p key=val --local-run --target-path ./preview   # 3. real files locally, no push/PR
loom run ./my-module -p key=val                            # 4. real run — pushes branches / opens PRs,
                                                           #    confirm with the user first
```

## Deep references

Each prints with `loom skill <topic>`; `loom skill list` shows them all.

- `loom skill spec/module` — full behavioral spec
- `loom skill spec/smp` — strategic-merge-patch semantics
- `loom skill guide/bulk-runs` — running one module against many param sets
- `loom skill reference/loom-yaml` — every field of loom.yaml
