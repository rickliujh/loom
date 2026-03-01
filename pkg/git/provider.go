package git

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/google/go-github/v60/github"
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
		return nil, fmt.Errorf("gitlab provider not yet implemented")
	default:
		return nil, fmt.Errorf("unsupported PR provider %q", provider)
	}
}

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

	// Add labels if specified.
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
	// Handle both HTTPS and SSH URLs.
	url = strings.TrimSuffix(url, ".git")

	// https://github.com/owner/repo
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
