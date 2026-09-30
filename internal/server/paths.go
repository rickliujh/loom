package server

import (
	"errors"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// moduleSource is a request's `source`, checked against the server's rules.
type moduleSource struct {
	// Raw is the source as the client sent it.
	Raw string
	// Local reports a local directory (an absolute path or file:// URL).
	Local bool
	// Dir is the canonical module directory of a local source.
	Dir string
	// URL is a remote source as given, //subdir included.
	URL string
}

// scpLike matches git's scp-style remote, "user@host:path".
var scpLike = regexp.MustCompile(`^[A-Za-z0-9._-]+@[A-Za-z0-9._-]+:`)

// schemeURL matches a URL with a scheme, "https://…", "ssh://…".
var schemeURL = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9+.-]*://`)

// resolveSource applies the source rules: a local path must be absolute,
// exist, and — unless AllowOutsideRoots — lie inside a root once symlinks are
// resolved; a relative path is refused, because the CLI would read a bare
// name as a git URL; a git URL is accepted unless NoRemoteModules. A file://
// URL is a local path.
func (s *Server) resolveSource(raw, field string) (*moduleSource, error) {
	if raw == "" {
		return nil, errInvalid(field, "%s is required", field)
	}
	if strings.HasPrefix(strings.ToLower(raw), "file://") {
		p, err := fileURLPath(raw)
		if err != nil {
			return nil, errInvalid(field, "%s: %v", field, err)
		}
		return s.resolveLocalSource(raw, p, field)
	}
	if filepath.IsAbs(raw) {
		return s.resolveLocalSource(raw, raw, field)
	}
	if schemeURL.MatchString(raw) || scpLike.MatchString(raw) {
		if s.cfg.NoRemoteModules {
			return nil, errForbidden("remote module sources are disabled on this server (--no-remote-modules)").withField(field)
		}
		return &moduleSource{Raw: raw, URL: raw}, nil
	}
	return nil, errInvalid(field, "%s must be an absolute path or a git URL", field)
}

// fileURLPath is the local directory a file:// source names, with a
// "//subdir" suffix joined on as the CLI would find it in the clone.
func fileURLPath(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	if u.Host != "" && u.Host != "localhost" {
		return "", errors.New("a file:// URL must name a local path")
	}
	p := u.Path
	if repo, sub, ok := strings.Cut(strings.TrimPrefix(p, "/"), "//"); ok {
		p = filepath.Join("/"+repo, sub)
	}
	if !filepath.IsAbs(p) {
		return "", errors.New("a file:// URL must name an absolute path")
	}
	return p, nil
}

func (s *Server) resolveLocalSource(raw, p, field string) (*moduleSource, error) {
	dir, err := filepath.EvalSymlinks(filepath.Clean(p))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, errNotFound("module directory %s does not exist", p).withField(field)
		}
		return nil, errInvalid(field, "%s: %v", field, err)
	}
	info, err := os.Stat(dir)
	if err != nil {
		return nil, errInvalid(field, "%s: %v", field, err)
	}
	if !info.IsDir() {
		return nil, errInvalid(field, "%s is not a directory", p)
	}
	if !s.cfg.AllowOutsideRoots && s.rootOf(dir) == "" {
		return nil, errForbidden("%s is outside the server's roots", p).withField(field)
	}
	return &moduleSource{Raw: raw, Local: true, Dir: dir}, nil
}

// rootOf is the root containing the canonical path p, or "" when none does.
func (s *Server) rootOf(p string) string {
	for _, r := range s.roots {
		if within(r, p) {
			return r
		}
	}
	return ""
}

// within reports whether the canonical path p is dir or lies beneath it.
func within(dir, p string) bool {
	rel, err := filepath.Rel(dir, p)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel))
}

// resolveOutput checks a path the server will write to — a run's
// targetPath, a generate or bulk output, a new module directory. It must be
// absolute and, once the symlinks of its existing part are resolved, lie
// inside a root or the server's workspace. The path itself need not exist.
func (s *Server) resolveOutput(p, field string) (string, error) {
	if p == "" {
		return "", errInvalid(field, "%s is required", field)
	}
	if !filepath.IsAbs(p) {
		return "", errInvalid(field, "%s must be an absolute path", field)
	}
	real, err := resolveExisting(filepath.Clean(p))
	if err != nil {
		return "", errInvalid(field, "%s: %v", field, err)
	}
	if s.rootOf(real) == "" && !within(s.workspaces, real) {
		return "", errForbidden("%s is outside the server's roots and workspace", p).withField(field)
	}
	return real, nil
}

// resolveExisting resolves the symlinks of p's longest existing prefix and
// joins the rest on, so a path that does not exist yet is judged by where
// it would actually be created.
func resolveExisting(p string) (string, error) {
	var rest []string
	cur := p
	for {
		real, err := filepath.EvalSymlinks(cur)
		if err == nil {
			return filepath.Join(append([]string{real}, rest...)...), nil
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return "", err
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return "", err
		}
		rest = append([]string{filepath.Base(cur)}, rest...)
		cur = parent
	}
}

// modulePath is a path inside a module directory, checked.
type modulePath struct {
	// Rel is the cleaned relative path, "/"-separated; "" is the module
	// directory itself.
	Rel string
	// Abs is the path on disk with symlinks in its existing part resolved.
	Abs string
	// Entry is the path with only its parent's symlinks resolved: the
	// directory entry itself, which delete and move act on so that removing
	// a symlink removes the link, not what it points to.
	Entry string
	// Exists reports whether Abs exists, following links: false for a
	// dangling symlink.
	Exists bool
}

// resolveModulePath confines a client-supplied path to the module directory:
// relative, no "..", nothing named .git, and — after symlinks are resolved —
// still inside the module.
func resolveModulePath(moduleDir, p, field string) (*modulePath, error) {
	if strings.Contains(p, "\\") {
		return nil, errInvalid(field, "%s must use / as the separator", field)
	}
	if strings.HasPrefix(p, "/") || filepath.IsAbs(p) {
		return nil, errInvalid(field, "%s must be relative to the module directory", field)
	}
	var parts []string
	for _, seg := range strings.Split(p, "/") {
		switch seg {
		case "", ".":
			continue
		case "..":
			return nil, errInvalid(field, "%s may not contain ..", field)
		}
		if strings.EqualFold(seg, ".git") {
			return nil, errNotFound("%s not found", p)
		}
		parts = append(parts, seg)
	}
	rel := strings.Join(parts, "/")
	joined := filepath.Join(moduleDir, filepath.FromSlash(rel))
	real, err := resolveExisting(joined)
	if err != nil {
		return nil, errInvalid(field, "%s: %v", field, err)
	}
	if !within(moduleDir, real) {
		return nil, errForbidden("%s leaves the module directory", p).withField(field)
	}
	// A symlink may land on .git from a harmless-looking name.
	if inGitDir(moduleDir, real) {
		return nil, errNotFound("%s not found", p)
	}
	entry := real
	if rel != "" {
		parent, err := resolveExisting(filepath.Dir(joined))
		if err != nil {
			return nil, errInvalid(field, "%s: %v", field, err)
		}
		entry = filepath.Join(parent, filepath.Base(joined))
		if !within(moduleDir, entry) || entry == moduleDir {
			return nil, errForbidden("%s leaves the module directory", p).withField(field)
		}
	}
	// Exists follows links: a dangling symlink names nothing to read.
	_, statErr := os.Stat(real)
	return &modulePath{Rel: rel, Abs: real, Entry: entry, Exists: statErr == nil}, nil
}
