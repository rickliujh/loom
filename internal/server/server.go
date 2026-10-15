// Package server is the HTTP server behind `loom serve`: a JSON API under
// /api/v1 that describes, edits and runs Loom modules, plus the embedded web
// UI. docs/reference/serve-api.md is its contract.
//
// Everything a job executes goes through pkg/engine, pkg/generate and
// pkg/bulk — the same entry points the CLI uses — so the server adds
// scheduling, isolation and presentation, never a second implementation.
package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rickliujh/loom/internal/skill"
	"github.com/rickliujh/loom/pkg/generate"
)

// History modes.
const (
	HistoryDisk   = "disk"
	HistoryMemory = "memory"
)

// Config configures a Server.
type Config struct {
	// Addr is the address the server is reachable at, host:port, with the
	// real port — the one a listener on ":0" was given. It decides the
	// session cookie's name and the Host and Origin the server accepts.
	Addr string
	// Roots are the directories modules may be read from and written to.
	// Each must exist. At least one is required.
	Roots []string
	// AllowOutsideRoots accepts local module sources outside the roots.
	AllowOutsideRoots bool
	// NoRemoteModules refuses module sources that are git URLs.
	NoRemoteModules bool
	// AllowedHosts are extra Host values accepted besides the listen address
	// and its loopback spellings: "name" (any port) or "name:port".
	AllowedHosts []string
	// StateDir holds job history, presets and job workspaces. Created 0700.
	StateDir string
	// History is HistoryDisk (default) or HistoryMemory.
	History string
	// MaxConcurrentJobs caps the executing jobs that run at once; 0 means 1.
	MaxConcurrentJobs int
	// Version is reported by /info.
	Version string
	// Docs is the documentation tree /docs serves (docs/guide,
	// docs/reference, specs). Nil disables /docs.
	Docs fs.FS
	// Logger receives the server's own log: the access log and job
	// lifecycle lines. Nil discards it.
	Logger *slog.Logger
	// TLS marks the server as served over HTTPS: the session cookie gets
	// Secure and the expected Origin is https.
	TLS bool
	// Token is the session token; empty generates a fresh 256-bit one.
	Token string
}

// Server is the loom serve HTTP server.
type Server struct {
	cfg        Config
	token      string
	port       string
	cookieName string
	hosts      hostSet
	roots      []string
	stateDir   string
	workspaces string
	presetsDir string
	logger     *slog.Logger
	docs       *skill.Library
	docsErr    error

	handler   http.Handler
	discovery discovery
	jobs      *jobManager
	// filesMu serialises module file writes, so a conditional write's check
	// and its write cannot interleave with another request's.
	filesMu sync.Mutex

	// stopping is closed when shutdown begins, releasing event streams.
	stopping chan struct{}
	stopOnce sync.Once

	// capsOnce guards caps, the installed CLIs /info reports.
	capsOnce sync.Once
	caps     map[string]bool

	// generateProvider replaces PR/MR fetching in tests.
	generateProvider generate.DiffProvider

	// pingEvery overrides pingInterval in tests.
	pingEvery time.Duration
}

// New validates cfg, prepares the state directory and loads job history.
func New(cfg Config) (*Server, error) {
	host, port, err := net.SplitHostPort(cfg.Addr)
	if err != nil {
		return nil, fmt.Errorf("server address %q: %w", cfg.Addr, err)
	}
	if n, err := strconv.Atoi(port); err != nil || n <= 0 {
		return nil, fmt.Errorf("server address %q: a concrete port is required", cfg.Addr)
	}
	if len(cfg.Roots) == 0 {
		return nil, errors.New("at least one root directory is required")
	}
	if cfg.StateDir == "" {
		return nil, errors.New("a state directory is required")
	}
	switch cfg.History {
	case "":
		cfg.History = HistoryDisk
	case HistoryDisk, HistoryMemory:
	default:
		return nil, fmt.Errorf("history %q: expected %q or %q", cfg.History, HistoryDisk, HistoryMemory)
	}
	if cfg.MaxConcurrentJobs <= 0 {
		cfg.MaxConcurrentJobs = 1
	}

	s := &Server{
		cfg:        cfg,
		port:       port,
		cookieName: "loom_session_" + port,
		logger:     cfg.Logger,
		stopping:   make(chan struct{}),
	}
	if s.logger == nil {
		s.logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}

	s.token = cfg.Token
	if s.token == "" {
		if s.token, err = newToken(); err != nil {
			return nil, err
		}
	}

	for _, r := range cfg.Roots {
		abs, err := canonicalDir(r)
		if err != nil {
			return nil, fmt.Errorf("root %q: %w", r, err)
		}
		s.roots = append(s.roots, abs)
	}

	// A state directory the server creates is private: history holds logs
	// that may carry secrets. One that already exists is the user's, and
	// keeps the mode they gave it; the files the server writes into it are
	// 0600 regardless.
	if err := mkdirPrivate(cfg.StateDir); err != nil {
		return nil, fmt.Errorf("creating state directory: %w", err)
	}
	if s.stateDir, err = canonicalDir(cfg.StateDir); err != nil {
		return nil, fmt.Errorf("state directory: %w", err)
	}
	s.workspaces = filepath.Join(s.stateDir, "workspaces")
	s.presetsDir = filepath.Join(s.stateDir, "presets")
	for _, d := range []string{s.workspaces, s.presetsDir} {
		if err := mkdirPrivate(d); err != nil {
			return nil, fmt.Errorf("creating %s: %w", d, err)
		}
	}

	s.hosts = newHostSet(host, port, cfg.AllowedHosts)

	if cfg.Docs != nil {
		s.docs, s.docsErr = skill.Load(cfg.Docs)
	}

	s.jobs, err = newJobManager(s)
	if err != nil {
		return nil, err
	}
	s.handler = s.routes()
	return s, nil
}

// Token is the session token clients must present.
func (s *Server) Token() string { return s.token }

// URL is the address to open in a browser: the token rides in the fragment,
// which a browser never sends to the server.
func (s *Server) URL() string {
	scheme := "http"
	if s.cfg.TLS {
		scheme = "https"
	}
	return scheme + "://" + s.cfg.Addr + "/#token=" + s.token
}

// Handler is the server's HTTP handler, security checks included.
func (s *Server) Handler() http.Handler { return s.handler }

// shutdownGrace is how long Serve waits for cancelled jobs to stop before
// recording them as interrupted.
const shutdownGrace = 10 * time.Second

// Serve serves on ln until ctx is done, then cancels running jobs, waits up
// to 10 seconds for them, records the rest as interrupted and closes the
// listener. It returns nil after a shutdown caused by ctx.
func (s *Server) Serve(ctx context.Context, ln net.Listener) error {
	hs := &http.Server{
		Handler:           s.handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       time.Minute,
		// Long enough for an inspect that clones a remote module; event
		// streams lift it for themselves.
		WriteTimeout: 5 * time.Minute,
		IdleTimeout:  2 * time.Minute,
		// The access log is the server's; the stdlib's own error log would
		// print TLS handshake noise and the like to stderr unformatted.
		ErrorLog: slog.NewLogLogger(s.logger.Handler(), slog.LevelDebug),
	}
	errc := make(chan error, 1)
	go func() { errc <- hs.Serve(ln) }()

	select {
	case err := <-errc:
		s.Shutdown()
		return err
	case <-ctx.Done():
	}

	s.Shutdown()
	shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := hs.Shutdown(shutCtx); err != nil {
		_ = hs.Close()
	}
	return nil
}

// Shutdown stops the job system: queued jobs never start, running ones are
// cancelled and given shutdownGrace to stop, and whatever is still not
// terminal is recorded as interrupted. Event streams end. Safe to call more
// than once.
func (s *Server) Shutdown() {
	s.stopOnce.Do(func() {
		s.jobs.shutdown(shutdownGrace)
		close(s.stopping)
	})
}

// mkdirPrivate creates dir, and any missing parents, as 0700 — whatever
// the umask — and leaves a directory that already exists as it is.
func mkdirPrivate(dir string) error {
	if info, err := os.Stat(dir); err == nil {
		if !info.IsDir() {
			return fmt.Errorf("%s is not a directory", dir)
		}
		return nil
	}
	// Record which directories are missing before creating them, so only
	// those get their mode set.
	var missing []string
	for d := filepath.Clean(dir); ; d = filepath.Dir(d) {
		if _, err := os.Stat(d); err == nil {
			break
		}
		missing = append(missing, d)
		if filepath.Dir(d) == d {
			break
		}
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	for _, d := range missing {
		if err := os.Chmod(d, 0o700); err != nil {
			return err
		}
	}
	return nil
}

func newToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generating session token: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// canonicalDir is dir made absolute with symlinks resolved; it must be a
// directory.
func canonicalDir(dir string) (string, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(real)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("%s is not a directory", dir)
	}
	return real, nil
}

// hostSet is the Host values the server answers to.
type hostSet struct {
	exact    map[string]bool
	anyPort  map[string]bool
	ownPort  string
	loopback bool
}

func newHostSet(host, port string, extra []string) hostSet {
	h := hostSet{exact: map[string]bool{}, anyPort: map[string]bool{}, ownPort: port}
	add := func(host string) { h.exact[strings.ToLower(net.JoinHostPort(host, port))] = true }
	add(host)
	// The loopback spellings of this server, whichever one it was bound
	// with: a browser may be pointed at any of them.
	if host == "" || isLoopbackHost(host) || isUnspecified(host) {
		for _, lh := range []string{"127.0.0.1", "localhost", "::1"} {
			add(lh)
		}
	}
	for _, e := range extra {
		e = strings.ToLower(strings.TrimSpace(e))
		if e == "" {
			continue
		}
		if _, _, err := net.SplitHostPort(e); err == nil {
			h.exact[e] = true
		} else {
			h.anyPort[strings.Trim(e, "[]")] = true
		}
	}
	return h
}

func (h hostSet) allows(hostHeader string) bool {
	hostHeader = strings.ToLower(hostHeader)
	if h.exact[hostHeader] {
		return true
	}
	name := hostHeader
	if hn, _, err := net.SplitHostPort(hostHeader); err == nil {
		name = hn
	}
	return h.anyPort[strings.Trim(name, "[]")]
}

// IsLoopbackListen reports whether a listen address binds loopback only. An
// empty host counts as loopback: `loom serve` binds an address like ":0" to
// 127.0.0.1 rather than to every interface.
func IsLoopbackListen(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	return host == "" || isLoopbackHost(host)
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	return ip != nil && ip.IsLoopback()
}

func isUnspecified(host string) bool {
	ip := net.ParseIP(strings.Trim(host, "[]"))
	return ip != nil && ip.IsUnspecified()
}
