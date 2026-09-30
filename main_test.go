package main

import (
	"io/fs"
	"os"
	"sort"
	"testing"
)

// SK2: the binary carries every document the repository holds. The embed
// patterns in main.go are written by hand, so a new documentation directory —
// or a pattern that stops matching — would otherwise go unnoticed until an
// agent asks for a topic that is not there.
func TestDocs_SK2_EmbedMatchesRepository(t *testing.T) {
	for _, dir := range []string{"docs/guide", "docs/reference", "specs"} {
		embedded := markdownFiles(t, docs, dir)
		onDisk := markdownFiles(t, os.DirFS("."), dir)

		if len(onDisk) == 0 {
			t.Fatalf("%s: no markdown files found on disk", dir)
		}
		if len(embedded) != len(onDisk) {
			t.Errorf("%s: %d files embedded, %d on disk", dir, len(embedded), len(onDisk))
		}
		for i := range onDisk {
			if i < len(embedded) && embedded[i] != onDisk[i] {
				t.Errorf("%s: embedded %q, on disk %q", dir, embedded[i], onDisk[i])
			}
		}
	}
}

func markdownFiles(t *testing.T, fsys fs.FS, dir string) []string {
	t.Helper()
	matches, err := fs.Glob(fsys, dir+"/*.md")
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(matches)
	return matches
}
