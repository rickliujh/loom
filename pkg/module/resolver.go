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
// Callers must call cleanup (if non-nil) when the directory is no longer needed.
func ResolveSource(source, parentDir string, logger *slog.Logger) (dir string, cleanup func(), err error) {
	if strings.HasPrefix(source, ".") || strings.HasPrefix(source, "/") {
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

	// Git URL — clone to temp directory.
	cloneDir, err := cloneToTemp(repoURL, logger)
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

// cloneToTemp clones a git URL to a temporary directory.
func cloneToTemp(url string, logger *slog.Logger) (string, error) {
	dir, err := os.MkdirTemp("", "loom-module-*")
	if err != nil {
		return "", err
	}

	_, err = git.Clone(context.Background(), url, dir, "", logger)
	if err != nil {
		os.RemoveAll(dir)
		return "", fmt.Errorf("cloning module %q: %w", url, err)
	}

	return dir, nil
}
