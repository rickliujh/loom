package util

import (
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

const FunctionsDir = "__functions"

// IsReservedDir returns true if the directory name is reserved by loom.
func IsReservedDir(name string) bool {
	return name == FunctionsDir
}

// WalkTemplateFiles walks a module directory and returns relative paths of
// template files, skipping the __functions directory and loom.yaml.
func WalkTemplateFiles(moduleDir string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(moduleDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		rel, _ := filepath.Rel(moduleDir, path)

		if d.IsDir() {
			if IsReservedDir(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}

		// Skip loom config files at root.
		if rel == "loom.yaml" || rel == "loom.jsonnet" {
			return nil
		}

		files = append(files, rel)
		return nil
	})
	return files, err
}

// CopyFile copies a single file from src to dst, creating parent directories.
func CopyFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}

	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	info, err := in.Stat()
	if err != nil {
		return err
	}

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode())
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}

// WriteFile writes content to a file, creating parent directories.
func WriteFile(path string, content []byte, perm os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, content, perm)
}

// ExpandPath resolves a source path relative to a base directory.
// Absolute paths are returned as-is. Everything else is relative to baseDir.
func ExpandPath(baseDir, source string) string {
	if filepath.IsAbs(source) {
		return source
	}
	return filepath.Join(baseDir, source)
}
