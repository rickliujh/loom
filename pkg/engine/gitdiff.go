package engine

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/rickliujh/loom/internal/proc"
	"github.com/rickliujh/loom/pkg/event"
)

// CollectOptions says how CollectTargetDiffs identifies targets and what it
// reads back from each.
type CollectOptions struct {
	// Labels maps a clone dir to the breadcrumb of the module that produced
	// it, so each target is identified as quick mode identifies it; a dir it
	// lacks falls back to the module name in its numbered dir name.
	Labels map[string][]string
	// Raw keeps git's output as `git diff --cached` prints it under the
	// user's config — coloured when Color is set — in TargetDiff.Raw; a repo
	// then counts as changed exactly when that output is non-blank.
	Raw, Color bool
	// Files parses each changed file into TargetDiff.Files, reading git's
	// output with fixed options, so user git config (prefixes, external diff
	// drivers, textconv, path quoting, rename detection) cannot change what
	// is parsed. Asking for neither Raw nor Files reads Files.
	Files bool
}

// CollectTargetDiffs stages and diffs every git repo directly under root (and
// root itself, if it is one) against its base branch, returning the repos that
// changed in directory order — the numbered order local mode clones in.
// Staging first is what makes newly created files show.
//
// On error it returns the targets read so far.
func CollectTargetDiffs(ctx context.Context, root string, opts CollectOptions) ([]TargetDiff, error) {
	return CollectDirDiffs(ctx, gitRepoDirs(root), opts)
}

// CollectDirDiffs is CollectTargetDiffs for exactly the given clone dirs, in
// the order given. It stages in each of them, so a caller passes only
// clones a run made, never a directory someone works in.
func CollectDirDiffs(ctx context.Context, dirs []string, opts CollectOptions) ([]TargetDiff, error) {
	colorFlag := "--color=never"
	if opts.Color {
		colorFlag = "--color=always"
	}
	files := opts.Files || !opts.Raw

	var targets []TargetDiff
	for _, dir := range dirs {
		// A dir that is not itself a repository — a clone that failed — is
		// skipped: git would find the repository around it and stage there.
		if !isGitRepo(dir) {
			continue
		}
		if out, err := runGit(ctx, dir, "add", "-A"); err != nil {
			return targets, fmt.Errorf("staging changes in %s: %w\n%s", dir, err, out)
		}
		base := baseRef(ctx, dir)
		t := TargetDiff{Dir: dir, Base: base}

		if opts.Raw {
			out, err := runGit(ctx, dir, "--no-pager", "diff", "--cached", colorFlag, base)
			if err != nil {
				return targets, fmt.Errorf("diffing %s: %w", dir, err)
			}
			if strings.TrimSpace(out) == "" {
				continue
			}
			t.Raw = out
		}

		if files {
			out, err := runGit(ctx, dir, "-c", "core.quotepath=off", "--no-pager", "diff", "--cached",
				"--no-color", "--no-ext-diff", "--no-textconv", "--src-prefix=a/", "--dst-prefix=b/", "-M", base)
			if err != nil {
				return targets, fmt.Errorf("diffing %s: %w", dir, err)
			}
			t.Files = ParseGitDiff(out)
			if !opts.Raw && len(t.Files) == 0 {
				continue
			}
		}

		t.Path, t.Repo, t.Branch, t.Label = targetIdentity(ctx, dir, base, opts.Labels)
		targets = append(targets, t)
	}
	return targets, nil
}

// targetIdentity names a clone for a diff header: the module's instance
// breadcrumb (recorded when the clone was made, so a bulk item keeps its
// unique instance name), the origin remote URL and the base branch — read from
// the clone itself — and the "repo (branch)" label combining them.
func targetIdentity(ctx context.Context, dir, base string, labels map[string][]string) (path []string, repo, branch, label string) {
	path, ok := labels[dir]
	if !ok {
		// No recorded breadcrumb (e.g. a repo that predates this run): fall
		// back to the module name carried in the numbered clone dir.
		path = []string{moduleFromDir(filepath.Base(dir))}
	}
	branch = strings.TrimPrefix(base, "refs/remotes/origin/")
	if branch == "HEAD" {
		branch = ""
	}
	if url, err := runGit(ctx, dir, "remote", "get-url", "origin"); err == nil {
		repo = strings.TrimSpace(url)
	}
	label = repo
	if branch != "" {
		if label != "" {
			label += " (" + branch + ")"
		} else {
			label = branch
		}
	}
	return path, repo, branch, label
}

// moduleFromDir strips the "NN-" execution-order prefix local mode adds to a
// clone directory, leaving the module name.
func moduleFromDir(name string) string {
	i := 0
	for i < len(name) && name[i] >= '0' && name[i] <= '9' {
		i++
	}
	if i > 0 && i < len(name) && name[i] == '-' {
		return name[i+1:]
	}
	return name
}

// gitRepoDirs returns root (if a git repo) followed by its immediate git-repo
// subdirectories, in directory-name order — matching the numbered subdirs that
// local mode clones into.
func gitRepoDirs(root string) []string {
	var dirs []string
	if isGitRepo(root) {
		dirs = append(dirs, root)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return dirs
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		p := filepath.Join(root, e.Name())
		if isGitRepo(p) {
			dirs = append(dirs, p)
		}
	}
	return dirs
}

func isGitRepo(dir string) bool {
	// .git is a directory for a normal clone (a file for worktrees/submodules).
	_, err := os.Stat(filepath.Join(dir, ".git"))
	return err == nil
}

// baseRef returns the remote-tracking branch a single-branch clone was made
// from — the pristine baseline to diff against. Falls back to HEAD.
func baseRef(ctx context.Context, dir string) string {
	out, err := runGit(ctx, dir, "for-each-ref", "--format=%(refname)", "refs/remotes/origin/")
	if err == nil {
		for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
			if line = strings.TrimSpace(line); line != "" {
				return line
			}
		}
	}
	return "HEAD"
}

func runGit(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := proc.Command(ctx, "git", append([]string{"-C", dir}, args...)...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return stdout.String(), fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}

// ParseGitDiff splits `git diff` output into one FileDiff per file. It expects
// the output CollectTargetDiffs asks for — no colour, "a/" and "b/" prefixes —
// and never fails: a line it does not recognise is skipped, or kept as part
// of a hunk.
//
// Paths are read from the "diff --git" line, whose two sides are equal for
// anything but a rename or copy, and from the "rename from/to" lines
// otherwise; that holds for paths with spaces, which git leaves unquoted, and
// quoted paths are unquoted. The "---"/"+++" lines are not used: git appends a
// tab to a path containing a space there.
func ParseGitDiff(out string) []FileDiff {
	var files []FileDiff
	for _, chunk := range splitChunks(out) {
		files = append(files, parseChunk(chunk))
	}
	return files
}

// splitChunks cuts the output at each "diff --git " line.
func splitChunks(out string) [][]string {
	var chunks [][]string
	for _, line := range strings.SplitAfter(out, "\n") {
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "diff --git ") {
			chunks = append(chunks, []string{line})
			continue
		}
		if len(chunks) > 0 {
			chunks[len(chunks)-1] = append(chunks[len(chunks)-1], line)
		}
	}
	return chunks
}

func parseChunk(lines []string) FileDiff {
	f := FileDiff{Status: event.StatusModified}
	oldPath, newPath := headerPaths(strings.TrimSuffix(strings.TrimPrefix(lines[0], "diff --git "), "\n"))
	var renameFrom, renameTo string
	var hunks strings.Builder
	inHunks := false
	for _, line := range lines[1:] {
		if inHunks {
			hunks.WriteString(line)
			continue
		}
		text := strings.TrimSuffix(line, "\n")
		switch {
		case strings.HasPrefix(text, "@@"):
			inHunks = true
			hunks.WriteString(line)
		case strings.HasPrefix(text, "new file mode "):
			f.Status = event.StatusAdded
		case strings.HasPrefix(text, "deleted file mode "):
			f.Status = event.StatusDeleted
		case strings.HasPrefix(text, "rename from "):
			f.Status = event.StatusRenamed
			renameFrom = unquotePath(strings.TrimPrefix(text, "rename from "))
		case strings.HasPrefix(text, "rename to "):
			f.Status = event.StatusRenamed
			renameTo = unquotePath(strings.TrimPrefix(text, "rename to "))
		case strings.HasPrefix(text, "Binary files ") || text == "GIT binary patch":
			f.Binary = true
		}
	}
	f.Path = newPath
	if f.Status == event.StatusRenamed {
		if renameTo != "" {
			f.Path = renameTo
		}
		f.OldPath = oldPath
		if renameFrom != "" {
			f.OldPath = renameFrom
		}
	}
	if f.Status == event.StatusDeleted && f.Path == "" {
		f.Path = oldPath
	}
	f.Unified = hunks.String()
	return f
}

// headerPaths reads the two paths of a "diff --git" line (without its
// prefix): each is "a/…" / "b/…", C-quoted when it holds a special character.
func headerPaths(s string) (oldPath, newPath string) {
	if strings.HasPrefix(s, `"`) {
		// A quoted old side ends at its closing quote.
		if q, rest, ok := cutQuoted(s); ok {
			return strings.TrimPrefix(q, "a/"), sidePath(strings.TrimPrefix(rest, " "), "b/")
		}
	}
	if strings.HasSuffix(s, `"`) {
		if i := strings.LastIndex(s, ` "b/`); i >= 0 {
			return strings.TrimPrefix(s[:i], "a/"), sidePath(s[i+1:], "b/")
		}
	}
	// Unquoted on both sides. The sides are equal unless the file was renamed
	// or copied, so the line splits exactly in the middle; a renamed file's
	// paths come from its "rename from/to" lines instead.
	if len(s)%2 == 1 {
		half := len(s) / 2
		a, b := s[:half], s[half+1:]
		if strings.HasPrefix(a, "a/") && strings.HasPrefix(b, "b/") && a[2:] == b[2:] {
			return a[2:], b[2:]
		}
	}
	if i := strings.Index(s, " b/"); i >= 0 {
		return strings.TrimPrefix(s[:i], "a/"), s[i+3:]
	}
	return s, s
}

// sidePath unquotes one side of a "diff --git" line and strips its prefix.
func sidePath(s, prefix string) string {
	return strings.TrimPrefix(unquotePath(s), prefix)
}

// unquotePath undoes git's C-style quoting of a path; an unquoted path is
// returned as is.
func unquotePath(s string) string {
	if q, rest, ok := cutQuoted(s); ok && rest == "" {
		return q
	}
	return s
}

// cutQuoted reads a C-quoted string at the start of s, returning its value
// and what follows the closing quote.
func cutQuoted(s string) (value, rest string, ok bool) {
	if !strings.HasPrefix(s, `"`) {
		return "", s, false
	}
	for i := 1; i < len(s); i++ {
		switch s[i] {
		case '\\':
			i++
		case '"':
			v, err := unquoteC(s[1:i])
			if err != nil {
				return "", s, false
			}
			return v, s[i+1:], true
		}
	}
	return "", s, false
}

// unquoteC decodes git's quote_c_style escapes: \a \b \t \n \v \f \r \" \\
// and three-digit octal bytes (git writes non-ASCII this way unless
// core.quotepath is off).
func unquoteC(s string) (string, error) {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c != '\\' {
			b.WriteByte(c)
			continue
		}
		i++
		if i >= len(s) {
			return "", fmt.Errorf("trailing backslash in %q", s)
		}
		switch e := s[i]; e {
		case 'a':
			b.WriteByte('\a')
		case 'b':
			b.WriteByte('\b')
		case 't':
			b.WriteByte('\t')
		case 'n':
			b.WriteByte('\n')
		case 'v':
			b.WriteByte('\v')
		case 'f':
			b.WriteByte('\f')
		case 'r':
			b.WriteByte('\r')
		case '"', '\\':
			b.WriteByte(e)
		default:
			if i+3 > len(s) {
				return "", fmt.Errorf("bad escape in %q", s)
			}
			n, err := strconv.ParseUint(s[i:i+3], 8, 8)
			if err != nil {
				return "", fmt.Errorf("bad escape in %q", s)
			}
			b.WriteByte(byte(n))
			i += 2
		}
	}
	return b.String(), nil
}
