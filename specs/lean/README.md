# LoomSpec — formal model of the SMP spec

A Lean 4 proof-of-concept that models the Strategic Merge Patch semantics
from [`specs/smp.md`](../smp.md) and proves the spec's behavioral claims as
theorems.

## What is proven

| Spec rule | Theorem | Statement |
|-----------|---------|-----------|
| B4 | `addNew_prefix` | Target list order is preserved (target is a prefix of the result) |
| B4 | `mem_addNew_iff` | The result is exactly the union — no values invented, none lost |
| B4 | `addNew_of_subset` | If every patch value already exists, the target is unchanged |
| B4 | `addNew_idem` | **Applying the same patch twice equals applying it once** |
| B4 | `addNew_nodup` | The merge never introduces a duplicate |
| B2 | `merge_preserves_absent` | A key absent from the patch keeps its target value |
| B1/B3 | `merge_merges_present` | A key in both maps to the recursive merge (scalar overwrite / deep-merge) |
| B6 | `merge_adds_new` | A key only in the patch is added with the patch's value |

Out of scope for the PoC: B5 (map-list merge by inferred key), B7
(templating), B8 (error propagation).

## What this does and does not guarantee

These proofs verify that the **specification is coherent** — e.g. that B4's
append-unique really is idempotent, which is what makes Loom patches safe to
re-run. They do **not** verify the Go implementation in `pkg/action/patch.go`
(which delegates to kustomize's `merge2`). Binding the implementation to this
model requires differential testing: extract an executable oracle from these
definitions (`merge` is a computable function) and compare its output against
`loom`'s on generated patch/target pairs.

## Building

```bash
# install elan (Lean toolchain manager) once:
curl -sSfL https://elan.lean-lang.org/elan-init.sh | sh -s -- -y

cd specs/lean
lake build   # toolchain pinned by ./lean-toolchain; zero errors = all theorems hold
```

There are no `sorry` placeholders; `lake build` succeeding means every
theorem above is machine-checked. The project has no dependencies (no
mathlib) — it builds with core Lean only, in seconds.

## Layout

- `LoomSpec/Smp.lean` — the model (`addNew`, `Yaml`, `merge`) and all theorems
- `lean-toolchain` — pinned Lean version
- `lakefile.toml` — lake build configuration
