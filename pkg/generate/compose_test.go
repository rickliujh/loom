package generate

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

func mustCompose(t *testing.T, sets []*ChangeSet) *ChangeSet {
	t.Helper()
	merged, err := Compose(sets, testLogger())
	if err != nil {
		t.Fatalf("Compose: %v", err)
	}
	return merged
}

func findFile(t *testing.T, cs *ChangeSet, path string) FileChange {
	t.Helper()
	for _, f := range cs.Files {
		if f.Path == path {
			return f
		}
	}
	t.Fatalf("file %q not found in composed changeset: %+v", path, cs.Files)
	return FileChange{}
}

// CS1: old content from the first source, new content from the last.
func TestCompose_CS1_ModifyThenModify(t *testing.T) {
	merged := mustCompose(t, []*ChangeSet{
		{Files: []FileChange{{Type: ChangeModified, Path: "f.yaml", OldContent: []byte("v: 1"), NewContent: []byte("v: 2")}}},
		{Files: []FileChange{{Type: ChangeModified, Path: "f.yaml", OldContent: []byte("v: 2"), NewContent: []byte("v: 3")}}},
	})

	if len(merged.Files) != 1 {
		t.Fatalf("expected 1 net change, got %d", len(merged.Files))
	}
	f := merged.Files[0]
	if f.Type != ChangeModified {
		t.Errorf("expected modified, got %v", f.Type)
	}
	if string(f.OldContent) != "v: 1" || string(f.NewContent) != "v: 3" {
		t.Errorf("expected v:1 → v:3, got %q → %q", f.OldContent, f.NewContent)
	}
}

// CS2: a mid-chain source whose old content disagrees with the accumulated
// state signals interleaved out-of-band commits — warn, don't block.
func TestCompose_CS2_WarnsOnDiscontinuity(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn}))

	merged, err := Compose([]*ChangeSet{
		{Files: []FileChange{{Type: ChangeModified, Path: "f.yaml", OldContent: []byte("v: 1"), NewContent: []byte("v: 2")}}},
		// Old content v:9 does not match the accumulated v:2.
		{Files: []FileChange{{Type: ChangeModified, Path: "f.yaml", OldContent: []byte("v: 9"), NewContent: []byte("v: 3")}}},
	}, logger)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(buf.String(), "changed between sources") {
		t.Errorf("expected continuity warning, got log: %s", buf.String())
	}
	// Still composes: last-wins on new content.
	f := merged.Files[0]
	if string(f.OldContent) != "v: 1" || string(f.NewContent) != "v: 3" {
		t.Errorf("expected v:1 → v:3, got %q → %q", f.OldContent, f.NewContent)
	}
}

func TestCompose_AddThenModify(t *testing.T) {
	merged := mustCompose(t, []*ChangeSet{
		{Files: []FileChange{{Type: ChangeAdded, Path: "f.yaml", NewContent: []byte("v: 1")}}},
		{Files: []FileChange{{Type: ChangeModified, Path: "f.yaml", OldContent: []byte("v: 1"), NewContent: []byte("v: 2")}}},
	})

	f := findFile(t, merged, "f.yaml")
	if f.Type != ChangeAdded {
		t.Errorf("add+modify should net to added, got %v", f.Type)
	}
	if string(f.NewContent) != "v: 2" {
		t.Errorf("expected latest content, got %q", f.NewContent)
	}
}

func TestCompose_AddThenDelete_Drops(t *testing.T) {
	merged := mustCompose(t, []*ChangeSet{
		{Files: []FileChange{
			{Type: ChangeAdded, Path: "temp.yaml", NewContent: []byte("t: 1")},
			{Type: ChangeAdded, Path: "keep.yaml", NewContent: []byte("k: 1")},
		}},
		{Files: []FileChange{{Type: ChangeDeleted, Path: "temp.yaml", OldContent: []byte("t: 1")}}},
	})

	if len(merged.Files) != 1 {
		t.Fatalf("expected only keep.yaml to survive, got %+v", merged.Files)
	}
	if merged.Files[0].Path != "keep.yaml" {
		t.Errorf("expected keep.yaml, got %q", merged.Files[0].Path)
	}
}

// Delete-then-re-add nets to modified so YAML stays SMP-able.
func TestCompose_DeleteThenAdd_NetsModified(t *testing.T) {
	merged := mustCompose(t, []*ChangeSet{
		{Files: []FileChange{{Type: ChangeDeleted, Path: "f.yaml", OldContent: []byte("v: 1")}}},
		{Files: []FileChange{{Type: ChangeAdded, Path: "f.yaml", NewContent: []byte("v: 2")}}},
	})

	f := findFile(t, merged, "f.yaml")
	if f.Type != ChangeModified {
		t.Errorf("delete+add should net to modified, got %v", f.Type)
	}
	if string(f.OldContent) != "v: 1" || string(f.NewContent) != "v: 2" {
		t.Errorf("expected v:1 → v:2, got %q → %q", f.OldContent, f.NewContent)
	}
}

func TestCompose_ModifyThenDelete(t *testing.T) {
	merged := mustCompose(t, []*ChangeSet{
		{Files: []FileChange{{Type: ChangeModified, Path: "f.yaml", OldContent: []byte("v: 1"), NewContent: []byte("v: 2")}}},
		{Files: []FileChange{{Type: ChangeDeleted, Path: "f.yaml", OldContent: []byte("v: 2")}}},
	})

	f := findFile(t, merged, "f.yaml")
	if f.Type != ChangeDeleted {
		t.Errorf("modify+delete should net to deleted, got %v", f.Type)
	}
}

// Rename chains collapse: a→b, edit b, b→c becomes a single a→c rename.
func TestCompose_RenameChain(t *testing.T) {
	merged := mustCompose(t, []*ChangeSet{
		{Files: []FileChange{{Type: ChangeRenamed, Path: "b.yaml", OldPath: "a.yaml", OldContent: []byte("v: 1"), NewContent: []byte("v: 1")}}},
		{Files: []FileChange{{Type: ChangeModified, Path: "b.yaml", OldContent: []byte("v: 1"), NewContent: []byte("v: 2")}}},
		{Files: []FileChange{{Type: ChangeRenamed, Path: "c.yaml", OldPath: "b.yaml", OldContent: []byte("v: 2"), NewContent: []byte("v: 2")}}},
	})

	if len(merged.Files) != 1 {
		t.Fatalf("expected 1 net change, got %+v", merged.Files)
	}
	f := merged.Files[0]
	if f.Type != ChangeRenamed {
		t.Fatalf("expected renamed, got %v", f.Type)
	}
	if f.OldPath != "a.yaml" || f.Path != "c.yaml" {
		t.Errorf("expected a.yaml → c.yaml, got %q → %q", f.OldPath, f.Path)
	}
	if string(f.OldContent) != "v: 1" || string(f.NewContent) != "v: 2" {
		t.Errorf("expected v:1 → v:2, got %q → %q", f.OldContent, f.NewContent)
	}
}

// Rename back to the original name with original content cancels out.
func TestCompose_RenameRoundTrip_Drops(t *testing.T) {
	content := []byte("v: 1")
	_, err := Compose([]*ChangeSet{
		{Files: []FileChange{{Type: ChangeRenamed, Path: "b.yaml", OldPath: "a.yaml", OldContent: content, NewContent: content}}},
		{Files: []FileChange{{Type: ChangeRenamed, Path: "a.yaml", OldPath: "b.yaml", OldContent: content, NewContent: content}}},
	}, testLogger())

	// CS5: nothing left.
	if err == nil || !strings.Contains(err.Error(), "no net file changes") {
		t.Errorf("expected no-net-changes error, got %v", err)
	}
}

// CS5: a PR and its revert cancel out.
func TestCompose_CS5_RevertCancelsOut(t *testing.T) {
	_, err := Compose([]*ChangeSet{
		{Files: []FileChange{{Type: ChangeModified, Path: "f.yaml", OldContent: []byte("v: 1"), NewContent: []byte("v: 2")}}},
		{Files: []FileChange{{Type: ChangeModified, Path: "f.yaml", OldContent: []byte("v: 2"), NewContent: []byte("v: 1")}}},
	}, testLogger())

	if err == nil || !strings.Contains(err.Error(), "no net file changes") {
		t.Errorf("expected no-net-changes error, got %v", err)
	}
}

// CS3: sources must reference the same repository; spelling may differ.
func TestCompose_CS3_RepoMismatch(t *testing.T) {
	files := []FileChange{{Type: ChangeAdded, Path: "f.yaml", NewContent: []byte("x: 1")}}
	_, err := Compose([]*ChangeSet{
		{RepoURL: "https://github.com/org/repo.git", Files: files},
		{RepoURL: "git@github.com:org/other.git", Files: files},
	}, testLogger())

	if err == nil || !strings.Contains(err.Error(), "different repositories") {
		t.Errorf("expected repo mismatch error, got %v", err)
	}
}

func TestCompose_RepoSpellingVariantsMatch(t *testing.T) {
	merged := mustCompose(t, []*ChangeSet{
		{RepoURL: "https://github.com/org/repo.git", Files: []FileChange{{Type: ChangeAdded, Path: "a.yaml", NewContent: []byte("a: 1")}}},
		{RepoURL: "git@github.com:org/repo.git", Files: []FileChange{{Type: ChangeAdded, Path: "b.yaml", NewContent: []byte("b: 1")}}},
	})
	if len(merged.Files) != 2 {
		t.Errorf("expected 2 files, got %d", len(merged.Files))
	}
}

// CS4: metadata comes from the last source that has it.
func TestCompose_CS4_MetadataFromLastSource(t *testing.T) {
	merged := mustCompose(t, []*ChangeSet{
		{
			Title: "first PR", Body: "first body", BaseBranch: "main", HeadBranch: "feat/one",
			RepoURL: "https://github.com/o/r.git", Provider: "github",
			Files: []FileChange{{Type: ChangeAdded, Path: "a.yaml", NewContent: []byte("a: 1")}},
		},
		{
			Title: "follow-up fix", // no HeadBranch (e.g. a commit source)
			Files: []FileChange{{Type: ChangeAdded, Path: "b.yaml", NewContent: []byte("b: 1")}},
		},
	})

	if merged.Title != "follow-up fix" {
		t.Errorf("expected last title, got %q", merged.Title)
	}
	if merged.Body != "first body" {
		t.Errorf("expected first body to survive (last source has none), got %q", merged.Body)
	}
	if merged.HeadBranch != "feat/one" {
		t.Errorf("expected head branch from last PR source, got %q", merged.HeadBranch)
	}
	if merged.RepoURL != "https://github.com/o/r.git" || merged.Provider != "github" {
		t.Errorf("expected repo metadata preserved, got %q / %q", merged.RepoURL, merged.Provider)
	}
}

func TestCompose_SinglePassthrough(t *testing.T) {
	cs := &ChangeSet{
		Title: "only",
		Files: []FileChange{{Type: ChangeAdded, Path: "f.yaml", NewContent: []byte("x: 1")}},
	}
	merged := mustCompose(t, []*ChangeSet{cs})
	if merged != cs {
		t.Error("single changeset should pass through unchanged")
	}
}

func TestCompose_UnrelatedFilesInFirstTouchOrder(t *testing.T) {
	merged := mustCompose(t, []*ChangeSet{
		{Files: []FileChange{{Type: ChangeAdded, Path: "one.yaml", NewContent: []byte("1")}}},
		{Files: []FileChange{{Type: ChangeAdded, Path: "two.yaml", NewContent: []byte("2")}}},
	})
	if len(merged.Files) != 2 {
		t.Fatalf("expected 2 files, got %d", len(merged.Files))
	}
	if merged.Files[0].Path != "one.yaml" || merged.Files[1].Path != "two.yaml" {
		t.Errorf("expected first-touch order, got %+v", merged.Files)
	}
}

func TestNormalizeRepoURL(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"https://github.com/org/repo.git", "github.com/org/repo"},
		{"git@github.com:org/repo.git", "github.com/org/repo"},
		{"ssh://git@github.com/org/repo.git", "github.com/org/repo"},
		{"HTTPS://GitHub.com/Org/Repo", "github.com/org/repo"},
		{"https://gitlab.example.com/group/sub/repo.git", "gitlab.example.com/group/sub/repo"},
	}
	for _, tt := range tests {
		if got := normalizeRepoURL(tt.in); got != tt.want {
			t.Errorf("normalizeRepoURL(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
