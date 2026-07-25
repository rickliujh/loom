package module

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSplitSourceURL_NoSeparator(t *testing.T) {
	repo, sub := splitSourceURL("https://github.com/org/repo.git")
	if repo != "https://github.com/org/repo.git" || sub != "" {
		t.Errorf("got repo=%q sub=%q", repo, sub)
	}
}

func TestSplitSourceURL_HTTPSWithSubdir(t *testing.T) {
	repo, sub := splitSourceURL("https://github.com/org/repo.git//path/to/mod")
	if repo != "https://github.com/org/repo.git" || sub != "path/to/mod" {
		t.Errorf("got repo=%q sub=%q", repo, sub)
	}
}

func TestSplitSourceURL_SSHColonStyle(t *testing.T) {
	repo, sub := splitSourceURL("git@github.com:org/repo.git//sub")
	if repo != "git@github.com:org/repo.git" || sub != "sub" {
		t.Errorf("got repo=%q sub=%q", repo, sub)
	}
}

func TestSplitSourceURL_SSHSchemeStyle(t *testing.T) {
	repo, sub := splitSourceURL("ssh://git@github.com/org/repo.git//sub/dir")
	if repo != "ssh://git@github.com/org/repo.git" || sub != "sub/dir" {
		t.Errorf("got repo=%q sub=%q", repo, sub)
	}
}

func TestSplitSourceURL_SchemeNotMatched(t *testing.T) {
	// "https://" should not be treated as separator
	repo, sub := splitSourceURL("https://github.com/org/repo.git")
	if sub != "" {
		t.Errorf("scheme matched as separator: repo=%q sub=%q", repo, sub)
	}
}

func TestSplitSourceRef_M5_NoRef(t *testing.T) {
	base, ref := splitSourceRef("https://github.com/org/repo.git//mod")
	if base != "https://github.com/org/repo.git//mod" || ref != "" {
		t.Errorf("got base=%q ref=%q", base, ref)
	}
}

func TestSplitSourceRef_M5_TagAfterSubdir(t *testing.T) {
	// The ref query is the last component, following any //subdir.
	base, ref := splitSourceRef("https://github.com/org/repo.git//mod?ref=v1.2.0")
	if base != "https://github.com/org/repo.git//mod" || ref != "v1.2.0" {
		t.Errorf("got base=%q ref=%q", base, ref)
	}
}

func TestSplitSourceRef_M5_CommitSSH(t *testing.T) {
	base, ref := splitSourceRef("git@github.com:org/repo.git?ref=abc1234")
	if base != "git@github.com:org/repo.git" || ref != "abc1234" {
		t.Errorf("got base=%q ref=%q", base, ref)
	}
}

func TestResolveSource_M5_LocalRefSuffixRejected(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "child"), 0o755)

	// ?ref= suffix on a local source.
	_, _, err := ResolveSource("./child?ref=v1", "", dir, testLogger())
	if err == nil {
		t.Fatal("expected error: version pinning is not supported for local sources")
	}
}

func TestResolveSource_M5_LocalVersionFieldRejected(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "child"), 0o755)

	// Explicit version argument (spec.modules[].version) on a local source.
	_, _, err := ResolveSource("./child", "v1", dir, testLogger())
	if err == nil {
		t.Fatal("expected error: version pinning is not supported for local sources")
	}
}

func TestResolveSource_LocalRelative(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "child")
	os.MkdirAll(sub, 0o755)

	resolved, cleanup, err := ResolveSource("./child", "", dir, testLogger())
	if err != nil {
		t.Fatal(err)
	}
	if cleanup != nil {
		t.Error("local source should have nil cleanup")
	}
	if resolved != sub {
		t.Errorf("expected %q, got %q", sub, resolved)
	}
}

func TestResolveSource_LocalAbsolute(t *testing.T) {
	dir := t.TempDir()

	resolved, cleanup, err := ResolveSource(dir, "", "/ignored", testLogger())
	if err != nil {
		t.Fatal(err)
	}
	if cleanup != nil {
		t.Error("local source should have nil cleanup")
	}
	if resolved != dir {
		t.Errorf("expected %q, got %q", dir, resolved)
	}
}

func TestResolveSource_LocalNotDir(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "file.txt")
	os.WriteFile(f, []byte("hi"), 0o644)

	_, _, err := ResolveSource(f, "", "/", testLogger())
	if err == nil {
		t.Fatal("expected error for non-directory source")
	}
}
