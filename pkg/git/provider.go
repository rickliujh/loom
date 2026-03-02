package git

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"

	"github.com/google/go-github/v60/github"
	gitlab "gitlab.com/gitlab-org/api/client-go"
	"golang.org/x/oauth2"
)

// PRProvider is the interface for creating pull requests / merge requests.
type PRProvider interface {
	CreatePR(ctx context.Context, opts PROptions) (string, error)
}

// PROptions holds the parameters for creating a PR.
type PROptions struct {
	RepoURL    string
	Title      string
	Body       string
	HeadBranch string
	BaseBranch string
	Labels     []string
	WorkDir    string // repo working directory, used by CLI fallback
}

// NewProvider returns a PRProvider for the given provider name.
// It checks for the API token first; if the token is missing but the
// corresponding CLI binary (gh / glab) is available, it returns a
// CLI-only provider instead of an error.
func NewProvider(provider, tokenEnv string, logger *slog.Logger) (PRProvider, error) {
	token := os.Getenv(tokenEnv)

	switch provider {
	case "github":
		if token != "" {
			return &GitHubProvider{Token: token, Logger: logger}, nil
		}
		if hasBinary("gh") {
			logger.Info("token env not set, using gh CLI for PR creation", "tokenEnv", tokenEnv)
			return &ghCLIProvider{logger: logger}, nil
		}
		return nil, fmt.Errorf("environment variable %q is not set and gh CLI is not available", tokenEnv)

	case "gitlab":
		if token != "" {
			return &GitLabProvider{Token: token, Logger: logger}, nil
		}
		if hasBinary("glab") {
			logger.Info("token env not set, using glab CLI for MR creation", "tokenEnv", tokenEnv)
			return &glabCLIProvider{logger: logger}, nil
		}
		return nil, fmt.Errorf("environment variable %q is not set and glab CLI is not available", tokenEnv)

	default:
		return nil, fmt.Errorf("unsupported PR provider %q", provider)
	}
}

// ===========================================================================
// GitHub — library with gh CLI fallback
// ===========================================================================

// GitHubProvider implements PRProvider using the GitHub API.
type GitHubProvider struct {
	Token  string
	Logger *slog.Logger
}

func (p *GitHubProvider) CreatePR(ctx context.Context, opts PROptions) (string, error) {
	url, err := p.createPRAPI(ctx, opts)
	if err == nil {
		return url, nil
	}

	if !hasBinary("gh") {
		return "", err
	}

	p.Logger.Info("GitHub API failed, falling back to gh CLI", "error", err)
	return ghCLICreatePR(ctx, opts)
}

func (p *GitHubProvider) createPRAPI(ctx context.Context, opts PROptions) (string, error) {
	owner, repo, err := parseGitHubURL(opts.RepoURL)
	if err != nil {
		return "", err
	}

	ts := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: p.Token})
	tc := oauth2.NewClient(ctx, ts)
	client := github.NewClient(tc)

	pr, _, err := client.PullRequests.Create(ctx, owner, repo, &github.NewPullRequest{
		Title: github.String(opts.Title),
		Body:  github.String(opts.Body),
		Head:  github.String(opts.HeadBranch),
		Base:  github.String(opts.BaseBranch),
	})
	if err != nil {
		return "", fmt.Errorf("creating PR: %w", err)
	}

	if len(opts.Labels) > 0 {
		_, _, err = client.Issues.AddLabelsToIssue(ctx, owner, repo, pr.GetNumber(), opts.Labels)
		if err != nil {
			return pr.GetHTMLURL(), fmt.Errorf("adding labels: %w", err)
		}
	}

	return pr.GetHTMLURL(), nil
}

// ghCLIProvider is a GitHub PRProvider that uses only the gh CLI.
type ghCLIProvider struct {
	logger *slog.Logger
}

func (p *ghCLIProvider) CreatePR(ctx context.Context, opts PROptions) (string, error) {
	return ghCLICreatePR(ctx, opts)
}

func ghCLICreatePR(ctx context.Context, opts PROptions) (string, error) {
	args := []string{"pr", "create",
		"--title", opts.Title,
		"--body", opts.Body,
		"--base", opts.BaseBranch,
		"--head", opts.HeadBranch,
	}

	owner, repo, err := parseGitHubURL(opts.RepoURL)
	if err == nil {
		args = append(args, "--repo", owner+"/"+repo)
	}

	for _, l := range opts.Labels {
		args = append(args, "--label", l)
	}

	cmd := exec.CommandContext(ctx, "gh", args...)
	if opts.WorkDir != "" {
		cmd.Dir = opts.WorkDir
	}

	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("gh pr create: %w\n%s", err, output)
	}

	// gh outputs the PR URL on stdout.
	return strings.TrimSpace(string(output)), nil
}

// parseGitHubURL extracts owner and repo from a GitHub URL.
func parseGitHubURL(url string) (string, string, error) {
	url = strings.TrimSuffix(url, ".git")

	if strings.Contains(url, "github.com/") {
		parts := strings.Split(url, "github.com/")
		if len(parts) != 2 {
			return "", "", fmt.Errorf("cannot parse GitHub URL %q", url)
		}
		segments := strings.SplitN(parts[1], "/", 2)
		if len(segments) != 2 {
			return "", "", fmt.Errorf("cannot parse GitHub URL %q", url)
		}
		return segments[0], segments[1], nil
	}

	return "", "", fmt.Errorf("cannot parse GitHub URL %q", url)
}

// ===========================================================================
// GitLab — library with glab CLI fallback
// ===========================================================================

// GitLabProvider implements PRProvider using the GitLab API.
type GitLabProvider struct {
	Token  string
	Logger *slog.Logger
}

func (p *GitLabProvider) CreatePR(ctx context.Context, opts PROptions) (string, error) {
	url, err := p.createMRAPI(ctx, opts)
	if err == nil {
		return url, nil
	}

	if !hasBinary("glab") {
		return "", err
	}

	p.Logger.Info("GitLab API failed, falling back to glab CLI", "error", err)
	return glabCLICreateMR(ctx, opts)
}

func (p *GitLabProvider) createMRAPI(ctx context.Context, opts PROptions) (string, error) {
	baseURL, projectPath, err := parseGitLabURL(opts.RepoURL)
	if err != nil {
		return "", err
	}

	client, err := gitlab.NewClient(p.Token, gitlab.WithBaseURL(baseURL))
	if err != nil {
		return "", fmt.Errorf("creating GitLab client: %w", err)
	}

	mrOpts := &gitlab.CreateMergeRequestOptions{
		Title:        gitlab.Ptr(opts.Title),
		Description:  gitlab.Ptr(opts.Body),
		SourceBranch: gitlab.Ptr(opts.HeadBranch),
		TargetBranch: gitlab.Ptr(opts.BaseBranch),
	}

	if len(opts.Labels) > 0 {
		labels := gitlab.LabelOptions(opts.Labels)
		mrOpts.Labels = &labels
	}

	mr, _, err := client.MergeRequests.CreateMergeRequest(projectPath, mrOpts)
	if err != nil {
		return "", fmt.Errorf("creating MR: %w", err)
	}

	return mr.WebURL, nil
}

// glabCLIProvider is a GitLab PRProvider that uses only the glab CLI.
type glabCLIProvider struct {
	logger *slog.Logger
}

func (p *glabCLIProvider) CreatePR(ctx context.Context, opts PROptions) (string, error) {
	return glabCLICreateMR(ctx, opts)
}

func glabCLICreateMR(ctx context.Context, opts PROptions) (string, error) {
	_, projectPath, err := parseGitLabURL(opts.RepoURL)
	if err != nil {
		// If URL parsing fails, run glab from repo dir and let it infer.
		projectPath = ""
	}

	args := []string{"mr", "create",
		"--title", opts.Title,
		"--description", opts.Body,
		"--source-branch", opts.HeadBranch,
		"--target-branch", opts.BaseBranch,
		"--no-editor",
	}

	if projectPath != "" {
		args = append(args, "--repo", projectPath)
	}

	if len(opts.Labels) > 0 {
		args = append(args, "--label", strings.Join(opts.Labels, ","))
	}

	cmd := exec.CommandContext(ctx, "glab", args...)
	if opts.WorkDir != "" {
		cmd.Dir = opts.WorkDir
	}

	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("glab mr create: %w\n%s", err, output)
	}

	// glab outputs structured text; try to extract the URL.
	return parseMRURL(string(output)), nil
}

// parseMRURL extracts a merge request URL from glab output.
func parseMRURL(output string) string {
	// glab may output JSON with --output json, but by default it prints
	// human-readable text containing the MR URL. Try JSON first.
	var result struct {
		WebURL string `json:"web_url"`
	}
	if json.Unmarshal([]byte(output), &result) == nil && result.WebURL != "" {
		return result.WebURL
	}

	// Fallback: scan lines for an HTTP URL.
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "https://") || strings.HasPrefix(line, "http://") {
			return line
		}
	}

	return strings.TrimSpace(output)
}

// parseGitLabURL extracts the API base URL and project path from a GitLab repo URL.
func parseGitLabURL(raw string) (string, string, error) {
	raw = strings.TrimSuffix(raw, ".git")

	// SSH: git@host:path
	if strings.HasPrefix(raw, "git@") {
		trimmed := strings.TrimPrefix(raw, "git@")
		idx := strings.Index(trimmed, ":")
		if idx < 0 {
			return "", "", fmt.Errorf("cannot parse GitLab SSH URL %q", raw)
		}
		host := trimmed[:idx]
		path := trimmed[idx+1:]
		if path == "" {
			return "", "", fmt.Errorf("cannot parse GitLab SSH URL %q: empty project path", raw)
		}
		return "https://" + host, path, nil
	}

	// HTTPS: https://host/path
	for _, scheme := range []string{"https://", "http://"} {
		if strings.HasPrefix(raw, scheme) {
			withoutScheme := strings.TrimPrefix(raw, scheme)
			idx := strings.Index(withoutScheme, "/")
			if idx < 0 {
				return "", "", fmt.Errorf("cannot parse GitLab URL %q: no project path", raw)
			}
			host := withoutScheme[:idx]
			path := withoutScheme[idx+1:]
			if path == "" {
				return "", "", fmt.Errorf("cannot parse GitLab URL %q: empty project path", raw)
			}
			return scheme + host, path, nil
		}
	}

	return "", "", fmt.Errorf("cannot parse GitLab URL %q", raw)
}
