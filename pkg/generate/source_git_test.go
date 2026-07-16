package generate

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// fixtureRepo drives a throwaway git repository for source tests.
type fixtureRepo struct {
	t   *testing.T
	dir string
}

func newFixtureRepo(t *testing.T) *fixtureRepo {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git CLI not available")
	}
	r := &fixtureRepo{t: t, dir: t.TempDir()}
	r.git("init", "-b", "main")
	r.git("config", "user.email", "test@example.com")
	r.git("config", "user.name", "Test")
	r.git("config", "commit.gpgsign", "false")
	return r
}

func (r *fixtureRepo) git(args ...string) string {
	r.t.Helper()
	out, err := gitOut(context.Background(), r.dir, args...)
	if err != nil {
		r.t.Fatalf("fixture: %v", err)
	}
	return strings.TrimSpace(string(out))
}

func (r *fixtureRepo) write(rel, content string) {
	r.t.Helper()
	path := filepath.Join(r.dir, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		r.t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		r.t.Fatal(err)
	}
}

func (r *fixtureRepo) commit(message string) string {
	r.t.Helper()
	r.git("add", "-A")
	r.git("commit", "-m", message)
	return r.git("rev-parse", "HEAD")
}

// seedHistory builds three commits and returns their SHAs:
//
//	c1: base.yaml (replicas: 1), app.json
//	c2: base.yaml → replicas: 3, +new.yaml, -app.json   (has a body)
//	c3: new.yaml renamed to renamed.yaml
func seedHistory(r *fixtureRepo) (c1, c2, c3 string) {
	r.write("config/base.yaml", "name: app\nreplicas: 1\n")
	r.write("app.json", `{"a":1}`)
	c1 = r.commit("initial commit")

	r.write("config/base.yaml", "name: app\nreplicas: 3\n")
	r.write("config/new.yaml", "fresh: true\n")
	r.git("rm", "-q", "app.json")
	c2 = r.commit("scale app to three replicas\n\nthe body text")

	r.git("mv", "config/new.yaml", "config/renamed.yaml")
	c3 = r.commit("rename new to renamed")
	return c1, c2, c3
}

func fileByPath(t *testing.T, files []FileChange, path string) FileChange {
	t.Helper()
	for _, f := range files {
		if f.Path == path {
			return f
		}
	}
	t.Fatalf("file %q not found in %+v", path, files)
	return FileChange{}
}

// --- CommitSource ---

func TestCommitSource_SingleCommit(t *testing.T) {
	r := newFixtureRepo(t)
	_, c2, _ := seedHistory(r)

	src := &CommitSource{LocalPath: r.dir, HeadRev: c2}
	cs, err := src.Fetch(context.Background(), "", "", testLogger())
	if err != nil {
		t.Fatal(err)
	}

	if cs.Title != "scale app to three replicas" {
		t.Errorf("title = %q", cs.Title)
	}
	if cs.Body != "the body text" {
		t.Errorf("body = %q", cs.Body)
	}
	if cs.BaseBranch != "main" {
		t.Errorf("baseBranch = %q, want main", cs.BaseBranch)
	}
	if len(cs.Files) != 3 {
		t.Fatalf("expected 3 file changes, got %+v", cs.Files)
	}

	mod := fileByPath(t, cs.Files, "config/base.yaml")
	if mod.Type != ChangeModified {
		t.Errorf("base.yaml type = %v, want modified", mod.Type)
	}
	if !strings.Contains(string(mod.OldContent), "replicas: 1") || !strings.Contains(string(mod.NewContent), "replicas: 3") {
		t.Errorf("base.yaml contents wrong: %q → %q", mod.OldContent, mod.NewContent)
	}

	added := fileByPath(t, cs.Files, "config/new.yaml")
	if added.Type != ChangeAdded || string(added.NewContent) != "fresh: true\n" {
		t.Errorf("new.yaml: %v %q", added.Type, added.NewContent)
	}

	deleted := fileByPath(t, cs.Files, "app.json")
	if deleted.Type != ChangeDeleted {
		t.Errorf("app.json type = %v, want deleted", deleted.Type)
	}
}

func TestCommitSource_Range(t *testing.T) {
	r := newFixtureRepo(t)
	c1, _, c3 := seedHistory(r)

	src := &CommitSource{LocalPath: r.dir, BaseRev: c1, HeadRev: c3}
	cs, err := src.Fetch(context.Background(), "", "", testLogger())
	if err != nil {
		t.Fatal(err)
	}

	// Across the whole range: base.yaml modified, renamed.yaml added
	// (new.yaml never existed at c1), app.json deleted.
	if len(cs.Files) != 3 {
		t.Fatalf("expected 3 file changes, got %+v", cs.Files)
	}
	if f := fileByPath(t, cs.Files, "config/renamed.yaml"); f.Type != ChangeAdded {
		t.Errorf("renamed.yaml type = %v, want added", f.Type)
	}
	// Range metadata comes from the head commit.
	if cs.Title != "rename new to renamed" {
		t.Errorf("title = %q", cs.Title)
	}
}

func TestCommitSource_Rename(t *testing.T) {
	r := newFixtureRepo(t)
	_, c2, c3 := seedHistory(r)

	src := &CommitSource{LocalPath: r.dir, BaseRev: c2, HeadRev: c3}
	cs, err := src.Fetch(context.Background(), "", "", testLogger())
	if err != nil {
		t.Fatal(err)
	}

	if len(cs.Files) != 1 {
		t.Fatalf("expected 1 file change, got %+v", cs.Files)
	}
	f := cs.Files[0]
	if f.Type != ChangeRenamed || f.OldPath != "config/new.yaml" || f.Path != "config/renamed.yaml" {
		t.Errorf("unexpected rename: %+v", f)
	}
}

func TestCommitSource_RemoteClone(t *testing.T) {
	r := newFixtureRepo(t)
	_, c2, _ := seedHistory(r)

	src := &CommitSource{RepoURL: "file://" + r.dir, HeadRev: c2}
	cs, err := src.Fetch(context.Background(), "", "", testLogger())
	if err != nil {
		t.Fatal(err)
	}
	if len(cs.Files) != 3 {
		t.Errorf("expected 3 file changes, got %+v", cs.Files)
	}
	if cs.RepoURL != "file://"+r.dir {
		t.Errorf("repoURL = %q", cs.RepoURL)
	}
	if cs.BaseBranch != "main" {
		t.Errorf("baseBranch = %q, want main", cs.BaseBranch)
	}
}

func TestCommitSource_BadRev(t *testing.T) {
	r := newFixtureRepo(t)
	seedHistory(r)

	src := &CommitSource{LocalPath: r.dir, HeadRev: "deadbeef1234567"}
	if _, err := src.Fetch(context.Background(), "", "", testLogger()); err == nil {
		t.Error("expected error for unresolvable rev")
	}
}

func TestCommitSource_NotARepo(t *testing.T) {
	src := &CommitSource{LocalPath: t.TempDir(), HeadRev: "abc1234"}
	if _, err := src.Fetch(context.Background(), "", "", testLogger()); err == nil {
		t.Error("expected error for non-repo path")
	}
}
