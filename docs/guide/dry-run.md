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

To see the actual rendered file contents and patch results as a colored unified diff, use the [`loom diff`](/reference/cli-diff) command. Add `--quick` for a fast, no-execution preview equivalent to a dry-run:

```bash
loom diff ./onboard-service \
  -p serviceName=payments \
  --quick
```

`--quick` simulates the run -- no files are written and nothing executes.

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

Full `loom diff` (without `--quick`) already runs the module in local mode and prints a real `git diff` — including changes made by shell commands, which the dry-run preview cannot show. Add `--target-path` to keep the clones in numbered subdirectories you can inspect afterwards:

```bash
loom diff ./onboard-service \
  -p serviceName=payments \
  --target-path ./preview
```

For modules without a `target` spec, `--target-path` is used directly as the target directory.

::: tip
`--dry-run` takes precedence over `--local-run`. If both are set, dry-run wins and nothing is written.
:::
