package git

import (
	"context"
	"fmt"
	"os"
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
}

// NewProvider returns a PRProvider for the given provider name.
func NewProvider(provider, tokenEnv string) (PRProvider, error) {
	token := os.Getenv(tokenEnv)
	if token == "" {
		return nil, fmt.Errorf("environment variable %q is not set", tokenEnv)
	}

	switch provider {
	case "github":
		return &GitHubProvider{Token: token}, nil
	case "gitlab":
		return &GitLabProvider{Token: token}, nil
	default:
		return nil, fmt.Errorf("unsupported PR provider %q", provider)
	}
}

// ---------------------------------------------------------------------------
// GitHub
// ---------------------------------------------------------------------------

// GitHubProvider implements PRProvider for GitHub.
type GitHubProvider struct {
	Token string
}

func (p *GitHubProvider) CreatePR(ctx context.Context, opts PROptions) (string, error) {
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

// ---------------------------------------------------------------------------
// GitLab
// ---------------------------------------------------------------------------

// GitLabProvider implements PRProvider for GitLab merge requests.
type GitLabProvider struct {
	Token string
}

func (p *GitLabProvider) CreatePR(ctx context.Context, opts PROptions) (string, error) {
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

// parseGitLabURL extracts the API base URL and project path from a GitLab repo URL.
// Supports:
//
//	https://gitlab.com/group/subgroup/repo.git  -> https://gitlab.com, group/subgroup/repo
//	https://gitlab.example.com/team/repo.git    -> https://gitlab.example.com, team/repo
//	git@gitlab.com:group/repo.git               -> https://gitlab.com, group/repo
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
