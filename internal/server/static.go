package server

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strings"
	"sync"
)

//go:embed web
var webFS embed.FS

// staticFile is one embedded UI file with its validator.
type staticFile struct {
	body        []byte
	etag        string
	contentType string
}

var (
	staticOnce  sync.Once
	staticFiles map[string]*staticFile
)

// loadStatic reads the embedded UI once. Its files never change for the life
// of the binary, so a content hash is a correct, cheap ETag.
func loadStatic() map[string]*staticFile {
	staticOnce.Do(func() {
		staticFiles = map[string]*staticFile{}
		sub, err := fs.Sub(webFS, "web")
		if err != nil {
			return
		}
		_ = fs.WalkDir(sub, ".", func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			body, err := fs.ReadFile(sub, p)
			if err != nil {
				return err
			}
			sum := sha256.Sum256(body)
			ct := mime.TypeByExtension(path.Ext(p))
			switch path.Ext(p) {
			case ".js", ".mjs":
				ct = "text/javascript; charset=utf-8"
			case ".html":
				ct = "text/html; charset=utf-8"
			case ".css":
				ct = "text/css; charset=utf-8"
			}
			if ct == "" {
				ct = "application/octet-stream"
			}
			staticFiles[p] = &staticFile{body: body, etag: `"` + hex.EncodeToString(sum[:16]) + `"`, contentType: ct}
			return nil
		})
	})
	return staticFiles
}

// handleStatic serves the embedded UI. A path with no file behind it gets
// index.html, since the UI routes in the URL fragment and any other path is
// a stale or mistyped link best answered with the app.
func (s *Server) handleStatic(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	files := loadStatic()
	name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
	// The UI's unit tests ship in the embedded tree beside the modules
	// they test; they are not part of the app.
	if strings.HasSuffix(name, ".test.js") {
		http.NotFound(w, r)
		return
	}
	f, ok := files[name]
	if !ok {
		f, ok = files["index.html"]
	}
	if !ok {
		http.NotFound(w, r)
		return
	}
	h := w.Header()
	h.Set("Content-Type", f.contentType)
	h.Set("ETag", f.etag)
	// Revalidate every time: a new binary serves new files under the same
	// names, and the ETag makes the check a 304 when nothing changed.
	h.Set("Cache-Control", "no-cache")
	if match := r.Header.Get("If-None-Match"); match != "" && etagMatches(match, f.etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	if r.Method == http.MethodHead {
		return
	}
	_, _ = w.Write(f.body)
}

func etagMatches(header, etag string) bool {
	for _, part := range strings.Split(header, ",") {
		part = strings.TrimSpace(part)
		if part == "*" || strings.TrimPrefix(part, "W/") == etag {
			return true
		}
	}
	return false
}
