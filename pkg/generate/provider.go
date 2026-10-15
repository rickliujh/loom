package generate

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"strings"
)

// ChangeSource fetches file changes from a source (PR/MR, commit range, or
// local snapshot).
type ChangeSource interface {
	Fetch(ctx context.Context, ref string, token string, logger *slog.Logger) (*ChangeSet, error)
}

// SourceKind classifies what kind of source a reference points at.
type SourceKind int

const (
	// KindPR is a GitHub pull request or GitLab merge request.
	KindPR SourceKind = iota
	// KindCommit is a single commit or commit range, fetched via git.
	KindCommit
	// KindSnapshot is the current state of files in a local checkout.
	KindSnapshot
)

func (k SourceKind) String() string {
	switch k {
	case KindPR:
		return "pr"
	case KindCommit:
		return "commit"
	case KindSnapshot:
		return "snapshot"
	default:
		return "unknown"
	}
}

// Source is a parsed source reference.
type Source struct {
	Kind SourceKind
	// Provider is "github" or "gitlab" when known, "" otherwise. Only PR
	// sources require a provider; commit and snapshot sources are git-native
	// and use it solely to populate the generated pr operation.
	Provider     string
	ChangeSource ChangeSource
}

// SnapshotOptions configures snapshot (local file) sources. The options are
// shared by all snapshot refs of a single generate invocation.
type SnapshotOptions struct {
	Include []string
	Exclude []string
	Base    string
}

// Regex patterns for URL-based provider detection.
// These validate the full URL structure, not just a substring.
var (
	// Matches: https://<host>/<owner>/<repo>/pull/<number>[/]
	githubPRURLPattern = regexp.MustCompile(`^https?://[^/]+/.+/pull/\d+/?$`)
	// Matches: https://<host>/<path>/-/merge_requests/<number>[/]
	gitlabMRURLPattern = regexp.MustCompile(`^https?://[^/]+/.+/-/merge_requests/\d+/?$`)
	// Matches: https://<host>/<path>[/-]/commit/<sha>[/] (GitHub and GitLab).
	commitURLPattern = regexp.MustCompile(`^(https?://[^/]+/.+?)(?:/-)?/commit/([0-9a-fA-F]{7,40})/?$`)
	// Matches: https://<host>/<path>[/-]/compare/<base>...<head>[/] (GitHub and GitLab).
	compareURLPattern = regexp.MustCompile(`^(https?://[^/]+/.+?)(?:/-)?/compare/([^/]+?)\.\.\.([^/]+?)/?$`)

	// A hex commit SHA (abbreviated or full).
	shaPattern = regexp.MustCompile(`^[0-9a-fA-F]{7,40}$`)
	// A git rev token (SHA or tag name) — no path separators or colons, so
	// SSH URL remainders can never match.
	revTokenPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._\-]*$`)
)

// ParseSourceRef parses a source reference and returns the parsed Source.
//
// PR/MR references (see also the generate spec):
//   - github:owner/repo#123 / gitlab:group/repo!123
//   - https://<host>/owner/repo/pull/123
//   - https://<host>/group/repo/-/merge_requests/123
//
// Commit references (git-native, any host):
//   - <repo>@<sha> or <repo>@<base>...<head>, where <repo> is a short-form
//     repo (github:o/r), any git URL, or a local checkout path
//   - commit / compare URLs (sugar, normalized to <repo>@<range>)
//
// Snapshot references (current state of local files):
//   - a local path: ./checkout, /abs/path, or file:<path>
func ParseSourceRef(ref string, snap SnapshotOptions, logger *slog.Logger) (*Source, error) {
	logger.Debug("parsing source reference", "ref", ref)

	// Explicit snapshot references — canonical form for both remote repos
	// and local paths; a trailing @<ref> pins the committed tree at that ref.
	if body, ok := strings.CutPrefix(ref, "snapshot:"); ok {
		return parseSnapshotRef(body, snap, logger)
	}

	// Local paths — snapshot, or commit source when an @<rev> suffix is
	// present. Only hex SHAs are accepted as rev suffixes on paths so that
	// directory names containing '@' are not misclassified.
	if path, ok := strings.CutPrefix(ref, "file:"); ok {
		return parseLocalRef(path, snap, logger)
	}
	if isPathLike(ref) {
		return parseLocalRef(ref, snap, logger)
	}

	// Short-form references — unambiguous, checked first.
	if body, ok := strings.CutPrefix(ref, "github:"); ok {
		if repo, base, head, ok := splitRevSuffix(body, true); ok {
			logger.Debug("detected GitHub short-form commit reference")
			return &Source{Kind: KindCommit, Provider: "github", ChangeSource: &CommitSource{
				RepoURL: fmt.Sprintf("git@github.com:%s.git", repo), BaseRev: base, HeadRev: head, Provider: "github",
			}}, nil
		}
		logger.Debug("detected GitHub short-form reference")
		return &Source{Kind: KindPR, Provider: "github", ChangeSource: &GitHubSource{}}, nil
	}
	if body, ok := strings.CutPrefix(ref, "gitlab:"); ok {
		if repo, base, head, ok := splitRevSuffix(body, true); ok {
			logger.Debug("detected GitLab short-form commit reference")
			return &Source{Kind: KindCommit, Provider: "gitlab", ChangeSource: &CommitSource{
				RepoURL: fmt.Sprintf("git@gitlab.com:%s.git", repo), BaseRev: base, HeadRev: head, Provider: "gitlab",
			}}, nil
		}
		logger.Debug("detected GitLab short-form reference")
		return &Source{Kind: KindPR, Provider: "gitlab", ChangeSource: &GitLabSource{}}, nil
	}

	// URL-based detection with strict pattern matching.
	if githubPRURLPattern.MatchString(ref) {
		logger.Debug("detected GitHub PR URL")
		return &Source{Kind: KindPR, Provider: "github", ChangeSource: &GitHubSource{}}, nil
	}
	if gitlabMRURLPattern.MatchString(ref) {
		logger.Debug("detected GitLab MR URL")
		return &Source{Kind: KindPR, Provider: "gitlab", ChangeSource: &GitLabSource{}}, nil
	}

	// Commit / compare URLs — sugar for <repo>@<range>, handled via git.
	if m := commitURLPattern.FindStringSubmatch(ref); m != nil {
		logger.Debug("detected commit URL")
		repoURL := m[1] + ".git"
		provider := inferProviderFromURL(repoURL)
		return &Source{Kind: KindCommit, Provider: provider, ChangeSource: &CommitSource{
			RepoURL: repoURL, HeadRev: m[2], Provider: provider,
		}}, nil
	}
	if m := compareURLPattern.FindStringSubmatch(ref); m != nil {
		logger.Debug("detected compare URL")
		repoURL := m[1] + ".git"
		provider := inferProviderFromURL(repoURL)
		return &Source{Kind: KindCommit, Provider: provider, ChangeSource: &CommitSource{
			RepoURL: repoURL, BaseRev: m[2], HeadRev: m[3], Provider: provider,
		}}, nil
	}

	// Any other git URL with an @<rev> suffix — platform-agnostic commit ref.
	if repo, base, head, ok := splitRevSuffix(ref, true); ok && looksLikeGitURL(repo) {
		logger.Debug("detected git URL commit reference")
		provider := inferProviderFromURL(repo)
		return &Source{Kind: KindCommit, Provider: provider, ChangeSource: &CommitSource{
			RepoURL: repo, BaseRev: base, HeadRev: head, Provider: provider,
		}}, nil
	}

	return nil, fmt.Errorf("cannot detect source kind from reference %q; use a PR/MR URL, a <repo>@<sha> commit reference, a local path, or prefix with github: or gitlab:", ref)
}

// parseSnapshotRef parses the body of a snapshot:<repo-or-path>[@<ref>]
// reference. Because the snapshot: prefix is explicit, tag and branch names
// are accepted as refs (unlike bare local paths, which require hex SHAs).
func parseSnapshotRef(body string, snap SnapshotOptions, logger *slog.Logger) (*Source, error) {
	repo, refName := body, ""
	if r, base, head, ok := splitRevSuffix(body, true); ok {
		if base != "" {
			return nil, fmt.Errorf("snapshot references take a single ref, not a range: %q", body)
		}
		repo, refName = r, head
	}
	if r, ok := strings.CutPrefix(repo, "github:"); ok {
		repo = fmt.Sprintf("git@github.com:%s.git", r)
	} else if r, ok := strings.CutPrefix(repo, "gitlab:"); ok {
		repo = fmt.Sprintf("git@gitlab.com:%s.git", r)
	}

	src := &SnapshotSource{Ref: refName, Include: snap.Include, Exclude: snap.Exclude, Base: snap.Base}
	if looksLikeGitURL(repo) {
		logger.Debug("detected remote snapshot reference", "repo", repo, "ref", refName)
		src.RepoURL = repo
		return &Source{Kind: KindSnapshot, Provider: inferProviderFromURL(repo), ChangeSource: src}, nil
	}
	logger.Debug("detected local snapshot reference", "path", repo, "ref", refName)
	src.Path = repo
	return &Source{Kind: KindSnapshot, ChangeSource: src}, nil
}

func parseLocalRef(path string, snap SnapshotOptions, logger *slog.Logger) (*Source, error) {
	if repo, base, head, ok := splitRevSuffix(path, false); ok {
		logger.Debug("detected local commit reference", "path", repo)
		return &Source{Kind: KindCommit, ChangeSource: &CommitSource{
			LocalPath: repo, BaseRev: base, HeadRev: head,
		}}, nil
	}
	logger.Debug("detected snapshot reference", "path", path)
	return &Source{Kind: KindSnapshot, ChangeSource: &SnapshotSource{
		Path: path, Include: snap.Include, Exclude: snap.Exclude, Base: snap.Base,
	}}, nil
}

func isPathLike(ref string) bool {
	return ref == "." || ref == ".." ||
		strings.HasPrefix(ref, "./") || strings.HasPrefix(ref, "../") || strings.HasPrefix(ref, "/")
}

// splitRevSuffix splits "<repo>@<rev>" or "<repo>@<base>...<head>" on the
// last '@'. When allowTags is false only hex SHAs are accepted (used for
// local paths, where '@' may legitimately appear in directory names).
func splitRevSuffix(s string, allowTags bool) (repo, base, head string, ok bool) {
	i := strings.LastIndex(s, "@")
	if i <= 0 || i == len(s)-1 {
		return "", "", "", false
	}
	repo, rev := s[:i], s[i+1:]
	validToken := func(t string) bool {
		if shaPattern.MatchString(t) {
			return true
		}
		return allowTags && revTokenPattern.MatchString(t)
	}
	if from, to, found := strings.Cut(rev, "..."); found {
		if validToken(from) && validToken(to) {
			return repo, from, to, true
		}
		return "", "", "", false
	}
	if validToken(rev) {
		return repo, "", rev, true
	}
	return "", "", "", false
}

// looksLikeGitURL reports whether s is plausibly a remote git repository URL.
func looksLikeGitURL(s string) bool {
	if strings.Contains(s, "://") {
		return true
	}
	// scp-like syntax: user@host:path
	head, rest, found := strings.Cut(s, "@")
	return found && head != "" && strings.Contains(rest, ":")
}

// inferProviderFromURL guesses the provider from the repository URL host.
// It is only used to populate the generated pr operation; an empty result
// degrades to omitting that operation.
func inferProviderFromURL(repoURL string) string {
	host := hostOf(repoURL)
	switch {
	case strings.Contains(host, "github"):
		return "github"
	case strings.Contains(host, "gitlab"):
		return "gitlab"
	default:
		return ""
	}
}

func hostOf(repoURL string) string {
	if parsed, err := url.Parse(repoURL); err == nil && parsed.Host != "" {
		return strings.ToLower(parsed.Hostname())
	}
	// scp-like syntax: user@host:path
	if _, rest, found := strings.Cut(repoURL, "@"); found {
		if host, _, ok := strings.Cut(rest, ":"); ok {
			return strings.ToLower(host)
		}
	}
	return ""
}

// tokenFromEnv returns the API token from the given env var, or falls back
// to standard defaults.
func tokenFromEnv(tokenEnv, provider string, logger *slog.Logger) string {
	if tokenEnv != "" {
		v := os.Getenv(tokenEnv)
		if v != "" {
			logger.Debug("using token from custom env var", "env", tokenEnv)
		} else {
			logger.Debug("custom env var is empty", "env", tokenEnv)
		}
		return v
	}
	switch provider {
	case "github":
		t := os.Getenv("GITHUB_TOKEN")
		if t != "" {
			logger.Debug("using token from GITHUB_TOKEN")
		} else {
			logger.Debug("GITHUB_TOKEN not set")
		}
		return t
	case "gitlab":
		if t := os.Getenv("GITLAB_TOKEN"); t != "" {
			logger.Debug("using token from GITLAB_TOKEN")
			return t
		}
		if t := os.Getenv("GITLAB_PRIVATE_TOKEN"); t != "" {
			logger.Debug("using token from GITLAB_PRIVATE_TOKEN")
			return t
		}
		logger.Debug("no GitLab token found (checked GITLAB_TOKEN, GITLAB_PRIVATE_TOKEN)")
	}
	return ""
}

func hasBinary(name string, logger *slog.Logger) bool {
	path, err := exec.LookPath(name)
	if err != nil {
		logger.Debug("CLI not found in PATH", "binary", name)
		return false
	}
	logger.Debug("CLI found", "binary", name, "path", path)
	return true
}
