package git

import (
	"context"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
}

// initBareRepo creates a bare git repo with an initial commit, suitable for
// cloning in tests. Returns the path to the bare repo.
func initBareRepo(t *testing.T) string {
	t.Helper()

	work := t.TempDir()
	for _, args := range [][]string{
		{"git", "init", work},
		{"git", "-C", work, "config", "user.email", "test@test.com"},
		{"git", "-C", work, "config", "user.name", "Test"},
	} {
		if out, err := exec.Command(args[0], args[1:]...).CombinedOutput(); err != nil {
			t.Fatalf("git command %v failed: %v\n%s", args, err, out)
		}
	}

	if err := os.WriteFile(filepath.Join(work, "README.md"), []byte("# test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"git", "-C", work, "add", "."},
		{"git", "-C", work, "commit", "-m", "init"},
	} {
		if out, err := exec.Command(args[0], args[1:]...).CombinedOutput(); err != nil {
			t.Fatalf("git command %v failed: %v\n%s", args, err, out)
		}
	}

	bare := t.TempDir()
	if out, err := exec.Command("git", "clone", "--bare", work, bare).CombinedOutput(); err != nil {
		t.Fatalf("bare clone failed: %v\n%s", err, out)
	}
	return bare
}

// gitCmd runs a git command in dir and returns the trimmed stdout.
func gitCmd(t *testing.T, dir string, args ...string) string {
	t.Helper()
	full := append([]string{"-C", dir}, args...)
	out, err := exec.Command("git", full...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// cliCloneRepo clones a bare repo using only the git CLI (gg=nil), simulating
// environments where go-git is unavailable or where go-git clone failed and
// the CLI fallback was used.
func cliCloneRepo(t *testing.T, bare, branch string) *Repo {
	t.Helper()
	dir := t.TempDir()
	ctx := context.Background()
	if err := cliClone(ctx, bare, dir, branch); err != nil {
		t.Fatal(err)
	}
	return &Repo{gg: nil, dir: dir, logger: testLogger()}
}

// stripGoGit returns a copy of the Repo with gg set to nil, forcing all
// subsequent operations through the CLI path.
func stripGoGit(r Repository) *Repo {
	real := r.(*Repo)
	return &Repo{gg: nil, dir: real.dir, logger: real.logger}
}

// ---------------------------------------------------------------------------
// Push on default branch (sanity check)
// ---------------------------------------------------------------------------

func TestPush_DefaultBranch(t *testing.T) {
	bare := initBareRepo(t)
	ctx := context.Background()
	logger := testLogger()

	cloneDir := t.TempDir()
	repo, err := Clone(ctx, bare, cloneDir, "", logger)
	if err != nil {
		t.Fatal(err)
	}

	// Make a change, commit, push.
	if err := os.WriteFile(filepath.Join(cloneDir, "file.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := repo.AddAll(); err != nil {
		t.Fatal(err)
	}
	if err := repo.Commit("add file", "Test", "test@test.com"); err != nil {
		t.Fatal(err)
	}
	if err := repo.Push(ctx, ""); err != nil {
		t.Fatalf("push on default branch failed: %v", err)
	}

	// Verify the commit arrived at the bare repo.
	log := gitCmd(t, bare, "log", "--oneline")
	if !strings.Contains(log, "add file") {
		t.Errorf("expected 'add file' commit in bare repo, got:\n%s", log)
	}
}

// ---------------------------------------------------------------------------
// Push on feature branch (single go-git instance)
// ---------------------------------------------------------------------------

func TestPush_FeatureBranch_SameInstance(t *testing.T) {
	bare := initBareRepo(t)
	ctx := context.Background()
	logger := testLogger()

	cloneDir := t.TempDir()
	repo, err := Clone(ctx, bare, cloneDir, "", logger)
	if err != nil {
		t.Fatal(err)
	}

	if err := repo.CreateBranch("feat/new-stuff"); err != nil {
		t.Fatal(err)
	}

	branch, err := repo.CurrentBranch()
	if err != nil {
		t.Fatal(err)
	}
	if branch != "feat/new-stuff" {
		t.Fatalf("expected branch feat/new-stuff, got %q", branch)
	}

	if err := os.WriteFile(filepath.Join(cloneDir, "feature.txt"), []byte("feature"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := repo.AddAll(); err != nil {
		t.Fatal(err)
	}
	if err := repo.Commit("feat commit", "Test", "test@test.com"); err != nil {
		t.Fatal(err)
	}
	if err := repo.Push(ctx, ""); err != nil {
		t.Fatalf("push on feature branch (same instance) failed: %v", err)
	}

	// Verify the feature branch exists on the bare remote.
	refs := gitCmd(t, bare, "branch")
	if !strings.Contains(refs, "feat/new-stuff") {
		t.Errorf("expected feat/new-stuff on remote, got:\n%s", refs)
	}
}

// ---------------------------------------------------------------------------
// Push on feature branch opened from a SECOND go-git instance.
// This reproduces the exact bug scenario: commitAndPush calls git.Open()
// which creates a new go-git instance, then commits and pushes through it.
// ---------------------------------------------------------------------------

func TestPush_FeatureBranch_SeparateInstance(t *testing.T) {
	bare := initBareRepo(t)
	ctx := context.Background()
	logger := testLogger()

	// --- Step 1: Clone + create feature branch (first go-git instance) ---
	cloneDir := t.TempDir()
	repo1, err := Clone(ctx, bare, cloneDir, "", logger)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo1.CreateBranch("feat/cross-instance"); err != nil {
		t.Fatal(err)
	}

	// Simulate shell operations modifying files on disk (not through go-git).
	if err := os.WriteFile(filepath.Join(cloneDir, "changed.txt"), []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}

	// --- Step 2: Open the same directory with a NEW go-git instance ---
	// This is what commitAndPush does via git.Open().
	repo2, err := Open(cloneDir, logger)
	if err != nil {
		t.Fatal(err)
	}

	// Verify the second instance sees the feature branch.
	branch, err := repo2.CurrentBranch()
	if err != nil {
		t.Fatal(err)
	}
	if branch != "feat/cross-instance" {
		t.Fatalf("second instance: expected branch feat/cross-instance, got %q", branch)
	}

	// --- Step 3: AddAll, Commit, Push from the second instance ---
	if err := repo2.AddAll(); err != nil {
		t.Fatal(err)
	}
	if err := repo2.Commit("cross-instance commit", "Test", "test@test.com"); err != nil {
		t.Fatal(err)
	}
	if err := repo2.Push(ctx, ""); err != nil {
		t.Fatalf("push from second go-git instance on feature branch failed: %v", err)
	}

	// Verify the feature branch arrived at the bare remote.
	refs := gitCmd(t, bare, "branch")
	if !strings.Contains(refs, "feat/cross-instance") {
		t.Errorf("expected feat/cross-instance on remote, got:\n%s", refs)
	}

	// Verify the commit content.
	log := gitCmd(t, bare, "log", "feat/cross-instance", "--oneline")
	if !strings.Contains(log, "cross-instance commit") {
		t.Errorf("expected commit on remote feature branch, got:\n%s", log)
	}
}

// ---------------------------------------------------------------------------
// Push on feature branch with nested path (slashes in name).
// Ensures refs/heads/feat/deep/name is handled correctly.
// ---------------------------------------------------------------------------

func TestPush_FeatureBranch_NestedPath(t *testing.T) {
	bare := initBareRepo(t)
	ctx := context.Background()
	logger := testLogger()

	cloneDir := t.TempDir()
	repo, err := Clone(ctx, bare, cloneDir, "", logger)
	if err != nil {
		t.Fatal(err)
	}

	if err := repo.CreateBranch("feat/team/deep/branch"); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(cloneDir, "deep.txt"), []byte("nested"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Open with a separate instance to reproduce the real flow.
	repo2, err := Open(cloneDir, logger)
	if err != nil {
		t.Fatal(err)
	}

	if err := repo2.AddAll(); err != nil {
		t.Fatal(err)
	}
	if err := repo2.Commit("deep branch commit", "Test", "test@test.com"); err != nil {
		t.Fatal(err)
	}
	if err := repo2.Push(ctx, ""); err != nil {
		t.Fatalf("push on deeply nested feature branch failed: %v", err)
	}

	refs := gitCmd(t, bare, "branch")
	if !strings.Contains(refs, "feat/team/deep/branch") {
		t.Errorf("expected feat/team/deep/branch on remote, got:\n%s", refs)
	}
}

// ---------------------------------------------------------------------------
// Push on feature branch cloned with --single-branch (specific base branch).
// Mirrors the real scenario where spec.target.branch is set.
// ---------------------------------------------------------------------------

func TestPush_FeatureBranch_SingleBranchClone(t *testing.T) {
	bare := initBareRepo(t)
	ctx := context.Background()
	logger := testLogger()

	// Create a "release" branch on the bare repo.
	work := t.TempDir()
	for _, args := range [][]string{
		{"git", "clone", bare, work},
		{"git", "-C", work, "config", "user.email", "test@test.com"},
		{"git", "-C", work, "config", "user.name", "Test"},
		{"git", "-C", work, "checkout", "-b", "release"},
		{"git", "-C", work, "commit", "--allow-empty", "-m", "release branch"},
		{"git", "-C", work, "push", "origin", "release"},
	} {
		if out, err := exec.Command(args[0], args[1:]...).CombinedOutput(); err != nil {
			t.Fatalf("git command %v failed: %v\n%s", args, err, out)
		}
	}

	// Clone only the "release" branch (single-branch clone).
	cloneDir := t.TempDir()
	repo, err := Clone(ctx, bare, cloneDir, "release", logger)
	if err != nil {
		t.Fatal(err)
	}

	if err := repo.CreateBranch("feat/from-release"); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(cloneDir, "release-feat.txt"), []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Second instance — the commitAndPush flow.
	repo2, err := Open(cloneDir, logger)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo2.AddAll(); err != nil {
		t.Fatal(err)
	}
	if err := repo2.Commit("feat on release", "Test", "test@test.com"); err != nil {
		t.Fatal(err)
	}
	if err := repo2.Push(ctx, ""); err != nil {
		t.Fatalf("push feature branch from single-branch clone failed: %v", err)
	}

	refs := gitCmd(t, bare, "branch")
	if !strings.Contains(refs, "feat/from-release") {
		t.Errorf("expected feat/from-release on remote, got:\n%s", refs)
	}
}

// ---------------------------------------------------------------------------
// End-to-end: full loom flow simulation.
// Clone → CreateBranch → shell modifies files → Open (new instance) →
// AddAll → Commit → Push. This is exactly what happens when a child module
// has spec.target with featureBranch and a commitPush operation.
// ---------------------------------------------------------------------------

func TestPush_FullLoomFlow_FeatureBranch(t *testing.T) {
	bare := initBareRepo(t)
	ctx := context.Background()
	logger := testLogger()

	// === resolveChildTarget phase ===
	cloneDir := t.TempDir()
	cloneRepo, err := Clone(ctx, bare, cloneDir, "", logger)
	if err != nil {
		t.Fatal(err)
	}
	if err := cloneRepo.CreateBranch("feat/f5-infra-update"); err != nil {
		t.Fatal(err)
	}

	// === operations phase (shell commands modify files) ===
	if err := os.WriteFile(filepath.Join(cloneDir, "terraform.tf"), []byte("resource {}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(cloneDir, "modules"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cloneDir, "modules", "main.tf"), []byte("module {}"), 0o644); err != nil {
		t.Fatal(err)
	}

	// === commitAndPush phase (new go-git instance via git.Open) ===
	pushRepo, err := Open(cloneDir, logger)
	if err != nil {
		t.Fatalf("Open (commitAndPush) failed: %v", err)
	}

	if err := pushRepo.AddAll(); err != nil {
		t.Fatalf("AddAll failed: %v", err)
	}
	if err := pushRepo.Commit("automated update by loom", "loom[bot]", "loom@bot.dev"); err != nil {
		t.Fatalf("Commit failed: %v", err)
	}
	if err := pushRepo.Push(ctx, ""); err != nil {
		t.Fatalf("Push failed (this is the bug!): %v", err)
	}

	// === Verify remote state ===
	refs := gitCmd(t, bare, "branch")
	if !strings.Contains(refs, "feat/f5-infra-update") {
		t.Errorf("expected feat/f5-infra-update on remote, got:\n%s", refs)
	}

	log := gitCmd(t, bare, "log", "feat/f5-infra-update", "--oneline")
	if !strings.Contains(log, "automated update by loom") {
		t.Errorf("expected commit message on remote, got:\n%s", log)
	}

	// Verify both files were pushed.
	files := gitCmd(t, bare, "ls-tree", "--name-only", "-r", "feat/f5-infra-update")
	for _, expected := range []string{"terraform.tf", "modules/main.tf", "README.md"} {
		if !strings.Contains(files, expected) {
			t.Errorf("expected %q in pushed tree, got:\n%s", expected, files)
		}
	}
}

// ===========================================================================
// Empty author/email: verify go-git is skipped and CLI fallback is used.
// When author or email is empty, go-git would create a commit with an empty
// author which remote servers (e.g. GitLab) reject. The fix ensures Commit()
// bypasses go-git and falls through to cliCommit, which omits -c user.name/
// user.email flags, letting git use the repo-local or global config.
// ===========================================================================

func TestCommit_EmptyAuthor_UsesGitConfig(t *testing.T) {
	bare := initBareRepo(t)
	ctx := context.Background()
	logger := testLogger()

	cloneDir := t.TempDir()
	repo, err := Clone(ctx, bare, cloneDir, "", logger)
	if err != nil {
		t.Fatal(err)
	}

	// Set git config in the clone so CLI commit has values to use.
	for _, args := range [][]string{
		{"git", "-C", cloneDir, "config", "user.name", "Config User"},
		{"git", "-C", cloneDir, "config", "user.email", "config@example.com"},
	} {
		if out, err := exec.Command(args[0], args[1:]...).CombinedOutput(); err != nil {
			t.Fatalf("git command %v failed: %v\n%s", args, err, out)
		}
	}

	if err := os.WriteFile(filepath.Join(cloneDir, "file.txt"), []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := repo.AddAll(); err != nil {
		t.Fatal(err)
	}

	// Commit with empty author and email — should use git config fallback.
	if err := repo.Commit("empty author commit", "", ""); err != nil {
		t.Fatalf("Commit with empty author/email failed: %v", err)
	}

	// Verify the commit used git config values, not empty strings.
	authorName := gitCmd(t, cloneDir, "log", "-1", "--format=%an")
	authorEmail := gitCmd(t, cloneDir, "log", "-1", "--format=%ae")

	if authorName != "Config User" {
		t.Errorf("expected author 'Config User', got %q", authorName)
	}
	if authorEmail != "config@example.com" {
		t.Errorf("expected email 'config@example.com', got %q", authorEmail)
	}
}

func TestCommit_EmptyAuthorOnly_UsesGitConfig(t *testing.T) {
	bare := initBareRepo(t)
	ctx := context.Background()
	logger := testLogger()

	cloneDir := t.TempDir()
	repo, err := Clone(ctx, bare, cloneDir, "", logger)
	if err != nil {
		t.Fatal(err)
	}

	for _, args := range [][]string{
		{"git", "-C", cloneDir, "config", "user.name", "Config User"},
		{"git", "-C", cloneDir, "config", "user.email", "config@example.com"},
	} {
		if out, err := exec.Command(args[0], args[1:]...).CombinedOutput(); err != nil {
			t.Fatalf("git command %v failed: %v\n%s", args, err, out)
		}
	}

	if err := os.WriteFile(filepath.Join(cloneDir, "file.txt"), []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := repo.AddAll(); err != nil {
		t.Fatal(err)
	}

	// Only author is empty, email is provided — should still fall through to CLI.
	if err := repo.Commit("partial empty commit", "", "provided@example.com"); err != nil {
		t.Fatalf("Commit with empty author failed: %v", err)
	}

	authorName := gitCmd(t, cloneDir, "log", "-1", "--format=%an")
	if authorName != "Config User" {
		t.Errorf("expected author from git config 'Config User', got %q", authorName)
	}
}

func TestCommit_EmptyEmailOnly_UsesGitConfig(t *testing.T) {
	bare := initBareRepo(t)
	ctx := context.Background()
	logger := testLogger()

	cloneDir := t.TempDir()
	repo, err := Clone(ctx, bare, cloneDir, "", logger)
	if err != nil {
		t.Fatal(err)
	}

	for _, args := range [][]string{
		{"git", "-C", cloneDir, "config", "user.name", "Config User"},
		{"git", "-C", cloneDir, "config", "user.email", "config@example.com"},
	} {
		if out, err := exec.Command(args[0], args[1:]...).CombinedOutput(); err != nil {
			t.Fatalf("git command %v failed: %v\n%s", args, err, out)
		}
	}

	if err := os.WriteFile(filepath.Join(cloneDir, "file.txt"), []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := repo.AddAll(); err != nil {
		t.Fatal(err)
	}

	// Only email is empty, author is provided — should still fall through to CLI.
	if err := repo.Commit("partial empty commit", "Provided Author", ""); err != nil {
		t.Fatalf("Commit with empty email failed: %v", err)
	}

	authorEmail := gitCmd(t, cloneDir, "log", "-1", "--format=%ae")
	if authorEmail != "config@example.com" {
		t.Errorf("expected email from git config 'config@example.com', got %q", authorEmail)
	}
}

func TestCommit_WithAuthorEmail_UsesProvidedValues(t *testing.T) {
	bare := initBareRepo(t)
	ctx := context.Background()
	logger := testLogger()

	cloneDir := t.TempDir()
	repo, err := Clone(ctx, bare, cloneDir, "", logger)
	if err != nil {
		t.Fatal(err)
	}

	// Set different git config to confirm it's NOT used when author/email are provided.
	for _, args := range [][]string{
		{"git", "-C", cloneDir, "config", "user.name", "Config User"},
		{"git", "-C", cloneDir, "config", "user.email", "config@example.com"},
	} {
		if out, err := exec.Command(args[0], args[1:]...).CombinedOutput(); err != nil {
			t.Fatalf("git command %v failed: %v\n%s", args, err, out)
		}
	}

	if err := os.WriteFile(filepath.Join(cloneDir, "file.txt"), []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := repo.AddAll(); err != nil {
		t.Fatal(err)
	}

	if err := repo.Commit("explicit author commit", "Explicit User", "explicit@example.com"); err != nil {
		t.Fatalf("Commit with explicit author/email failed: %v", err)
	}

	authorName := gitCmd(t, cloneDir, "log", "-1", "--format=%an")
	authorEmail := gitCmd(t, cloneDir, "log", "-1", "--format=%ae")

	if authorName != "Explicit User" {
		t.Errorf("expected author 'Explicit User', got %q", authorName)
	}
	if authorEmail != "explicit@example.com" {
		t.Errorf("expected email 'explicit@example.com', got %q", authorEmail)
	}
}

// ===========================================================================
// CLI-only tests (gg=nil): verify the pure git-CLI path works for push.
// These cover the fallback path that real users hit when go-git can't
// handle SSH auth, custom credential helpers, etc.
// ===========================================================================

func TestCLI_Push_DefaultBranch(t *testing.T) {
	bare := initBareRepo(t)
	ctx := context.Background()

	repo := cliCloneRepo(t, bare, "")

	if err := os.WriteFile(filepath.Join(repo.dir, "cli.txt"), []byte("cli"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := repo.AddAll(); err != nil {
		t.Fatal(err)
	}
	if err := repo.Commit("cli commit", "Test", "test@test.com"); err != nil {
		t.Fatal(err)
	}
	if err := repo.Push(ctx, ""); err != nil {
		t.Fatalf("CLI push on default branch failed: %v", err)
	}

	log := gitCmd(t, bare, "log", "--oneline")
	if !strings.Contains(log, "cli commit") {
		t.Errorf("expected 'cli commit' in bare repo, got:\n%s", log)
	}
}

func TestCLI_Push_FeatureBranch(t *testing.T) {
	bare := initBareRepo(t)
	ctx := context.Background()

	repo := cliCloneRepo(t, bare, "")

	if err := repo.CreateBranch("feat/cli-branch"); err != nil {
		t.Fatal(err)
	}

	branch, err := repo.CurrentBranch()
	if err != nil {
		t.Fatal(err)
	}
	if branch != "feat/cli-branch" {
		t.Fatalf("expected feat/cli-branch, got %q", branch)
	}

	if err := os.WriteFile(filepath.Join(repo.dir, "feat.txt"), []byte("feature"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := repo.AddAll(); err != nil {
		t.Fatal(err)
	}
	if err := repo.Commit("cli feat commit", "Test", "test@test.com"); err != nil {
		t.Fatal(err)
	}
	if err := repo.Push(ctx, ""); err != nil {
		t.Fatalf("CLI push on feature branch failed: %v", err)
	}

	refs := gitCmd(t, bare, "branch")
	if !strings.Contains(refs, "feat/cli-branch") {
		t.Errorf("expected feat/cli-branch on remote, got:\n%s", refs)
	}
}

func TestCLI_Push_FeatureBranch_NestedPath(t *testing.T) {
	bare := initBareRepo(t)
	ctx := context.Background()

	repo := cliCloneRepo(t, bare, "")

	if err := repo.CreateBranch("feat/team/deep/cli"); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(repo.dir, "deep.txt"), []byte("nested"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := repo.AddAll(); err != nil {
		t.Fatal(err)
	}
	if err := repo.Commit("deep cli commit", "Test", "test@test.com"); err != nil {
		t.Fatal(err)
	}
	if err := repo.Push(ctx, ""); err != nil {
		t.Fatalf("CLI push on nested feature branch failed: %v", err)
	}

	refs := gitCmd(t, bare, "branch")
	if !strings.Contains(refs, "feat/team/deep/cli") {
		t.Errorf("expected feat/team/deep/cli on remote, got:\n%s", refs)
	}
}

func TestCLI_Push_FeatureBranch_SingleBranchClone(t *testing.T) {
	bare := initBareRepo(t)
	ctx := context.Background()

	// Create a "release" branch on the bare repo.
	work := t.TempDir()
	for _, args := range [][]string{
		{"git", "clone", bare, work},
		{"git", "-C", work, "config", "user.email", "test@test.com"},
		{"git", "-C", work, "config", "user.name", "Test"},
		{"git", "-C", work, "checkout", "-b", "release"},
		{"git", "-C", work, "commit", "--allow-empty", "-m", "release branch"},
		{"git", "-C", work, "push", "origin", "release"},
	} {
		if out, err := exec.Command(args[0], args[1:]...).CombinedOutput(); err != nil {
			t.Fatalf("git command %v failed: %v\n%s", args, err, out)
		}
	}

	// CLI clone with single branch.
	repo := cliCloneRepo(t, bare, "release")

	if err := repo.CreateBranch("feat/cli-from-release"); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(repo.dir, "rel.txt"), []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := repo.AddAll(); err != nil {
		t.Fatal(err)
	}
	if err := repo.Commit("cli feat on release", "Test", "test@test.com"); err != nil {
		t.Fatal(err)
	}
	if err := repo.Push(ctx, ""); err != nil {
		t.Fatalf("CLI push feature branch from single-branch clone failed: %v", err)
	}

	refs := gitCmd(t, bare, "branch")
	if !strings.Contains(refs, "feat/cli-from-release") {
		t.Errorf("expected feat/cli-from-release on remote, got:\n%s", refs)
	}
}

// ---------------------------------------------------------------------------
// Cross-path: go-git Clone+CreateBranch, then CLI-only Commit+Push.
// This simulates the real-world scenario where go-git handles clone/branch
// but the push falls back to CLI due to SSH auth issues.
// ---------------------------------------------------------------------------

func TestCrossPath_GoGitClone_CLIPush_FeatureBranch(t *testing.T) {
	bare := initBareRepo(t)
	ctx := context.Background()
	logger := testLogger()

	// Clone and create branch via go-git (instance 1).
	cloneDir := t.TempDir()
	repo1, err := Clone(ctx, bare, cloneDir, "", logger)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo1.CreateBranch("feat/gogit-to-cli"); err != nil {
		t.Fatal(err)
	}

	// Shell operations modify files.
	if err := os.WriteFile(filepath.Join(cloneDir, "cross.txt"), []byte("cross-path"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Open a second instance and strip go-git to force CLI path for
	// AddAll, Commit, Push — simulating go-git push failure + CLI fallback.
	repo2 := stripGoGit(repo1)

	if err := repo2.AddAll(); err != nil {
		t.Fatal(err)
	}
	if err := repo2.Commit("cross-path commit", "Test", "test@test.com"); err != nil {
		t.Fatal(err)
	}
	if err := repo2.Push(ctx, ""); err != nil {
		t.Fatalf("cross-path (go-git clone → CLI push) failed: %v", err)
	}

	refs := gitCmd(t, bare, "branch")
	if !strings.Contains(refs, "feat/gogit-to-cli") {
		t.Errorf("expected feat/gogit-to-cli on remote, got:\n%s", refs)
	}

	log := gitCmd(t, bare, "log", "feat/gogit-to-cli", "--oneline")
	if !strings.Contains(log, "cross-path commit") {
		t.Errorf("expected commit on remote, got:\n%s", log)
	}
}

func TestCrossPath_GoGitClone_CLIPush_NestedBranch(t *testing.T) {
	bare := initBareRepo(t)
	ctx := context.Background()
	logger := testLogger()

	cloneDir := t.TempDir()
	repo1, err := Clone(ctx, bare, cloneDir, "", logger)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo1.CreateBranch("feat/org/team/deep-cli"); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(cloneDir, "deep.txt"), []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}

	repo2 := stripGoGit(repo1)

	if err := repo2.AddAll(); err != nil {
		t.Fatal(err)
	}
	if err := repo2.Commit("deep cross commit", "Test", "test@test.com"); err != nil {
		t.Fatal(err)
	}
	if err := repo2.Push(ctx, ""); err != nil {
		t.Fatalf("cross-path (go-git clone → CLI push) nested branch failed: %v", err)
	}

	refs := gitCmd(t, bare, "branch")
	if !strings.Contains(refs, "feat/org/team/deep-cli") {
		t.Errorf("expected feat/org/team/deep-cli on remote, got:\n%s", refs)
	}
}

func TestCrossPath_GoGitClone_CLIPush_SingleBranchClone(t *testing.T) {
	bare := initBareRepo(t)
	ctx := context.Background()
	logger := testLogger()

	// Create a "develop" branch on the bare repo.
	work := t.TempDir()
	for _, args := range [][]string{
		{"git", "clone", bare, work},
		{"git", "-C", work, "config", "user.email", "test@test.com"},
		{"git", "-C", work, "config", "user.name", "Test"},
		{"git", "-C", work, "checkout", "-b", "develop"},
		{"git", "-C", work, "commit", "--allow-empty", "-m", "develop branch"},
		{"git", "-C", work, "push", "origin", "develop"},
	} {
		if out, err := exec.Command(args[0], args[1:]...).CombinedOutput(); err != nil {
			t.Fatalf("git command %v failed: %v\n%s", args, err, out)
		}
	}

	// Clone single branch via go-git, create feature branch.
	cloneDir := t.TempDir()
	repo1, err := Clone(ctx, bare, cloneDir, "develop", logger)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo1.CreateBranch("feat/from-develop-cli"); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(cloneDir, "dev.txt"), []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Force CLI path for commit+push.
	repo2 := stripGoGit(repo1)

	if err := repo2.AddAll(); err != nil {
		t.Fatal(err)
	}
	if err := repo2.Commit("cli push from develop", "Test", "test@test.com"); err != nil {
		t.Fatal(err)
	}
	if err := repo2.Push(ctx, ""); err != nil {
		t.Fatalf("cross-path single-branch clone → CLI push failed: %v", err)
	}

	refs := gitCmd(t, bare, "branch")
	if !strings.Contains(refs, "feat/from-develop-cli") {
		t.Errorf("expected feat/from-develop-cli on remote, got:\n%s", refs)
	}
}

// ---------------------------------------------------------------------------
// Full loom flow with CLI-only push path.
// Clone via go-git → CreateBranch → shell modifies → Open new instance
// (stripped to CLI-only) → AddAll → Commit → Push.
// ---------------------------------------------------------------------------

func TestCrossPath_FullLoomFlow_CLIPush(t *testing.T) {
	bare := initBareRepo(t)
	ctx := context.Background()
	logger := testLogger()

	// === resolveChildTarget phase (go-git) ===
	cloneDir := t.TempDir()
	cloneRepo, err := Clone(ctx, bare, cloneDir, "", logger)
	if err != nil {
		t.Fatal(err)
	}
	if err := cloneRepo.CreateBranch("feat/f5-infra-cli"); err != nil {
		t.Fatal(err)
	}

	// === operations phase (shell commands) ===
	if err := os.WriteFile(filepath.Join(cloneDir, "terraform.tf"), []byte("resource {}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(cloneDir, "modules"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cloneDir, "modules", "main.tf"), []byte("module {}"), 0o644); err != nil {
		t.Fatal(err)
	}

	// === commitAndPush phase (CLI-only, simulating go-git push failure) ===
	pushRepo := stripGoGit(cloneRepo)

	if err := pushRepo.AddAll(); err != nil {
		t.Fatalf("CLI AddAll failed: %v", err)
	}
	if err := pushRepo.Commit("automated update by loom", "loom[bot]", "loom@bot.dev"); err != nil {
		t.Fatalf("CLI Commit failed: %v", err)
	}
	if err := pushRepo.Push(ctx, ""); err != nil {
		t.Fatalf("CLI Push failed (this is the bug!): %v", err)
	}

	// === Verify remote state ===
	refs := gitCmd(t, bare, "branch")
	if !strings.Contains(refs, "feat/f5-infra-cli") {
		t.Errorf("expected feat/f5-infra-cli on remote, got:\n%s", refs)
	}

	log := gitCmd(t, bare, "log", "feat/f5-infra-cli", "--oneline")
	if !strings.Contains(log, "automated update by loom") {
		t.Errorf("expected commit on remote, got:\n%s", log)
	}

	files := gitCmd(t, bare, "ls-tree", "--name-only", "-r", "feat/f5-infra-cli")
	for _, expected := range []string{"terraform.tf", "modules/main.tf", "README.md"} {
		if !strings.Contains(files, expected) {
			t.Errorf("expected %q in pushed tree, got:\n%s", expected, files)
		}
	}
}
