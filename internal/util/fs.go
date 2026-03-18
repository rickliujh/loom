package util

import (
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

const FunctionsDir = "__functions"

// implicitExcludeDirs are directories excluded by default.
var implicitExcludeDirs = []string{".git"}

// implicitExcludeFiles are files excluded by default.
var implicitExcludeFiles = []string{"README.md"}

// IsReservedDir returns true if the directory name is reserved by loom.
func IsReservedDir(name string) bool {
	return name == FunctionsDir
}

// matchesAny returns true if name matches any of the given glob patterns.
func matchesAny(name string, patterns []string) bool {
	for _, p := range patterns {
		if matched, _ := filepath.Match(p, name); matched {
			return true
		}
	}
	return false
}

// FilterOptions controls which files and directories are included or excluded
// during template file walking.
type FilterOptions struct {
	// Excludes are additional glob patterns for files/dirs to exclude.
	Excludes []string
	// Includes are glob patterns that override excludes (including implicit ones).
	Includes []string
}

// isExcludedDir checks whether a directory should be skipped.
func isExcludedDir(name string, opts *FilterOptions) bool {
	// Check if explicitly included — includes override excludes.
	if opts != nil && matchesAny(name, opts.Includes) {
		return false
	}

	// Check implicit directory excludes.
	for _, d := range implicitExcludeDirs {
		if name == d {
			return true
		}
	}

	// Check user-defined excludes.
	if opts != nil && matchesAny(name, opts.Excludes) {
		return true
	}

	return false
}

// isExcludedFile checks whether a file should be skipped.
func isExcludedFile(rel string, opts *FilterOptions) bool {
	name := filepath.Base(rel)

	// Check if explicitly included — includes override excludes.
	if opts != nil && matchesAny(name, opts.Includes) {
		return false
	}

	// Check implicit file excludes (case-insensitive for README).
	for _, f := range implicitExcludeFiles {
		if strings.EqualFold(name, f) {
			return true
		}
	}

	// Check user-defined excludes.
	if opts != nil && matchesAny(name, opts.Excludes) {
		return true
	}

	return false
}

// WalkTemplateFiles walks a module directory and returns relative paths of
// template files, applying implicit and user-defined exclude/include rules.
//
// Implicit excludes:
//   - Directories: .git
//   - Files: README.md (case-insensitive)
//   - Config files: loom.yaml, loom.jsonnet (always excluded, cannot be overridden)
//
// User-defined excludes add additional patterns. Includes override both
// implicit and user-defined excludes.
func WalkTemplateFiles(moduleDir string, opts *FilterOptions) ([]string, error) {
	var files []string
	err := filepath.WalkDir(moduleDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		rel, _ := filepath.Rel(moduleDir, path)

		if d.IsDir() {
			if rel == "." {
				return nil
			}
			if isExcludedDir(d.Name(), opts) {
				return filepath.SkipDir
			}
			return nil
		}

		// Config files are always excluded and cannot be overridden.
		if rel == "loom.yaml" || rel == "loom.jsonnet" {
			return nil
		}

		if isExcludedFile(rel, opts) {
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
