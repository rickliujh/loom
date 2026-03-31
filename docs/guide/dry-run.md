# Dry Run & Diff

## Dry Run

With `--dry-run`, nothing is written, committed, or pushed. Loom just shows you what _would_ happen.

```bash
loom run ./onboard-service \
  -p serviceName=payments \
  --dry-run
```

All operations are simulated:
- `newFiles` logs which files would be written and their sizes
- `patch` logs which files would be patched
- `shell` logs the command that would run
- `commitPush` logs the commit message
- `pr` logs the PR title

## Diff

Add `--diff` to see the actual rendered file contents and patch results as a colored unified diff in your terminal.

```bash
loom run ./onboard-service \
  -p serviceName=payments \
  --diff
```

`--diff` implies `--dry-run` -- no files are written.

### Example output

For a new file:

```diff
--- /dev/null
+++ src/INTEGRATIONDEVOPS-NPRD/test.loom.yaml
@@ -0,0 +1,7 @@
+version: v1
+spec:
+  certificate_authority:
+    name: Let's Encrypt
+  domains:
+  - test.loom
```

For a patched file:

```diff
--- config/my-config.yaml
+++ config/my-config.yaml
@@ -2,6 +2,9 @@
 kind: ConfigMap
 metadata:
   name: my-config
+  labels:
+    added-by: loom
 data:
   key: value
+  newKey: newValue
```

The diff output is colored in terminals: green for additions, red for removals, cyan for headers.

## Combining with Local Run

Both `--dry-run` and `--diff` can be combined with `--target-path` to preview against a local checkout:

```bash
loom run ./onboard-service \
  -p serviceName=payments \
  --target-path ~/repos/gitops \
  --diff
```

::: tip
`--dry-run` takes precedence over `--local-run`. If both are set, dry-run wins and nothing is written.
:::
