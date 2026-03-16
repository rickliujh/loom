package generate

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
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

	if token != "" {
		info, err := p.fetchAPI(ctx, baseURL, projectPath, mrIID, token, logger)
		if err == nil {
			return info, nil
		}
		logger.Warn("GitLab API failed, trying glab CLI", "error", err)
	}

	if hasBinary("glab") {
		return p.fetchCLI(ctx, projectPath, mrIID, logger)
	}

	if token == "" {
		return nil, fmt.Errorf("set GITLAB_TOKEN or install the glab CLI to fetch MR data")
	}
	return nil, fmt.Errorf("GitLab API failed and glab CLI is not available")
}

func (p *GitLabDiffProvider) fetchAPI(ctx context.Context, baseURL, projectPath string, mrIID int64, token string, logger *slog.Logger) (*PRInfo, error) {
	client, err := gogitlab.NewClient(token, gogitlab.WithBaseURL(baseURL))
	if err != nil {
		return nil, fmt.Errorf("creating GitLab client: %w", err)
	}

	mr, _, err := client.MergeRequests.GetMergeRequest(projectPath, mrIID, nil)
	if err != nil {
		return nil, fmt.Errorf("getting MR: %w", err)
	}

	info := &PRInfo{
		Title:      mr.Title,
		Body:       mr.Description,
		BaseBranch: mr.TargetBranch,
		HeadBranch: mr.SourceBranch,
		RepoURL:    fmt.Sprintf("%s/%s.git", baseURL, projectPath),
		Provider:   "gitlab",
	}

	// Fetch MR diffs.
	diffs, _, err := client.MergeRequests.ListMergeRequestDiffs(projectPath, mrIID, nil)
	if err != nil {
		return nil, fmt.Errorf("listing MR diffs: %w", err)
	}

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

		// Fetch file content for added/modified/renamed files.
		if fc.Type == ChangeAdded || fc.Type == ChangeModified || fc.Type == ChangeRenamed {
			content, _, err := client.RepositoryFiles.GetRawFile(projectPath, fc.Path, &gogitlab.GetRawFileOptions{
				Ref: gogitlab.Ptr(mr.SourceBranch),
			})
			if err != nil {
				logger.Warn("failed to fetch file content", "file", fc.Path, "error", err)
				continue
			}
			fc.NewContent = content
		}

		// Fetch old content for modified files.
		if fc.Type == ChangeModified {
			content, _, err := client.RepositoryFiles.GetRawFile(projectPath, fc.Path, &gogitlab.GetRawFileOptions{
				Ref: gogitlab.Ptr(mr.TargetBranch),
			})
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

func (p *GitLabDiffProvider) fetchCLI(ctx context.Context, projectPath string, mrIID int64, logger *slog.Logger) (*PRInfo, error) {
	// Fetch MR metadata.
	mrJSON, err := exec.CommandContext(ctx, "glab", "api",
		fmt.Sprintf("projects/%s/merge_requests/%d", strings.ReplaceAll(projectPath, "/", "%%2F"), mrIID)).Output()
	if err != nil {
		return nil, fmt.Errorf("glab api: %w", err)
	}

	var mrMeta struct {
		Title        string `json:"title"`
		Description  string `json:"description"`
		TargetBranch string `json:"target_branch"`
		SourceBranch string `json:"source_branch"`
		WebURL       string `json:"web_url"`
	}
	if err := json.Unmarshal(mrJSON, &mrMeta); err != nil {
		return nil, fmt.Errorf("parsing glab output: %w", err)
	}

	info := &PRInfo{
		Title:      mrMeta.Title,
		Body:       mrMeta.Description,
		BaseBranch: mrMeta.TargetBranch,
		HeadBranch: mrMeta.SourceBranch,
		Provider:   "gitlab",
	}

	// Fetch diffs.
	diffsJSON, err := exec.CommandContext(ctx, "glab", "api",
		fmt.Sprintf("projects/%s/merge_requests/%d/diffs", strings.ReplaceAll(projectPath, "/", "%%2F"), mrIID)).Output()
	if err != nil {
		return nil, fmt.Errorf("glab api diffs: %w", err)
	}

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

	encodedProject := strings.ReplaceAll(projectPath, "/", "%%2F")

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

		if fc.Type == ChangeAdded || fc.Type == ChangeModified || fc.Type == ChangeRenamed {
			content, err := exec.CommandContext(ctx, "glab", "api",
				fmt.Sprintf("projects/%s/repository/files/%s/raw?ref=%s",
					encodedProject,
					strings.ReplaceAll(fc.Path, "/", "%%2F"),
					info.HeadBranch)).Output()
			if err != nil {
				logger.Warn("failed to fetch file content", "file", fc.Path, "error", err)
				continue
			}
			fc.NewContent = content
		}

		if fc.Type == ChangeModified {
			content, err := exec.CommandContext(ctx, "glab", "api",
				fmt.Sprintf("projects/%s/repository/files/%s/raw?ref=%s",
					encodedProject,
					strings.ReplaceAll(fc.Path, "/", "%%2F"),
					info.BaseBranch)).Output()
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
