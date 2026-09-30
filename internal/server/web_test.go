package server

// Static checks on the web UI in ./web. The UI has no build step, so these
// stand in for what a bundler and a linter would catch: a broken import, a
// file index.html points at that does not exist, and anything that would
// either break under the server's Content-Security-Policy or turn data into
// markup.

import (
	"io/fs"
	"os"
	"path"
	"regexp"
	"strings"
	"testing"
)

func webSourceFS(t *testing.T) fs.FS {
	t.Helper()
	if _, err := os.Stat("web/index.html"); err != nil {
		t.Fatalf("web/index.html: %v", err)
	}
	return os.DirFS("web")
}

// webFiles returns every file under web with the given extension.
func webFiles(t *testing.T, fsys fs.FS, ext string) []string {
	t.Helper()
	var out []string
	err := fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(p, ext) {
			out = append(out, p)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) == 0 {
		t.Fatalf("no %s files under web", ext)
	}
	return out
}

func read(t *testing.T, fsys fs.FS, p string) string {
	t.Helper()
	b, err := fs.ReadFile(fsys, p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// resolve maps a module specifier or URL path used in file `from` to a path
// in the web tree: "/x" is rooted at web, "./x" and "../x" are relative.
func resolve(from, spec string) (string, bool) {
	switch {
	case strings.HasPrefix(spec, "/"):
		return path.Clean(strings.TrimPrefix(spec, "/")), true
	case strings.HasPrefix(spec, "./"), strings.HasPrefix(spec, "../"):
		p := path.Clean(path.Join(path.Dir(from), spec))
		return p, !strings.HasPrefix(p, "../")
	default:
		return "", false
	}
}

var (
	// Static imports and re-exports, including multi-line import lists, and
	// dynamic import("…").
	importRe = regexp.MustCompile(`(?s)(?:^|[;\n])\s*(?:import|export)\s+(?:[\w*{}\s,$]*?\s+from\s+)?['"]([^'"\n]+)['"]`)
	dynImpRe = regexp.MustCompile(`\bimport\(\s*['"]([^'"\n]+)['"]\s*\)`)
)

func TestWeb_ImportsResolve(t *testing.T) {
	fsys := webSourceFS(t)
	for _, f := range webFiles(t, fsys, ".js") {
		src := read(t, fsys, f)
		var specs []string
		for _, m := range importRe.FindAllStringSubmatch(src, -1) {
			specs = append(specs, m[1])
		}
		for _, m := range dynImpRe.FindAllStringSubmatch(src, -1) {
			specs = append(specs, m[1])
		}
		for _, spec := range specs {
			if strings.HasPrefix(spec, "node:") && strings.HasSuffix(f, ".test.js") {
				continue // node --test files import node's own modules
			}
			p, ok := resolve(f, spec)
			if !ok {
				t.Errorf("%s: import %q is neither relative nor rooted; the UI has no bundler or CDN", f, spec)
				continue
			}
			if _, err := fs.Stat(fsys, p); err != nil {
				t.Errorf("%s: import %q: %s does not exist", f, spec, p)
			}
		}
	}
}

// Sinks that turn a string into markup or code. The UI builds DOM from text
// nodes only (js/dom.js), and the CSP forbids eval.
var bannedJS = []string{"innerHTML", "outerHTML", "insertAdjacentHTML", "document.write", "eval(", "new Function", "setAttribute('style'", `setAttribute("style"`}

func TestWeb_NoMarkupOrCodeSinks(t *testing.T) {
	fsys := webSourceFS(t)
	for _, f := range webFiles(t, fsys, ".js") {
		src := read(t, fsys, f)
		for _, bad := range bannedJS {
			if strings.Contains(src, bad) {
				t.Errorf("%s contains %q", f, bad)
			}
		}
	}
}

var (
	scriptRe   = regexp.MustCompile(`(?is)<script\b([^>]*)>(.*?)</script>`)
	styleAttr  = regexp.MustCompile(`(?i)\sstyle\s*=`)
	eventAttr  = regexp.MustCompile(`(?i)<[^>]*\son[a-z]+\s*=`)
	refAttrRe  = regexp.MustCompile(`(?i)\s(?:src|href)\s*=\s*["']([^"']+)["']`)
	srcAttrRe  = regexp.MustCompile(`(?i)\ssrc\s*=`)
	externalRe = regexp.MustCompile(`(?i)^(?:https?:)?//`)
)

func TestWeb_IndexIsCSPClean(t *testing.T) {
	fsys := webSourceFS(t)
	html := read(t, fsys, "index.html")

	for _, m := range scriptRe.FindAllStringSubmatch(html, -1) {
		if !srcAttrRe.MatchString(m[1]) {
			t.Errorf("index.html: <script%s> has no src; inline scripts are blocked by the CSP", m[1])
		}
		if strings.TrimSpace(m[2]) != "" {
			t.Errorf("index.html: a <script> has an inline body: %.60q", m[2])
		}
	}
	if styleAttr.MatchString(html) {
		t.Error("index.html has a style= attribute; inline styles are blocked by the CSP")
	}
	if strings.Contains(strings.ToLower(html), "<style") {
		t.Error("index.html has a <style> element; inline styles are blocked by the CSP")
	}
	if m := eventAttr.FindString(html); m != "" {
		t.Errorf("index.html has an inline event handler: %.60q", m)
	}
}

func TestWeb_IndexReferencesExist(t *testing.T) {
	fsys := webSourceFS(t)
	html := read(t, fsys, "index.html")
	seenJS, seenCSS := false, false
	for _, m := range refAttrRe.FindAllStringSubmatch(html, -1) {
		ref := m[1]
		if strings.HasPrefix(ref, "#") || strings.HasPrefix(ref, "data:") {
			continue
		}
		if externalRe.MatchString(ref) {
			t.Errorf("index.html references %s; the UI loads nothing from outside the server", ref)
			continue
		}
		if !strings.HasSuffix(ref, ".js") && !strings.HasSuffix(ref, ".css") && !strings.HasSuffix(ref, ".svg") {
			continue
		}
		seenJS = seenJS || strings.HasSuffix(ref, ".js")
		seenCSS = seenCSS || strings.HasSuffix(ref, ".css")
		p, ok := resolve("index.html", ref)
		if !ok {
			t.Errorf("index.html: %q is neither relative nor rooted", ref)
			continue
		}
		if _, err := fs.Stat(fsys, p); err != nil {
			t.Errorf("index.html references %s, which does not exist", ref)
		}
	}
	if !seenJS || !seenCSS {
		t.Errorf("index.html should load a script and a stylesheet (js: %v, css: %v)", seenJS, seenCSS)
	}
}

func TestWeb_CSSLoadsNothingExternal(t *testing.T) {
	fsys := webSourceFS(t)
	urlRe := regexp.MustCompile(`url\(\s*['"]?([^'")]+)`)
	for _, f := range webFiles(t, fsys, ".css") {
		src := read(t, fsys, f)
		if strings.Contains(src, "@import") {
			t.Errorf("%s uses @import; keep the stylesheet in one file", f)
		}
		for _, m := range urlRe.FindAllStringSubmatch(src, -1) {
			if externalRe.MatchString(m[1]) {
				t.Errorf("%s loads %s; fonts and images must come from the server", f, m[1])
			}
		}
	}
}
