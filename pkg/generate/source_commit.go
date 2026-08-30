package generate

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"
)

// CommitSource fetches the diff of a commit or commit range using git only —
// no provider API. It works against any git host and authenticates with the
// user's existing git credentials.
type CommitSource struct {
	// RepoURL is the remote repository URL. Empty when LocalPath is set.
	RepoURL string
	// LocalPath points at an existing local checkout to read from instead of
	// cloning.
	LocalPath string
	// BaseRev is the base of the range. Empty means HeadRev's first parent
	// (single-commit form).
	BaseRev string
	// HeadRev is the commit whose state the module should reproduce.
	HeadRev string
	// Provider is a hint inferred from the repo host ("github", "gitlab", or "").
	Provider string
}

// Fetch implements ChangeSource.
func (s *CommitSource) Fetch(ctx context.Context, _ string, _ string, logger *slog.Logger) (*ChangeSet, error) {
	if !hasBinary("git", logger) {
		return nil, fmt.Errorf("git CLI is required for commit sources")
	}

	dir := s.LocalPath
	repoURL := s.RepoURL
	if dir == "" {
		tmp, err := os.MkdirTemp("", "loom-commit-*")
		if err != nil {
			return nil, err
		}
		defer os.RemoveAll(tmp)
		if err := cloneBare(ctx, s.RepoURL, tmp, logger); err != nil {
			return nil, fmt.Errorf("fetching %s: %w", s.RepoURL, err)
		}
		dir = tmp
	} else {
		if _, err := gitOut(ctx, dir, "rev-parse", "--git-dir"); err != nil {
			return nil, fmt.Errorf("%s is not a git repository: %w", dir, err)
		}
		if repoURL == "" {
			if out, err := gitOut(ctx, dir, "remote", "get-url", "origin"); err == nil {
				repoURL = strings.TrimSpace(string(out))
			}
		}
	}

	head := s.HeadRev
	base := s.BaseRev
	if base == "" {
		// Single commit: diff against the first parent.
		base = head + "^"
	}
	for _, rev := range []string{s.HeadRev, s.BaseRev} {
		if rev == "" {
			continue
		}
		if err := ensureRev(ctx, dir, rev, logger); err != nil {
			return nil, err
		}
	}
	// The synthesized parent rev only resolves once the head commit exists.
	if s.BaseRev == "" {
		if _, err := gitOut(ctx, dir, "rev-parse", "--verify", "--quiet", base+"^{commit}"); err != nil {
			return nil, fmt.Errorf("cannot resolve parent of commit %q (shallow history?): %w", head, err)
		}
	}

	files, err := gitDiffFiles(ctx, dir, base, head, logger)
	if err != nil {
		return nil, err
	}

	title, body := commitMessage(ctx, dir, head)
	provider := s.Provider
	if provider == "" {
		provider = inferProviderFromURL(repoURL)
	}

	return &ChangeSet{
		Title:      title,
		Body:       body,
		BaseBranch: defaultBranch(ctx, dir, logger),
		RepoURL:    repoURL,
		Provider:   provider,
		Files:      files,
	}, nil
}

// cloneBare clones url into dir as a bare partial clone (blobs fetched
// lazily). Servers that reject partial clone fall back to a full bare clone.
func cloneBare(ctx context.Context, url, dir string, logger *slog.Logger) error {
	logger.Info("cloning repository", "url", url)
	_, err := gitOut(ctx, ".", "clone", "--bare", "--filter=blob:none", url, dir)
	if err == nil {
		return nil
	}
	logger.Debug("partial clone failed; retrying full bare clone", "error", err)
	if rmErr := os.RemoveAll(dir); rmErr != nil {
		return rmErr
	}
	_, err = gitOut(ctx, ".", "clone", "--bare", url, dir)
	return err
}

// ensureRev makes rev resolvable in dir, fetching it from origin if needed
// (e.g. a SHA not reachable from any branch, or missing from a local clone).
func ensureRev(ctx context.Context, dir, rev string, logger *slog.Logger) error {
	if _, err := gitOut(ctx, dir, "rev-parse", "--verify", "--quiet", rev+"^{commit}"); err == nil {
		return nil
	}
	logger.Debug("rev not present locally; fetching from origin", "rev", rev)
	if _, err := gitOut(ctx, dir, "fetch", "origin", rev); err != nil {
		return fmt.Errorf("cannot resolve commit %q: %w", rev, err)
	}
	if _, err := gitOut(ctx, dir, "rev-parse", "--verify", "--quiet", rev+"^{commit}"); err != nil {
		return fmt.Errorf("cannot resolve commit %q: %w", rev, err)
	}
	return nil
}

// gitDiffFiles diffs base against head (or against the working tree when
// head is empty) and materializes old/new contents for each changed file.
func gitDiffFiles(ctx context.Context, dir, base, head string, logger *slog.Logger) ([]FileChange, error) {
	args := []string{"diff", "-M", "--name-status", "-z", base}
	if head != "" {
		args = append(args, head)
	}
	out, err := gitOut(ctx, dir, args...)
	if err != nil {
		return nil, err
	}

	readOld := func(path string) []byte {
		content, err := gitOut(ctx, dir, "show", base+":"+path)
		if err != nil {
			logger.Warn("cannot read old content", "file", path, "error", err)
			return nil
		}
		return content
	}
	readNew := func(path string) []byte {
		var content []byte
		var err error
		if head != "" {
			content, err = gitOut(ctx, dir, "show", head+":"+path)
		} else {
			content, err = os.ReadFile(dir + "/" + path)
		}
		if err != nil {
			logger.Warn("cannot read new content", "file", path, "error", err)
			return nil
		}
		return content
	}

	var files []FileChange
	fields := strings.Split(strings.TrimSuffix(string(out), "\x00"), "\x00")
	for i := 0; i < len(fields); i++ {
		status := fields[i]
		if status == "" {
			continue
		}
		switch status[0] {
		case 'A':
			i++
			files = append(files, FileChange{Type: ChangeAdded, Path: fields[i], NewContent: readNew(fields[i])})
		case 'M', 'T':
			i++
			files = append(files, FileChange{Type: ChangeModified, Path: fields[i], OldContent: readOld(fields[i]), NewContent: readNew(fields[i])})
		case 'D':
			i++
			files = append(files, FileChange{Type: ChangeDeleted, Path: fields[i], OldContent: readOld(fields[i])})
		case 'R':
			oldPath, newPath := fields[i+1], fields[i+2]
			i += 2
			files = append(files, FileChange{Type: ChangeRenamed, Path: newPath, OldPath: oldPath, OldContent: readOld(oldPath), NewContent: readNew(newPath)})
		case 'C':
			// Copies: the new path is a new file.
			newPath := fields[i+2]
			i += 2
			files = append(files, FileChange{Type: ChangeAdded, Path: newPath, NewContent: readNew(newPath)})
		default:
			logger.Warn("unhandled diff status; file skipped", "status", status, "file", fields[i+1])
			i++
		}
	}
	return files, nil
}

func commitMessage(ctx context.Context, dir, rev string) (title, body string) {
	if out, err := gitOut(ctx, dir, "log", "-1", "--format=%s", rev); err == nil {
		title = strings.TrimSpace(string(out))
	}
	if out, err := gitOut(ctx, dir, "log", "-1", "--format=%b", rev); err == nil {
		body = strings.TrimSpace(string(out))
	}
	return title, body
}

// defaultBranch returns the repository's default branch: for bare clones the
// clone's HEAD mirrors the remote default; for checkouts, origin's HEAD, then
// the current branch.
func defaultBranch(ctx context.Context, dir string, logger *slog.Logger) string {
	if out, err := gitOut(ctx, dir, "symbolic-ref", "--short", "refs/remotes/origin/HEAD"); err == nil {
		return strings.TrimPrefix(strings.TrimSpace(string(out)), "origin/")
	}
	if out, err := gitOut(ctx, dir, "symbolic-ref", "--short", "HEAD"); err == nil {
		return strings.TrimSpace(string(out))
	}
	logger.Debug("cannot determine default branch")
	return ""
}

// gitOut runs git with -C dir and returns stdout.
func gitOut(ctx context.Context, dir string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}
