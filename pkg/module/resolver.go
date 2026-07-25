package module

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/rickliujh/loom/pkg/git"
)

// splitSourceRef splits an optional "?ref=<version>" version selector off the
// end of a module source. Returns (base, ref); ref is empty when no selector is
// present. Following the Terraform convention, the ref query is the last
// component of the source — after any "//subdir" — so it is stripped before URL
// and local-path parsing.
func splitSourceRef(source string) (base, ref string) {
	const marker = "?ref="
	idx := strings.LastIndex(source, marker)
	if idx < 0 {
		return source, ""
	}
	return source[:idx], source[idx+len(marker):]
}

// splitSourceURL splits a source URL at the first "//" that is not part of a
// scheme (i.e. not preceded by ":"). Returns (repoURL, subDir). If no "//"
// separator is found, subDir is empty.
func splitSourceURL(source string) (repoURL, subDir string) {
	idx := 0
	for {
		pos := strings.Index(source[idx:], "//")
		if pos < 0 {
			return source, ""
		}
		abs := idx + pos
		if abs > 0 && source[abs-1] == ':' {
			// Part of scheme like "https://" — skip past it.
			idx = abs + 2
			continue
		}
		return source[:abs], source[abs+2:]
	}
}

// ResolveSource resolves a module source to a local directory.
// Sources starting with "." or "/" are treated as local paths (cleanup is nil).
// Other sources are treated as git URLs and cloned to a temp directory.
// The "//" separator in git URLs denotes a subdirectory within the repo.
//
// version optionally pins a git source to a branch, tag, or commit. It may be
// given as the explicit `version` argument (spec.modules[].version, used by
// child modules) or as a "?ref=<version>" suffix on the source (used on the
// CLI, where the source is the whole argument); the explicit argument wins when
// both are present.
//
// Callers must call cleanup (if non-nil) when the directory is no longer needed.
func ResolveSource(source, version, parentDir string, logger *slog.Logger) (dir string, cleanup func(), err error) {
	// Strip any "?ref=<version>" suffix before path/URL parsing so the
	// local-path and "//subdir" checks operate on the bare source. The explicit
	// version argument takes precedence over the suffix.
	source, srcRef := splitSourceRef(source)
	ref := version
	if ref == "" {
		ref = srcRef
	}

	if strings.HasPrefix(source, ".") || strings.HasPrefix(source, "/") {
		if ref != "" {
			return "", nil, fmt.Errorf("module source %q: version pinning is only supported for git sources, not local paths", source)
		}
		path := source
		if strings.HasPrefix(source, ".") {
			path = filepath.Join(parentDir, source)
		}
		info, err := os.Stat(path)
		if err != nil {
			return "", nil, fmt.Errorf("module source %q: %w", source, err)
		}
		if !info.IsDir() {
			return "", nil, fmt.Errorf("module source %q is not a directory", source)
		}
		return path, nil, nil
	}

	// Split repo URL from optional subdir.
	repoURL, subDir := splitSourceURL(source)

	// Git URL — clone to temp directory, pinned to ref when given.
	cloneDir, err := cloneToTemp(repoURL, ref, logger)
	if err != nil {
		return "", nil, err
	}

	resolved := cloneDir
	if subDir != "" {
		resolved = filepath.Join(cloneDir, subDir)
		info, err := os.Stat(resolved)
		if err != nil {
			os.RemoveAll(cloneDir)
			return "", nil, fmt.Errorf("subdir %q in %q: %w", subDir, source, err)
		}
		if !info.IsDir() {
			os.RemoveAll(cloneDir)
			return "", nil, fmt.Errorf("subdir %q in %q is not a directory", subDir, source)
		}
	}

	return resolved, func() { os.RemoveAll(cloneDir) }, nil
}

// cloneToTemp clones a git URL to a temporary directory. When ref is non-empty
// the clone is checked out at that branch, tag, or commit, pinning the module
// to a specific version.
func cloneToTemp(url, ref string, logger *slog.Logger) (string, error) {
	dir, err := os.MkdirTemp("", "loom-module-*")
	if err != nil {
		return "", err
	}

	// Clone the default branch in full so any ref (tag/commit/other branch) is
	// present for the checkout below.
	_, err = git.Clone(context.Background(), url, dir, "", logger)
	if err != nil {
		os.RemoveAll(dir)
		return "", fmt.Errorf("cloning module %q: %w", url, err)
	}

	if ref != "" {
		if err := git.Checkout(context.Background(), dir, ref, logger); err != nil {
			os.RemoveAll(dir)
			return "", fmt.Errorf("pinning module %q to version %q: %w", url, ref, err)
		}
	}

	return dir, nil
}
