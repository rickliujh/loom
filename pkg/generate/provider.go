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
func ParsePRRef(ref string) (provider string, _ DiffProvider, _ error) {
	// GitHub URL
	if strings.Contains(ref, "github.com/") && strings.Contains(ref, "/pull/") {
		return "github", &GitHubDiffProvider{}, nil
	}

	// GitLab URL
	if strings.Contains(ref, "/merge_requests/") {
		return "gitlab", &GitLabDiffProvider{}, nil
	}

	// Short-form: github:owner/repo#123
	if strings.HasPrefix(ref, "github:") {
		return "github", &GitHubDiffProvider{}, nil
	}

	// Short-form: gitlab:group/repo!123
	if strings.HasPrefix(ref, "gitlab:") {
		return "gitlab", &GitLabDiffProvider{}, nil
	}

	return "", nil, fmt.Errorf("cannot detect provider from reference %q; use a full URL or prefix with github: or gitlab:", ref)
}

// tokenFromEnv returns the API token from the given env var, or falls back
// to standard defaults.
func tokenFromEnv(tokenEnv, provider string) string {
	if tokenEnv != "" {
		return os.Getenv(tokenEnv)
	}
	switch provider {
	case "github":
		return os.Getenv("GITHUB_TOKEN")
	case "gitlab":
		if t := os.Getenv("GITLAB_TOKEN"); t != "" {
			return t
		}
		return os.Getenv("GITLAB_PRIVATE_TOKEN")
	}
	return ""
}

func hasBinary(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}
