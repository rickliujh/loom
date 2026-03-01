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
// Sources starting with "." or "/" are treated as local paths.
// Other sources are treated as git URLs and cloned to a temp directory.
func ResolveSource(source, parentDir string) (string, error) {
	if strings.HasPrefix(source, ".") || strings.HasPrefix(source, "/") {
		path := source
		if strings.HasPrefix(source, ".") {
			path = filepath.Join(parentDir, source)
		}
		info, err := os.Stat(path)
		if err != nil {
			return "", fmt.Errorf("module source %q: %w", source, err)
		}
		if !info.IsDir() {
			return "", fmt.Errorf("module source %q is not a directory", source)
		}
		return path, nil
	}

	// Git URL — clone to temp directory.
	return cloneToTemp(source)
}

// cloneToTemp clones a git URL to a temporary directory.
func cloneToTemp(url string) (string, error) {
	dir, err := os.MkdirTemp("", "loom-module-*")
	if err != nil {
		return "", err
	}

	_, err = git.Clone(context.Background(), url, dir, "", slog.Default())
	if err != nil {
		os.RemoveAll(dir)
		return "", fmt.Errorf("cloning module %q: %w", url, err)
	}

	return dir, nil
}
