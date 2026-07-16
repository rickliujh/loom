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

	// Short-form references — unambiguous, checked first.
	if strings.HasPrefix(ref, "github:") {
		logger.Debug("detected GitHub short-form reference")
		return &Source{Kind: KindPR, Provider: "github", ChangeSource: &GitHubSource{}}, nil
	}
	if strings.HasPrefix(ref, "gitlab:") {
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

	return nil, fmt.Errorf("cannot detect source kind from reference %q; use a PR/MR URL, a <repo>@<sha> commit reference, a local path, or prefix with github: or gitlab:", ref)
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
