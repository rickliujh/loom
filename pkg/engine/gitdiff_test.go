package engine

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/rickliujh/loom/pkg/event"
)

// git runs a git command in dir, failing the test on error.
func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

// writeFiles writes path -> content under dir, creating parent directories.
func writeFiles(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for name, content := range files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// initRepo creates a repo on main with one commit holding files.
func initRepo(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	git(t, dir, "init", "-q", "-b", "main")
	git(t, dir, "config", "user.email", "t@t")
	git(t, dir, "config", "user.name", "T")
	writeFiles(t, dir, files)
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-qm", "init")
	return dir
}

// cloneInto clones upstream as local mode would: single branch, into a
// numbered dir of workspace.
func cloneInto(t *testing.T, upstream, workspace, name string) string {
	t.Helper()
	dir := filepath.Join(workspace, name)
	if out, err := exec.Command("git", "clone", "-q", "--branch", "main", "--single-branch", upstream, dir).CombinedOutput(); err != nil {
		t.Fatalf("clone: %v\n%s", err, out)
	}
	git(t, dir, "config", "user.email", "t@t")
	git(t, dir, "config", "user.name", "T")
	return dir
}

func fileByPath(files []FileDiff, path string) (FileDiff, bool) {
	for _, f := range files {
		if f.Path == path {
			return f, true
		}
	}
	return FileDiff{}, false
}

// Every status full mode can report, read from a real clone — including paths
// with spaces, which git leaves unquoted in its headers, and a committed
// change, since the diff is against the base branch, not the index.
func TestCollectTargetDiffs_Statuses(t *testing.T) {
	upstream := initRepo(t, map[string]string{
		"README.md":      "readme\n",
		"old.txt":        "old\n",
		"a.txt":          "a\nb\nc\nd\ne\nf\n",
		"bin.dat":        "\x00\x01\x02bin",
		"with space.txt": "spaced\n",
		"dir/keep.txt":   "keep\n",
	})
	ws := t.TempDir()
	clone := cloneInto(t, upstream, ws, "00-demo")

	git(t, clone, "checkout", "-q", "-b", "loom/change")
	git(t, clone, "rm", "-q", "old.txt")
	git(t, clone, "mv", "a.txt", "b.txt")
	writeFiles(t, clone, map[string]string{
		"b.txt":          "a\nb\nc\nd\ne\nf\ng\n",
		"bin.dat":        "\x00\x01\x02bin\x03",
		"with space.txt": "spaced\nx\n",
		"new file.txt":   "new\n",
	})
	// A committed change must show as well: local mode commits.
	git(t, clone, "add", "-A")
	git(t, clone, "commit", "-qm", "work")
	writeFiles(t, clone, map[string]string{"README.md": "readme\nmore\n"})

	labels := map[string][]string{clone: {"root", "item"}}
	targets, err := CollectTargetDiffs(context.Background(), ws, CollectOptions{Labels: labels})
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 1 {
		t.Fatalf("got %d targets, want 1", len(targets))
	}
	tgt := targets[0]
	if !slices.Equal(tgt.Path, []string{"root", "item"}) {
		t.Errorf("Path = %v, want the recorded breadcrumb", tgt.Path)
	}
	if tgt.Repo != upstream || tgt.Branch != "main" || tgt.Base != "refs/remotes/origin/main" {
		t.Errorf("identity = %q %q %q", tgt.Repo, tgt.Branch, tgt.Base)
	}
	if tgt.Label != upstream+" (main)" {
		t.Errorf("Label = %q", tgt.Label)
	}
	if tgt.Raw != "" {
		t.Errorf("Raw = %q without raw requested", tgt.Raw)
	}

	want := []FileDiff{
		{Path: "README.md", Status: event.StatusModified, Unified: "@@ -1 +1,2 @@\n readme\n+more\n"},
		{Path: "b.txt", OldPath: "a.txt", Status: event.StatusRenamed, Unified: "@@ -4,3 +4,4 @@ c\n d\n e\n f\n+g\n"},
		{Path: "bin.dat", Status: event.StatusModified, Binary: true},
		{Path: "new file.txt", Status: event.StatusAdded, Unified: "@@ -0,0 +1 @@\n+new\n"},
		{Path: "old.txt", Status: event.StatusDeleted, Unified: "@@ -1 +0,0 @@\n-old\n"},
		{Path: "with space.txt", Status: event.StatusModified, Unified: "@@ -1 +1,2 @@\n spaced\n+x\n"},
	}
	if !slices.Equal(tgt.Files, want) {
		t.Errorf("files:\n got %+v\nwant %+v", tgt.Files, want)
	}
}

// User git config must not change what is parsed: no prefixes, mnemonic
// prefixes, quoted paths, disabled rename detection and an external diff
// driver would each break a naive reader of `git diff`.
func TestCollectTargetDiffs_IgnoresUserConfig(t *testing.T) {
	upstream := initRepo(t, map[string]string{"a.txt": "1\n2\n3\n4\n5\n", "ü.txt": "x\n"})
	ws := t.TempDir()
	clone := cloneInto(t, upstream, ws, "00-demo")
	for _, kv := range [][2]string{
		{"diff.noprefix", "true"},
		{"diff.mnemonicPrefix", "true"},
		{"core.quotepath", "true"},
		{"diff.renames", "false"},
		{"diff.external", "false"},
		{"color.diff", "always"},
	} {
		git(t, clone, "config", kv[0], kv[1])
	}
	git(t, clone, "mv", "a.txt", "renamed.txt")
	writeFiles(t, clone, map[string]string{"ü.txt": "y\n"})

	targets, err := CollectTargetDiffs(context.Background(), ws, CollectOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 1 {
		t.Fatalf("got %d targets, want 1", len(targets))
	}
	// No recorded label: the module name comes from the numbered dir.
	if !slices.Equal(targets[0].Path, []string{"demo"}) {
		t.Errorf("Path = %v, want [demo]", targets[0].Path)
	}
	want := []FileDiff{
		{Path: "renamed.txt", OldPath: "a.txt", Status: event.StatusRenamed},
		{Path: "ü.txt", Status: event.StatusModified, Unified: "@@ -1 +1 @@\n-x\n+y\n"},
	}
	if !slices.Equal(targets[0].Files, want) {
		t.Errorf("files:\n got %+v\nwant %+v", targets[0].Files, want)
	}
}

// Raw keeps git's own output — the bytes `loom diff` prints — alongside the
// parsed files, and an unchanged clone is not a target.
func TestCollectTargetDiffs_RawAndUnchanged(t *testing.T) {
	upstream := initRepo(t, map[string]string{"f.txt": "a\n"})
	ws := t.TempDir()
	changed := cloneInto(t, upstream, ws, "00-changed")
	cloneInto(t, upstream, ws, "01-untouched")
	writeFiles(t, changed, map[string]string{"f.txt": "b\n"})

	targets, err := CollectTargetDiffs(context.Background(), ws, CollectOptions{Raw: true, Files: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 1 || targets[0].Dir != changed {
		t.Fatalf("targets = %+v, want only the changed clone", targets)
	}
	raw := targets[0].Raw
	if !strings.HasPrefix(raw, "diff --git a/f.txt b/f.txt\n") || !strings.Contains(raw, "-a\n+b\n") {
		t.Errorf("Raw is not git's diff:\n%s", raw)
	}
	if strings.Contains(raw, "\033[") {
		t.Errorf("Raw is coloured without Color:\n%q", raw)
	}
	if len(targets[0].Files) != 1 || targets[0].Files[0].Unified != "@@ -1 +1 @@\n-a\n+b\n" {
		t.Errorf("Files = %+v", targets[0].Files)
	}

	targets, err = CollectTargetDiffs(context.Background(), ws, CollectOptions{Raw: true, Color: true, Files: true})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(targets[0].Raw, "\033[") {
		t.Errorf("Raw is not coloured with Color:\n%q", targets[0].Raw)
	}
}

// Paths git has to C-quote — a tab, a quote, a backslash — come back as the
// real file names.
func TestCollectTargetDiffs_QuotedPaths(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("these file names are not valid on Windows")
	}
	upstream := initRepo(t, map[string]string{"keep.txt": "k\n"})
	ws := t.TempDir()
	clone := cloneInto(t, upstream, ws, "00-demo")
	names := []string{"tab\there.txt", `quo"te.txt`, `back\slash.txt`}
	for _, n := range names {
		writeFiles(t, clone, map[string]string{n: "x\n"})
	}
	git(t, clone, "add", "-A")
	git(t, clone, "commit", "-qm", "add")
	git(t, clone, "mv", "tab\there.txt", "moved\tfile.txt")

	targets, err := CollectTargetDiffs(context.Background(), ws, CollectOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, f := range targets[0].Files {
		got = append(got, f.Status+" "+f.Path)
		if f.Status == event.StatusAdded && f.Unified != "@@ -0,0 +1 @@\n+x\n" {
			t.Errorf("%q: Unified = %q", f.Path, f.Unified)
		}
	}
	want := []string{"added " + `back\slash.txt`, "added moved\tfile.txt", "added " + `quo"te.txt`}
	if !slices.Equal(got, want) {
		t.Errorf("files = %q, want %q", got, want)
	}
}

func TestParseGitDiff_Headers(t *testing.T) {
	for _, tc := range []struct {
		name, out string
		want      FileDiff
	}{
		{
			name: "path containing the other side's prefix",
			out:  "diff --git a/x b/y b/x b/y\nindex 1..2 100644\n--- a/x b/y\n+++ b/x b/y\n@@ -1 +1 @@\n-1\n+2\n",
			want: FileDiff{Path: "x b/y", Status: event.StatusModified, Unified: "@@ -1 +1 @@\n-1\n+2\n"},
		},
		{
			name: "rename with spaces",
			out:  "diff --git a/one two b/three four\nsimilarity index 100%\nrename from one two\nrename to three four\n",
			want: FileDiff{Path: "three four", OldPath: "one two", Status: event.StatusRenamed},
		},
		{
			name: "quoted octal (quotepath on)",
			out:  "diff --git \"a/\\303\\274.txt\" \"b/\\303\\274.txt\"\nnew file mode 100644\n",
			want: FileDiff{Path: "ü.txt", Status: event.StatusAdded},
		},
		{
			name: "mode change only",
			out:  "diff --git a/run.sh b/run.sh\nold mode 100644\nnew mode 100755\n",
			want: FileDiff{Path: "run.sh", Status: event.StatusModified},
		},
		{
			name: "no newline at end of file",
			out:  "diff --git a/f b/f\n--- a/f\n+++ b/f\n@@ -1 +1 @@\n-a\n\\ No newline at end of file\n+b\n",
			want: FileDiff{Path: "f", Status: event.StatusModified, Unified: "@@ -1 +1 @@\n-a\n\\ No newline at end of file\n+b\n"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := ParseGitDiff(tc.out)
			if len(got) != 1 || got[0] != tc.want {
				t.Errorf("got %+v, want %+v", got, tc.want)
			}
		})
	}
	if got := ParseGitDiff(""); got != nil {
		t.Errorf("empty output parsed to %+v", got)
	}
}

// A caller that prints git's output and nothing else — the CLI — gets Raw
// alone: no parse, and one `git diff` per clone rather than two.
func TestCollectTargetDiffs_RawOnly(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the git wrapper is a shell script")
	}
	real, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not installed")
	}
	upstream := initRepo(t, map[string]string{"f.txt": "a\n"})
	ws := t.TempDir()
	changed := cloneInto(t, upstream, ws, "00-changed")
	writeFiles(t, changed, map[string]string{"f.txt": "b\n"})

	// A git that records its arguments, so the test can count diffs.
	bin := t.TempDir()
	calls := filepath.Join(t.TempDir(), "calls")
	script := "#!/bin/sh\necho \"$*\" >> '" + calls + "'\nexec '" + real + "' \"$@\"\n"
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	targets, err := CollectTargetDiffs(context.Background(), ws, CollectOptions{Raw: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 1 || targets[0].Files != nil || !strings.Contains(targets[0].Raw, "+b") {
		t.Fatalf("targets = %+v, want Raw only", targets)
	}
	log, err := os.ReadFile(calls)
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(log), "diff --cached"); n != 1 {
		t.Errorf("%d git diff runs, want 1:\n%s", n, log)
	}
}

// A dir that is not itself a repository — a clone that failed partway — is
// skipped rather than diffed, so git never falls back to the repository
// around it and stages someone's work there.
func TestCollectDirDiffs_SkipsNonRepos(t *testing.T) {
	outer := initRepo(t, map[string]string{"f.txt": "a\n"})
	writeFiles(t, outer, map[string]string{"f.txt": "changed\n"})
	failed := filepath.Join(outer, "00-failed")
	if err := os.MkdirAll(failed, 0o755); err != nil {
		t.Fatal(err)
	}
	targets, err := CollectDirDiffs(context.Background(), []string{failed}, CollectOptions{Files: true})
	if err != nil || len(targets) != 0 {
		t.Fatalf("targets = %+v, err %v", targets, err)
	}
	if st := git(t, outer, "status", "--porcelain"); !strings.Contains(st, " M f.txt") {
		t.Errorf("the surrounding repository was staged:\n%s", st)
	}
}
