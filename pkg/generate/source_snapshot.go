package generate

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// SnapshotSource captures the current state of files in a local checkout.
// Without Base every matched file becomes an added file (a stamp-out module);
// with Base the working tree is diffed against that git ref (a transform
// module), so modified YAML still produces SMP patches.
type SnapshotSource struct {
	Path    string
	Include []string
	Exclude []string
	Base    string
}

// Fetch implements ChangeSource.
func (s *SnapshotSource) Fetch(ctx context.Context, _ string, _ string, logger *slog.Logger) (*ChangeSet, error) {
	if len(s.Include) == 0 {
		return nil, fmt.Errorf("snapshot source %q requires at least one --include", s.Path)
	}
	info, err := os.Stat(s.Path)
	if err != nil || !info.IsDir() {
		return nil, fmt.Errorf("snapshot source %q is not a directory", s.Path)
	}

	isGit := false
	if hasBinary("git", logger) {
		if _, err := gitOut(ctx, s.Path, "rev-parse", "--git-dir"); err == nil {
			isGit = true
		}
	}

	var files []FileChange
	if s.Base == "" {
		files, err = s.walkFiles(logger)
	} else {
		if !isGit {
			return nil, fmt.Errorf("--base requires %s to be a git repository", s.Path)
		}
		files, err = s.diffAgainstBase(ctx, logger)
	}
	if err != nil {
		return nil, err
	}

	cs := &ChangeSet{Files: files}
	if isGit {
		if out, err := gitOut(ctx, s.Path, "remote", "get-url", "origin"); err == nil {
			cs.RepoURL = strings.TrimSpace(string(out))
			cs.Provider = inferProviderFromURL(cs.RepoURL)
		}
		cs.BaseBranch = s.baseBranch(ctx)
	}
	return cs, nil
}

// walkFiles snapshots every matched file in the working tree as added.
func (s *SnapshotSource) walkFiles(logger *slog.Logger) ([]FileChange, error) {
	var files []FileChange
	err := filepath.WalkDir(s.Path, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, relErr := filepath.Rel(s.Path, p)
		if relErr != nil {
			return relErr
		}
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if !s.matches(rel) {
			return nil
		}
		content, readErr := os.ReadFile(p)
		if readErr != nil {
			logger.Warn("cannot read file; skipped", "file", rel, "error", readErr)
			return nil
		}
		files = append(files, FileChange{Type: ChangeAdded, Path: rel, NewContent: content})
		return nil
	})
	return files, err
}

// diffAgainstBase diffs the working tree (including uncommitted and untracked
// files) against the base ref and keeps the matched entries.
func (s *SnapshotSource) diffAgainstBase(ctx context.Context, logger *slog.Logger) ([]FileChange, error) {
	// git reports diff paths relative to the repository root, so the
	// snapshot path must be the root for includes and reads to line up.
	if top, err := gitOut(ctx, s.Path, "rev-parse", "--show-toplevel"); err == nil {
		topPath, _ := filepath.EvalSymlinks(strings.TrimSpace(string(top)))
		absPath, _ := filepath.Abs(s.Path)
		absPath, _ = filepath.EvalSymlinks(absPath)
		if topPath != absPath {
			return nil, fmt.Errorf("snapshot path %s must be the repository root (%s) when using --base", s.Path, topPath)
		}
	}

	if _, err := gitOut(ctx, s.Path, "rev-parse", "--verify", "--quiet", s.Base+"^{commit}"); err != nil {
		return nil, fmt.Errorf("cannot resolve base ref %q in %s: %w", s.Base, s.Path, err)
	}

	all, err := gitDiffFiles(ctx, s.Path, s.Base, "", logger)
	if err != nil {
		return nil, err
	}
	var files []FileChange
	for _, f := range all {
		if s.matches(f.Path) {
			files = append(files, f)
		}
	}

	// git diff <base> does not report untracked files — capture them too;
	// they are part of the current state.
	out, err := gitOut(ctx, s.Path, "ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return nil, err
	}
	for _, rel := range strings.Split(strings.TrimSuffix(string(out), "\x00"), "\x00") {
		if rel == "" || !s.matches(rel) {
			continue
		}
		content, readErr := os.ReadFile(filepath.Join(s.Path, rel))
		if readErr != nil {
			logger.Warn("cannot read untracked file; skipped", "file", rel, "error", readErr)
			continue
		}
		files = append(files, FileChange{Type: ChangeAdded, Path: rel, NewContent: content})
	}
	return files, nil
}

func (s *SnapshotSource) baseBranch(ctx context.Context) string {
	if s.Base != "" {
		if _, err := gitOut(ctx, s.Path, "show-ref", "--verify", "--quiet", "refs/heads/"+s.Base); err == nil {
			return s.Base
		}
	}
	if out, err := gitOut(ctx, s.Path, "rev-parse", "--abbrev-ref", "HEAD"); err == nil {
		if branch := strings.TrimSpace(string(out)); branch != "HEAD" {
			return branch
		}
	}
	return ""
}

func (s *SnapshotSource) matches(rel string) bool {
	if !matchAnyGlob(s.Include, rel) {
		return false
	}
	return !matchAnyGlob(s.Exclude, rel)
}

func matchAnyGlob(patterns []string, rel string) bool {
	for _, p := range patterns {
		if matchGlob(p, rel) {
			return true
		}
	}
	return false
}

// matchGlob matches a slash-separated glob against a relative path, with **
// matching any number of path segments. A pattern without wildcards also
// matches everything under it when it names a directory prefix.
func matchGlob(pattern, rel string) bool {
	pattern = strings.Trim(path.Clean(pattern), "/")
	if !strings.ContainsAny(pattern, "*?[") {
		return rel == pattern || strings.HasPrefix(rel, pattern+"/")
	}
	return matchSegments(strings.Split(pattern, "/"), strings.Split(rel, "/"))
}

func matchSegments(pat, segs []string) bool {
	if len(pat) == 0 {
		return len(segs) == 0
	}
	if pat[0] == "**" {
		for i := 0; i <= len(segs); i++ {
			if matchSegments(pat[1:], segs[i:]) {
				return true
			}
		}
		return false
	}
	if len(segs) == 0 {
		return false
	}
	if ok, _ := path.Match(pat[0], segs[0]); !ok {
		return false
	}
	return matchSegments(pat[1:], segs[1:])
}
