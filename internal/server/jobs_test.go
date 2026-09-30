//go:build unix

package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/rickliujh/loom/pkg/engine"
	"github.com/rickliujh/loom/pkg/event"
)

// onboardModule writes a module that renders one file into a clone of
// targetURL on a feature branch and commits it.
func onboardModule(t *testing.T, dir, targetURL, extraOps string) string {
	t.Helper()
	writeFile(t, filepath.Join(dir, "templates", "apps", "app.yaml"), "name: {{ .serviceName }}\nreplicas: {{ .replicas }}\n")
	return writeModule(t, dir, "onboard", fmt.Sprintf(`  params:
    - name: serviceName
      required: true
    - name: replicas
      default: "2"
  target:
    url: %s
    branch: main
    featureBranch: onboard-{{ .serviceName }}
  operations:
    - name: render
      newFiles:
        source: templates
    - name: commit
      commitPush:
        message: onboard {{ .serviceName }}
        author: Loom Test
        email: loom@test
%s`, targetURL, extraOps))
}

// shellModule writes a module with no target whose operations are the given
// pure shell commands, in order.
func shellModule(t *testing.T, dir, name string, commands ...string) string {
	t.Helper()
	var ops strings.Builder
	for i, c := range commands {
		fmt.Fprintf(&ops, "    - name: step%d\n      shell:\n        pure: true\n        command: %q\n", i+1, c)
	}
	return writeModule(t, dir, name, "  operations:\n"+ops.String())
}

// SV11: a local run job clones, renders and commits through engine.Run, and
// leaves exactly what the same run through the engine directly — `loom run
// --local-run` — leaves.
func TestServe_SV11_LocalRunJob(t *testing.T) {
	e := newEnv(t)
	url, _ := initBareRepo(t)
	mod := onboardModule(t, filepath.Join(e.root, "onboard"), url, "")

	id := e.submit(map[string]any{"kind": "run", "source": mod, "mode": "local", "params": map[string]any{"serviceName": "payments"}})
	e.waitState(id, StateSucceeded, 30*time.Second)
	rec := e.job(id)

	ws := rec.Result.Workspace
	if ws != filepath.Join(e.srv.workspaces, id) || rec.Module.Name != "onboard" {
		t.Fatalf("record = %+v", rec)
	}
	clone := filepath.Join(ws, "00-onboard")
	if log := git(t, clone, "log", "--format=%s", "-n", "1"); strings.TrimSpace(log) != "onboard payments" {
		t.Errorf("the workspace clone's last commit is %q", log)
	}
	if b := git(t, clone, "rev-parse", "--abbrev-ref", "HEAD"); strings.TrimSpace(b) != "onboard-payments" {
		t.Errorf("clone is on %q", b)
	}

	d := e.diff(id)
	if d.Mode != "full" || d.Incomplete || len(d.Targets) != 1 || len(d.Targets[0].Files) != 1 {
		t.Fatalf("diff = %+v", d)
	}
	f := d.Targets[0].Files[0]
	if f.Path != "apps/app.yaml" || f.Status != "added" || !strings.Contains(f.Unified, "+name: payments") {
		t.Errorf("file = %+v", f)
	}
	if rec.Result.Diff == nil || rec.Result.Diff.Files != 1 || rec.Result.Diff.Targets != 1 {
		t.Errorf("diff summary = %+v", rec.Result.Diff)
	}
	if !strings.Contains(rec.CLI, "--local-run --target-path "+ws) {
		t.Errorf("cli = %q", rec.CLI)
	}

	text := e.jobLog(id)
	if !strings.Contains(text, "run complete") || strings.Contains(text, "\x1b[") {
		t.Errorf("log.txt = %q", text)
	}

	evs := e.allEvents(id)
	for _, typ := range []string{"op.start", "op.end", "module.start", "module.end", "log", "diff.file"} {
		if len(ofType(t, evs, typ)) == 0 {
			t.Errorf("no %s event", typ)
		}
	}

	// The same run straight through the engine.
	direct := realTempDir(t)
	if _, err := engine.Run(context.Background(), engine.RunRequest{
		ModuleDir: mod, Params: map[string]any{"serviceName": "payments"}, LocalRun: true, TargetPath: direct,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}); err != nil {
		t.Fatal(err)
	}
	want, err := engine.CollectTargetDiffs(context.Background(), direct, engine.CollectOptions{Files: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(want) != 1 || !slices.Equal(fileSummary(want[0].Files), fileSummary(d.Targets[0].Files)) {
		t.Errorf("job and CLI differ:\njob %v\ncli %v", fileSummary(d.Targets[0].Files), fileSummary(want[0].Files))
	}
}

func fileSummary(files []engine.FileDiff) []string {
	var out []string
	for _, f := range files {
		out = append(out, f.Path+" "+f.Status+"\n"+f.Unified)
	}
	return out
}

// SV19: an execute run pushes to the target and every PR it opens is in its
// result, with its breadcrumb. `gh` is a stand-in script on PATH, so the PR
// step stays offline.
func TestServe_SV19_ExecuteRunPushesAndReportsPRs(t *testing.T) {
	bin := realTempDir(t)
	writeFile(t, filepath.Join(bin, "gh"), "#!/bin/sh\necho https://github.com/o/r/pull/7\n")
	if err := os.Chmod(filepath.Join(bin, "gh"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("LOOM_TEST_NO_TOKEN", "")

	e := newEnv(t)
	url, bare := initBareRepo(t)
	mod := onboardModule(t, filepath.Join(e.root, "onboard"), url, `    - name: pr
      pr:
        provider: github
        title: Onboard {{ .serviceName }}
        tokenEnv: LOOM_TEST_NO_TOKEN
`)
	id := e.submit(map[string]any{"kind": "run", "source": mod, "mode": "execute", "params": map[string]any{"serviceName": "billing"}})
	e.waitState(id, StateSucceeded, 30*time.Second)

	if out := git(t, bare, "log", "--format=%s", "-n", "1", "onboard-billing"); strings.TrimSpace(out) != "onboard billing" {
		t.Errorf("pushed branch head = %q", out)
	}
	rec := e.job(id)
	want := []prOut{{Path: []string{"onboard"}, Module: "onboard", Title: "Onboard billing", URL: "https://github.com/o/r/pull/7"}}
	if len(rec.Result.PRs) != 1 || !slices.Equal(rec.Result.PRs[0].Path, want[0].Path) || rec.Result.PRs[0].URL != want[0].URL || rec.Result.PRs[0].Title != want[0].Title {
		t.Errorf("prs = %+v", rec.Result.PRs)
	}
	prs := ofType(t, e.allEvents(id), "pr.created")
	if len(prs) != 1 || prs[0].PR == nil || prs[0].PR.URL != want[0].URL || !slices.Equal(prs[0].Path, []string{"onboard"}) {
		t.Errorf("pr.created events = %+v", prs)
	}
	// An execute run has no diff to show.
	if c := e.errorCode(http.MethodGet, "/api/v1/jobs/"+id+"/diff", nil, http.StatusNotFound); c != codeNotFound {
		t.Errorf("diff of an execute run: %s", c)
	}
}

// SV18: quick and full diffs come back as structured per-file entries under
// their breadcrumb and target, and a failed diff keeps what it collected,
// flagged incomplete.
func TestServe_SV18_DiffJobs(t *testing.T) {
	e := newEnv(t)
	url, _ := initBareRepo(t)
	mod := onboardModule(t, filepath.Join(e.root, "onboard"), url, "")
	params := map[string]any{"serviceName": "search"}

	quick := e.submit(map[string]any{"kind": "diff", "source": mod, "quick": true, "params": params})
	e.waitState(quick, StateSucceeded, 30*time.Second)
	qd := e.diff(quick)
	if qd.Mode != "quick" || qd.Incomplete || len(qd.Targets) != 1 || len(qd.Targets[0].Files) != 1 {
		t.Fatalf("quick diff = %+v", qd)
	}
	if f := qd.Targets[0].Files[0]; f.Status != "added" || !strings.Contains(f.Unified, "+name: search") || !slices.Equal(qd.Targets[0].Path, []string{"onboard"}) || qd.Targets[0].Label == "" {
		t.Errorf("quick target = %+v", qd.Targets[0])
	}

	full := e.submit(map[string]any{"kind": "diff", "source": mod, "params": params})
	e.waitState(full, StateSucceeded, 30*time.Second)
	fd := e.diff(full)
	if fd.Mode != "full" || fd.Incomplete || len(fd.Targets) != 1 || fd.Targets[0].Files[0].Path != "apps/app.yaml" || fd.Targets[0].Repo != url || fd.Targets[0].Branch != "main" {
		t.Fatalf("full diff = %+v", fd)
	}
	if ws := filepath.Join(e.srv.workspaces, full); fileExists(ws) {
		t.Error("a full diff without keepWorkspace kept its workspace")
	}
	if n := len(ofType(t, e.allEvents(full), "diff.file")); n != 1 {
		t.Errorf("%d diff.file events", n)
	}

	kept := e.submit(map[string]any{"kind": "diff", "source": mod, "params": params, "keepWorkspace": true})
	e.waitState(kept, StateSucceeded, 30*time.Second)
	if rec := e.job(kept); rec.Result.Workspace == "" || !fileExists(filepath.Join(rec.Result.Workspace, "00-onboard")) {
		t.Errorf("keepWorkspace: workspace %q", rec.Result.Workspace)
	}

	// A failing step after the render: the rendered file is still shown.
	failing := onboardModule(t, filepath.Join(e.root, "failing"), url, `    - name: boom
      shell:
        pure: true
        command: exit 3
`)
	bad := e.submit(map[string]any{"kind": "diff", "source": failing, "params": params})
	e.waitState(bad, StateFailed, 30*time.Second)
	bd := e.diff(bad)
	if !bd.Incomplete || len(bd.Targets) != 1 || bd.Targets[0].Files[0].Path != "apps/app.yaml" {
		t.Errorf("failed diff = %+v", bd)
	}
	if rec := e.job(bad); !strings.Contains(rec.Error, "exit status 3") || rec.Result.Diff == nil || !rec.Result.Diff.Incomplete {
		t.Errorf("failed record = %+v", rec)
	}
}

// SV13: states only move forward, and terminal ones are final.
func TestServe_SV13_Lifecycle(t *testing.T) {
	e := newEnv(t)
	ok := writeModule(t, filepath.Join(e.root, "ok"), "ok", `  params:
    - name: x
  operations:
    - name: step
      shell:
        pure: true
        command: echo {{ .x }}
`)
	fail := shellModule(t, filepath.Join(e.root, "fail"), "fail", "echo nope; exit 4")

	id := e.submit(map[string]any{"kind": "run", "source": ok, "mode": "local"})
	e.waitState(id, StateSucceeded, 20*time.Second)
	if got := states(t, e.allEvents(id)); !slices.Equal(got, []string{StateQueued, StateRunning, StateSucceeded}) {
		t.Errorf("states = %v", got)
	}
	rec := e.job(id)
	if rec.StartedAt == nil || rec.EndedAt == nil || rec.Error != "" {
		t.Errorf("record = %+v", rec)
	}
	// Terminal is final: no cancel.
	if c := e.errorCode(http.MethodPost, "/api/v1/jobs/"+id+"/cancel", map[string]any{}, http.StatusConflict); c != codeConflict {
		t.Errorf("cancel a finished job: %s", c)
	}

	bad := e.submit(map[string]any{"kind": "run", "source": fail, "mode": "local"})
	e.waitState(bad, StateFailed, 20*time.Second)
	evs := e.allEvents(bad)
	last := ofType(t, evs, string(eventJobState))
	if got := states(t, evs); !slices.Equal(got, []string{StateQueued, StateRunning, StateFailed}) || !strings.Contains(last[len(last)-1].Error, "exit status 4") {
		t.Errorf("failed job: states %v, last %+v", got, last[len(last)-1])
	}

	// Rerun with overrides makes a new job from the same request.
	var rr struct {
		ID    string `json:"id"`
		State string `json:"state"`
	}
	e.call(http.MethodPost, "/api/v1/jobs/"+id+"/rerun", map[string]any{"overrides": map[string]any{"mode": "dry-run", "params": map[string]any{"x": "1"}}}, http.StatusAccepted, &rr)
	e.waitState(rr.ID, StateSucceeded, 20*time.Second)
	var req JobRequest
	if err := json.Unmarshal(e.job(rr.ID).Request, &req); err != nil {
		t.Fatal(err)
	}
	if rr.ID == id || req.Mode != ModeDryRun || req.Source != ok || req.Params["x"] != "1" {
		t.Errorf("rerun request = %+v", req)
	}
	if c := e.errorCode(http.MethodPost, "/api/v1/jobs/"+id+"/rerun", map[string]any{"overrides": map[string]any{"kind": "diff"}}, http.StatusBadRequest); c != codeInvalid {
		t.Errorf("rerun changing kind: %s", c)
	}
	e.call(http.MethodPost, "/api/v1/jobs/"+id+"/rerun", map[string]any{}, http.StatusAccepted, &rr)
	e.waitState(rr.ID, StateSucceeded, 20*time.Second)

	// Delete removes the job and its workspace.
	ws := e.job(id).Result.Workspace
	e.call(http.MethodDelete, "/api/v1/jobs/"+id, nil, http.StatusNoContent, nil)
	if c := e.errorCode(http.MethodGet, "/api/v1/jobs/"+id, nil, http.StatusNotFound); c != codeNotFound {
		t.Errorf("deleted job: %s", c)
	}
	if fileExists(ws) {
		t.Error("the deleted job's workspace remains")
	}

	// History lists newest first, filtered.
	var list struct {
		Jobs []jobRec `json:"jobs"`
	}
	e.call(http.MethodGet, "/api/v1/jobs?kind=run&state=failed", nil, http.StatusOK, &list)
	if len(list.Jobs) != 1 || list.Jobs[0].ID != bad {
		t.Errorf("filtered list = %+v", list.Jobs)
	}
	e.call(http.MethodGet, "/api/v1/jobs?module="+ok+"&limit=1", nil, http.StatusOK, &list)
	if len(list.Jobs) != 1 || list.Jobs[0].ID != rr.ID || string(list.Jobs[0].Request) != `{"mode":"local","source":"`+ok+`"}` {
		t.Errorf("module list = %+v", list.Jobs)
	}

	// Request errors.
	for _, body := range []map[string]any{
		{"kind": "nope"},
		{"kind": "run", "source": ok},
		{"kind": "run", "source": ok, "mode": "fast"},
		{"kind": "run", "source": "relative", "mode": "local"},
		{"kind": "generate", "refs": []string{}},
	} {
		if c := e.errorCode(http.MethodPost, "/api/v1/jobs", body, http.StatusBadRequest); c != codeInvalid {
			t.Errorf("POST %v: %s", body, c)
		}
	}
	if c := e.errorCode(http.MethodPost, "/api/v1/jobs/j_20200101T000000_abcd/cancel", map[string]any{}, http.StatusNotFound); c != codeNotFound {
		t.Errorf("cancel unknown: %s", c)
	}
}

// gate is a file a shell step waits for, so a test decides when the step
// may finish without sleeping.
type gate struct{ path string }

func newGate(t *testing.T) gate { return gate{filepath.Join(realTempDir(t), "open")} }

func (g gate) wait() string {
	return fmt.Sprintf("while [ ! -e '%s' ]; do sleep 0.02; done", g.path)
}

func (g gate) open(t *testing.T) {
	t.Helper()
	writeFile(t, g.path, "")
}

// SV15: executing jobs run strictly one after another at the default limit
// of one, in submission order, while a quick diff never waits behind them.
func TestServe_SV15_Scheduling(t *testing.T) {
	e := newEnv(t)
	order := filepath.Join(realTempDir(t), "order")
	gateA, gateB := newGate(t), newGate(t)
	a := shellModule(t, filepath.Join(e.root, "a"), "a", "echo A-start >> '"+order+"'; "+gateA.wait()+"; echo A-end >> '"+order+"'")
	b := shellModule(t, filepath.Join(e.root, "b"), "b", "echo B-start >> '"+order+"'; "+gateB.wait()+"; echo B-end >> '"+order+"'")

	ja := e.submit(map[string]any{"kind": "run", "source": a, "mode": "local"})
	jb := e.submit(map[string]any{"kind": "run", "source": b, "mode": "local"})
	e.openStream("/api/v1/jobs/"+ja+"/events", nil, 20*time.Second).until(func(ev sseEvent) bool { return ev.Type == "op.start" })
	if s := e.jobState(jb); s != StateQueued {
		t.Fatalf("second job is %s while the first runs", s)
	}

	// A quick diff finishes while both are still pending.
	quick := e.submit(map[string]any{"kind": "diff", "source": a, "quick": true})
	e.waitState(quick, StateSucceeded, 20*time.Second)
	if s := e.jobState(jb); s != StateQueued {
		t.Fatalf("second job is %s", s)
	}
	if s := e.jobState(ja); s != StateRunning {
		t.Fatalf("first job is %s", s)
	}

	gateA.open(t)
	e.waitState(ja, StateSucceeded, 20*time.Second)
	gateB.open(t)
	e.waitState(jb, StateSucceeded, 20*time.Second)
	got, _ := os.ReadFile(order)
	if string(got) != "A-start\nA-end\nB-start\nB-end\n" {
		t.Errorf("jobs overlapped:\n%s", got)
	}
	if ra, rb := e.job(ja), e.job(jb); rb.StartedAt.Before(*ra.EndedAt) {
		t.Errorf("second job started at %v, before the first ended at %v", rb.StartedAt, ra.EndedAt)
	}

	// Cancelling a queued job ends it without running it.
	gateC := newGate(t)
	c := shellModule(t, filepath.Join(e.root, "c"), "c", gateC.wait())
	jc := e.submit(map[string]any{"kind": "run", "source": c, "mode": "local"})
	jd := e.submit(map[string]any{"kind": "run", "source": b, "mode": "local"})
	e.call(http.MethodPost, "/api/v1/jobs/"+jd+"/cancel", map[string]any{}, http.StatusAccepted, nil)
	e.waitState(jd, StateCancelled, 5*time.Second)
	if got := states(t, e.allEvents(jd)); !slices.Equal(got, []string{StateQueued, StateCancelled}) {
		t.Errorf("cancelled queued job states = %v", got)
	}
	// A running job cannot be deleted.
	e.waitState(jc, StateRunning, 10*time.Second)
	if code := e.errorCode(http.MethodDelete, "/api/v1/jobs/"+jc, nil, http.StatusConflict); code != codeConflict {
		t.Errorf("delete running: %s", code)
	}
	gateC.open(t)
	e.waitState(jc, StateSucceeded, 20*time.Second)
}

// SV20: every local run and full diff gets a workspace of its own, and a
// target path one active job holds is refused to the next.
func TestServe_SV20_Workspaces(t *testing.T) {
	e := newEnv(t, func(c *Config) { c.MaxConcurrentJobs = 2 })
	g := newGate(t)
	mod := shellModule(t, filepath.Join(e.root, "m"), "m", g.wait())
	target := filepath.Join(e.root, "out")

	j1 := e.submit(map[string]any{"kind": "run", "source": mod, "mode": "local", "targetPath": target})
	if c := e.errorCode(http.MethodPost, "/api/v1/jobs", map[string]any{"kind": "run", "source": mod, "mode": "local", "targetPath": target}, http.StatusConflict); c != codeConflict {
		t.Errorf("same targetPath: %s", c)
	}
	if c := e.errorCode(http.MethodPost, "/api/v1/jobs", map[string]any{"kind": "run", "source": mod, "mode": "local", "targetPath": filepath.Join(target, "sub")}, http.StatusConflict); c != codeConflict {
		t.Errorf("nested targetPath: %s", c)
	}
	j2 := e.submit(map[string]any{"kind": "run", "source": mod, "mode": "local"})
	j3 := e.submit(map[string]any{"kind": "run", "source": mod, "mode": "local"})
	g.open(t)
	for _, id := range []string{j1, j2, j3} {
		e.waitState(id, StateSucceeded, 20*time.Second)
	}
	w2, w3 := e.job(j2).Result.Workspace, e.job(j3).Result.Workspace
	if w2 == w3 || !strings.HasPrefix(w2, e.srv.workspaces) || !fileExists(w2) || !fileExists(w3) {
		t.Errorf("workspaces %q and %q", w2, w3)
	}
	if e.job(j1).Result.Workspace != target {
		t.Errorf("targetPath job workspace %q", e.job(j1).Result.Workspace)
	}
	// Released once the holder is done.
	j4 := e.submit(map[string]any{"kind": "run", "source": mod, "mode": "local", "targetPath": target})
	e.waitState(j4, StateSucceeded, 20*time.Second)
}

// SV9: every path a job writes to is inside a root or the workspace.
func TestServe_SV9_OutputPaths(t *testing.T) {
	e := newEnv(t)
	mod := shellModule(t, filepath.Join(e.root, "m"), "m", "true")
	outside := realTempDir(t)
	if err := os.Symlink(outside, filepath.Join(e.root, "escape")); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		body map[string]any
		want int
	}{
		{map[string]any{"kind": "run", "source": mod, "mode": "local", "targetPath": outside}, http.StatusForbidden},
		{map[string]any{"kind": "run", "source": mod, "mode": "local", "targetPath": filepath.Join(e.root, "escape", "new")}, http.StatusForbidden},
		{map[string]any{"kind": "run", "source": mod, "mode": "local", "targetPath": filepath.Join(e.root, "..", filepath.Base(outside))}, http.StatusForbidden},
		{map[string]any{"kind": "run", "source": mod, "mode": "local", "targetPath": "out"}, http.StatusBadRequest},
		{map[string]any{"kind": "generate", "refs": []string{"github:o/r#1"}, "output": outside}, http.StatusForbidden},
		{map[string]any{"kind": "generate", "refs": []string{"github:o/r#1"}, "output": filepath.Join(e.root, "escape")}, http.StatusForbidden},
		{map[string]any{"kind": "generate", "refs": []string{"github:o/r#1"}}, http.StatusBadRequest},
		{map[string]any{"kind": "bulk", "module": mod, "output": filepath.Join(outside, "b")}, http.StatusForbidden},
	}
	for _, tc := range cases {
		resp := e.request(http.MethodPost, "/api/v1/jobs", tc.body)
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != tc.want {
			t.Errorf("POST %v: %d, want %d: %s", tc.body, resp.StatusCode, tc.want, body)
		}
	}
	if des, _ := os.ReadDir(outside); len(des) != 0 {
		t.Error("something was written outside the roots")
	}
	// Inside the workspace is allowed.
	id := e.submit(map[string]any{"kind": "run", "source": mod, "mode": "local", "targetPath": filepath.Join(e.srv.workspaces, "mine")})
	e.waitState(id, StateSucceeded, 20*time.Second)
}

// SV16: the stream replays from any point with no gap and no duplicate,
// continues live, pings while idle, and ends with an end event.
func TestServe_SV16_EventStream(t *testing.T) {
	e := newEnv(t)
	e.srv.pingEvery = 20 * time.Millisecond
	g := newGate(t)
	mod := shellModule(t, filepath.Join(e.root, "m"), "m", "echo one", g.wait(), "echo three")
	id := e.submit(map[string]any{"kind": "run", "source": mod, "mode": "local"})

	// Live: read up to the blocked step, see pings while it waits, release
	// it and read to the end.
	live := e.openStream("/api/v1/jobs/"+id+"/events", nil, 20*time.Second)
	head := live.until(func(ev sseEvent) bool {
		return ev.Type == "op.start" && ev.decoded(t).Index == 2
	})
	var pinged bool
	for !pinged {
		ev, ok := live.next()
		if !ok {
			t.Fatal("stream closed while the job waits")
		}
		if ev.Comment == "ping" {
			pinged = true
		} else if ev.Comment == "" {
			head = append(head, ev)
		}
	}
	g.open(t)
	tail, _ := live.rest()
	all := append(head, tail...)

	end := all[len(all)-1]
	all = all[:len(all)-1]
	if end.Type != "end" || end.Data != "{}" {
		t.Fatalf("last event = %+v, want end with data {}", end)
	}
	for i, ev := range all {
		if ev.seq(t) != int64(i+1) {
			t.Fatalf("event %d has id %s: gap or duplicate", i, ev.ID)
		}
		if je := ev.decoded(t); je.Seq != ev.seq(t) || string(je.Type) != ev.Type {
			t.Fatalf("event %s: data %s disagrees with its id/type", ev.ID, ev.Data)
		}
	}
	if end.seq(t) != int64(len(all)+1) {
		t.Errorf("end id %s, want %d", end.ID, len(all)+1)
	}
	if got := states(t, all); !slices.Equal(got, []string{StateQueued, StateRunning, StateSucceeded}) {
		t.Errorf("states = %v", got)
	}

	// Replay: from the start, from Last-Event-ID k, from ?after=k.
	full := e.allEvents(id)
	if len(full) != len(all)+1 {
		t.Fatalf("replay has %d events, live had %d", len(full), len(all)+1)
	}
	for _, k := range []int{1, len(all) / 2, len(all) - 1, len(all)} {
		for _, how := range []string{"header", "query"} {
			path := "/api/v1/jobs/" + id + "/events"
			var hdr map[string]string
			if how == "header" {
				hdr = map[string]string{"Last-Event-ID": strconv.Itoa(k)}
			} else {
				path += "?after=" + strconv.Itoa(k)
			}
			got, _ := e.openStream(path, hdr, 10*time.Second).rest()
			if len(got) != len(all)-k+1 {
				t.Fatalf("resume %s %d: %d events, want %d", how, k, len(got), len(all)-k+1)
			}
			if k < len(all) && got[0].seq(t) != int64(k+1) {
				t.Errorf("resume %s %d starts at %s", how, k, got[0].ID)
			}
			if got[len(got)-1].Type != "end" {
				t.Errorf("resume %s %d did not end", how, k)
			}
		}
	}
	if c := e.errorCode(http.MethodGet, "/api/v1/jobs/"+id+"/events?after=x", nil, http.StatusBadRequest); c != codeInvalid {
		t.Errorf("bad cursor: %s", c)
	}
}

// SV17: every event of the run carries the executor's breadcrumb: a child's
// operations and logs are under [parent child]. Only the root's setup logs,
// before it starts, may have an empty path.
func TestServe_SV17_Breadcrumbs(t *testing.T) {
	e := newEnv(t)
	parent := writeModule(t, filepath.Join(e.root, "parent"), "parent", `  modules:
    - name: kid-a
      source: ./kid
  operations:
    - name: own
      shell:
        pure: true
        command: echo parent
`)
	shellModule(t, filepath.Join(parent, "kid"), "kid", "echo child")
	id := e.submit(map[string]any{"kind": "run", "source": parent, "mode": "local"})
	e.waitState(id, StateSucceeded, 20*time.Second)

	var current [][]string // module paths currently open, innermost last
	seen := map[string]bool{}
	for _, ev := range e.allEvents(id) {
		if ev.Type == "end" || ev.Type == string(eventJobState) {
			continue
		}
		je := ev.decoded(t)
		key := strings.Join(je.Path, "/")
		switch je.Type {
		case event.ModuleStart:
			current = append(current, je.Path)
			seen[key] = true
			continue
		case event.ModuleEnd:
			current = current[:len(current)-1]
			continue
		}
		if len(current) == 0 {
			if je.Type != event.Log || len(je.Path) > 1 {
				t.Errorf("event before the root started: %s", ev.Data)
			}
			continue
		}
		if want := current[len(current)-1]; !slices.Equal(je.Path, want) && !(je.Type == event.Log && len(je.Path) == 0) {
			t.Errorf("%s under %v has path %v", je.Type, want, je.Path)
		}
	}
	if !seen["parent"] || !seen["parent/kid-a"] {
		t.Errorf("module paths seen: %v", seen)
	}
}

// SV14: cancelling a running job stops its whole process tree — a shell
// step's backgrounded grandchild included — and the job ends cancelled
// within three seconds. Isolation is process-global, so this runs in a child
// copy of the test binary that has it on.
func TestServe_SV14_CancelStopsProcessTree(t *testing.T) {
	if os.Getenv(isolatedEnv) != "1" {
		cmd := exec.Command(os.Args[0], "-test.run=^TestServe_SV14_CancelStopsProcessTree$", "-test.count=1", "-test.v")
		cmd.Env = append(os.Environ(), isolatedEnv+"=1")
		out, err := cmd.CombinedOutput()
		if err != nil || !strings.Contains(string(out), "--- PASS: TestServe_SV14_CancelStopsProcessTree") {
			t.Fatalf("isolated run failed: %v\n%s", err, out)
		}
		return
	}

	e := newEnv(t)
	pidFile := filepath.Join(realTempDir(t), "pid")
	marker := filepath.Join(realTempDir(t), "after")
	mod := shellModule(t, filepath.Join(e.root, "m"), "m", "sleep 60 & echo $! > '"+pidFile+"'; wait", "touch '"+marker+"'")
	id := e.submit(map[string]any{"kind": "run", "source": mod, "mode": "local"})
	e.openStream("/api/v1/jobs/"+id+"/events", nil, 20*time.Second).until(func(ev sseEvent) bool { return ev.Type == "op.start" })

	var pid int
	waitFor(t, 10*time.Second, "the step to report its pid", func() bool {
		b, err := os.ReadFile(pidFile)
		if err != nil || !strings.HasSuffix(string(b), "\n") {
			return false
		}
		pid, err = strconv.Atoi(strings.TrimSpace(string(b)))
		return err == nil
	})
	t.Cleanup(func() { _ = syscall.Kill(pid, syscall.SIGKILL) })

	start := time.Now()
	var out map[string]string
	e.call(http.MethodPost, "/api/v1/jobs/"+id+"/cancel", map[string]any{}, http.StatusAccepted, &out)
	if out["state"] != StateCancelling {
		t.Errorf("cancel returned %v", out)
	}
	e.waitState(id, StateCancelled, 3*time.Second)
	if d := time.Since(start); d > 3*time.Second {
		t.Errorf("cancelled after %v", d)
	}
	waitFor(t, 3*time.Second, "the grandchild to exit", func() bool { return pidGone(pid) })
	if fileExists(marker) {
		t.Error("a step after the cancelled one ran")
	}
	if got := states(t, e.allEvents(id)); !slices.Equal(got, []string{StateQueued, StateRunning, StateCancelling, StateCancelled}) {
		t.Errorf("states = %v", got)
	}
}

// pidGone reports whether pid has exited; a zombie awaiting its reaper counts.
func pidGone(pid int) bool {
	if err := syscall.Kill(pid, 0); errors.Is(err, syscall.ESRCH) {
		return true
	}
	stat, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return false
	}
	s := string(stat)
	i := strings.LastIndexByte(s, ')')
	return i >= 0 && i+2 < len(s) && s[i+2] == 'Z'
}

// Shutdown cancels running jobs, interrupts queued ones and ends streams.
func TestServe_ShutdownInterruptsJobs(t *testing.T) {
	e := newEnv(t)
	g := newGate(t)
	t.Cleanup(func() { g.open(t) })
	mod := shellModule(t, filepath.Join(e.root, "m"), "m", g.wait())
	running := e.submit(map[string]any{"kind": "run", "source": mod, "mode": "local"})
	queued := e.submit(map[string]any{"kind": "run", "source": mod, "mode": "local"})
	e.waitState(running, StateRunning, 10*time.Second)
	s := e.openStream("/api/v1/jobs/"+running+"/events", nil, 20*time.Second)

	e.srv.Shutdown()
	for _, id := range []string{running, queued} {
		j, _ := e.srv.jobs.get(id)
		if rec := e.srv.jobs.snapshot(j, false); rec.State != StateInterrupted {
			t.Errorf("job %s is %s after shutdown", id, rec.State)
		}
	}
	evs, _ := s.rest()
	if len(evs) == 0 || evs[len(evs)-1].Type != "end" {
		t.Error("the event stream did not end at shutdown")
	}
	if c := e.errorCode(http.MethodPost, "/api/v1/jobs", map[string]any{"kind": "run", "source": mod, "mode": "local"}, http.StatusConflict); c != codeConflict {
		t.Errorf("submit after shutdown: %s", c)
	}
}

// porcelain is `git status --porcelain` of dir, one entry per line.
func porcelain(t *testing.T, dir string) []string {
	t.Helper()
	return strings.Split(strings.TrimRight(git(t, dir, "status", "--porcelain", "--untracked-files=all"), "\n"), "\n")
}

// A local run into a user's own checkout reads back only the clones it
// made: the checkout's uncommitted work is never staged.
func TestServe_SV11_LocalRunLeavesTargetPathIndexAlone(t *testing.T) {
	e := newEnv(t)
	url, _ := initBareRepo(t)
	checkout := filepath.Join(e.root, "checkout")
	if err := os.MkdirAll(checkout, 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, checkout, "init", "-q", "-b", "main")
	git(t, checkout, "config", "user.email", "t@t")
	git(t, checkout, "config", "user.name", "T")
	writeFile(t, filepath.Join(checkout, "tracked.txt"), "one\n")
	git(t, checkout, "add", ".")
	git(t, checkout, "commit", "-q", "-m", "init")
	writeFile(t, filepath.Join(checkout, "tracked.txt"), "one\ntwo\n")
	writeFile(t, filepath.Join(checkout, "notes.txt"), "draft\n")
	before := porcelain(t, checkout)
	if !slices.Equal(before, []string{" M tracked.txt", "?? notes.txt"}) {
		t.Fatalf("fixture status = %q", before)
	}

	// A module with a target clones into the checkout; only the clone is read.
	mod := onboardModule(t, filepath.Join(e.root, "onboard"), url, "")
	id := e.submit(map[string]any{"kind": "run", "source": mod, "mode": "local", "targetPath": checkout, "params": map[string]any{"serviceName": "a"}})
	e.waitState(id, StateSucceeded, 30*time.Second)
	d := e.diff(id)
	if len(d.Targets) != 1 || d.Targets[0].Files[0].Path != "apps/app.yaml" || d.Note != "" {
		t.Errorf("diff = %+v", d)
	}
	after := porcelain(t, checkout)
	for _, line := range after {
		if line != "" && line[0] != ' ' && line[0] != '?' {
			t.Errorf("the run staged %q in the user's checkout", line)
		}
	}
	if !slices.Contains(after, " M tracked.txt") || !slices.Contains(after, "?? notes.txt") {
		t.Errorf("checkout status after = %q", after)
	}

	// A module without a target runs in the checkout itself: no diff, a note
	// saying why, and still nothing staged.
	shell := shellModule(t, filepath.Join(e.root, "sh"), "sh", "echo more >> tracked.txt")
	id = e.submit(map[string]any{"kind": "run", "source": shell, "mode": "local", "targetPath": checkout})
	e.waitState(id, StateSucceeded, 30*time.Second)
	d = e.diff(id)
	if len(d.Targets) != 0 || d.Note == "" || e.job(id).Result.Diff.Note == "" {
		t.Errorf("diff = %+v", d)
	}
	for _, line := range porcelain(t, checkout) {
		if line != "" && line[0] != ' ' && line[0] != '?' {
			t.Errorf("the run staged %q in the user's checkout", line)
		}
	}
}

// Bulk items that share a module keep their own breadcrumbs in a local
// run's diff, as they do in a diff job.
func TestServe_SV18_LocalRunDiffBreadcrumbs(t *testing.T) {
	e := newEnv(t)
	url, _ := initBareRepo(t)
	child := filepath.Join(e.root, "deploy")
	writeFile(t, filepath.Join(child, "templates", "{{ .svc }}.yaml"), "name: {{ .svc }}\n")
	writeModule(t, child, "deploy", fmt.Sprintf(`  params:
    - name: svc
      required: true
  target:
    url: %s
    branch: main
  operations:
    - name: render
      newFiles:
        source: templates
`, url))
	wrapper := writeModule(t, filepath.Join(e.root, "wrapper"), "wrapper", `  modules:
    - name: svc-a
      source: ../deploy
      params: {svc: a}
    - name: svc-b
      source: ../deploy
      params: {svc: b}
`)
	id := e.submit(map[string]any{"kind": "run", "source": wrapper, "mode": "local"})
	e.waitState(id, StateSucceeded, 30*time.Second)
	d := e.diff(id)
	if len(d.Targets) != 2 {
		t.Fatalf("diff = %+v", d)
	}
	if !slices.Equal(d.Targets[0].Path, []string{"wrapper", "svc-a"}) || !slices.Equal(d.Targets[1].Path, []string{"wrapper", "svc-b"}) {
		t.Errorf("breadcrumbs = %v, %v", d.Targets[0].Path, d.Targets[1].Path)
	}
	if d.Targets[0].Files[0].Path != "a.yaml" || d.Targets[1].Files[0].Path != "b.yaml" {
		t.Errorf("files = %+v, %+v", d.Targets[0].Files, d.Targets[1].Files)
	}
}

// Only a local run creates a missing targetPath: a dry run leaves nothing
// on disk, not even the directory.
func TestServe_SV9_DryRunCreatesNoTargetPath(t *testing.T) {
	e := newEnv(t)
	mod := shellModule(t, filepath.Join(e.root, "m"), "m", "touch made")
	missing := filepath.Join(e.root, "not", "yet")
	id := e.submit(map[string]any{"kind": "run", "source": mod, "mode": "dry-run", "targetPath": missing})
	e.waitState(id, StateSucceeded, 20*time.Second)
	if fileExists(filepath.Join(e.root, "not")) {
		t.Error("a dry run created its target path")
	}
	id = e.submit(map[string]any{"kind": "run", "source": mod, "mode": "local", "targetPath": missing})
	e.waitState(id, StateSucceeded, 20*time.Second)
	if !fileExists(filepath.Join(missing, "made")) {
		t.Error("a local run did not create its target path")
	}
}

// A generate job holds its output from submission: a second job for the
// same output is refused while the first waits in the queue, and an output
// that fills up while the job waits fails the job instead of being
// overwritten.
func TestServe_SV20_GenerateOutputHeldWhileQueued(t *testing.T) {
	e := newEnv(t)
	e.srv.generateProvider = fakePR{}
	g := newGate(t)
	t.Cleanup(func() { g.open(t) })
	blocker := shellModule(t, filepath.Join(e.root, "block"), "block", g.wait())
	out := filepath.Join(e.root, "modules", "gen")

	busy := e.submit(map[string]any{"kind": "run", "source": blocker, "mode": "local"})
	first := e.submit(map[string]any{"kind": "generate", "refs": []string{"github:o/r#1"}, "output": out})
	if s := e.jobState(first); s != StateQueued {
		t.Fatalf("first generate is %s", s)
	}
	if c := e.errorCode(http.MethodPost, "/api/v1/jobs", map[string]any{"kind": "generate", "refs": []string{"github:o/r#2"}, "output": out}, http.StatusConflict); c != codeConflict {
		t.Errorf("second generate for the same output: %s", c)
	}

	// Something outside the server writes the output while the job waits.
	writeFile(t, filepath.Join(out, "mine.txt"), "keep\n")
	g.open(t)
	e.waitState(busy, StateSucceeded, 20*time.Second)
	e.waitState(first, StateFailed, 20*time.Second)
	if rec := e.job(first); !strings.Contains(rec.Error, "is not empty") {
		t.Errorf("error = %q", rec.Error)
	}
	if des, _ := os.ReadDir(out); len(des) != 1 {
		t.Errorf("the output was written into: %v", des)
	}
}
