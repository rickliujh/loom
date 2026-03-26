package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// ===========================================================================
// Clone tests
// ===========================================================================

func TestClone_DefaultBranch(t *testing.T) {
	bare := initBareRepo(t)
	ctx := context.Background()
	logger := testLogger()

	dir := t.TempDir()
	repo, err := Clone(ctx, bare, dir, "", logger)
	if err != nil {
		t.Fatal(err)
	}

	if repo.Dir() != dir {
		t.Errorf("Dir() = %q, want %q", repo.Dir(), dir)
	}

	branch, err := repo.CurrentBranch()
	if err != nil {
		t.Fatal(err)
	}
	if branch != "master" && branch != "main" {
		t.Errorf("expected default branch (main or master), got %q", branch)
	}
}

func TestClone_SpecificBranch(t *testing.T) {
	bare := initBareRepo(t)

	// Create a "staging" branch.
	work := t.TempDir()
	gitCmdSetup(t, bare, work, "staging")

	ctx := context.Background()
	logger := testLogger()

	dir := t.TempDir()
	repo, err := Clone(ctx, bare, dir, "staging", logger)
	if err != nil {
		t.Fatal(err)
	}

	branch, err := repo.CurrentBranch()
	if err != nil {
		t.Fatal(err)
	}
	if branch != "staging" {
		t.Errorf("expected branch staging, got %q", branch)
	}
}

func TestClone_InvalidURL(t *testing.T) {
	ctx := context.Background()
	logger := testLogger()

	dir := t.TempDir()
	_, err := Clone(ctx, "/nonexistent/path/to/repo", dir, "", logger)
	if err == nil {
		t.Fatal("expected error for invalid URL")
	}
}

// ===========================================================================
// Open tests
// ===========================================================================

func TestOpen_ValidRepo(t *testing.T) {
	bare := initBareRepo(t)
	ctx := context.Background()
	logger := testLogger()

	dir := t.TempDir()
	_, err := Clone(ctx, bare, dir, "", logger)
	if err != nil {
		t.Fatal(err)
	}

	repo, err := Open(dir, logger)
	if err != nil {
		t.Fatal(err)
	}
	if repo.Dir() != dir {
		t.Errorf("Dir() = %q, want %q", repo.Dir(), dir)
	}
}

func TestOpen_InvalidDir(t *testing.T) {
	logger := testLogger()
	_, err := Open("/nonexistent/path", logger)
	if err == nil {
		t.Fatal("expected error for invalid directory")
	}
}

func TestOpen_NotARepo(t *testing.T) {
	logger := testLogger()
	dir := t.TempDir()
	_, err := Open(dir, logger)
	if err == nil {
		t.Fatal("expected error for non-repo directory")
	}
}

// ===========================================================================
// Dir
// ===========================================================================

func TestDir(t *testing.T) {
	repo := &Repo{dir: "/some/path", logger: testLogger()}
	if repo.Dir() != "/some/path" {
		t.Errorf("Dir() = %q, want %q", repo.Dir(), "/some/path")
	}
}

// ===========================================================================
// RemoteURL
// ===========================================================================

func TestRemoteURL_GoGit(t *testing.T) {
	bare := initBareRepo(t)
	ctx := context.Background()
	logger := testLogger()

	dir := t.TempDir()
	repo, err := Clone(ctx, bare, dir, "", logger)
	if err != nil {
		t.Fatal(err)
	}

	url, err := repo.RemoteURL()
	if err != nil {
		t.Fatal(err)
	}
	if url != bare {
		t.Errorf("RemoteURL() = %q, want %q", url, bare)
	}
}

func TestRemoteURL_CLI(t *testing.T) {
	bare := initBareRepo(t)

	repo := cliCloneRepo(t, bare, "")

	url, err := repo.RemoteURL()
	if err != nil {
		t.Fatal(err)
	}
	if url != bare {
		t.Errorf("RemoteURL() = %q, want %q", url, bare)
	}
}

// ===========================================================================
// CurrentBranch
// ===========================================================================

func TestCurrentBranch_GoGit_DefaultBranch(t *testing.T) {
	bare := initBareRepo(t)
	ctx := context.Background()
	logger := testLogger()

	dir := t.TempDir()
	repo, err := Clone(ctx, bare, dir, "", logger)
	if err != nil {
		t.Fatal(err)
	}

	branch, err := repo.CurrentBranch()
	if err != nil {
		t.Fatal(err)
	}
	if branch != "master" && branch != "main" {
		t.Errorf("expected default branch, got %q", branch)
	}
}

func TestCurrentBranch_CLI_DefaultBranch(t *testing.T) {
	bare := initBareRepo(t)

	repo := cliCloneRepo(t, bare, "")

	branch, err := repo.CurrentBranch()
	if err != nil {
		t.Fatal(err)
	}
	if branch != "master" && branch != "main" {
		t.Errorf("expected default branch, got %q", branch)
	}
}

func TestCurrentBranch_AfterCreateBranch(t *testing.T) {
	bare := initBareRepo(t)
	ctx := context.Background()
	logger := testLogger()

	dir := t.TempDir()
	repo, err := Clone(ctx, bare, dir, "", logger)
	if err != nil {
		t.Fatal(err)
	}

	if err := repo.CreateBranch("test-branch"); err != nil {
		t.Fatal(err)
	}

	branch, err := repo.CurrentBranch()
	if err != nil {
		t.Fatal(err)
	}
	if branch != "test-branch" {
		t.Errorf("expected test-branch, got %q", branch)
	}
}

// ===========================================================================
// CreateBranch
// ===========================================================================

func TestCreateBranch_GoGit(t *testing.T) {
	bare := initBareRepo(t)
	ctx := context.Background()
	logger := testLogger()

	dir := t.TempDir()
	repo, err := Clone(ctx, bare, dir, "", logger)
	if err != nil {
		t.Fatal(err)
	}

	if err := repo.CreateBranch("new-feature"); err != nil {
		t.Fatal(err)
	}

	branch, _ := repo.CurrentBranch()
	if branch != "new-feature" {
		t.Errorf("expected new-feature, got %q", branch)
	}
}

func TestCreateBranch_CLI(t *testing.T) {
	bare := initBareRepo(t)

	repo := cliCloneRepo(t, bare, "")

	if err := repo.CreateBranch("cli-feature"); err != nil {
		t.Fatal(err)
	}

	branch, _ := repo.CurrentBranch()
	if branch != "cli-feature" {
		t.Errorf("expected cli-feature, got %q", branch)
	}
}

func TestCreateBranch_NestedName(t *testing.T) {
	bare := initBareRepo(t)
	ctx := context.Background()
	logger := testLogger()

	dir := t.TempDir()
	repo, err := Clone(ctx, bare, dir, "", logger)
	if err != nil {
		t.Fatal(err)
	}

	if err := repo.CreateBranch("feat/team/work"); err != nil {
		t.Fatal(err)
	}

	branch, _ := repo.CurrentBranch()
	if branch != "feat/team/work" {
		t.Errorf("expected feat/team/work, got %q", branch)
	}
}

// ===========================================================================
// AddAll
// ===========================================================================

func TestAddAll_GoGit(t *testing.T) {
	bare := initBareRepo(t)
	ctx := context.Background()
	logger := testLogger()

	dir := t.TempDir()
	repo, err := Clone(ctx, bare, dir, "", logger)
	if err != nil {
		t.Fatal(err)
	}

	// Create files.
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0o644)
	os.WriteFile(filepath.Join(dir, "b.txt"), []byte("b"), 0o644)

	if err := repo.AddAll(); err != nil {
		t.Fatal(err)
	}

	// Verify files are staged by checking git status.
	status := gitCmd(t, dir, "status", "--porcelain")
	if !strings.Contains(status, "a.txt") || !strings.Contains(status, "b.txt") {
		t.Errorf("expected a.txt and b.txt staged, got:\n%s", status)
	}
}

func TestAddAll_CLI(t *testing.T) {
	bare := initBareRepo(t)

	repo := cliCloneRepo(t, bare, "")

	os.WriteFile(filepath.Join(repo.dir, "c.txt"), []byte("c"), 0o644)

	if err := repo.AddAll(); err != nil {
		t.Fatal(err)
	}

	status := gitCmd(t, repo.dir, "status", "--porcelain")
	if !strings.Contains(status, "c.txt") {
		t.Errorf("expected c.txt staged, got:\n%s", status)
	}
}

func TestAddAll_NoChanges(t *testing.T) {
	bare := initBareRepo(t)
	ctx := context.Background()
	logger := testLogger()

	dir := t.TempDir()
	repo, err := Clone(ctx, bare, dir, "", logger)
	if err != nil {
		t.Fatal(err)
	}

	// AddAll on a clean repo should not error.
	if err := repo.AddAll(); err != nil {
		t.Fatalf("AddAll on clean repo failed: %v", err)
	}
}

// ===========================================================================
// Commit
// ===========================================================================

func TestCommit_GoGit(t *testing.T) {
	bare := initBareRepo(t)
	ctx := context.Background()
	logger := testLogger()

	dir := t.TempDir()
	repo, err := Clone(ctx, bare, dir, "", logger)
	if err != nil {
		t.Fatal(err)
	}

	os.WriteFile(filepath.Join(dir, "file.txt"), []byte("data"), 0o644)
	repo.AddAll()

	if err := repo.Commit("test commit", "Tester", "tester@example.com"); err != nil {
		t.Fatal(err)
	}

	log := gitCmd(t, dir, "log", "--oneline", "-1")
	if !strings.Contains(log, "test commit") {
		t.Errorf("expected 'test commit' in log, got: %s", log)
	}

	// Verify author.
	author := gitCmd(t, dir, "log", "--format=%an <%ae>", "-1")
	if !strings.Contains(author, "Tester") || !strings.Contains(author, "tester@example.com") {
		t.Errorf("unexpected author: %s", author)
	}
}

func TestCommit_CLI(t *testing.T) {
	bare := initBareRepo(t)

	repo := cliCloneRepo(t, bare, "")

	os.WriteFile(filepath.Join(repo.dir, "file.txt"), []byte("data"), 0o644)
	repo.AddAll()

	if err := repo.Commit("cli test commit", "CLIUser", "cli@example.com"); err != nil {
		t.Fatal(err)
	}

	log := gitCmd(t, repo.dir, "log", "--oneline", "-1")
	if !strings.Contains(log, "cli test commit") {
		t.Errorf("expected 'cli test commit' in log, got: %s", log)
	}
}

func TestCommit_EmptyAuthor(t *testing.T) {
	bare := initBareRepo(t)
	ctx := context.Background()
	logger := testLogger()

	dir := t.TempDir()
	repo, err := Clone(ctx, bare, dir, "", logger)
	if err != nil {
		t.Fatal(err)
	}

	os.WriteFile(filepath.Join(dir, "file.txt"), []byte("data"), 0o644)
	repo.AddAll()

	// With empty author/email, go-git may use defaults or fail.
	// The CLI path uses -c flags only when non-empty, so git uses defaults.
	// Just ensure it doesn't panic.
	_ = repo.Commit("no author commit", "", "")
}

// ===========================================================================
// Push with no changes (should fail or be a no-op)
// ===========================================================================

func TestPush_NothingToPush(t *testing.T) {
	bare := initBareRepo(t)
	ctx := context.Background()
	logger := testLogger()

	dir := t.TempDir()
	repo, err := Clone(ctx, bare, dir, "", logger)
	if err != nil {
		t.Fatal(err)
	}

	// Push without any new commits — go-git returns "already up-to-date" error.
	err = repo.Push(ctx, "")
	// This may or may not error depending on the go-git version.
	// It's acceptable behavior either way, but it should not panic.
	_ = err
}

// ===========================================================================
// cliClone function directly
// ===========================================================================

func TestCliClone_DefaultBranch(t *testing.T) {
	bare := initBareRepo(t)
	ctx := context.Background()

	dir := t.TempDir()
	if err := cliClone(ctx, bare, dir, ""); err != nil {
		t.Fatal(err)
	}

	// Verify repo exists.
	if _, err := os.Stat(filepath.Join(dir, ".git")); err != nil {
		t.Fatal("expected .git directory")
	}
}

func TestCliClone_WithBranch(t *testing.T) {
	bare := initBareRepo(t)

	// Create a branch.
	work := t.TempDir()
	gitCmdSetup(t, bare, work, "dev")

	ctx := context.Background()
	dir := t.TempDir()
	if err := cliClone(ctx, bare, dir, "dev"); err != nil {
		t.Fatal(err)
	}

	branch := gitCmd(t, dir, "rev-parse", "--abbrev-ref", "HEAD")
	if branch != "dev" {
		t.Errorf("expected branch dev, got %q", branch)
	}
}

func TestCliClone_InvalidURL(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	err := cliClone(ctx, "/nonexistent/repo", dir, "")
	if err == nil {
		t.Fatal("expected error for invalid URL")
	}
}

// ===========================================================================
// cliCreateBranch
// ===========================================================================

func TestCliCreateBranch(t *testing.T) {
	bare := initBareRepo(t)
	ctx := context.Background()

	dir := t.TempDir()
	cliClone(ctx, bare, dir, "")

	if err := cliCreateBranch(dir, "test-cli-branch"); err != nil {
		t.Fatal(err)
	}

	branch := gitCmd(t, dir, "rev-parse", "--abbrev-ref", "HEAD")
	if branch != "test-cli-branch" {
		t.Errorf("expected test-cli-branch, got %q", branch)
	}
}

// ===========================================================================
// cliAddAll
// ===========================================================================

func TestCliAddAll(t *testing.T) {
	bare := initBareRepo(t)
	ctx := context.Background()

	dir := t.TempDir()
	cliClone(ctx, bare, dir, "")

	os.WriteFile(filepath.Join(dir, "new.txt"), []byte("content"), 0o644)

	if err := cliAddAll(dir); err != nil {
		t.Fatal(err)
	}

	status := gitCmd(t, dir, "status", "--porcelain")
	if !strings.Contains(status, "new.txt") {
		t.Errorf("expected new.txt to be staged, got:\n%s", status)
	}
}

// ===========================================================================
// cliCommit
// ===========================================================================

func TestCliCommit_WithAuthor(t *testing.T) {
	bare := initBareRepo(t)
	ctx := context.Background()

	dir := t.TempDir()
	cliClone(ctx, bare, dir, "")

	os.WriteFile(filepath.Join(dir, "commit.txt"), []byte("data"), 0o644)
	cliAddAll(dir)

	if err := cliCommit(dir, "test message", "Bot", "bot@test.com"); err != nil {
		t.Fatal(err)
	}

	log := gitCmd(t, dir, "log", "--format=%an <%ae> %s", "-1")
	if !strings.Contains(log, "Bot") || !strings.Contains(log, "bot@test.com") || !strings.Contains(log, "test message") {
		t.Errorf("unexpected log: %s", log)
	}
}

func TestCliCommit_WithoutAuthor(t *testing.T) {
	bare := initBareRepo(t)
	ctx := context.Background()

	dir := t.TempDir()
	cliClone(ctx, bare, dir, "")

	// Set default git config for this repo.
	gitCmd(t, dir, "config", "user.email", "default@test.com")
	gitCmd(t, dir, "config", "user.name", "Default")

	os.WriteFile(filepath.Join(dir, "noauthor.txt"), []byte("data"), 0o644)
	cliAddAll(dir)

	if err := cliCommit(dir, "no author", "", ""); err != nil {
		t.Fatal(err)
	}

	log := gitCmd(t, dir, "log", "--oneline", "-1")
	if !strings.Contains(log, "no author") {
		t.Errorf("expected 'no author' in log, got: %s", log)
	}
}

// ===========================================================================
// cliPush
// ===========================================================================

func TestCliPush_WithBranch(t *testing.T) {
	bare := initBareRepo(t)
	ctx := context.Background()

	dir := t.TempDir()
	cliClone(ctx, bare, dir, "")
	cliCreateBranch(dir, "push-test")

	os.WriteFile(filepath.Join(dir, "push.txt"), []byte("data"), 0o644)
	cliAddAll(dir)
	gitCmd(t, dir, "config", "user.email", "test@test.com")
	gitCmd(t, dir, "config", "user.name", "Test")
	gitCmd(t, dir, "commit", "-m", "push test")

	if err := cliPush(ctx, dir, "push-test"); err != nil {
		t.Fatal(err)
	}

	refs := gitCmd(t, bare, "branch")
	if !strings.Contains(refs, "push-test") {
		t.Errorf("expected push-test on remote, got:\n%s", refs)
	}
}

func TestCliPush_EmptyBranch_UsesHEAD(t *testing.T) {
	bare := initBareRepo(t)
	ctx := context.Background()

	dir := t.TempDir()
	cliClone(ctx, bare, dir, "")

	os.WriteFile(filepath.Join(dir, "head.txt"), []byte("data"), 0o644)
	cliAddAll(dir)
	gitCmd(t, dir, "config", "user.email", "test@test.com")
	gitCmd(t, dir, "config", "user.name", "Test")
	gitCmd(t, dir, "commit", "-m", "head push")

	if err := cliPush(ctx, dir, ""); err != nil {
		t.Fatal(err)
	}

	log := gitCmd(t, bare, "log", "--oneline")
	if !strings.Contains(log, "head push") {
		t.Errorf("expected 'head push' in bare repo, got:\n%s", log)
	}
}

// ===========================================================================
// cliCurrentBranch
// ===========================================================================

func TestCliCurrentBranch(t *testing.T) {
	bare := initBareRepo(t)
	ctx := context.Background()

	dir := t.TempDir()
	cliClone(ctx, bare, dir, "")

	branch, err := cliCurrentBranch(dir)
	if err != nil {
		t.Fatal(err)
	}
	if branch != "master" && branch != "main" {
		t.Errorf("expected default branch, got %q", branch)
	}
}

// ===========================================================================
// cliRemoteURL
// ===========================================================================

func TestCliRemoteURL(t *testing.T) {
	bare := initBareRepo(t)
	ctx := context.Background()

	dir := t.TempDir()
	cliClone(ctx, bare, dir, "")

	url, err := cliRemoteURL(dir)
	if err != nil {
		t.Fatal(err)
	}
	if url != bare {
		t.Errorf("remote URL = %q, want %q", url, bare)
	}
}

// ===========================================================================
// Helper: create a branch on a bare repo for clone tests
// ===========================================================================

func gitCmdSetup(t *testing.T, bare, work, branchName string) {
	t.Helper()
	for _, args := range [][]string{
		{"git", "clone", bare, work},
		{"git", "-C", work, "config", "user.email", "test@test.com"},
		{"git", "-C", work, "config", "user.name", "Test"},
		{"git", "-C", work, "checkout", "-b", branchName},
		{"git", "-C", work, "commit", "--allow-empty", "-m", branchName + " branch"},
		{"git", "-C", work, "push", "origin", branchName},
	} {
		out, err := runGitCmd(args[0], args[1:]...)
		if err != nil {
			t.Fatalf("git command %v failed: %v\n%s", args, err, out)
		}
	}
}

func runGitCmd(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}
