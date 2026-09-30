// Package docscheck guards the docs site against mistakes that only surface
// when the site is built, which happens after a change has already merged.
package docscheck

import (
	"bufio"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

// vPreRe matches the wrapper that tells the site's renderer to leave template
// braces alone.
var vPreRe = regexp.MustCompile(`<code v-pre>.*?</code>`)

// The docs site compiles every page as a Vue template, so "{{ … }}" in prose
// is evaluated as JavaScript — inline code included. A Go template expression
// is not valid JavaScript, and one of them fails the whole site build. Fenced
// code blocks are exempt: the renderer escapes those itself.
func TestDocs_TemplateBracesInProseAreEscaped(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate source file")
	}
	docs := filepath.Join(filepath.Dir(filepath.Dir(filepath.Dir(file))), "docs")

	pages := 0
	err := filepath.WalkDir(docs, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if name := d.Name(); name == "node_modules" || name == ".vitepress" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".md") {
			return nil
		}
		pages++
		rel, _ := filepath.Rel(filepath.Dir(docs), path)
		for _, line := range unescapedBraces(t, path) {
			t.Errorf("%s:%d: template braces in prose must be wrapped as <code v-pre>{{ … }}</code>, or the docs site build fails", rel, line)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if pages == 0 {
		t.Fatalf("no pages found under %s", docs)
	}
}

// unescapedBraces returns the line numbers in a page where "{{" appears
// outside both a fenced code block and a v-pre wrapper.
func unescapedBraces(t *testing.T, path string) []int {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	var lines []int
	var fence string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for n := 1; sc.Scan(); n++ {
		line := sc.Text()
		trimmed := strings.TrimLeft(line, " ")
		switch {
		case fence == "" && (strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~")):
			fence = trimmed[:3]
		case fence != "":
			if strings.HasPrefix(trimmed, fence) {
				fence = ""
			}
		case strings.Contains(vPreRe.ReplaceAllString(line, ""), "{{"):
			lines = append(lines, n)
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return lines
}
