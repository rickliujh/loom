//go:build unix

package server

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// reopen starts a second server on the first one's state directory.
func (e *testEnv) reopen(t *testing.T) *testEnv {
	t.Helper()
	e.srv.Shutdown()
	state := e.srv.cfg.StateDir
	root := e.root
	return newEnv(t, func(c *Config) {
		c.StateDir = state
		c.Roots = []string{root}
		c.History = HistoryDisk
	})
}

// SV22: with disk history a job — record, events, log and diff — survives a
// restart; one that was running when the server stopped comes back
// interrupted; and history is pruned to its limits.
func TestServe_SV22_HistorySurvivesRestart(t *testing.T) {
	e := newEnv(t, func(c *Config) { c.History = HistoryDisk })
	mod := shellModule(t, filepath.Join(e.root, "m"), "m", "echo hello-from-step")
	id := e.submit(map[string]any{"kind": "run", "source": mod, "mode": "local", "params": map[string]any{}})
	e.waitState(id, StateSucceeded, 20*time.Second)
	before := e.allEvents(id)

	dir := filepath.Join(e.srv.stateDir, "jobs", id)
	for _, f := range []string{"job.json", "events.jsonl", "log.txt"} {
		info, err := os.Stat(filepath.Join(dir, f))
		if err != nil || info.Mode().Perm() != 0o600 {
			t.Errorf("%s: %v %v", f, info, err)
		}
	}
	if info, err := os.Stat(dir); err != nil || info.Mode().Perm() != 0o700 {
		t.Errorf("job dir: %v %v", info, err)
	}

	// A job that was running when the server died.
	stale := "j_20260101T000000_beef"
	staleDir := filepath.Join(e.srv.stateDir, "jobs", stale)
	writeFile(t, filepath.Join(staleDir, "job.json"), fmt.Sprintf(`{"id":%q,"kind":"run","state":"running","request":{"kind":"run","source":%q,"mode":"local"},"createdAt":%q,"module":{"name":"m","dir":%q},"result":{"prs":[],"diff":null},"error":"","cli":""}`,
		stale, mod, time.Now().Add(-time.Hour).Format(time.RFC3339Nano), mod))
	writeFile(t, filepath.Join(staleDir, "events.jsonl"), `{"seq":1,"type":"job.state","time":"2026-01-01T00:00:00Z","state":"queued"}`+"\n"+`{"seq":2,"type":"job.state","time":"2026-01-01T00:00:01Z","state":"running"}`+"\n")

	e2 := e.reopen(t)
	rec := e2.job(id)
	if rec.State != StateSucceeded || rec.Module.Name != "m" {
		t.Errorf("reloaded job = %+v", rec)
	}
	if !strings.Contains(e2.jobLog(id), "hello-from-step") {
		t.Errorf("log.txt lost: %q", e2.jobLog(id))
	}
	after := e2.allEvents(id)
	if len(after) != len(before) || after[len(after)-2].Data != before[len(before)-2].Data || after[len(after)-1].ID != before[len(before)-1].ID {
		t.Errorf("events changed across the restart: %d vs %d", len(after), len(before))
	}
	e2.diff(id)

	srec := e2.job(stale)
	if srec.State != StateInterrupted || srec.Error == "" || srec.EndedAt == nil {
		t.Errorf("stale job = %+v", srec)
	}
	sevs := e2.allEvents(stale)
	if got := states(t, sevs); !slices.Equal(got, []string{StateQueued, StateRunning, StateInterrupted}) {
		t.Errorf("stale job states = %v", got)
	}
	if sevs[len(sevs)-2].ID != "3" || sevs[len(sevs)-1].Type != "end" {
		t.Errorf("stale stream tail = %+v", sevs[len(sevs)-2:])
	}
	// Rerun of a reloaded job.
	var rr map[string]string
	e2.call(http.MethodPost, "/api/v1/jobs/"+id+"/rerun", map[string]any{}, http.StatusAccepted, &rr)
	e2.waitState(rr["id"], StateSucceeded, 20*time.Second)
}

func TestServe_SV22_HistoryIsPruned(t *testing.T) {
	state := filepath.Join(realTempDir(t), "state")
	jobs := filepath.Join(state, "jobs")
	now := time.Now()
	write := func(i int, created time.Time) string {
		id := fmt.Sprintf("j_%s_%04x", created.UTC().Format("20060102T150405"), i)
		rec := map[string]any{"id": id, "kind": "run", "state": "succeeded", "createdAt": created,
			"request": map[string]any{"kind": "run", "source": "/x", "mode": "dry-run"}, "result": map[string]any{"prs": []any{}}}
		data, _ := json.Marshal(rec)
		writeFile(t, filepath.Join(jobs, id, "job.json"), string(data))
		return id
	}
	old := write(0, now.Add(-40*24*time.Hour))
	var ids []string
	for i := 1; i <= historyMaxJobs+5; i++ {
		ids = append(ids, write(i, now.Add(-time.Duration(historyMaxJobs+10-i)*time.Minute)))
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	srv, err := New(Config{Addr: ln.Addr().String(), Roots: []string{realTempDir(t)}, StateDir: state, History: HistoryDisk})
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Shutdown()
	if n := len(srv.jobs.jobs); n != historyMaxJobs {
		t.Errorf("%d jobs kept, want %d", n, historyMaxJobs)
	}
	if _, ok := srv.jobs.get(old); ok || fileExists(filepath.Join(jobs, old)) {
		t.Error("a job past the age limit was kept")
	}
	for _, id := range ids[:5] {
		if _, ok := srv.jobs.get(id); ok || fileExists(filepath.Join(jobs, id)) {
			t.Errorf("oldest job %s was kept", id)
		}
	}
	if _, ok := srv.jobs.get(ids[len(ids)-1]); !ok {
		t.Error("the newest job was pruned")
	}
}

// Memory history keeps nothing across a restart.
func TestServe_MemoryHistory(t *testing.T) {
	e := newEnv(t)
	mod := shellModule(t, filepath.Join(e.root, "m"), "m", "true")
	id := e.submit(map[string]any{"kind": "run", "source": mod, "mode": "local"})
	e.waitState(id, StateSucceeded, 20*time.Second)
	if fileExists(filepath.Join(e.srv.stateDir, "jobs")) {
		t.Error("memory history wrote to disk")
	}
	e.srv.Shutdown()
	state, root := e.srv.cfg.StateDir, e.root
	e2 := newEnv(t, func(c *Config) { c.StateDir = state; c.Roots = []string{root} })
	if c := e2.errorCode(http.MethodGet, "/api/v1/jobs/"+id, nil, http.StatusNotFound); c != codeNotFound {
		t.Errorf("job after restart: %s", c)
	}
}

// The server makes the state directories it creates private, and leaves
// the mode of one the user already had alone.
func TestServe_SV22_StateDirModes(t *testing.T) {
	mode := func(p string) os.FileMode {
		t.Helper()
		info, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		return info.Mode().Perm()
	}
	start := func(state string) *Server {
		t.Helper()
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { ln.Close() })
		srv, err := New(Config{Addr: ln.Addr().String(), Roots: []string{realTempDir(t)}, StateDir: state, History: HistoryDisk})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(srv.Shutdown)
		return srv
	}

	existing := filepath.Join(realTempDir(t), "mine")
	if err := os.Mkdir(existing, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(existing, 0o755); err != nil {
		t.Fatal(err)
	}
	start(existing)
	if m := mode(existing); m != 0o755 {
		t.Errorf("existing state dir changed to %v", m)
	}
	for _, sub := range []string{"jobs", "workspaces", "presets"} {
		if m := mode(filepath.Join(existing, sub)); m != 0o700 {
			t.Errorf("created %s is %v, want 0700", sub, m)
		}
	}

	fresh := filepath.Join(realTempDir(t), "a", "b", "state")
	start(fresh)
	for _, d := range []string{fresh, filepath.Dir(fresh)} {
		if m := mode(d); m != 0o700 {
			t.Errorf("created %s is %v, want 0700", d, m)
		}
	}
}
