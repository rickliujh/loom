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

// ResolveSource resolves a module source to a local directory.
// Sources starting with "." or "/" are treated as local paths (cleanup is nil).
// Other sources are treated as git URLs and cloned to a temp directory.
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

	// Git URL — clone to temp directory.
	cloneDir, err := cloneToTemp(source, logger)
	if err != nil {
		return "", nil, err
	}
	return cloneDir, func() { os.RemoveAll(cloneDir) }, nil
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
