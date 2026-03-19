package generate

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"
)

// DiffProvider fetches file changes from a PR/MR.
type DiffProvider interface {
	FetchDiff(ctx context.Context, ref string, token string, logger *slog.Logger) (*PRInfo, error)
}

// ParsePRRef parses a PR/MR URL and returns the provider type and a reference
// string suitable for the provider's FetchDiff method.
//
// Supported formats:
//   - https://github.com/owner/repo/pull/123
//   - https://gitlab.com/group/repo/-/merge_requests/123
//   - github:owner/repo#123
//   - gitlab:group/repo!123
func ParsePRRef(ref string, logger *slog.Logger) (provider string, _ DiffProvider, _ error) {
	logger.Debug("parsing PR/MR reference", "ref", ref)

	// GitHub URL
	if strings.Contains(ref, "github.com/") && strings.Contains(ref, "/pull/") {
		logger.Debug("detected GitHub PR URL")
		return "github", &GitHubDiffProvider{}, nil
	}

	// GitLab URL
	if strings.Contains(ref, "/merge_requests/") {
		logger.Debug("detected GitLab MR URL")
		return "gitlab", &GitLabDiffProvider{}, nil
	}

	// Short-form: github:owner/repo#123
	if strings.HasPrefix(ref, "github:") {
		logger.Debug("detected GitHub short-form reference")
		return "github", &GitHubDiffProvider{}, nil
	}

	// Short-form: gitlab:group/repo!123
	if strings.HasPrefix(ref, "gitlab:") {
		logger.Debug("detected GitLab short-form reference")
		return "gitlab", &GitLabDiffProvider{}, nil
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
