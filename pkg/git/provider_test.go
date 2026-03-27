package git

import (
	"log/slog"
	"os"
	"testing"
)

// ===========================================================================
// parseGitHubURL
// ===========================================================================

func TestParseGitHubURL(t *testing.T) {
	tests := []struct {
		name      string
		url       string
		wantOwner string
		wantRepo  string
		wantErr   bool
	}{
		{
			name:      "https URL",
			url:       "https://github.com/loomOrg/loom",
			wantOwner: "loomOrg",
			wantRepo:  "loom",
		},
		{
			name:      "https URL with .git suffix",
			url:       "https://github.com/loomOrg/loom.git",
			wantOwner: "loomOrg",
			wantRepo:  "loom",
		},
		{
			name:    "SSH URL not supported",
			url:     "git@github.com:loomOrg/loom",
			wantErr: true,
		},
		{
			name:    "SSH URL with .git suffix not supported",
			url:     "git@github.com:loomOrg/loom.git",
			wantErr: true,
		},
		{
			name:    "non-github URL",
			url:     "https://gitlab.com/org/repo",
			wantErr: true,
		},
		{
			name:    "empty string",
			url:     "",
			wantErr: true,
		},
		{
			name:    "github.com with no path",
			url:     "https://github.com/",
			wantErr: true,
		},
		{
			name:    "github.com with only owner",
			url:     "https://github.com/owner",
			wantErr: true,
		},
		{
			name:      "http URL (not https)",
			url:       "http://github.com/loomOrg/loom",
			wantOwner: "loomOrg",
			wantRepo:  "loom",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			owner, repo, err := parseGitHubURL(tt.url)
			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error, got owner=%q repo=%q", owner, repo)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if owner != tt.wantOwner {
				t.Errorf("owner = %q, want %q", owner, tt.wantOwner)
			}
			if repo != tt.wantRepo {
				t.Errorf("repo = %q, want %q", repo, tt.wantRepo)
			}
		})
	}
}

// ===========================================================================
// parseGitLabURL
// ===========================================================================

func TestParseGitLabURL(t *testing.T) {
	tests := []struct {
		name        string
		url         string
		wantBaseURL string
		wantPath    string
		wantErr     bool
	}{
		{
			name:        "HTTPS URL",
			url:         "https://gitlab.com/org/repo",
			wantBaseURL: "https://gitlab.com",
			wantPath:    "org/repo",
		},
		{
			name:        "HTTPS URL with .git suffix",
			url:         "https://gitlab.com/org/repo.git",
			wantBaseURL: "https://gitlab.com",
			wantPath:    "org/repo",
		},
		{
			name:        "HTTPS with subgroups",
			url:         "https://gitlab.com/org/subgroup/repo",
			wantBaseURL: "https://gitlab.com",
			wantPath:    "org/subgroup/repo",
		},
		{
			name:        "HTTP URL",
			url:         "http://gitlab.example.com/team/project",
			wantBaseURL: "http://gitlab.example.com",
			wantPath:    "team/project",
		},
		{
			name:        "SSH URL",
			url:         "git@gitlab.com:org/repo",
			wantBaseURL: "https://gitlab.com",
			wantPath:    "org/repo",
		},
		{
			name:        "SSH URL with .git suffix",
			url:         "git@gitlab.com:org/repo.git",
			wantBaseURL: "https://gitlab.com",
			wantPath:    "org/repo",
		},
		{
			name:        "SSH URL with subgroups",
			url:         "git@gitlab.example.io:org/team/repo",
			wantBaseURL: "https://gitlab.example.io",
			wantPath:    "org/team/repo",
		},
		{
			name:        "self-hosted HTTPS",
			url:         "https://git.internal.co/devops/infra",
			wantBaseURL: "https://git.internal.co",
			wantPath:    "devops/infra",
		},
		{
			name:    "SSH URL missing colon",
			url:     "git@gitlab.com/org/repo",
			wantErr: true,
		},
		{
			name:    "SSH URL empty project path",
			url:     "git@gitlab.com:",
			wantErr: true,
		},
		{
			name:    "HTTPS URL no project path",
			url:     "https://gitlab.com",
			wantErr: true,
		},
		{
			name:    "HTTPS URL trailing slash only",
			url:     "https://gitlab.com/",
			wantErr: true,
		},
		{
			name:    "empty string",
			url:     "",
			wantErr: true,
		},
		{
			name:    "unsupported scheme",
			url:     "ftp://gitlab.com/org/repo",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			baseURL, path, err := parseGitLabURL(tt.url)
			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error, got baseURL=%q path=%q", baseURL, path)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if baseURL != tt.wantBaseURL {
				t.Errorf("baseURL = %q, want %q", baseURL, tt.wantBaseURL)
			}
			if path != tt.wantPath {
				t.Errorf("path = %q, want %q", path, tt.wantPath)
			}
		})
	}
}

// ===========================================================================
// parseMRURL
// ===========================================================================

func TestParseMRURL(t *testing.T) {
	tests := []struct {
		name   string
		output string
		want   string
	}{
		{
			name:   "JSON output with web_url",
			output: `{"web_url": "https://gitlab.com/org/repo/-/merge_requests/42"}`,
			want:   "https://gitlab.com/org/repo/-/merge_requests/42",
		},
		{
			name:   "plain HTTPS URL",
			output: "https://gitlab.com/org/repo/-/merge_requests/42\n",
			want:   "https://gitlab.com/org/repo/-/merge_requests/42",
		},
		{
			name:   "URL among other text",
			output: "Creating merge request...\nhttps://gitlab.com/org/repo/-/merge_requests/42\nDone.",
			want:   "https://gitlab.com/org/repo/-/merge_requests/42",
		},
		{
			name:   "HTTP URL (not HTTPS)",
			output: "http://gitlab.local/org/repo/-/merge_requests/1\n",
			want:   "http://gitlab.local/org/repo/-/merge_requests/1",
		},
		{
			name:   "no URL — returns trimmed output",
			output: "  some random output  \n",
			want:   "some random output",
		},
		{
			name:   "empty output",
			output: "",
			want:   "",
		},
		{
			name:   "empty JSON",
			output: `{}`,
			want:   "{}",
		},
		{
			name:   "JSON with empty web_url falls back to line scan",
			output: `{"web_url": ""}` + "\nhttps://gitlab.com/org/repo/-/merge_requests/1",
			want:   "https://gitlab.com/org/repo/-/merge_requests/1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseMRURL(tt.output)
			if got != tt.want {
				t.Errorf("parseMRURL() = %q, want %q", got, tt.want)
			}
		})
	}
}

// ===========================================================================
// NewProvider
// ===========================================================================

func TestNewProvider_UnsupportedProvider(t *testing.T) {
	logger := testLogger()
	_, err := NewProvider("bitbucket", "BITBUCKET_TOKEN", logger)
	if err == nil {
		t.Fatal("expected error for unsupported provider")
	}
	if got := err.Error(); got != `unsupported PR provider "bitbucket"` {
		t.Errorf("unexpected error: %s", got)
	}
}

func TestNewProvider_GitHubWithToken(t *testing.T) {
	logger := testLogger()
	t.Setenv("TEST_GH_TOKEN_PROVIDER", "ghp_faketoken123")

	p, err := NewProvider("github", "TEST_GH_TOKEN_PROVIDER", logger)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := p.(*GitHubProvider); !ok {
		t.Errorf("expected *GitHubProvider, got %T", p)
	}
}

func TestNewProvider_GitLabWithToken(t *testing.T) {
	logger := testLogger()
	t.Setenv("TEST_GL_TOKEN_PROVIDER", "glpat-faketoken123")

	p, err := NewProvider("gitlab", "TEST_GL_TOKEN_PROVIDER", logger)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := p.(*GitLabProvider); !ok {
		t.Errorf("expected *GitLabProvider, got %T", p)
	}
}

func TestNewProvider_GitHubNoToken_WithCLI(t *testing.T) {
	logger := testLogger()
	// Ensure the env var is NOT set.
	t.Setenv("TEST_GH_TOKEN_NONE", "")
	os.Unsetenv("TEST_GH_TOKEN_NONE")

	p, err := NewProvider("github", "TEST_GH_TOKEN_NONE", logger)
	if err != nil {
		// gh CLI might not be available in CI — that's okay, just check error message.
		if !hasBinary("gh") {
			if got := err.Error(); got != `environment variable "TEST_GH_TOKEN_NONE" is not set and gh CLI is not available` {
				t.Errorf("unexpected error: %s", got)
			}
			return
		}
		t.Fatal(err)
	}
	if _, ok := p.(*ghCLIProvider); !ok {
		t.Errorf("expected *ghCLIProvider, got %T", p)
	}
}

func TestNewProvider_GitLabNoToken_WithCLI(t *testing.T) {
	logger := testLogger()
	t.Setenv("TEST_GL_TOKEN_NONE", "")
	os.Unsetenv("TEST_GL_TOKEN_NONE")

	p, err := NewProvider("gitlab", "TEST_GL_TOKEN_NONE", logger)
	if err != nil {
		if !hasBinary("glab") {
			if got := err.Error(); got != `environment variable "TEST_GL_TOKEN_NONE" is not set and glab CLI is not available` {
				t.Errorf("unexpected error: %s", got)
			}
			return
		}
		t.Fatal(err)
	}
	if _, ok := p.(*glabCLIProvider); !ok {
		t.Errorf("expected *glabCLIProvider, got %T", p)
	}
}

// ===========================================================================
// hasBinary
// ===========================================================================

func TestHasBinary(t *testing.T) {
	// "git" should always be available in the test environment.
	if !hasBinary("git") {
		t.Error("expected git to be available")
	}
	// A binary that doesn't exist.
	if hasBinary("nonexistent-binary-xyz-12345") {
		t.Error("expected nonexistent binary to not be found")
	}
}

// ===========================================================================
// PROptions — verify struct fields are correctly accessible
// ===========================================================================

func TestPROptions_Fields(t *testing.T) {
	opts := PROptions{
		RepoURL:    "https://github.com/org/repo",
		Title:      "test PR",
		Body:       "body text",
		HeadBranch: "feat/test",
		BaseBranch: "main",
		Labels:     []string{"bug", "priority"},
		WorkDir:    "/tmp/test",
	}

	if opts.RepoURL != "https://github.com/org/repo" {
		t.Error("RepoURL mismatch")
	}
	if len(opts.Labels) != 2 {
		t.Errorf("expected 2 labels, got %d", len(opts.Labels))
	}
}

// ===========================================================================
// GitHubProvider / GitLabProvider token storage
// ===========================================================================

func TestGitHubProvider_TokenStored(t *testing.T) {
	p := &GitHubProvider{
		Token:  "test-token",
		Logger: slog.Default(),
	}
	if p.Token != "test-token" {
		t.Errorf("token = %q, want %q", p.Token, "test-token")
	}
}

func TestGitLabProvider_TokenStored(t *testing.T) {
	p := &GitLabProvider{
		Token:  "glpat-test",
		Logger: slog.Default(),
	}
	if p.Token != "glpat-test" {
		t.Errorf("token = %q, want %q", p.Token, "glpat-test")
	}
}
