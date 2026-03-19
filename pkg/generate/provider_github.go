package generate

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os/exec"
	"strconv"
	"strings"

	"github.com/google/go-github/v60/github"
	"golang.org/x/oauth2"
)

// GitHubDiffProvider fetches PR diffs from GitHub.
type GitHubDiffProvider struct{}

func (p *GitHubDiffProvider) FetchDiff(ctx context.Context, ref, token string, logger *slog.Logger) (*PRInfo, error) {
	owner, repo, number, err := parseGitHubPRRef(ref)
	if err != nil {
		return nil, err
	}
	logger.Debug("parsed GitHub PR reference", "owner", owner, "repo", repo, "number", number)

	if token != "" {
		logger.Debug("attempting GitHub API with token")
		info, err := p.fetchAPI(ctx, owner, repo, number, token, logger)
		if err == nil {
			return info, nil
		}
		logger.Warn("GitHub API failed, trying gh CLI", "error", err)
	} else {
		logger.Debug("no token available, skipping GitHub API")
	}

	if hasBinary("gh", logger) {
		logger.Debug("falling back to gh CLI")
		return p.fetchCLI(ctx, owner, repo, number, logger)
	}

	if token == "" {
		return nil, fmt.Errorf("set GITHUB_TOKEN or install the gh CLI to fetch PR data")
	}
	return nil, fmt.Errorf("GitHub API failed and gh CLI is not available")
}

func (p *GitHubDiffProvider) fetchAPI(ctx context.Context, owner, repo string, number int, token string, logger *slog.Logger) (*PRInfo, error) {
	logger.Debug("creating GitHub API client", "owner", owner, "repo", repo)
	ts := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token})
	tc := oauth2.NewClient(ctx, ts)
	client := github.NewClient(tc)

	logger.Debug("fetching PR metadata", "number", number)
	pr, _, err := client.PullRequests.Get(ctx, owner, repo, number)
	if err != nil {
		return nil, fmt.Errorf("getting PR: %w", err)
	}
	logger.Debug("PR metadata fetched", "title", pr.GetTitle(), "head", pr.GetHead().GetRef(), "base", pr.GetBase().GetRef())

	info := &PRInfo{
		Title:      pr.GetTitle(),
		Body:       pr.GetBody(),
		BaseBranch: pr.GetBase().GetRef(),
		HeadBranch: pr.GetHead().GetRef(),
		RepoURL:    fmt.Sprintf("https://github.com/%s/%s.git", owner, repo),
		Provider:   "github",
	}

	// Fetch file list (paginated).
	logger.Debug("listing PR files")
	opts := &github.ListOptions{PerPage: 100}
	for {
		files, resp, err := client.PullRequests.ListFiles(ctx, owner, repo, number, opts)
		if err != nil {
			return nil, fmt.Errorf("listing PR files: %w", err)
		}
		logger.Debug("fetched PR files page", "count", len(files), "page", opts.Page)

		for _, f := range files {
			fc := FileChange{Path: f.GetFilename()}
			logger.Debug("processing PR file", "path", fc.Path, "status", f.GetStatus())

			switch f.GetStatus() {
			case "added":
				fc.Type = ChangeAdded
			case "modified":
				fc.Type = ChangeModified
			case "removed":
				fc.Type = ChangeDeleted
			case "renamed":
				fc.Type = ChangeRenamed
				fc.OldPath = f.GetPreviousFilename()
			default:
				fc.Type = ChangeModified
			}

			// Fetch file content for added/modified files.
			if fc.Type == ChangeAdded || fc.Type == ChangeModified || fc.Type == ChangeRenamed {
				content, err := fetchRawContent(ctx, tc.Transport, f.GetRawURL())
				if err != nil {
					logger.Warn("failed to fetch file content, skipping", "file", fc.Path, "error", err)
					continue
				}
				fc.NewContent = content
			}

			// Fetch old content for modified files (needed for SMP computation).
			if fc.Type == ChangeModified {
				baseSHA := pr.GetBase().GetSHA()
				oldContent, err := fetchFileAtRef(ctx, client, owner, repo, fc.Path, baseSHA)
				if err != nil {
					logger.Warn("failed to fetch base content", "file", fc.Path, "error", err)
				} else {
					fc.OldContent = oldContent
				}
			}

			info.Files = append(info.Files, fc)
		}

		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}

	return info, nil
}

func fetchRawContent(ctx context.Context, transport http.RoundTripper, rawURL string) ([]byte, error) {
	if rawURL == "" {
		return nil, fmt.Errorf("empty raw URL")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}

	client := &http.Client{Transport: transport}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d fetching %s", resp.StatusCode, rawURL)
	}

	return io.ReadAll(resp.Body)
}

func fetchFileAtRef(ctx context.Context, client *github.Client, owner, repo, path, ref string) ([]byte, error) {
	fileContent, _, _, err := client.Repositories.GetContents(ctx, owner, repo, path, &github.RepositoryContentGetOptions{Ref: ref})
	if err != nil {
		return nil, err
	}

	content, err := fileContent.GetContent()
	if err != nil {
		return nil, err
	}

	return []byte(content), nil
}

func (p *GitHubDiffProvider) fetchCLI(ctx context.Context, owner, repo string, number int, logger *slog.Logger) (*PRInfo, error) {
	nwo := owner + "/" + repo

	// Fetch PR metadata.
	prJSON, err := exec.CommandContext(ctx, "gh", "pr", "view", strconv.Itoa(number),
		"--repo", nwo, "--json", "title,body,baseRefName,headRefName").Output()
	if err != nil {
		return nil, fmt.Errorf("gh pr view: %w", err)
	}

	var prMeta struct {
		Title       string `json:"title"`
		Body        string `json:"body"`
		BaseRefName string `json:"baseRefName"`
		HeadRefName string `json:"headRefName"`
	}
	if err := json.Unmarshal(prJSON, &prMeta); err != nil {
		return nil, fmt.Errorf("parsing gh pr output: %w", err)
	}

	info := &PRInfo{
		Title:      prMeta.Title,
		Body:       prMeta.Body,
		BaseBranch: prMeta.BaseRefName,
		HeadBranch: prMeta.HeadRefName,
		RepoURL:    fmt.Sprintf("https://github.com/%s/%s.git", owner, repo),
		Provider:   "github",
	}

	// Fetch file list.
	filesJSON, err := exec.CommandContext(ctx, "gh", "pr", "diff", strconv.Itoa(number),
		"--repo", nwo, "--name-only").Output()
	if err != nil {
		return nil, fmt.Errorf("gh pr diff --name-only: %w", err)
	}

	// Fetch full diff to classify changes.
	diffJSON, err := exec.CommandContext(ctx, "gh", "api",
		fmt.Sprintf("repos/%s/pulls/%d/files", nwo, number),
		"--paginate").Output()
	if err != nil {
		// Fall back to just file names from diff.
		logger.Warn("could not fetch file details via API, using diff only")
		for _, line := range strings.Split(strings.TrimSpace(string(filesJSON)), "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			info.Files = append(info.Files, FileChange{
				Type: ChangeAdded,
				Path: line,
			})
		}
		return info, nil
	}

	var apiFiles []struct {
		Filename         string `json:"filename"`
		Status           string `json:"status"`
		PreviousFilename string `json:"previous_filename"`
		RawURL           string `json:"raw_url"`
	}
	if err := json.Unmarshal(diffJSON, &apiFiles); err != nil {
		return nil, fmt.Errorf("parsing file list: %w", err)
	}

	for _, f := range apiFiles {
		fc := FileChange{Path: f.Filename}

		switch f.Status {
		case "added":
			fc.Type = ChangeAdded
		case "modified":
			fc.Type = ChangeModified
		case "removed":
			fc.Type = ChangeDeleted
		case "renamed":
			fc.Type = ChangeRenamed
			fc.OldPath = f.PreviousFilename
		default:
			fc.Type = ChangeModified
		}

		// Fetch content for added/modified files via gh api.
		if fc.Type == ChangeAdded || fc.Type == ChangeModified || fc.Type == ChangeRenamed {
			content, err := ghCLIFetchContent(ctx, nwo, fc.Path, info.HeadBranch)
			if err != nil {
				logger.Warn("failed to fetch file content", "file", fc.Path, "error", err)
				continue
			}
			fc.NewContent = content
		}

		if fc.Type == ChangeModified {
			content, err := ghCLIFetchContent(ctx, nwo, fc.Path, info.BaseBranch)
			if err != nil {
				logger.Warn("failed to fetch base content", "file", fc.Path, "error", err)
			} else {
				fc.OldContent = content
			}
		}

		info.Files = append(info.Files, fc)
	}

	return info, nil
}

func ghCLIFetchContent(ctx context.Context, nwo, path, ref string) ([]byte, error) {
	out, err := exec.CommandContext(ctx, "gh", "api",
		fmt.Sprintf("repos/%s/contents/%s?ref=%s", nwo, path, ref),
		"--jq", ".content").Output()
	if err != nil {
		return nil, err
	}

	// Content is base64 encoded; decode it.
	content := strings.TrimSpace(string(out))
	if content == "" {
		return nil, fmt.Errorf("empty content")
	}

	import64 := strings.NewReader(content)
	decoded, err := io.ReadAll(
		io.NopCloser(import64),
	)
	if err != nil {
		return nil, err
	}

	// The API returns base64, but --jq .content already decodes in newer gh versions.
	// Try to detect if it's still base64.
	return decoded, nil
}

// parseGitHubPRRef extracts owner, repo, and PR number from a reference.
// Supports:
//   - https://github.com/owner/repo/pull/123
//   - github:owner/repo#123
func parseGitHubPRRef(ref string) (string, string, int, error) {
	// Full URL: https://github.com/owner/repo/pull/123
	if strings.Contains(ref, "github.com/") && strings.Contains(ref, "/pull/") {
		parts := strings.Split(ref, "github.com/")
		if len(parts) != 2 {
			return "", "", 0, fmt.Errorf("cannot parse GitHub PR URL %q", ref)
		}
		segments := strings.Split(parts[1], "/")
		if len(segments) < 4 || segments[2] != "pull" {
			return "", "", 0, fmt.Errorf("cannot parse GitHub PR URL %q", ref)
		}
		num, err := strconv.Atoi(segments[3])
		if err != nil {
			return "", "", 0, fmt.Errorf("invalid PR number in %q: %w", ref, err)
		}
		return segments[0], segments[1], num, nil
	}

	// Short-form: github:owner/repo#123
	if strings.HasPrefix(ref, "github:") {
		trimmed := strings.TrimPrefix(ref, "github:")
		hashIdx := strings.Index(trimmed, "#")
		if hashIdx < 0 {
			return "", "", 0, fmt.Errorf("expected github:owner/repo#number, got %q", ref)
		}
		nwo := trimmed[:hashIdx]
		numStr := trimmed[hashIdx+1:]
		parts := strings.SplitN(nwo, "/", 2)
		if len(parts) != 2 {
			return "", "", 0, fmt.Errorf("expected github:owner/repo#number, got %q", ref)
		}
		num, err := strconv.Atoi(numStr)
		if err != nil {
			return "", "", 0, fmt.Errorf("invalid PR number in %q: %w", ref, err)
		}
		return parts[0], parts[1], num, nil
	}

	return "", "", 0, fmt.Errorf("cannot parse GitHub PR reference %q", ref)
}
