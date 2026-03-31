package generate

import (
	"context"
	"encoding/base64"
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
	repoURL := githubRepoURL(ref, owner, repo)
	logger.Debug("parsed GitHub PR reference", "owner", owner, "repo", repo, "number", number, "repoURL", repoURL)

	if token != "" {
		logger.Debug("attempting GitHub API with token")
		info, err := p.fetchAPI(ctx, owner, repo, number, repoURL, token, logger)
		if err == nil {
			return info, nil
		}
		logger.Warn("GitHub API failed, trying gh CLI", "error", err)
	} else {
		logger.Debug("no token available, skipping GitHub API")
	}

	if hasBinary("gh", logger) {
		logger.Debug("falling back to gh CLI")
		return p.fetchCLI(ctx, owner, repo, number, repoURL, logger)
	}

	if token == "" {
		return nil, fmt.Errorf("set GITHUB_TOKEN or install the gh CLI to fetch PR data")
	}
	return nil, fmt.Errorf("GitHub API failed and gh CLI is not available")
}

// githubRepoURL derives the repo clone URL from the original reference.
// For full URLs, it extracts the host; for short-form, defaults to github.com.
func githubRepoURL(ref, owner, repo string) string {
	if pullIdx := strings.Index(ref, "/pull/"); pullIdx >= 0 {
		return ref[:pullIdx] + ".git"
	}
	// Short-form fallback — github: prefix implies github.com.
	return fmt.Sprintf("https://github.com/%s/%s.git", owner, repo)
}

func (p *GitHubDiffProvider) fetchAPI(ctx context.Context, owner, repo string, number int, repoURL, token string, logger *slog.Logger) (*PRInfo, error) {
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
		RepoURL:    repoURL,
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

			// Fetch old content for modified/renamed files (needed for SMP computation).
			if fc.Type == ChangeModified || fc.Type == ChangeRenamed {
				baseSHA := pr.GetBase().GetSHA()
				// For renamed files, the old content lives at the previous path.
				oldPath := fc.Path
				if fc.Type == ChangeRenamed {
					oldPath = fc.OldPath
				}
				oldContent, err := fetchFileAtRef(ctx, client, owner, repo, oldPath, baseSHA)
				if err != nil {
					logger.Warn("failed to fetch base content", "file", oldPath, "error", err)
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

func (p *GitHubDiffProvider) fetchCLI(ctx context.Context, owner, repo string, number int, repoURL string, logger *slog.Logger) (*PRInfo, error) {
	nwo := owner + "/" + repo

	// Fetch PR metadata including commit SHAs for resilience against branch deletion.
	prJSON, err := exec.CommandContext(ctx, "gh", "pr", "view", strconv.Itoa(number),
		"--repo", nwo, "--json", "title,body,baseRefName,headRefName,headRefOid,baseRefOid").Output()
	if err != nil {
		return nil, fmt.Errorf("gh pr view: %w", err)
	}

	var prMeta struct {
		Title       string `json:"title"`
		Body        string `json:"body"`
		BaseRefName string `json:"baseRefName"`
		HeadRefName string `json:"headRefName"`
		HeadRefOid  string `json:"headRefOid"`
		BaseRefOid  string `json:"baseRefOid"`
	}
	if err := json.Unmarshal(prJSON, &prMeta); err != nil {
		return nil, fmt.Errorf("parsing gh pr output: %w", err)
	}

	// Use commit SHAs instead of branch names — branches may be deleted after merge.
	headRef := prMeta.HeadRefOid
	baseRef := prMeta.BaseRefOid
	if headRef == "" {
		headRef = prMeta.HeadRefName
		logger.Debug("headRefOid empty, falling back to branch name", "ref", headRef)
	}
	if baseRef == "" {
		baseRef = prMeta.BaseRefName
		logger.Debug("baseRefOid empty, falling back to branch name", "ref", baseRef)
	}
	logger.Debug("using refs for file content (gh CLI)", "headRef", headRef, "baseRef", baseRef)

	info := &PRInfo{
		Title:      prMeta.Title,
		Body:       prMeta.Body,
		BaseBranch: prMeta.BaseRefName,
		HeadBranch: prMeta.HeadRefName,
		RepoURL:    repoURL,
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
			content, err := ghCLIFetchContent(ctx, nwo, fc.Path, headRef)
			if err != nil {
				logger.Warn("failed to fetch file content", "file", fc.Path, "error", err)
				continue
			}
			fc.NewContent = content
		}

		if fc.Type == ChangeModified || fc.Type == ChangeRenamed {
			// For renamed files, the old content lives at the previous path.
			oldPath := fc.Path
			if fc.Type == ChangeRenamed {
				oldPath = fc.OldPath
			}
			content, err := ghCLIFetchContent(ctx, nwo, oldPath, baseRef)
			if err != nil {
				logger.Warn("failed to fetch base content", "file", oldPath, "error", err)
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

	// The GitHub API returns file content as base64-encoded. The --jq .content
	// flag extracts the raw base64 string; we must decode it.
	content := strings.TrimSpace(string(out))
	if content == "" {
		return nil, fmt.Errorf("empty content")
	}

	decoded, err := base64.StdEncoding.DecodeString(content)
	if err != nil {
		// gh with newer --jq may already decode; if base64 decode fails,
		// assume the content is already plain text.
		return []byte(content), nil
	}

	return decoded, nil
}

// parseGitHubPRRef extracts owner, repo, and PR number from a reference.
// Supports any host (github.com, GitHub Enterprise, self-hosted):
//   - https://<host>/owner/repo/pull/123
//   - github:owner/repo#123
func parseGitHubPRRef(ref string) (string, string, int, error) {
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

	// Full URL: https://<host>/owner/repo/pull/123
	// Extract owner/repo as the two path segments immediately before /pull/.
	pullIdx := strings.Index(ref, "/pull/")
	if pullIdx < 0 {
		return "", "", 0, fmt.Errorf("cannot parse GitHub PR reference %q", ref)
	}

	numStr := strings.TrimRight(ref[pullIdx+len("/pull/"):], "/")
	num, err := strconv.Atoi(numStr)
	if err != nil {
		return "", "", 0, fmt.Errorf("invalid PR number in %q: %w", ref, err)
	}

	// Split the path before /pull/ and take the last two segments as owner/repo.
	beforePull := ref[:pullIdx]
	segments := strings.Split(beforePull, "/")
	if len(segments) < 2 {
		return "", "", 0, fmt.Errorf("cannot parse GitHub PR URL %q", ref)
	}
	owner := segments[len(segments)-2]
	repo := segments[len(segments)-1]
	if owner == "" || repo == "" {
		return "", "", 0, fmt.Errorf("cannot parse GitHub PR URL %q", ref)
	}
	return owner, repo, num, nil
}
