package server

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rickliujh/loom/pkg/config"
	"github.com/rickliujh/loom/pkg/generate"
)

// fakePR serves a fixed PR in place of GitHub.
type fakePR struct{}

func (fakePR) FetchDiff(context.Context, string, string, *slog.Logger) (*generate.PRInfo, error) {
	return &generate.PRInfo{
		Title:      "Onboard payments",
		BaseBranch: "main",
		RepoURL:    "https://github.com/o/r",
		Provider:   "github",
		Files: []generate.FileChange{
			{Type: generate.ChangeAdded, Path: "apps/payments/app.yaml", NewContent: []byte("name: payments\n")},
		},
	}, nil
}

func TestServe_GenerateJob(t *testing.T) {
	e := newEnv(t)
	e.srv.generateProvider = fakePR{}
	out := filepath.Join(e.root, "modules", "generated")

	id := e.submit(map[string]any{"kind": "generate", "refs": []string{"github:o/r#12"}, "values": map[string]string{"svc": "payments"}, "output": out})
	e.waitState(id, StateSucceeded, 20*time.Second)
	lf, err := config.Load(out)
	if err != nil {
		t.Fatalf("generated module does not load: %v", err)
	}
	if lf.Metadata.Name != "onboard-payments" || len(lf.Spec.Params) != 1 || lf.Spec.Params[0].Name != "svc" {
		t.Errorf("generated config = %+v", lf)
	}
	if b, err := os.ReadFile(filepath.Join(out, "apps", "{{ .svc }}", "app.yaml")); err != nil || !strings.Contains(string(b), "{{ .svc }}") {
		t.Errorf("template not parameterized: %q %v", b, err)
	}
	rec := e.job(id)
	if rec.Result.Output != out || !strings.HasPrefix(rec.CLI, "loom generate 'github:o/r#12' -p svc=payments -o "+out) || rec.Module.Name != "onboard-payments" {
		t.Errorf("record = %+v", rec)
	}

	// Non-empty output: refused unless overwrite.
	if c := e.errorCode(http.MethodPost, "/api/v1/jobs", map[string]any{"kind": "generate", "refs": []string{"github:o/r#12"}, "output": out}, http.StatusConflict); c != codeConflict {
		t.Errorf("non-empty output: %s", c)
	}
	again := e.submit(map[string]any{"kind": "generate", "refs": []string{"github:o/r#12"}, "output": out, "overwrite": true, "name": "renamed"})
	e.waitState(again, StateSucceeded, 20*time.Second)

	// One ref only in this version.
	resp := e.request(http.MethodPost, "/api/v1/jobs", map[string]any{"kind": "generate", "refs": []string{"github:o/r#1", "github:o/r#2"}, "output": filepath.Join(e.root, "x")})
	defer resp.Body.Close()
	var body struct {
		Error apiError `json:"error"`
	}
	decodeTestJSON(t, resp, &body)
	if resp.StatusCode != http.StatusBadRequest || body.Error.Code != codeInvalid || !strings.Contains(body.Error.Message, "multi-source generate is not available") {
		t.Errorf("two refs: %d %+v", resp.StatusCode, body.Error)
	}
}

func TestServe_BulkJob(t *testing.T) {
	e := newEnv(t)
	child := writeModule(t, filepath.Join(e.root, "modules", "onboard"), "onboard", `  params:
    - name: serviceName
      required: true
    - name: regions
      type: list
  operations: []
`)
	out := filepath.Join(e.root, "batches", "q3")
	id := e.submit(map[string]any{
		"kind": "bulk", "module": child, "nameParam": "serviceName", "output": out,
		"items": []map[string]any{
			{"serviceName": "a", "regions": "- eu\n- us\n"},
			{"serviceName": "b", "regions": []string{"ap"}},
		},
	})
	e.waitState(id, StateSucceeded, 20*time.Second)
	lf, err := config.Load(out)
	if err != nil {
		t.Fatalf("wrapper does not load: %v", err)
	}
	if lf.Metadata.Name != "bulk-onboard" || len(lf.Spec.Modules) != 2 || lf.Spec.Modules[0].Name != "onboard-a" {
		t.Fatalf("wrapper = %+v", lf.Spec.Modules)
	}
	// A list sent as YAML text is written as a list.
	if regions, ok := lf.Spec.Modules[0].Params["regions"].([]any); !ok || len(regions) != 2 {
		t.Errorf("item a regions = %#v", lf.Spec.Modules[0].Params["regions"])
	}
	rec := e.job(id)
	if !strings.Contains(rec.CLI, "--items items.yaml") {
		t.Errorf("cli = %q", rec.CLI)
	}

	// The wrapper runs: a dry run of the batch.
	run := e.submit(map[string]any{"kind": "run", "source": out, "mode": "dry-run"})
	e.waitState(run, StateSucceeded, 20*time.Second)

	// An undeclared param fails the job, not the request.
	bad := e.submit(map[string]any{"kind": "bulk", "module": child, "output": filepath.Join(e.root, "batches", "bad"), "items": []map[string]any{{"typo": "x"}}})
	e.waitState(bad, StateFailed, 20*time.Second)
	if rec := e.job(bad); !strings.Contains(rec.Error, `undeclared parameter "typo"`) {
		t.Errorf("error = %q", rec.Error)
	}
}

func TestServe_Presets(t *testing.T) {
	e := newEnv(t)
	mod := writeModule(t, filepath.Join(e.root, "m"), "m", structuredSpec)
	q := "?source=" + mod

	var list struct {
		Presets []presetEntry `json:"presets"`
	}
	e.call(http.MethodGet, "/api/v1/presets"+q, nil, http.StatusOK, &list)
	if len(list.Presets) != 0 {
		t.Fatalf("presets = %+v", list)
	}

	text := "# production\nserviceName: payments\nsources:\n  - repoURL: x\n    targetRevision: 1.10\n"
	e.call(http.MethodPut, "/api/v1/presets/prod"+q, map[string]string{"yaml": text}, http.StatusOK, nil)
	var got struct {
		Name   string         `json:"name"`
		Params map[string]any `json:"params"`
		YAML   string         `json:"yaml"`
	}
	e.call(http.MethodGet, "/api/v1/presets/prod"+q, nil, http.StatusOK, &got)
	if got.Name != "prod" || got.YAML != text || got.Params["serviceName"] != "payments" {
		t.Errorf("preset = %+v", got)
	}

	e.call(http.MethodPut, "/api/v1/presets/dev.eu-1"+q, map[string]any{"params": map[string]any{"serviceName": "dev", "sources": "- repoURL: y\n"}}, http.StatusOK, nil)
	e.call(http.MethodGet, "/api/v1/presets/dev.eu-1"+q, nil, http.StatusOK, &got)
	if got.Params["sources"] != "- repoURL: y\n" || !strings.Contains(got.YAML, "serviceName: dev") {
		t.Errorf("params preset = %+v", got)
	}
	// Presets live in the state directory, never in the module.
	if des, _ := os.ReadDir(mod); len(des) != 1 {
		t.Errorf("the module directory gained files: %v", des)
	}
	// Another spelling of the same module shares its presets.
	e.call(http.MethodGet, "/api/v1/presets?source=file://"+mod, nil, http.StatusOK, &list)
	if len(list.Presets) != 2 || list.Presets[0].Name != "dev.eu-1" || list.Presets[1].Name != "prod" {
		t.Errorf("presets = %+v", list)
	}

	for _, name := range []string{"..", "a%2Fb", "a b", "..%2F..%2Fx"} {
		resp := e.request(http.MethodPut, "/api/v1/presets/"+name+q, map[string]string{"yaml": "a: b\n"})
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest && resp.StatusCode != http.StatusNotFound {
			t.Errorf("preset name %q: %d", name, resp.StatusCode)
		}
	}
	if c := e.errorCode(http.MethodPut, "/api/v1/presets/x"+q, map[string]string{"yaml": "- not a mapping\n"}, http.StatusBadRequest); c != codeInvalid {
		t.Errorf("non-mapping yaml: %s", c)
	}
	e.call(http.MethodDelete, "/api/v1/presets/prod"+q, nil, http.StatusNoContent, nil)
	if c := e.errorCode(http.MethodGet, "/api/v1/presets/prod"+q, nil, http.StatusNotFound); c != codeNotFound {
		t.Errorf("deleted preset: %s", c)
	}
	if c := e.errorCode(http.MethodGet, "/api/v1/presets?source=relative", nil, http.StatusBadRequest); c != codeInvalid {
		t.Errorf("relative source: %s", c)
	}
}

func TestServe_CLIEndpoint(t *testing.T) {
	e := newEnv(t)
	var c CLI
	e.call(http.MethodPost, "/api/v1/cli", map[string]any{
		"kind": "run", "source": "/home/u/gitops/modules/onboard", "mode": "local",
		"params": map[string]any{"serviceName": "payments", "sources": []any{map[string]any{"repoURL": "x"}}},
	}, http.StatusOK, &c)
	want := "loom run /home/u/gitops/modules/onboard -p serviceName=payments --params-file params.yaml --local-run --target-path ./out"
	if c.Command != want || c.ParamsFile != "sources:\n  - repoURL: x\n" {
		t.Errorf("cli = %+v\nwant %s", c, want)
	}
	e.call(http.MethodPost, "/api/v1/cli", map[string]any{"kind": "diff", "source": "/m", "quick": true, "params": map[string]any{"name": "it's"}}, http.StatusOK, &c)
	if c.Command != `loom diff /m -p 'name=it'\''s' --quick --partial` {
		t.Errorf("diff cli = %q", c.Command)
	}
	if code := e.errorCode(http.MethodPost, "/api/v1/cli", map[string]any{"kind": "deploy"}, http.StatusBadRequest); code != codeInvalid {
		t.Errorf("unknown kind: %s", code)
	}
}

func decodeTestJSON(t *testing.T, resp *http.Response, out any) {
	t.Helper()
	if err := jsonDecode(resp, out); err != nil {
		t.Fatal(err)
	}
}
