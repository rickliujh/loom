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

// SnapshotSource captures the state of files in a repository. The source is
// either a local checkout (Path) or a remote repository (RepoURL, cloned
// bare with lazy blobs).
//
// Without Ref, a local snapshot captures the working tree — including
// uncommitted and untracked files. With Ref (always for remotes, where no
// working tree exists), it captures the committed tree at that ref.
//
// Without Base every matched file becomes an added file (a stamp-out module);
// with Base the captured state is diffed against that git ref (a transform
// module), so modified YAML still produces SMP patches.
type SnapshotSource struct {
	Path    string // local checkout; mutually exclusive with RepoURL
	RepoURL string // remote repository URL
	Ref     string // committed ref to capture; empty = worktree (local) or default branch (remote)
	Include []string
	Exclude []string
	Base    string
}

// Fetch implements ChangeSource.
func (s *SnapshotSource) Fetch(ctx context.Context, _ string, _ string, logger *slog.Logger) (*ChangeSet, error) {
	if len(s.Include) == 0 {
		name := s.Path
		if name == "" {
			name = s.RepoURL
		}
		return nil, fmt.Errorf("snapshot source %q requires at least one --include", name)
	}
	if s.RepoURL != "" {
		return s.fetchRemote(ctx, logger)
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

	if s.Ref != "" {
		// Committed tree at a ref — same semantics as a remote snapshot,
		// minus the clone.
		if !isGit {
			return nil, fmt.Errorf("snapshot ref %q requires %s to be a git repository", s.Ref, s.Path)
		}
		if err := s.requireRepoRoot(ctx); err != nil {
			return nil, err
		}
		cs, err := s.snapshotAtRef(ctx, s.Path, logger)
		if err != nil {
			return nil, err
		}
		s.addOriginMetadata(ctx, cs)
		return cs, nil
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
		s.addOriginMetadata(ctx, cs)
		cs.BaseBranch = s.baseBranch(ctx)
	}
	return cs, nil
}

// fetchRemote clones the repository bare (blobs fetched lazily) and captures
// the committed tree at Ref, or its diff against Base.
func (s *SnapshotSource) fetchRemote(ctx context.Context, logger *slog.Logger) (*ChangeSet, error) {
	if !hasBinary("git", logger) {
		return nil, fmt.Errorf("git CLI is required for remote snapshot sources")
	}
	tmp, err := os.MkdirTemp("", "loom-snapshot-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)
	if err := cloneBare(ctx, s.RepoURL, tmp, logger); err != nil {
		return nil, fmt.Errorf("fetching %s: %w", s.RepoURL, err)
	}
	cs, err := s.snapshotAtRef(ctx, tmp, logger)
	if err != nil {
		return nil, err
	}
	cs.RepoURL = s.RepoURL
	cs.Provider = inferProviderFromURL(s.RepoURL)
	return cs, nil
}

// snapshotAtRef captures the committed state at Ref (HEAD when empty) in the
// given git dir: the full matched tree, or its diff against Base.
func (s *SnapshotSource) snapshotAtRef(ctx context.Context, dir string, logger *slog.Logger) (*ChangeSet, error) {
	ref := s.Ref
	if ref == "" {
		ref = "HEAD"
	} else if err := ensureRev(ctx, dir, ref, logger); err != nil {
		return nil, err
	}

	var files []FileChange
	if s.Base != "" {
		if err := ensureRev(ctx, dir, s.Base, logger); err != nil {
			return nil, err
		}
		all, err := gitDiffFiles(ctx, dir, s.Base, ref, logger)
		if err != nil {
			return nil, err
		}
		for _, f := range all {
			if s.matches(f.Path) {
				files = append(files, f)
			}
		}
	} else {
		var err error
		files, err = s.lsTreeFiles(ctx, dir, ref, logger)
		if err != nil {
			return nil, err
		}
	}

	cs := &ChangeSet{Files: files}
	for _, cand := range []string{s.Base, s.Ref} {
		if cand == "" {
			continue
		}
		if _, err := gitOut(ctx, dir, "show-ref", "--verify", "--quiet", "refs/heads/"+cand); err == nil {
			cs.BaseBranch = cand
			break
		}
	}
	if cs.BaseBranch == "" {
		cs.BaseBranch = defaultBranch(ctx, dir, logger)
	}
	return cs, nil
}

// lsTreeFiles captures every matched regular file in the tree at ref.
func (s *SnapshotSource) lsTreeFiles(ctx context.Context, dir, ref string, logger *slog.Logger) ([]FileChange, error) {
	out, err := gitOut(ctx, dir, "ls-tree", "-r", "-z", ref)
	if err != nil {
		return nil, err
	}
	var files []FileChange
	for _, entry := range strings.Split(strings.TrimSuffix(string(out), "\x00"), "\x00") {
		meta, rel, ok := strings.Cut(entry, "\t")
		if !ok {
			continue
		}
		fields := strings.Fields(meta) // <mode> <type> <sha>
		if len(fields) < 2 {
			continue
		}
		if !s.matches(rel) {
			continue
		}
		// Only regular files: skip symlinks (mode 120000) and submodules
		// (type commit) — neither can be reproduced as a template.
		if fields[1] != "blob" || fields[0] == "120000" {
			logger.Warn("skipping non-regular file in snapshot", "file", rel, "mode", fields[0], "type", fields[1])
			continue
		}
		content, err := gitOut(ctx, dir, "show", ref+":"+rel)
		if err != nil {
			logger.Warn("cannot read file at ref; skipped", "file", rel, "error", err)
			continue
		}
		files = append(files, FileChange{Type: ChangeAdded, Path: rel, NewContent: content})
	}
	return files, nil
}

// addOriginMetadata fills repo URL and provider from the local checkout's
// origin remote, when it has one.
func (s *SnapshotSource) addOriginMetadata(ctx context.Context, cs *ChangeSet) {
	if out, err := gitOut(ctx, s.Path, "remote", "get-url", "origin"); err == nil {
		cs.RepoURL = strings.TrimSpace(string(out))
		cs.Provider = inferProviderFromURL(cs.RepoURL)
	}
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

// requireRepoRoot rejects snapshot paths below the repository root: git
// reports paths relative to the root, so includes and reads would not line up.
func (s *SnapshotSource) requireRepoRoot(ctx context.Context) error {
	top, err := gitOut(ctx, s.Path, "rev-parse", "--show-toplevel")
	if err != nil {
		return nil
	}
	topPath, _ := filepath.EvalSymlinks(strings.TrimSpace(string(top)))
	absPath, _ := filepath.Abs(s.Path)
	absPath, _ = filepath.EvalSymlinks(absPath)
	if topPath != absPath {
		return fmt.Errorf("snapshot path %s must be the repository root (%s) when using --base or a ref", s.Path, topPath)
	}
	return nil
}

// diffAgainstBase diffs the working tree (including uncommitted and untracked
// files) against the base ref and keeps the matched entries.
func (s *SnapshotSource) diffAgainstBase(ctx context.Context, logger *slog.Logger) ([]FileChange, error) {
	if err := s.requireRepoRoot(ctx); err != nil {
		return nil, err
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
