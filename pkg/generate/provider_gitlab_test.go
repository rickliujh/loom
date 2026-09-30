package generate

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// Regression: the glab CLI fallback used to build PRInfo without RepoURL,
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

// cancelOn is a slog handler that cancels a context when fetchAPI logs msg.
// fetchAPI logs between its API calls, so this cancels at an exact point in
// its sequence — from the calling goroutine, with no race against the HTTP
// client still reading the previous response.
type cancelOn struct {
	msg    string
	cancel context.CancelFunc
}

func (h cancelOn) Enabled(context.Context, slog.Level) bool { return true }
func (h cancelOn) WithAttrs([]slog.Attr) slog.Handler       { return h }
func (h cancelOn) WithGroup(string) slog.Handler            { return h }
func (h cancelOn) Handle(_ context.Context, r slog.Record) error {
	if r.Message == h.msg {
		h.cancel()
	}
	return nil
}

// Every GitLab API call in fetchAPI must carry the caller's context: once it
// is cancelled, no further request may reach the server.
func TestGitLabFetchAPI_HonoursContext(t *testing.T) {
	for _, tt := range []struct {
		name     string
		cancelOn string // log message after which the context is cancelled
		want     int32  // requests that may reach the server
	}{
		{"before MR metadata", "fetching MR metadata", 0},
		{"before diff list", "MR metadata fetched", 1},
		{"before head content", "MR diffs fetched", 2},
		{"before base content", "fetched file content", 3},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var hits atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				hits.Add(1)
				switch {
				case strings.HasSuffix(r.URL.Path, "/merge_requests/1"):
					fmt.Fprint(w, `{"iid":1,"title":"t","source_branch":"feature","target_branch":"main",`+
						`"diff_refs":{"base_sha":"base","head_sha":"head"}}`)
				case strings.HasSuffix(r.URL.Path, "/merge_requests/1/diffs"):
					fmt.Fprint(w, `[{"new_path":"a.txt","old_path":"a.txt"}]`)
				default:
					fmt.Fprint(w, "content")
				}
			}))
			defer srv.Close()

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			logger := slog.New(cancelOn{msg: tt.cancelOn, cancel: cancel})

			p := &GitLabDiffProvider{}
			_, _ = p.fetchAPI(ctx, srv.URL, "group/project", 1, "glpat-test", logger)
			if ctx.Err() == nil {
				t.Fatalf("fetchAPI never logged %q; the test no longer matches its log sequence", tt.cancelOn)
			}
			if got := hits.Load(); got != tt.want {
				t.Errorf("server received %d request(s), want %d: a call ignored the cancelled context", got, tt.want)
			}
		})
	}
}
