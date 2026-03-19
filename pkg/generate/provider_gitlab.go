package generate

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/url"
	"os/exec"
	"strconv"
	"strings"

	gogitlab "gitlab.com/gitlab-org/api/client-go"
)

// GitLabDiffProvider fetches MR diffs from GitLab.
type GitLabDiffProvider struct{}

func (p *GitLabDiffProvider) FetchDiff(ctx context.Context, ref, token string, logger *slog.Logger) (*PRInfo, error) {
	baseURL, projectPath, mrIID, err := parseGitLabMRRef(ref)
	if err != nil {
		return nil, err
	}
	logger.Debug("parsed GitLab MR reference", "baseURL", baseURL, "project", projectPath, "mrIID", mrIID)

	var apiErr error
	if token != "" {
		logger.Debug("attempting GitLab API with token")
		info, err := p.fetchAPI(ctx, baseURL, projectPath, mrIID, token, logger)
		if err == nil {
			return info, nil
		}
		apiErr = err
		logger.Warn("GitLab API failed, trying glab CLI", "error", err)
	} else {
		logger.Debug("no token available, skipping GitLab API")
	}

	if hasBinary("glab", logger) {
		logger.Debug("falling back to glab CLI")
		return p.fetchCLI(ctx, baseURL, projectPath, mrIID, logger)
	}

	if token == "" {
		return nil, fmt.Errorf("set GITLAB_TOKEN or install the glab CLI to fetch MR data")
	}
	return nil, fmt.Errorf("GitLab API failed: %w; glab CLI is not available", apiErr)
}

func (p *GitLabDiffProvider) fetchAPI(ctx context.Context, baseURL, projectPath string, mrIID int64, token string, logger *slog.Logger) (*PRInfo, error) {
	logger.Debug("creating GitLab API client", "baseURL", baseURL, "project", projectPath)
	client, err := gogitlab.NewClient(token, gogitlab.WithBaseURL(baseURL))
	if err != nil {
		return nil, fmt.Errorf("creating GitLab client: %w", err)
	}

	logger.Debug("fetching MR metadata", "project", projectPath, "mrIID", mrIID)
	mr, resp, err := client.MergeRequests.GetMergeRequest(projectPath, mrIID, nil)
	if err != nil {
		status := 0
		if resp != nil {
			status = resp.StatusCode
		}
		logger.Debug("GetMergeRequest failed", "status", status, "error", err)
		return nil, fmt.Errorf("getting MR %s!%d (HTTP %d): %w", projectPath, mrIID, status, err)
	}
	logger.Debug("MR metadata fetched", "title", mr.Title, "source", mr.SourceBranch, "target", mr.TargetBranch)

	// Use commit SHAs from DiffRefs instead of branch names — branches may be
	// deleted after merge, but the commits remain reachable.
	headRef := mr.DiffRefs.HeadSha
	baseRef := mr.DiffRefs.BaseSha
	if headRef == "" {
		headRef = mr.SourceBranch
		logger.Debug("DiffRefs.HeadSha empty, falling back to source branch name", "ref", headRef)
	}
	if baseRef == "" {
		baseRef = mr.TargetBranch
		logger.Debug("DiffRefs.BaseSha empty, falling back to target branch name", "ref", baseRef)
	}
	logger.Debug("using refs for file content", "headRef", headRef, "baseRef", baseRef)

	info := &PRInfo{
		Title:      mr.Title,
		Body:       mr.Description,
		BaseBranch: mr.TargetBranch,
		HeadBranch: mr.SourceBranch,
		RepoURL:    fmt.Sprintf("%s/%s.git", baseURL, projectPath),
		Provider:   "gitlab",
	}

	// Fetch MR diffs via /diffs endpoint (GitLab 15.7+).
	logger.Debug("listing MR diffs via API")
	diffs, resp, err := client.MergeRequests.ListMergeRequestDiffs(projectPath, mrIID, nil)
	if err != nil {
		status := 0
		if resp != nil {
			status = resp.StatusCode
		}
		logger.Debug("ListMergeRequestDiffs failed", "status", status, "error", err)
		return nil, fmt.Errorf("listing MR diffs (HTTP %d): %w", status, err)
	}
	logger.Debug("MR diffs fetched", "count", len(diffs))

	for _, d := range diffs {
		fc := FileChange{Path: d.NewPath}

		if d.NewFile {
			fc.Type = ChangeAdded
		} else if d.DeletedFile {
			fc.Type = ChangeDeleted
		} else if d.RenamedFile {
			fc.Type = ChangeRenamed
			fc.OldPath = d.OldPath
		} else {
			fc.Type = ChangeModified
		}
		logger.Debug("processing diff entry", "path", d.NewPath, "type", fc.Type, "newFile", d.NewFile, "deleted", d.DeletedFile, "renamed", d.RenamedFile)

		// Fetch file content for added/modified/renamed files.
		if fc.Type == ChangeAdded || fc.Type == ChangeModified || fc.Type == ChangeRenamed {
			logger.Debug("fetching file content at head ref", "file", fc.Path, "ref", headRef)
			content, _, err := client.RepositoryFiles.GetRawFile(projectPath, fc.Path, &gogitlab.GetRawFileOptions{
				Ref: gogitlab.Ptr(headRef),
			})
			if err != nil {
				logger.Warn("failed to fetch file content", "file", fc.Path, "error", err)
				continue
			}
			logger.Debug("fetched file content", "file", fc.Path, "size", len(content))
			fc.NewContent = content
		}

		// Fetch old content for modified files.
		if fc.Type == ChangeModified {
			logger.Debug("fetching file content at base ref", "file", fc.Path, "ref", baseRef)
			content, _, err := client.RepositoryFiles.GetRawFile(projectPath, fc.Path, &gogitlab.GetRawFileOptions{
				Ref: gogitlab.Ptr(baseRef),
			})
			if err != nil {
				logger.Warn("failed to fetch base content", "file", fc.Path, "error", err)
			} else {
				logger.Debug("fetched base content", "file", fc.Path, "size", len(content))
				fc.OldContent = content
			}
		}

		info.Files = append(info.Files, fc)
	}

	return info, nil
}

func (p *GitLabDiffProvider) fetchCLI(ctx context.Context, baseURL, projectPath string, mrIID int64, logger *slog.Logger) (*PRInfo, error) {
	encodedProject := url.PathEscape(projectPath)

	// Extract hostname from baseURL for --hostname flag.
	hostname := ""
	if u, err := url.Parse(baseURL); err == nil {
		hostname = u.Host
	}
	logger.Debug("using glab CLI", "project", projectPath, "encodedProject", encodedProject, "mrIID", mrIID, "hostname", hostname)

	glabCall := func(ctx context.Context, endpoint string) ([]byte, error) {
		return glabAPI(ctx, endpoint, hostname, logger)
	}

	// Fetch MR metadata. Use the encoded project path for this first call.
	mrEndpoint := fmt.Sprintf("projects/%s/merge_requests/%d", encodedProject, mrIID)
	logger.Debug("glab api call", "endpoint", mrEndpoint)
	mrJSON, err := glabCall(ctx, mrEndpoint)
	if err != nil {
		return nil, fmt.Errorf("glab api (MR metadata): %w", err)
	}
	logger.Debug("glab MR metadata fetched", "responseSize", len(mrJSON))

	var mrMeta struct {
		Title        string `json:"title"`
		Description  string `json:"description"`
		TargetBranch string `json:"target_branch"`
		SourceBranch string `json:"source_branch"`
		WebURL       string `json:"web_url"`
		ProjectID    int64  `json:"project_id"`
		DiffRefs     struct {
			HeadSha string `json:"head_sha"`
			BaseSha string `json:"base_sha"`
		} `json:"diff_refs"`
	}
	if err := json.Unmarshal(mrJSON, &mrMeta); err != nil {
		return nil, fmt.Errorf("parsing glab output: %w", err)
	}

	// Use numeric project_id for subsequent API calls — avoids URL-encoding issues
	// where glab may double-encode %2F in the project path.
	projectID := strconv.FormatInt(mrMeta.ProjectID, 10)
	logger.Debug("resolved numeric project ID", "projectID", projectID)

	// Use commit SHAs instead of branch names — branches may be deleted after merge.
	headRef := mrMeta.DiffRefs.HeadSha
	baseRef := mrMeta.DiffRefs.BaseSha
	if headRef == "" {
		headRef = mrMeta.SourceBranch
		logger.Debug("diff_refs.head_sha empty, falling back to source branch name", "ref", headRef)
	}
	if baseRef == "" {
		baseRef = mrMeta.TargetBranch
		logger.Debug("diff_refs.base_sha empty, falling back to target branch name", "ref", baseRef)
	}
	logger.Debug("using refs for file content (CLI)", "headRef", headRef, "baseRef", baseRef)

	info := &PRInfo{
		Title:      mrMeta.Title,
		Body:       mrMeta.Description,
		BaseBranch: mrMeta.TargetBranch,
		HeadBranch: mrMeta.SourceBranch,
		Provider:   "gitlab",
	}

	// Fetch diffs using numeric project ID.
	diffsEndpoint := fmt.Sprintf("projects/%s/merge_requests/%d/diffs", projectID, mrIID)
	logger.Debug("glab api call", "endpoint", diffsEndpoint)
	diffsJSON, err := glabCall(ctx, diffsEndpoint)
	if err != nil {
		return nil, fmt.Errorf("glab api (MR diffs): %w", err)
	}
	logger.Debug("glab MR diffs fetched", "responseSize", len(diffsJSON))

	var diffs []struct {
		NewPath     string `json:"new_path"`
		OldPath     string `json:"old_path"`
		NewFile     bool   `json:"new_file"`
		DeletedFile bool   `json:"deleted_file"`
		RenamedFile bool   `json:"renamed_file"`
	}
	if err := json.Unmarshal(diffsJSON, &diffs); err != nil {
		return nil, fmt.Errorf("parsing diffs: %w", err)
	}

	logger.Debug("parsed diffs", "count", len(diffs))
	for _, d := range diffs {
		fc := FileChange{Path: d.NewPath}

		if d.NewFile {
			fc.Type = ChangeAdded
		} else if d.DeletedFile {
			fc.Type = ChangeDeleted
		} else if d.RenamedFile {
			fc.Type = ChangeRenamed
			fc.OldPath = d.OldPath
		} else {
			fc.Type = ChangeModified
		}
		logger.Debug("processing diff entry (CLI)", "path", d.NewPath, "type", fc.Type)

		if fc.Type == ChangeAdded || fc.Type == ChangeModified || fc.Type == ChangeRenamed {
			fileEndpoint := fmt.Sprintf("projects/%s/repository/files/%s/raw?ref=%s",
				projectID,
				url.PathEscape(fc.Path),
				url.QueryEscape(headRef))
			logger.Debug("glab api call (file content)", "endpoint", fileEndpoint)
			content, err := glabCall(ctx, fileEndpoint)
			if err != nil {
				logger.Warn("failed to fetch file content", "file", fc.Path, "error", err)
				continue
			}
			logger.Debug("fetched file content (CLI)", "file", fc.Path, "size", len(content))
			fc.NewContent = content
		}

		if fc.Type == ChangeModified {
			baseEndpoint := fmt.Sprintf("projects/%s/repository/files/%s/raw?ref=%s",
				projectID,
				url.PathEscape(fc.Path),
				url.QueryEscape(baseRef))
			logger.Debug("glab api call (base content)", "endpoint", baseEndpoint)
			content, err := glabCall(ctx, baseEndpoint)
			if err != nil {
				logger.Warn("failed to fetch base content", "file", fc.Path, "error", err)
			} else {
				logger.Debug("fetched base content (CLI)", "file", fc.Path, "size", len(content))
				fc.OldContent = content
			}
		}

		info.Files = append(info.Files, fc)
	}

	return info, nil
}

// glabAPI runs `glab api <path>` and returns stdout, including stderr in errors.
// If hostname is non-empty, it is passed via --hostname.
// If token is non-empty, it is set as GITLAB_TOKEN in the command environment.
func glabAPI(ctx context.Context, path, hostname string, logger *slog.Logger) ([]byte, error) {
	args := []string{"api"}
	if hostname != "" {
		args = append(args, "--hostname", hostname)
	}
	args = append(args, path)

	logger.Debug("executing glab", "args", args)
	cmd := exec.CommandContext(ctx, "glab", args...)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return out, nil
}

// parseGitLabMRRef extracts base URL, project path, and MR IID from a reference.
// Supports:
//   - https://gitlab.com/group/repo/-/merge_requests/123
//   - gitlab:group/repo!123
func parseGitLabMRRef(ref string) (string, string, int64, error) {
	// Full URL: https://gitlab.example.com/group/repo/-/merge_requests/123
	if strings.Contains(ref, "/merge_requests/") {
		// Find the /-/merge_requests/ part.
		mrIdx := strings.Index(ref, "/-/merge_requests/")
		if mrIdx < 0 {
			// Try without /-/ prefix.
			mrIdx = strings.Index(ref, "/merge_requests/")
			if mrIdx < 0 {
				return "", "", 0, fmt.Errorf("cannot parse GitLab MR URL %q", ref)
			}
			numStr := ref[mrIdx+len("/merge_requests/"):]
			num, err := strconv.ParseInt(strings.TrimRight(numStr, "/"), 10, 64)
			if err != nil {
				return "", "", 0, fmt.Errorf("invalid MR number in %q: %w", ref, err)
			}

			// Extract baseURL and project path.
			urlPart := ref[:mrIdx]
			scheme, rest := splitScheme(urlPart)
			if scheme == "" {
				return "", "", 0, fmt.Errorf("cannot parse GitLab MR URL %q", ref)
			}
			slashIdx := strings.Index(rest, "/")
			if slashIdx < 0 {
				return "", "", 0, fmt.Errorf("cannot parse GitLab MR URL %q", ref)
			}
			return scheme + rest[:slashIdx], rest[slashIdx+1:], num, nil
		}

		numStr := ref[mrIdx+len("/-/merge_requests/"):]
		num, err := strconv.ParseInt(strings.TrimRight(numStr, "/"), 10, 64)
		if err != nil {
			return "", "", 0, fmt.Errorf("invalid MR number in %q: %w", ref, err)
		}

		urlPart := ref[:mrIdx]
		scheme, rest := splitScheme(urlPart)
		if scheme == "" {
			return "", "", 0, fmt.Errorf("cannot parse GitLab MR URL %q", ref)
		}
		slashIdx := strings.Index(rest, "/")
		if slashIdx < 0 {
			return "", "", 0, fmt.Errorf("cannot parse GitLab MR URL %q", ref)
		}
		return scheme + rest[:slashIdx], rest[slashIdx+1:], num, nil
	}

	// Short-form: gitlab:group/repo!123
	if strings.HasPrefix(ref, "gitlab:") {
		trimmed := strings.TrimPrefix(ref, "gitlab:")
		bangIdx := strings.Index(trimmed, "!")
		if bangIdx < 0 {
			return "", "", 0, fmt.Errorf("expected gitlab:group/repo!number, got %q", ref)
		}
		projectPath := trimmed[:bangIdx]
		numStr := trimmed[bangIdx+1:]
		num, err := strconv.ParseInt(numStr, 10, 64)
		if err != nil {
			return "", "", 0, fmt.Errorf("invalid MR number in %q: %w", ref, err)
		}
		return "https://gitlab.com", projectPath, num, nil
	}

	return "", "", 0, fmt.Errorf("cannot parse GitLab MR reference %q", ref)
}

func splitScheme(url string) (string, string) {
	for _, scheme := range []string{"https://", "http://"} {
		if strings.HasPrefix(url, scheme) {
			return scheme, strings.TrimPrefix(url, scheme)
		}
	}
	return "", url
}
