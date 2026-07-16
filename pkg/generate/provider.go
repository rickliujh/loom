package generate

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"regexp"
	"strings"
)

// ChangeSource fetches file changes from a PR/MR.
type ChangeSource interface {
	Fetch(ctx context.Context, ref string, token string, logger *slog.Logger) (*ChangeSet, error)
}

// Regex patterns for URL-based provider detection.
// These validate the full URL structure, not just a substring.
var (
	// Matches: https://<host>/<owner>/<repo>/pull/<number>[/]
	githubPRURLPattern = regexp.MustCompile(`^https?://[^/]+/.+/pull/\d+/?$`)
	// Matches: https://<host>/<path>/-/merge_requests/<number>[/]
	gitlabMRURLPattern = regexp.MustCompile(`^https?://[^/]+/.+/-/merge_requests/\d+/?$`)
)

// ParseSourceRef parses a PR/MR URL and returns the provider type and a
// ChangeSource suitable for fetching PR/MR data.
//
// Short-form references (unambiguous, preferred for self-hosted instances):
//   - github:owner/repo#123
//   - gitlab:group/repo!123
//
// URL-based detection (works for any host, including self-hosted):
//   - https://<host>/owner/repo/pull/123           → GitHub
//   - https://<host>/group/repo/-/merge_requests/123 → GitLab
//
// For self-hosted instances where URL patterns may be ambiguous, use the
// short-form prefix (github: or gitlab:) to explicitly specify the provider.
func ParseSourceRef(ref string, logger *slog.Logger) (provider string, _ ChangeSource, _ error) {
	logger.Debug("parsing PR/MR reference", "ref", ref)

	// Short-form references — unambiguous, checked first.
	if strings.HasPrefix(ref, "github:") {
		logger.Debug("detected GitHub short-form reference")
		return "github", &GitHubSource{}, nil
	}
	if strings.HasPrefix(ref, "gitlab:") {
		logger.Debug("detected GitLab short-form reference")
		return "gitlab", &GitLabSource{}, nil
	}

	// URL-based detection with strict pattern matching.
	if githubPRURLPattern.MatchString(ref) {
		logger.Debug("detected GitHub PR URL")
		return "github", &GitHubSource{}, nil
	}
	if gitlabMRURLPattern.MatchString(ref) {
		logger.Debug("detected GitLab MR URL")
		return "gitlab", &GitLabSource{}, nil
	}

	return "", nil, fmt.Errorf("cannot detect provider from reference %q; use a full URL or prefix with github: or gitlab:", ref)
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
