package server

import (
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func filesURL(source, path string) string {
	return "/api/v1/files?source=" + url.QueryEscape(source) + "&path=" + url.QueryEscape(path)
}

type fileOut struct {
	Kind      string      `json:"kind"`
	Entries   []fileEntry `json:"entries"`
	Content   *string     `json:"content"`
	Etag      string      `json:"etag"`
	Truncated bool        `json:"truncated"`
	Binary    bool        `json:"binary"`
}

func TestServe_SV8_FileBrowsingIsConfined(t *testing.T) {
	e := newEnv(t)
	mod := writeModule(t, filepath.Join(e.root, "m"), "m", "  operations: []\n")
	writeFile(t, filepath.Join(mod, "argocd", "app.yaml"), "kind: App\n")
	writeFile(t, filepath.Join(mod, ".git", "config"), "[core]\n")
	secret := filepath.Join(e.root, "secret.txt")
	writeFile(t, secret, "top secret\n")
	if err := os.Symlink(secret, filepath.Join(mod, "out-link")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(mod, ".git"), filepath.Join(mod, "gitlink")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(mod, "argocd", "app.yaml"), filepath.Join(mod, "in-link")); err != nil {
		t.Fatal(err)
	}

	var dir fileOut
	e.call(http.MethodGet, filesURL(mod, ""), nil, http.StatusOK, &dir)
	var names []string
	for _, en := range dir.Entries {
		names = append(names, en.Name+":"+en.Kind)
	}
	if dir.Kind != "dir" || strings.Join(names, ",") != "argocd:dir,in-link:file,loom.yaml:file" {
		t.Errorf("listing = %v (a .git entry or an escaping link was shown)", names)
	}

	var f fileOut
	e.call(http.MethodGet, filesURL(mod, "argocd/app.yaml"), nil, http.StatusOK, &f)
	if f.Kind != "file" || f.Content == nil || *f.Content != "kind: App\n" || f.Etag != etagOf([]byte("kind: App\n")) {
		t.Errorf("file = %+v", f)
	}
	e.call(http.MethodGet, filesURL(mod, "in-link"), nil, http.StatusOK, &f)

	cases := []struct {
		path string
		want int
		code string
	}{
		{"../secret.txt", http.StatusBadRequest, codeInvalid},
		{"argocd/../../secret.txt", http.StatusBadRequest, codeInvalid},
		{secret, http.StatusBadRequest, codeInvalid},
		{"/etc/passwd", http.StatusBadRequest, codeInvalid},
		{"out-link", http.StatusForbidden, codeForbidden},
		{".git/config", http.StatusNotFound, codeNotFound},
		{"argocd/.GIT", http.StatusNotFound, codeNotFound},
		{"gitlink/config", http.StatusNotFound, codeNotFound},
		{"nope.yaml", http.StatusNotFound, codeNotFound},
		{`argocd\app.yaml`, http.StatusBadRequest, codeInvalid},
	}
	for _, tc := range cases {
		if c := e.errorCode(http.MethodGet, filesURL(mod, tc.path), nil, tc.want); c != tc.code {
			t.Errorf("GET %q: code %s, want %s", tc.path, c, tc.code)
		}
	}
	// Writes are confined the same way.
	for _, p := range []string{"../evil.yaml", "out-link", ".git/hooks/pre-commit"} {
		resp := e.request(http.MethodPut, filesURL(mod, p), map[string]string{"content": "x"})
		resp.Body.Close()
		if resp.StatusCode < 400 {
			t.Errorf("PUT %q was accepted", p)
		}
	}
	if b, _ := os.ReadFile(secret); string(b) != "top secret\n" {
		t.Error("a write escaped the module")
	}
	if _, err := os.Stat(filepath.Join(e.root, "evil.yaml")); !os.IsNotExist(err) {
		t.Error("a write escaped the module")
	}
	// A git-URL source is not browsable.
	if c := e.errorCode(http.MethodGet, filesURL("https://example.com/r.git", ""), nil, http.StatusBadRequest); c != codeInvalid {
		t.Errorf("remote source: %s", c)
	}
}

func TestServe_FileEditing(t *testing.T) {
	e := newEnv(t)
	mod := writeModule(t, filepath.Join(e.root, "m"), "m", "  operations: []\n")

	// Create a new file with its parents.
	var put map[string]string
	e.call(http.MethodPut, filesURL(mod, "argocd/prod/app.yaml"), map[string]string{"content": "a: 1\n"}, http.StatusOK, &put)
	if put["etag"] != etagOf([]byte("a: 1\n")) {
		t.Fatalf("etag = %v", put)
	}
	// An existing file needs the etag it was read with.
	if c := e.errorCode(http.MethodPut, filesURL(mod, "argocd/prod/app.yaml"), map[string]string{"content": "a: 2\n"}, http.StatusConflict); c != codeConflict {
		t.Errorf("overwrite without ifMatch: %s", c)
	}
	e.call(http.MethodPut, filesURL(mod, "argocd/prod/app.yaml"), map[string]string{"content": "a: 2\n", "ifMatch": put["etag"]}, http.StatusOK, &put)
	// A stale etag is refused, so an edit made elsewhere survives.
	writeFile(t, filepath.Join(mod, "argocd/prod/app.yaml"), "edited elsewhere\n")
	if c := e.errorCode(http.MethodPut, filesURL(mod, "argocd/prod/app.yaml"), map[string]string{"content": "a: 3\n", "ifMatch": put["etag"]}, http.StatusConflict); c != codeConflict {
		t.Errorf("stale ifMatch: %s", c)
	}
	if b, _ := os.ReadFile(filepath.Join(mod, "argocd/prod/app.yaml")); string(b) != "edited elsewhere\n" {
		t.Errorf("a conflicting write went through: %q", b)
	}

	// Create an empty file and directory.
	var entry fileEntry
	e.call(http.MethodPost, "/api/v1/files", map[string]string{"source": mod, "path": "argocd/dev", "kind": "dir"}, http.StatusCreated, &entry)
	if entry.Name != "dev" || entry.Kind != "dir" {
		t.Errorf("entry = %+v", entry)
	}
	e.call(http.MethodPost, "/api/v1/files", map[string]string{"source": mod, "path": "argocd/dev/x.yaml", "kind": "file"}, http.StatusCreated, &entry)
	if entry.Name != "x.yaml" || entry.Kind != "file" || entry.Size == nil || *entry.Size != 0 {
		t.Errorf("entry = %+v", entry)
	}
	if c := e.errorCode(http.MethodPost, "/api/v1/files", map[string]string{"source": mod, "path": "argocd/dev", "kind": "dir"}, http.StatusConflict); c != codeConflict {
		t.Errorf("existing: %s", c)
	}

	// Move: confined, and never over an existing file.
	var moved map[string]string
	e.call(http.MethodPost, "/api/v1/files/move", map[string]string{"source": mod, "from": "argocd/dev/x.yaml", "to": "argocd/{{ .serviceName }}.yaml"}, http.StatusOK, &moved)
	if moved["path"] != "argocd/{{ .serviceName }}.yaml" {
		t.Errorf("move = %v", moved)
	}
	if c := e.errorCode(http.MethodPost, "/api/v1/files/move", map[string]string{"source": mod, "from": "argocd/prod/app.yaml", "to": "argocd/{{ .serviceName }}.yaml"}, http.StatusConflict); c != codeConflict {
		t.Errorf("move onto an existing file: %s", c)
	}
	if c := e.errorCode(http.MethodPost, "/api/v1/files/move", map[string]string{"source": mod, "from": "argocd/prod/app.yaml", "to": "../out.yaml"}, http.StatusBadRequest); c != codeInvalid {
		t.Errorf("move out of the module: %s", c)
	}
	if c := e.errorCode(http.MethodPost, "/api/v1/files/move", map[string]string{"source": mod, "from": "loom.yaml", "to": "other.yaml"}, http.StatusForbidden); c != codeForbidden {
		t.Errorf("move loom.yaml: %s", c)
	}

	// Delete: files and empty directories; never the module config.
	if c := e.errorCode(http.MethodDelete, filesURL(mod, "loom.yaml"), nil, http.StatusForbidden); c != codeForbidden {
		t.Errorf("delete loom.yaml: %s", c)
	}
	if c := e.errorCode(http.MethodDelete, filesURL(mod, "argocd"), nil, http.StatusConflict); c != codeConflict {
		t.Errorf("delete a non-empty dir: %s", c)
	}
	e.call(http.MethodDelete, filesURL(mod, "argocd/dev"), nil, http.StatusNoContent, nil)
	e.call(http.MethodDelete, filesURL(mod, "argocd/prod/app.yaml"), nil, http.StatusNoContent, nil)
	if c := e.errorCode(http.MethodDelete, filesURL(mod, "argocd/prod/app.yaml"), nil, http.StatusNotFound); c != codeNotFound {
		t.Errorf("delete a missing file: %s", c)
	}
	// Deleting a link removes the link, not its target.
	writeFile(t, filepath.Join(mod, "target.txt"), "keep\n")
	if err := os.Symlink(filepath.Join(mod, "target.txt"), filepath.Join(mod, "alias")); err != nil {
		t.Fatal(err)
	}
	e.call(http.MethodDelete, filesURL(mod, "alias"), nil, http.StatusNoContent, nil)
	if b, _ := os.ReadFile(filepath.Join(mod, "target.txt")); string(b) != "keep\n" {
		t.Error("deleting a symlink removed its target")
	}
}

func TestServe_FileLimits(t *testing.T) {
	e := newEnv(t)
	mod := writeModule(t, filepath.Join(e.root, "m"), "m", "  operations: []\n")
	big := strings.Repeat("x", maxFileSize+10)
	writeFile(t, filepath.Join(mod, "big.txt"), big)
	writeFile(t, filepath.Join(mod, "bin.dat"), "a\x00b")

	var f fileOut
	e.call(http.MethodGet, filesURL(mod, "big.txt"), nil, http.StatusOK, &f)
	if !f.Truncated || f.Content == nil || len(*f.Content) != maxFileSize || f.Etag != etagOf([]byte(big)) {
		t.Errorf("big file: truncated=%v len=%d", f.Truncated, len(*f.Content))
	}
	if c := e.errorCode(http.MethodPut, filesURL(mod, "big.txt"), map[string]string{"content": "x", "ifMatch": f.Etag}, http.StatusBadRequest); c != codeInvalid {
		t.Errorf("editing a truncated file: %s", c)
	}
	if c := e.errorCode(http.MethodPut, filesURL(mod, "new.txt"), map[string]string{"content": big}, http.StatusBadRequest); c != codeInvalid {
		t.Errorf("writing over 1 MiB: %s", c)
	}
	var bin fileOut
	e.call(http.MethodGet, filesURL(mod, "bin.dat"), nil, http.StatusOK, &bin)
	if !bin.Binary || bin.Content != nil {
		t.Errorf("binary file = %+v", bin)
	}
}

// A dangling symlink is a missing file, not a server error; it can still be
// deleted, and it is not listed.
func TestServe_SV8_DanglingSymlink(t *testing.T) {
	e := newEnv(t)
	mod := writeModule(t, filepath.Join(e.root, "m"), "m", "  operations: []\n")
	if err := os.Symlink(filepath.Join(mod, "gone.yaml"), filepath.Join(mod, "dangling")); err != nil {
		t.Fatal(err)
	}
	if c := e.errorCode(http.MethodGet, filesURL(mod, "dangling"), nil, http.StatusNotFound); c != codeNotFound {
		t.Errorf("GET a dangling link: %s", c)
	}
	var dir fileOut
	e.call(http.MethodGet, filesURL(mod, ""), nil, http.StatusOK, &dir)
	for _, en := range dir.Entries {
		if en.Name == "dangling" {
			t.Error("a dangling link is listed")
		}
	}
	e.call(http.MethodDelete, filesURL(mod, "dangling"), nil, http.StatusNoContent, nil)
	if _, err := os.Lstat(filepath.Join(mod, "dangling")); !os.IsNotExist(err) {
		t.Error("the dangling link was not deleted")
	}
}
