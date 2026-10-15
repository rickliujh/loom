package generate

import (
	"bytes"
	"fmt"
	"log/slog"
	"strings"
)

// netChange accumulates the net effect of all changesets on one file.
type netChange struct {
	origPath    string // path in the state before the first change
	currentPath string // path after the latest change
	// existedBefore is true when the file was present before the first
	// changeset that touched it (i.e. the first change was not an add).
	existedBefore bool
	oldContent    []byte // content before the first change (nil if !existedBefore)
	exists        bool   // present after the latest change
	newContent    []byte // content after the latest change (nil if !exists)
}

// Compose squashes changesets, applied in order, into a single net ChangeSet.
// Per file, the old content comes from the first changeset that touched it
// and the new content from the last, so the result transforms the original
// base state directly into the final desired state.
func Compose(sets []*ChangeSet, logger *slog.Logger) (*ChangeSet, error) {
	if len(sets) == 0 {
		return nil, fmt.Errorf("no change sets to compose")
	}

	// CS3: all sources with a repo URL must reference the same repository.
	repoURL := ""
	for _, cs := range sets {
		if cs.RepoURL == "" {
			continue
		}
		if repoURL == "" {
			repoURL = cs.RepoURL
			continue
		}
		if normalizeRepoURL(cs.RepoURL) != normalizeRepoURL(repoURL) {
			return nil, fmt.Errorf("sources reference different repositories: %s vs %s", repoURL, cs.RepoURL)
		}
	}

	if len(sets) == 1 {
		return sets[0], nil
	}

	// CS1: fold changesets left to right, keyed by current path.
	byPath := make(map[string]*netChange)
	var order []*netChange // first-touch order, for deterministic output

	for i, cs := range sets {
		for _, f := range cs.Files {
			key := f.Path
			if f.Type == ChangeRenamed {
				key = f.OldPath
			}
			e, found := byPath[key]
			if !found {
				e = newNetChange(f)
				byPath[e.currentPath] = e
				order = append(order, e)
				continue
			}

			// CS2: continuity check — this source's view of the file before
			// its change should match the state accumulated so far.
			if e.exists && f.Type != ChangeAdded && f.OldContent != nil && e.newContent != nil &&
				!bytes.Equal(e.newContent, f.OldContent) {
				logger.Warn("file changed between sources; composing anyway (manual review recommended)",
					"file", key, "source", i+1)
			}

			switch f.Type {
			case ChangeAdded:
				if e.exists {
					logger.Warn("file re-added while already present; using later content", "file", key, "source", i+1)
				}
				e.exists = true
				e.newContent = f.NewContent
			case ChangeModified:
				e.exists = true
				e.newContent = f.NewContent
			case ChangeDeleted:
				e.exists = false
				e.newContent = nil
			case ChangeRenamed:
				delete(byPath, key)
				e.currentPath = f.Path
				byPath[e.currentPath] = e
				e.exists = true
				if f.NewContent != nil {
					e.newContent = f.NewContent
				}
			}
		}
	}

	var files []FileChange
	for _, e := range order {
		if f, ok := e.net(); ok {
			files = append(files, f)
		}
	}

	// CS5: everything cancelled out.
	if len(files) == 0 {
		return nil, fmt.Errorf("no net file changes after composing sources")
	}

	// CS4: metadata from the last source that has it — the later sources
	// represent the desired state.
	merged := &ChangeSet{Files: files}
	for _, cs := range sets {
		if cs.Title != "" {
			merged.Title = cs.Title
		}
		if cs.Body != "" {
			merged.Body = cs.Body
		}
		if cs.BaseBranch != "" {
			merged.BaseBranch = cs.BaseBranch
		}
		if cs.HeadBranch != "" {
			merged.HeadBranch = cs.HeadBranch
		}
		if cs.RepoURL != "" {
			merged.RepoURL = cs.RepoURL
		}
		if cs.Provider != "" {
			merged.Provider = cs.Provider
		}
	}

	return merged, nil
}

func newNetChange(f FileChange) *netChange {
	switch f.Type {
	case ChangeAdded:
		return &netChange{
			origPath: f.Path, currentPath: f.Path,
			existedBefore: false, exists: true, newContent: f.NewContent,
		}
	case ChangeDeleted:
		return &netChange{
			origPath: f.Path, currentPath: f.Path,
			existedBefore: true, oldContent: f.OldContent, exists: false,
		}
	case ChangeRenamed:
		return &netChange{
			origPath: f.OldPath, currentPath: f.Path,
			existedBefore: true, oldContent: f.OldContent, exists: true, newContent: f.NewContent,
		}
	default: // ChangeModified
		return &netChange{
			origPath: f.Path, currentPath: f.Path,
			existedBefore: true, oldContent: f.OldContent, exists: true, newContent: f.NewContent,
		}
	}
}

// net reduces the accumulated state to a single FileChange relative to the
// original base state. ok is false when the changes cancelled out.
func (e *netChange) net() (FileChange, bool) {
	switch {
	case !e.existedBefore && !e.exists:
		// Added then deleted — nothing to do.
		return FileChange{}, false
	case !e.existedBefore:
		return FileChange{Type: ChangeAdded, Path: e.currentPath, NewContent: e.newContent}, true
	case !e.exists:
		// Deleted — target the original path; the generated module operates
		// on a repo at base state.
		return FileChange{Type: ChangeDeleted, Path: e.origPath, OldContent: e.oldContent}, true
	case e.origPath != e.currentPath:
		return FileChange{
			Type: ChangeRenamed, Path: e.currentPath, OldPath: e.origPath,
			OldContent: e.oldContent, NewContent: e.newContent,
		}, true
	case bytes.Equal(e.oldContent, e.newContent):
		// Modified back to the original content — nothing to do.
		return FileChange{}, false
	default:
		return FileChange{
			Type: ChangeModified, Path: e.currentPath,
			OldContent: e.oldContent, NewContent: e.newContent,
		}, true
	}
}

// normalizeRepoURL reduces a repository URL to host/path form so that HTTPS,
// SSH, and scp-like spellings of the same repository compare equal.
func normalizeRepoURL(u string) string {
	s := strings.ToLower(strings.TrimSpace(u))
	s = strings.TrimSuffix(s, "/")
	s = strings.TrimSuffix(s, ".git")
	for _, p := range []string{"https://", "http://", "ssh://", "git://"} {
		s = strings.TrimPrefix(s, p)
	}
	// user@host:path or user@host/path
	if i := strings.Index(s, "@"); i >= 0 {
		s = s[i+1:]
	}
	s = strings.Replace(s, ":", "/", 1)
	return strings.TrimSuffix(s, "/")
}
