package action

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"slices"
	"testing"

	"github.com/rickliujh/loom/pkg/config"
	"github.com/rickliujh/loom/pkg/event"
)

// Opening a PR records it with the breadcrumb of the module that opened it —
// which tells bulk items apart where the shared module name cannot — and
// emits a pr.created event. The GitLab API is a local test server.
func TestPRAction_RecordsPathAndEmitsEvent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.EscapedPath() != "/api/v4/projects/group%2Fproject/merge_requests" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		io.WriteString(w, `{"iid": 1, "web_url": "`+"http://"+r.Host+`/group/project/-/merge_requests/1"}`)
	}))
	defer srv.Close()
	t.Setenv("LOOM_TEST_GITLAB_TOKEN", "glpat-test")

	target := t.TempDir()
	for _, args := range [][]string{
		{"init", "-q", "-b", "loom/feature"},
		{"remote", "add", "origin", srv.URL + "/group/project.git"},
		{"-c", "user.name=T", "-c", "user.email=t@t", "commit", "-q", "--allow-empty", "-m", "init"},
	} {
		if out, err := exec.Command("git", append([]string{"-C", target}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	var events []event.Event
	summary := &RunSummary{}
	execCtx := &ExecutionContext{
		ModuleName: "onboard",
		ModulePath: []string{"bulk", "item-a"},
		TargetDir:  target,
		Params:     map[string]any{},
		Summary:    summary,
		Events:     func(e event.Event) { events = append(events, e) },
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	act := &PRAction{Config: config.PR{Provider: "gitlab", TokenEnv: "LOOM_TEST_GITLAB_TOKEN", Title: "Onboard a"}}
	if err := act.Execute(context.Background(), execCtx); err != nil {
		t.Fatal(err)
	}

	url := srv.URL + "/group/project/-/merge_requests/1"
	want := PRResult{Path: []string{"bulk", "item-a"}, Module: "onboard", Title: "Onboard a", URL: url}
	if len(summary.PRs) != 1 || !slices.Equal(summary.PRs[0].Path, want.Path) || summary.PRs[0].URL != url ||
		summary.PRs[0].Module != want.Module || summary.PRs[0].Title != want.Title {
		t.Errorf("summary = %+v, want %+v", summary.PRs, want)
	}
	if len(events) != 1 {
		t.Fatalf("events = %+v, want one pr.created", events)
	}
	e := events[0]
	if e.Type != event.PRCreated || !slices.Equal(e.Path, want.Path) || e.PR == nil ||
		*e.PR != (event.PR{Module: "onboard", Title: "Onboard a", URL: url}) || e.Time.IsZero() {
		t.Errorf("event = %+v (pr %+v)", e, e.PR)
	}
}
