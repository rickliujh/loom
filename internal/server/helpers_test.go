package server

import (
	"bytes"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

// testEnv is a server on a real loopback port, so Host, Origin and the
// cookie name are what a browser would see.
type testEnv struct {
	t    *testing.T
	srv  *Server
	ts   *httptest.Server
	root string
	base string
}

// repoRoot is the repository checkout, for the documentation tree.
func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate source file")
	}
	return filepath.Dir(filepath.Dir(filepath.Dir(file)))
}

// realTempDir is t.TempDir with symlinks resolved, so paths compare equal
// to the canonical ones the server reports (macOS's /var is a symlink).
func realTempDir(t *testing.T) string {
	t.Helper()
	d, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func newEnv(t *testing.T, mutate ...func(*Config)) *testEnv {
	t.Helper()
	root := realTempDir(t)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	cfg := Config{
		Addr:     ln.Addr().String(),
		Roots:    []string{root},
		StateDir: filepath.Join(realTempDir(t), "state"),
		History:  HistoryMemory,
		Version:  "test",
		Docs:     os.DirFS(repoRoot(t)),
	}
	for _, m := range mutate {
		m(&cfg)
	}
	srv, err := New(cfg)
	if err != nil {
		ln.Close()
		t.Fatal(err)
	}
	ts := httptest.NewUnstartedServer(srv.Handler())
	ts.Listener.Close()
	ts.Listener = ln
	ts.Start()
	t.Cleanup(func() {
		srv.Shutdown()
		ts.Close()
	})
	return &testEnv{t: t, srv: srv, ts: ts, root: root, base: ts.URL}
}

// request sends an authenticated JSON request.
func (e *testEnv) request(method, path string, body any) *http.Response {
	e.t.Helper()
	var r io.Reader
	if body != nil {
		switch b := body.(type) {
		case string:
			r = strings.NewReader(b)
		default:
			data, err := json.Marshal(b)
			if err != nil {
				e.t.Fatal(err)
			}
			r = bytes.NewReader(data)
		}
	}
	req, err := http.NewRequest(method, e.base+path, r)
	if err != nil {
		e.t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+e.srv.Token())
	if method != http.MethodGet {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	return resp
}

// call sends a request, checks the status and decodes the JSON body into out
// (when out is not nil).
func (e *testEnv) call(method, path string, body any, want int, out any) {
	e.t.Helper()
	resp := e.request(method, path, body)
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != want {
		e.t.Fatalf("%s %s: status %d, want %d\n%s", method, path, resp.StatusCode, want, data)
	}
	if out != nil {
		if err := json.Unmarshal(data, out); err != nil {
			e.t.Fatalf("%s %s: decoding %q: %v", method, path, data, err)
		}
	}
}

// errorCode sends a request expecting a failure and returns its code.
func (e *testEnv) errorCode(method, path string, body any, want int) string {
	e.t.Helper()
	var out struct {
		Error apiError `json:"error"`
	}
	e.call(method, path, body, want, &out)
	return out.Error.Code
}

// writeFile writes content to p, creating parents.
func writeFile(t *testing.T, p, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// writeModule writes a loom.yaml with the given spec body under dir.
func writeModule(t *testing.T, dir, name, spec string) string {
	t.Helper()
	writeFile(t, filepath.Join(dir, "loom.yaml"), "apiVersion: loom.rickliujh.github.io/v1beta1\nkind: Loom\nmetadata:\n  name: "+name+"\nspec:\n"+spec)
	return dir
}

// lockedBuffer is a bytes.Buffer safe for the server's goroutines to log to.
type lockedBuffer struct {
	mu sync.Mutex
	b  *bytes.Buffer
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

func jsonDecode(resp *http.Response, out any) error {
	return json.NewDecoder(resp.Body).Decode(out)
}
