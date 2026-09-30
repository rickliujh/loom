package action

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// resolveTargetPath joins rel onto the target directory and rejects a result
// that lands outside it (WP1). validate can only check a destination that is
// written literally in the config; one built from a template is known at run
// time alone, so a param value carrying ".." would otherwise steer the write
// out of the target repository. field names the config field for the error,
// in the same words validate uses for its static check.
func resolveTargetPath(targetDir, field, rel string) (string, error) {
	joined := filepath.Join(targetDir, rel)
	if !isWithin(targetDir, joined) {
		return "", fmt.Errorf("%s %q escapes the target directory", field, rel)
	}

	// WP3: the lexical check cannot see a symlink inside the target that
	// points out of it, so compare the resolved locations as well. Only what
	// already exists can be resolved — the rest of the path is about to be
	// created as plain directories, which cannot escape.
	realTarget, err := filepath.EvalSymlinks(targetDir)
	if err != nil {
		// No target on disk to resolve against; the lexical check stands.
		return joined, nil
	}
	realAncestor, err := filepath.EvalSymlinks(existingAncestor(joined))
	if err != nil {
		return "", fmt.Errorf("resolving %s %q: %w", field, rel, err)
	}
	if !isWithin(realTarget, realAncestor) {
		return "", fmt.Errorf("%s %q escapes the target directory through a symlink", field, rel)
	}
	return joined, nil
}

// isWithin reports whether path is dir itself or lies beneath it, judged
// lexically.
func isWithin(dir, path string) bool {
	rel, err := filepath.Rel(dir, path)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// existingAncestor returns the deepest prefix of path that exists on disk.
// Lstat, not Stat: a dangling symlink still exists as a link, and resolving it
// is what reveals where a write through it would land.
func existingAncestor(path string) string {
	for {
		if _, err := os.Lstat(path); err == nil {
			return path
		}
		parent := filepath.Dir(path)
		if parent == path {
			return path
		}
		path = parent
	}
}
