---
name: verify
description: Build and drive the loom CLI end-to-end to verify a change against a real module run.
---

# Verifying loom changes

Build: `go build -o bin/loom .`

Drive a change through a real module run (see `.claude/skills/loom` for the
golden workflow). Gotchas learned the hard way:

- `--local-run --target-path <dir>` still **clones** the module's
  `spec.target.url` into a numbered subdir (`<dir>/00-<module-name>/`).
  To run fully offline, point `target.url` at a local git repo:
  `url: "file:///abs/path/to/repo"` (must be a committed git repo,
  `git init -b main` + one commit is enough).
- Minimal module: `loom.yaml` with `params`, `excludes: [__functions]`,
  `target` (url/branch/featureBranch), and `operations`. Patch files live
  in `<module>/__functions/patches/`.
- `loom validate <module>` first; `--diff` (implies `--dry-run`) exercises
  the separate `showPatchDiff` code path in `pkg/action/patch.go` — drive
  it too when patch logic changes.
- To compare against `main`, build a second binary from a temp worktree:
  `git worktree add /tmp/wt main && (cd /tmp/wt && go build -o /tmp/loom-main .)`.
- The interactive shell has zsh `noclobber` set — use `>|` to overwrite
  files in test fixtures, and check the write actually happened.
