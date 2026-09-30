package server

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"
)

// maxFileSize caps what the file API returns and accepts.
const maxFileSize = 1 << 20

// fileModule resolves the source of a file request: local only, since a git
// URL's checkout is a throwaway clone with nothing worth editing.
func (s *Server) fileModule(source string) (*moduleSource, error) {
	src, err := s.resolveSource(source, "source")
	if err != nil {
		return nil, err
	}
	if !src.Local {
		return nil, errInvalid("source", "a git-URL source is not browsable or editable; clone it under a root first")
	}
	return src, nil
}

type fileEntry struct {
	Name string `json:"name"`
	Kind string `json:"kind"`
	Size *int64 `json:"size,omitempty"`
}

func (s *Server) handleFilesGet(w http.ResponseWriter, r *http.Request) error {
	q := r.URL.Query()
	src, err := s.fileModule(q.Get("source"))
	if err != nil {
		return err
	}
	mp, err := resolveModulePath(src.Dir, q.Get("path"), "path")
	if err != nil {
		return err
	}
	if !mp.Exists {
		return errNotFound("%s not found", q.Get("path"))
	}
	info, err := os.Stat(mp.Abs)
	if err != nil {
		return err
	}
	if info.IsDir() {
		entries, err := listDir(src.Dir, mp.Abs)
		if err != nil {
			return err
		}
		writeJSON(w, http.StatusOK, map[string]any{"kind": "dir", "entries": entries})
		return nil
	}
	if !info.Mode().IsRegular() {
		return errInvalid("path", "%s is not a regular file", q.Get("path"))
	}
	content, etag, truncated, err := readCapped(mp.Abs)
	if err != nil {
		return err
	}
	out := map[string]any{"kind": "file", "etag": etag, "truncated": truncated, "binary": false, "size": info.Size()}
	if isBinary(content, truncated) {
		out["binary"] = true
	} else {
		out["content"] = string(content)
	}
	writeJSON(w, http.StatusOK, out)
	return nil
}

// listDir lists a directory inside the module: .git left out, and a symlink
// shown as what it points to, or left out when that is outside the module.
func listDir(moduleDir, dir string) ([]fileEntry, error) {
	des, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	entries := []fileEntry{}
	for _, de := range des {
		if strings.EqualFold(de.Name(), ".git") {
			continue
		}
		p := filepath.Join(dir, de.Name())
		if de.Type()&os.ModeSymlink != 0 {
			real, err := filepath.EvalSymlinks(p)
			if err != nil || !within(moduleDir, real) || inGitDir(moduleDir, real) {
				continue
			}
		}
		info, err := os.Stat(p)
		if err != nil {
			continue
		}
		e := fileEntry{Name: de.Name(), Kind: "file"}
		if info.IsDir() {
			e.Kind = "dir"
		} else {
			size := info.Size()
			e.Size = &size
		}
		entries = append(entries, e)
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Kind != entries[j].Kind {
			return entries[i].Kind == "dir"
		}
		return entries[i].Name < entries[j].Name
	})
	return entries, nil
}

// inGitDir reports a canonical path inside the module that lies in a .git
// directory.
func inGitDir(moduleDir, real string) bool {
	rel, err := filepath.Rel(moduleDir, real)
	if err != nil {
		return false
	}
	for _, seg := range strings.Split(filepath.ToSlash(rel), "/") {
		if strings.EqualFold(seg, ".git") {
			return true
		}
	}
	return false
}

// readCapped returns up to maxFileSize bytes of a file, whether there was
// more, and the etag of the whole file.
func readCapped(p string) (content []byte, etag string, truncated bool, err error) {
	f, err := os.Open(p)
	if err != nil {
		return nil, "", false, err
	}
	defer f.Close()
	h := sha256.New()
	var buf bytes.Buffer
	n, err := io.Copy(io.MultiWriter(h, &limitedBuffer{buf: &buf, max: maxFileSize}), f)
	if err != nil {
		return nil, "", false, err
	}
	return buf.Bytes(), "sha256:" + hex.EncodeToString(h.Sum(nil)), n > maxFileSize, nil
}

// limitedBuffer keeps the first max bytes written and discards the rest
// without failing the copy, so the hash still sees every byte.
type limitedBuffer struct {
	buf *bytes.Buffer
	max int
}

func (l *limitedBuffer) Write(p []byte) (int, error) {
	if room := l.max - l.buf.Len(); room > 0 {
		if len(p) > room {
			l.buf.Write(p[:room])
		} else {
			l.buf.Write(p)
		}
	}
	return len(p), nil
}

func etagOf(content []byte) string {
	sum := sha256.Sum256(content)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// isBinary reports content that is not text: a NUL byte, or invalid UTF-8
// (ignoring a rune cut in half by truncation).
func isBinary(content []byte, truncated bool) bool {
	if bytes.IndexByte(content, 0) >= 0 {
		return true
	}
	if truncated {
		for i := 0; i < utf8.UTFMax && len(content) > 0 && !utf8.Valid(content); i++ {
			content = content[:len(content)-1]
		}
	}
	return !utf8.Valid(content)
}

type putFileRequest struct {
	Content *string `json:"content"`
	IfMatch string  `json:"ifMatch"`
}

func (s *Server) handleFilesPut(w http.ResponseWriter, r *http.Request) error {
	q := r.URL.Query()
	src, err := s.fileModule(q.Get("source"))
	if err != nil {
		return err
	}
	var req putFileRequest
	if err := decodeBody(r, &req); err != nil {
		return err
	}
	if req.Content == nil {
		return errInvalid("content", "content is required")
	}
	if len(*req.Content) > maxFileSize {
		return errInvalid("content", "content is larger than 1 MiB")
	}

	s.filesMu.Lock()
	defer s.filesMu.Unlock()
	mp, err := resolveModulePath(src.Dir, q.Get("path"), "path")
	if err != nil {
		return err
	}
	if mp.Rel == "" {
		return errInvalid("path", "path must name a file")
	}
	mode := os.FileMode(0o644)
	if mp.Exists {
		info, err := os.Stat(mp.Abs)
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return errConflict("%s is not a regular file", mp.Rel).withField("path")
		}
		if info.Size() > maxFileSize {
			return errInvalid("path", "%s is larger than 1 MiB and cannot be edited through the API", mp.Rel)
		}
		if req.IfMatch == "" {
			// Without an etag there is no telling whether the caller saw
			// the current content; a blind overwrite is what ifMatch exists
			// to prevent.
			return errConflict("%s already exists; send the etag of the version you edited as ifMatch", mp.Rel).withField("ifMatch")
		}
		current, err := os.ReadFile(mp.Abs)
		if err != nil {
			return err
		}
		if etagOf(current) != req.IfMatch {
			return errConflict("%s changed since it was read", mp.Rel).withField("ifMatch").with("etag", etagOf(current))
		}
		mode = info.Mode().Perm()
	} else if req.IfMatch != "" {
		return errConflict("%s no longer exists", mp.Rel).withField("ifMatch")
	}

	if err := os.MkdirAll(filepath.Dir(mp.Abs), 0o755); err != nil {
		return err
	}
	if err := writeFileAtomic(mp.Abs, []byte(*req.Content), mode); err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]string{"etag": etagOf([]byte(*req.Content))})
	return nil
}

// writeFileAtomic replaces p through a temporary file in the same directory,
// so a reader never sees half a file.
func writeFileAtomic(p string, content []byte, mode os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(p), ".loom-write-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(content); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), p)
}

type createFileRequest struct {
	Source string `json:"source"`
	Path   string `json:"path"`
	Kind   string `json:"kind"`
}

func (s *Server) handleFilesCreate(w http.ResponseWriter, r *http.Request) error {
	var req createFileRequest
	if err := decodeBody(r, &req); err != nil {
		return err
	}
	if req.Kind != "dir" && req.Kind != "file" {
		return errInvalid("kind", `kind must be "dir" or "file"`)
	}
	src, err := s.fileModule(req.Source)
	if err != nil {
		return err
	}
	s.filesMu.Lock()
	defer s.filesMu.Unlock()
	mp, err := resolveModulePath(src.Dir, req.Path, "path")
	if err != nil {
		return err
	}
	if mp.Rel == "" {
		return errInvalid("path", "path is required")
	}
	if mp.Exists {
		return errConflict("%s already exists", mp.Rel).withField("path")
	}
	if err := os.MkdirAll(filepath.Dir(mp.Abs), 0o755); err != nil {
		return err
	}
	if req.Kind == "dir" {
		err = os.Mkdir(mp.Abs, 0o755)
	} else {
		var f *os.File
		f, err = os.OpenFile(mp.Abs, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err == nil {
			err = f.Close()
		}
	}
	if errors.Is(err, os.ErrExist) {
		return errConflict("%s already exists", mp.Rel).withField("path")
	}
	if err != nil {
		return err
	}
	// The new entry as a directory listing shows it.
	entry := fileEntry{Name: filepath.Base(mp.Abs), Kind: req.Kind}
	if req.Kind == "file" {
		var zero int64
		entry.Size = &zero
	}
	writeJSON(w, http.StatusCreated, entry)
	return nil
}

// isModuleConfig reports the module's own config file, which the API edits
// but never deletes or moves: without it the directory is no longer a module.
func isModuleConfig(rel string) bool {
	return rel == discoveryConfigYAML || rel == discoveryConfigJsonnet
}

func (s *Server) handleFilesDelete(w http.ResponseWriter, r *http.Request) error {
	q := r.URL.Query()
	src, err := s.fileModule(q.Get("source"))
	if err != nil {
		return err
	}
	s.filesMu.Lock()
	defer s.filesMu.Unlock()
	mp, err := resolveModulePath(src.Dir, q.Get("path"), "path")
	if err != nil {
		return err
	}
	if mp.Rel == "" {
		return errInvalid("path", "path is required; the module directory itself cannot be deleted")
	}
	if isModuleConfig(mp.Rel) {
		return errForbidden("%s cannot be deleted through the API; edit it instead", mp.Rel).withField("path")
	}
	info, err := os.Lstat(mp.Entry)
	if err != nil {
		if errIsNotExist(err) {
			return errNotFound("%s not found", mp.Rel)
		}
		return err
	}
	if info.IsDir() {
		des, err := os.ReadDir(mp.Entry)
		if err != nil {
			return err
		}
		if len(des) > 0 {
			return errConflict("%s is not empty", mp.Rel).withField("path")
		}
	}
	if err := os.Remove(mp.Entry); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

type moveFileRequest struct {
	Source string `json:"source"`
	From   string `json:"from"`
	To     string `json:"to"`
}

func (s *Server) handleFilesMove(w http.ResponseWriter, r *http.Request) error {
	var req moveFileRequest
	if err := decodeBody(r, &req); err != nil {
		return err
	}
	src, err := s.fileModule(req.Source)
	if err != nil {
		return err
	}
	s.filesMu.Lock()
	defer s.filesMu.Unlock()
	from, err := resolveModulePath(src.Dir, req.From, "from")
	if err != nil {
		return err
	}
	to, err := resolveModulePath(src.Dir, req.To, "to")
	if err != nil {
		return err
	}
	if from.Rel == "" || to.Rel == "" {
		return errInvalid("", "from and to must name paths inside the module")
	}
	if isModuleConfig(from.Rel) {
		return errForbidden("%s cannot be moved through the API", from.Rel).withField("from")
	}
	if _, err := os.Lstat(from.Entry); err != nil {
		if errIsNotExist(err) {
			return errNotFound("%s not found", from.Rel)
		}
		return err
	}
	if _, err := os.Lstat(to.Entry); err == nil {
		return errConflict("%s already exists", to.Rel).withField("to")
	}
	if within(from.Entry, to.Entry) {
		return errInvalid("to", "cannot move %s into itself", from.Rel)
	}
	if err := os.MkdirAll(filepath.Dir(to.Entry), 0o755); err != nil {
		return err
	}
	if err := os.Rename(from.Entry, to.Entry); err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]string{"path": to.Rel})
	return nil
}
