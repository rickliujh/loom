package generate

import "testing"

// Regression: the glab CLI fallback used to build ChangeSet without RepoURL,
// leaving the generated module's target.url empty. Both fetch paths must
// derive the same clone URL.
func TestGitlabRepoURL(t *testing.T) {
	tests := []struct {
		baseURL     string
		projectPath string
		want        string
	}{
		{"https://gitlab.com", "mygroup/myrepo", "https://gitlab.com/mygroup/myrepo.git"},
		{"https://gitlab.example.com", "group/sub/repo", "https://gitlab.example.com/group/sub/repo.git"},
	}
	for _, tt := range tests {
		if got := gitlabRepoURL(tt.baseURL, tt.projectPath); got != tt.want {
			t.Errorf("gitlabRepoURL(%q, %q) = %q, want %q", tt.baseURL, tt.projectPath, got, tt.want)
		}
	}
}
