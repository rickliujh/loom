package server

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/rickliujh/loom/internal/proc"
)

// isolatedEnv makes TestMain switch the test binary into server mode
// (proc.EnableIsolation) before running tests. Isolation is process-global
// and cannot be switched off, so the test that needs it re-runs itself in a
// child copy of this binary instead of flipping it for every test here.
const isolatedEnv = "LOOM_SERVER_TEST_ISOLATED"

func TestMain(m *testing.M) {
	if os.Getenv(isolatedEnv) == "1" {
		proc.EnableIsolation()
	}
	os.Exit(m.Run())
}

// sseEvent is one parsed Server-Sent Event, or a comment when Comment is set.
type sseEvent struct {
	ID      string
	Type    string
	Data    string
	Comment string
}

func (ev sseEvent) seq(t *testing.T) int64 {
	t.Helper()
	n, err := strconv.ParseInt(ev.ID, 10, 64)
	if err != nil {
		t.Fatalf("event id %q: %v", ev.ID, err)
	}
	return n
}

// decoded is the event's data as a jobEvent.
func (ev sseEvent) decoded(t *testing.T) jobEvent {
	t.Helper()
	var je jobEvent
	if err := json.Unmarshal([]byte(ev.Data), &je); err != nil {
		t.Fatalf("event data %q: %v", ev.Data, err)
	}
	return je
}

// stream is an open event stream.
type stream struct {
	t      *testing.T
	resp   *http.Response
	sc     *bufio.Scanner
	cancel context.CancelFunc
}

// openStream opens GET path as an event stream; the client gives up after
// limit, so a stream that never ends fails the test instead of hanging it.
func (e *testEnv) openStream(path string, headers map[string]string, limit time.Duration) *stream {
	e.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), limit)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, e.base+path, nil)
	if err != nil {
		cancel()
		e.t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+e.srv.Token())
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		cancel()
		e.t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		cancel()
		e.t.Fatalf("GET %s: status %d", path, resp.StatusCode)
	}
	for k, want := range map[string]string{"Content-Type": "text/event-stream", "Cache-Control": "no-cache", "X-Accel-Buffering": "no"} {
		if got := resp.Header.Get(k); got != want {
			e.t.Errorf("%s = %q, want %q", k, got, want)
		}
	}
	s := &stream{t: e.t, resp: resp, sc: bufio.NewScanner(resp.Body), cancel: cancel}
	s.sc.Buffer(make([]byte, 0, 64*1024), 4<<20)
	e.t.Cleanup(s.close)
	return s
}

func (s *stream) close() {
	s.cancel()
	s.resp.Body.Close()
}

// next returns the next event or comment; ok is false once the server has
// closed the stream.
func (s *stream) next() (sseEvent, bool) {
	s.t.Helper()
	var ev sseEvent
	seen := false
	for s.sc.Scan() {
		line := s.sc.Text()
		switch {
		case line == "":
			if seen {
				return ev, true
			}
		case strings.HasPrefix(line, ":"):
			return sseEvent{Comment: strings.TrimSpace(line[1:])}, true
		case strings.HasPrefix(line, "id: "):
			ev.ID, seen = line[4:], true
		case strings.HasPrefix(line, "event: "):
			ev.Type, seen = line[7:], true
		case strings.HasPrefix(line, "data: "):
			ev.Data, seen = line[6:], true
		default:
			s.t.Fatalf("unexpected stream line %q", line)
		}
	}
	if err := s.sc.Err(); err != nil {
		s.t.Fatalf("reading the stream: %v", err)
	}
	return ev, false
}

// until reads events, skipping comments, until one satisfies match.
func (s *stream) until(match func(sseEvent) bool) []sseEvent {
	s.t.Helper()
	var got []sseEvent
	for {
		ev, ok := s.next()
		if !ok {
			s.t.Fatalf("stream closed before the awaited event; got %d events", len(got))
		}
		if ev.Comment != "" {
			continue
		}
		got = append(got, ev)
		if match(ev) {
			return got
		}
	}
}

// rest reads everything up to the server closing the stream.
func (s *stream) rest() (events []sseEvent, comments []string) {
	s.t.Helper()
	for {
		ev, ok := s.next()
		if !ok {
			return events, comments
		}
		if ev.Comment != "" {
			comments = append(comments, ev.Comment)
			continue
		}
		events = append(events, ev)
	}
}

// allEvents reads a finished job's whole stream.
func (e *testEnv) allEvents(id string) []sseEvent {
	e.t.Helper()
	evs, _ := e.openStream("/api/v1/jobs/"+id+"/events", nil, 10*time.Second).rest()
	return evs
}

// ofType filters decoded events.
func ofType(t *testing.T, evs []sseEvent, typ string) []jobEvent {
	t.Helper()
	var out []jobEvent
	for _, ev := range evs {
		if ev.Type == typ {
			out = append(out, ev.decoded(t))
		}
	}
	return out
}

// states is the sequence of job.state values in a stream.
func states(t *testing.T, evs []sseEvent) []string {
	t.Helper()
	var out []string
	for _, je := range ofType(t, evs, string(eventJobState)) {
		out = append(out, je.State)
	}
	return out
}

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

// initBareRepo makes a bare repository on branch main with one commit and
// returns its file:// URL and path.
func initBareRepo(t *testing.T) (string, string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	work := realTempDir(t)
	git(t, work, "init", "-q", "-b", "main")
	git(t, work, "config", "user.email", "test@test.com")
	git(t, work, "config", "user.name", "Test")
	writeFile(t, filepath.Join(work, "README.md"), "# target\n")
	git(t, work, "add", ".")
	git(t, work, "commit", "-q", "-m", "init")
	bare := filepath.Join(realTempDir(t), "origin.git")
	git(t, work, "clone", "-q", "--bare", work, bare)
	return "file://" + bare, bare
}

// waitFor polls cond until it holds, failing after limit. For conditions
// with no event to wait on; the poll interval is short and bounded.
func waitFor(t *testing.T, limit time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(limit)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out after %v waiting for %s", limit, what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// jobState is the job's current state.
func (e *testEnv) jobState(id string) string {
	e.t.Helper()
	var rec struct {
		State string `json:"state"`
	}
	e.call(http.MethodGet, "/api/v1/jobs/"+id, nil, http.StatusOK, &rec)
	return rec.State
}

// waitState waits for a job to reach state.
func (e *testEnv) waitState(id, state string, limit time.Duration) {
	e.t.Helper()
	var last string
	waitFor(e.t, limit, "job "+id+" to be "+state+" (last: "+last+")", func() bool {
		last = e.jobState(id)
		if isTerminal(last) && last != state {
			e.t.Fatalf("job %s ended %s, want %s\n%s", id, last, state, e.jobLog(id))
		}
		return last == state
	})
}

func (e *testEnv) jobLog(id string) string {
	e.t.Helper()
	resp := e.request(http.MethodGet, "/api/v1/jobs/"+id+"/log.txt", nil)
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return string(b)
}

// submit posts a job and returns its id.
func (e *testEnv) submit(req map[string]any) string {
	e.t.Helper()
	var out struct {
		ID    string `json:"id"`
		State string `json:"state"`
	}
	e.call(http.MethodPost, "/api/v1/jobs", req, http.StatusAccepted, &out)
	if out.State != StateQueued || out.ID == "" {
		e.t.Fatalf("POST /jobs returned %+v", out)
	}
	return out.ID
}

type jobRec struct {
	ID        string          `json:"id"`
	Kind      string          `json:"kind"`
	State     string          `json:"state"`
	Request   json.RawMessage `json:"request"`
	StartedAt *time.Time      `json:"startedAt"`
	EndedAt   *time.Time      `json:"endedAt"`
	Module    jobModule       `json:"module"`
	Result    jobResult       `json:"result"`
	Error     string          `json:"error"`
	CLI       string          `json:"cli"`
}

func (e *testEnv) job(id string) jobRec {
	e.t.Helper()
	var rec jobRec
	e.call(http.MethodGet, "/api/v1/jobs/"+id, nil, http.StatusOK, &rec)
	return rec
}

func (e *testEnv) diff(id string) diffOut {
	e.t.Helper()
	var d diffOut
	e.call(http.MethodGet, "/api/v1/jobs/"+id+"/diff", nil, http.StatusOK, &d)
	return d
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
