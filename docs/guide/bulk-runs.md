# Bulk Runs

A single `loom run` executes one module with one set of parameters. But operational work rarely comes one at a time: you onboard ten services, roll a change across every environment, or wire up a whole team's repositories in one sitting.

There are three ways to run the same module in bulk. All of the examples below reuse this child module:

```
onboard-service/
├── loom.yaml            # declares serviceName + namespace params
└── templates/...
```

## 1. YAML Wrapper with Child Modules

[Module composition](/guide/module-composition) already gives you bulk for free: a wrapper module lists the same module as a child once per item, each entry with its own params. Children execute in declaration order.

```yaml
# bulk-onboard/loom.yaml
apiVersion: loom.rickliujh.github.io/v1beta1
kind: Loom
metadata:
  name: bulk-onboard
spec:
  modules:
    - name: onboard-payments
      source: ../onboard-service
      params: { serviceName: payments, namespace: fintech }
    - name: onboard-billing
      source: ../onboard-service
      params: { serviceName: billing, namespace: fintech }
    - name: onboard-auth
      source: ../onboard-service
      params: { serviceName: auth, namespace: platform }
```

```bash
loom run ./bulk-onboard
```

**Best for**: small, stable batches where the item list itself should be reviewable in Git. The wrapper is plain declarative YAML — anyone can read exactly what will run.

**Trade-off**: N items means N hand-written entries. When the list grows or changes often, move to Jsonnet.

## 2. Jsonnet Wrapper

The same wrapper written as [`loom.jsonnet`](/reference/loom-yaml#loom-jsonnet) generates the child entries from a list, so the item list is data and the entry shape is written once:

```jsonnet
// bulk-onboard/loom.jsonnet
local services = [
  { name: 'payments', namespace: 'fintech' },
  { name: 'billing', namespace: 'fintech' },
  { name: 'auth', namespace: 'platform' },
];

{
  apiVersion: 'loom.rickliujh.github.io/v1beta1',
  kind: 'Loom',
  metadata: { name: 'bulk-onboard' },
  spec: {
    modules: [
      {
        name: 'onboard-' + svc.name,
        source: '../onboard-service',
        params: { serviceName: svc.name, namespace: svc.namespace },
      }
      for svc in services
    ],
  },
}
```

```bash
loom run ./bulk-onboard
```

**Best for**: batches with more than a handful of items, or where the list changes regularly. Adding an item is a one-line diff. Shared logic can live in `.libsonnet` files next to the config.

**Trade-off**: the config is evaluated before parameter resolution, so Jsonnet cannot see CLI params — the item list lives in the file (or an imported one), not on the command line.

## 3. Shell Loop

When the item list is dynamic — produced by a script, a service catalog query, a CSV export — loop over `loom run` itself:

```bash
for svc in payments billing auth; do
  loom run ./onboard-service -p serviceName="$svc" -p namespace=fintech
done
```

Or keep one params file per item and iterate:

```bash
for f in params/*.yaml; do
  loom run ./onboard-service --params-file "$f"
done
```

Independent runs can be parallelized:

```bash
printf '%s\n' payments billing auth |
  xargs -P 4 -I {} loom run ./onboard-service -p serviceName={}
```

**Best for**: ad-hoc batches, dynamic item lists, and parallel execution.

**Trade-off**: each iteration is an independent process — there is no shared clone, so this always produces one branch/PR per item, and failure handling is up to the shell (a failed item doesn't stop the others under `xargs`).

## One PR per Item, or One PR for the Batch?

With the wrapper approaches (1 and 2), the child module's `spec.target` decides how the batch ships:

- **Child has its own `spec.target`** → every entry clones, branches, and opens a pull request independently. Ten items, ten PRs.
- **Child has no `spec.target`** → children fall back to the parent's target directory. Put `target`, `commitPush`, and `pr` on the *wrapper*, and the whole batch lands in one clone and ships as a **single commit and PR**:

```yaml
# bulk-onboard/loom.yaml — single PR for the whole batch
spec:
  target:
    url: "https://github.com/myorg/gitops-repo.git"
    featureBranch: "loom/bulk-onboard"
  modules:
    - name: onboard-payments
      source: ../onboard-service        # child has no target spec
      params: { serviceName: payments, namespace: fintech }
    # ...more entries...
  operations:
    - name: commit
      commitPush:
        message: "feat: onboard payments, billing, auth"
    - name: open-pr
      pr:
        provider: github
        title: "Bulk onboard services"
        tokenEnv: GITHUB_TOKEN
```

A shell loop (3) cannot do this — each run is isolated, so batching into one PR requires a wrapper module.

## Previewing a Batch

All three approaches work with the usual [preview modes](/guide/dry-run):

```bash
loom run ./bulk-onboard --dry-run          # log what every entry would do
loom run ./bulk-onboard --diff             # show rendered file diffs
loom run ./bulk-onboard --local-run --target-path ./preview   # inspect real clones
```

In `--local-run` mode, each child with its own target clones into a numbered subdirectory (`00-`, `01-`, …) under `--target-path`, in execution order — see [Local Run](/guide/local-run).
