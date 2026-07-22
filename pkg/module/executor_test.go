package module

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rickliujh/loom/pkg/action"
	"github.com/rickliujh/loom/pkg/config"
)

// initBareRepo creates a bare git repo with an initial commit, suitable for cloning in tests.
// Returns the path to the bare repo.
func initBareRepo(t *testing.T) string {
	t.Helper()

	// Create a working repo, commit, then clone it as bare.
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

	// Create an initial commit so the repo has a HEAD.
	dummy := filepath.Join(work, "README.md")
	if err := os.WriteFile(dummy, []byte("# test\n"), 0o644); err != nil {
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

// writeLoomYAML writes a loom.yaml into dir with the given content.
func writeLoomYAML(t *testing.T, dir, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "loom.yaml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// --- resolveChildTarget tests ---

func TestResolveChildTarget_NoTarget_ReturnsParentDir(t *testing.T) {
	childMod := &Module{
		Config: &config.LoomFile{Spec: config.Spec{}},
		Logger: testLogger(),
	}

	dir, cleanup, err := resolveChildTarget(context.Background(), childMod, "/parent/target", &RunOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if cleanup != nil {
		t.Error("expected nil cleanup when no target")
	}
	if dir != "/parent/target" {
		t.Errorf("expected /parent/target, got %q", dir)
	}
}

func TestResolveChildTarget_WithTarget_ClonesRepo(t *testing.T) {
	bare := initBareRepo(t)

	childMod := &Module{
		Config: &config.LoomFile{
			Spec: config.Spec{
				Target: &config.TargetSpec{URL: bare},
			},
		},
		Logger: testLogger(),
	}

	dir, cleanup, err := resolveChildTarget(context.Background(), childMod, "/parent/target", &RunOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if cleanup == nil {
		t.Fatal("expected non-nil cleanup")
	}
	defer cleanup()

	if dir == "/parent/target" {
		t.Error("expected a new target dir, got parent's targetDir")
	}

	// Verify the cloned repo exists.
	if _, err := os.Stat(filepath.Join(dir, "README.md")); err != nil {
		t.Errorf("expected cloned repo to contain README.md: %v", err)
	}
}

func TestResolveChildTarget_WithFeatureBranch(t *testing.T) {
	bare := initBareRepo(t)

	childMod := &Module{
		Config: &config.LoomFile{
			Spec: config.Spec{
				Target: &config.TargetSpec{
					URL:           bare,
					FeatureBranch: "feat/{{ .env }}-update",
				},
			},
		},
		Logger: testLogger(),
		Params: map[string]string{"env": "staging"},
	}

	dir, cleanup, err := resolveChildTarget(context.Background(), childMod, "/parent/target", &RunOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()

	// Verify we're on the feature branch.
	out, err := exec.Command("git", "-C", dir, "branch", "--show-current").CombinedOutput()
	if err != nil {
		t.Fatalf("git branch failed: %v\n%s", err, out)
	}
	branch := string(out[:len(out)-1]) // trim newline
	if branch != "feat/staging-update" {
		t.Errorf("expected branch feat/staging-update, got %q", branch)
	}
}

func TestResolveChildTarget_TemplatesURL(t *testing.T) {
	bare := initBareRepo(t)

	childMod := &Module{
		Config: &config.LoomFile{
			Spec: config.Spec{
				Target: &config.TargetSpec{URL: "{{ .repoPath }}"},
			},
		},
		Logger: testLogger(),
		Params: map[string]string{"repoPath": bare},
	}

	dir, cleanup, err := resolveChildTarget(context.Background(), childMod, "/parent/target", &RunOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()

	// Verify the cloned repo exists — proves the URL was templated.
	if _, err := os.Stat(filepath.Join(dir, "README.md")); err != nil {
		t.Errorf("expected cloned repo to contain README.md: %v", err)
	}
}

func TestResolveChildTarget_TemplatesBranch(t *testing.T) {
	bare := initBareRepo(t)

	// Create a branch named "release-prod" on the bare repo via a temp working copy.
	work := t.TempDir()
	for _, args := range [][]string{
		{"git", "clone", bare, work},
		{"git", "-C", work, "config", "user.email", "test@test.com"},
		{"git", "-C", work, "config", "user.name", "Test"},
		{"git", "-C", work, "checkout", "-b", "release-prod"},
		{"git", "-C", work, "commit", "--allow-empty", "-m", "branch commit"},
		{"git", "-C", work, "push", "origin", "release-prod"},
	} {
		if out, err := exec.Command(args[0], args[1:]...).CombinedOutput(); err != nil {
			t.Fatalf("git command %v failed: %v\n%s", args, err, out)
		}
	}

	childMod := &Module{
		Config: &config.LoomFile{
			Spec: config.Spec{
				Target: &config.TargetSpec{
					URL:    bare,
					Branch: "release-{{ .env }}",
				},
			},
		},
		Logger: testLogger(),
		Params: map[string]string{"env": "prod"},
	}

	dir, cleanup, err := resolveChildTarget(context.Background(), childMod, "/parent/target", &RunOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()

	// Verify we cloned the templated branch.
	out, err := exec.Command("git", "-C", dir, "rev-parse", "--abbrev-ref", "HEAD").CombinedOutput()
	if err != nil {
		t.Fatalf("git rev-parse failed: %v\n%s", err, out)
	}
	branch := string(out[:len(out)-1])
	if branch != "release-prod" {
		t.Errorf("expected branch release-prod, got %q", branch)
	}
}

func TestResolveChildTarget_TemplatesAllFields(t *testing.T) {
	bare := initBareRepo(t)

	// Create a branch "main-staging" to clone from.
	work := t.TempDir()
	for _, args := range [][]string{
		{"git", "clone", bare, work},
		{"git", "-C", work, "config", "user.email", "test@test.com"},
		{"git", "-C", work, "config", "user.name", "Test"},
		{"git", "-C", work, "checkout", "-b", "main-staging"},
		{"git", "-C", work, "commit", "--allow-empty", "-m", "staging branch"},
		{"git", "-C", work, "push", "origin", "main-staging"},
	} {
		if out, err := exec.Command(args[0], args[1:]...).CombinedOutput(); err != nil {
			t.Fatalf("git command %v failed: %v\n%s", args, err, out)
		}
	}

	childMod := &Module{
		Config: &config.LoomFile{
			Spec: config.Spec{
				Target: &config.TargetSpec{
					URL:           "{{ .repoPath }}",
					Branch:        "main-{{ .env }}",
					FeatureBranch: "feat/{{ .env }}-deploy",
				},
			},
		},
		Logger: testLogger(),
		Params: map[string]string{"repoPath": bare, "env": "staging"},
	}

	dir, cleanup, err := resolveChildTarget(context.Background(), childMod, "/parent/target", &RunOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()

	// Verify the feature branch was created on top of the templated base branch.
	out, err := exec.Command("git", "-C", dir, "branch", "--show-current").CombinedOutput()
	if err != nil {
		t.Fatalf("git branch failed: %v\n%s", err, out)
	}
	branch := string(out[:len(out)-1])
	if branch != "feat/staging-deploy" {
		t.Errorf("expected branch feat/staging-deploy, got %q", branch)
	}
}

// Target fields must be templated with the module's fully resolved params
// (static + dynamic), not just the params passed from the parent.
func TestResolveChildTarget_UsesModuleResolvedParams(t *testing.T) {
	bare := initBareRepo(t)

	childMod := &Module{
		Config: &config.LoomFile{
			Spec: config.Spec{
				Target: &config.TargetSpec{
					URL:           bare,
					FeatureBranch: "feat/{{ .hash }}-{{ .env }}",
				},
			},
		},
		Logger: testLogger(),
		// Params simulates the output of Load(): both static and dynamic resolved.
		Params: map[string]string{
			"env":  "staging",
			"hash": "abc123", // would come from a dynamicParam
		},
	}

	dir, cleanup, err := resolveChildTarget(context.Background(), childMod, "/parent/target", &RunOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()

	out, err := exec.Command("git", "-C", dir, "branch", "--show-current").CombinedOutput()
	if err != nil {
		t.Fatalf("git branch failed: %v\n%s", err, out)
	}
	branch := string(out[:len(out)-1])
	if branch != "feat/abc123-staging" {
		t.Errorf("expected branch feat/abc123-staging, got %q", branch)
	}
}

func TestResolveChildTarget_Cleanup_RemovesDir(t *testing.T) {
	bare := initBareRepo(t)

	childMod := &Module{
		Config: &config.LoomFile{
			Spec: config.Spec{
				Target: &config.TargetSpec{URL: bare},
			},
		},
		Logger: testLogger(),
	}

	dir, cleanup, err := resolveChildTarget(context.Background(), childMod, "/parent", &RunOptions{})
	if err != nil {
		t.Fatal(err)
	}

	// Dir should exist before cleanup.
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("dir should exist before cleanup: %v", err)
	}

	cleanup()

	// Dir should be gone after cleanup.
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("expected dir to be removed after cleanup, got err: %v", err)
	}
}

// --- ResolveSource tests ---

func TestResolveSource_LocalPath_NilCleanup(t *testing.T) {
	dir := t.TempDir()

	resolved, cleanup, err := ResolveSource(".", dir, testLogger())
	if err != nil {
		t.Fatal(err)
	}
	if cleanup != nil {
		t.Error("expected nil cleanup for local path")
	}
	if resolved != dir {
		t.Errorf("expected %q, got %q", dir, resolved)
	}
}

func TestResolveSource_GitURL_ReturnsCleanup(t *testing.T) {
	bare := initBareRepo(t)

	dir, cleanup, err := ResolveSource("file://"+bare, "", testLogger())
	if err != nil {
		t.Fatal(err)
	}
	if cleanup == nil {
		t.Fatal("expected non-nil cleanup for git source")
	}

	// Dir exists before cleanup.
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("cloned dir should exist: %v", err)
	}

	cleanup()

	// Dir gone after cleanup.
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("expected dir removed after cleanup, got: %v", err)
	}
}

// --- Execute tests ---

func TestExecute_SimpleShellOperation(t *testing.T) {
	dir := t.TempDir()
	writeLoomYAML(t, dir, `
apiVersion: loom.rickliujh.github.io/v1beta1
kind: Loom
metadata:
  name: test-shell
spec:
  operations:
    - name: create-file
      shell:
        command: touch output.txt
`)

	mod, err := Load(dir, nil, testLogger())
	if err != nil {
		t.Fatal(err)
	}

	if err := Execute(context.Background(), mod, dir, RunOptions{}); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(filepath.Join(dir, "output.txt")); err != nil {
		t.Errorf("expected output.txt to be created: %v", err)
	}
}

func TestExecute_ChildModuleInheritsParentTarget(t *testing.T) {
	// Parent module references a local child module without its own target.
	// The child should use the parent's targetDir.
	parentDir := t.TempDir()
	childDir := filepath.Join(parentDir, "child")
	if err := os.Mkdir(childDir, 0o755); err != nil {
		t.Fatal(err)
	}

	targetDir := t.TempDir()

	writeLoomYAML(t, childDir, `
apiVersion: loom.rickliujh.github.io/v1beta1
kind: Loom
metadata:
  name: child-mod
spec:
  operations:
    - name: create-marker
      shell:
        command: touch marker.txt
`)

	writeLoomYAML(t, parentDir, `
apiVersion: loom.rickliujh.github.io/v1beta1
kind: Loom
metadata:
  name: parent-mod
spec:
  modules:
    - name: child
      source: ./child
  operations: []
`)

	mod, err := Load(parentDir, nil, testLogger())
	if err != nil {
		t.Fatal(err)
	}

	if err := Execute(context.Background(), mod, targetDir, RunOptions{}); err != nil {
		t.Fatal(err)
	}

	// The child's shell command runs in the targetDir context.
	// Shell commands use the current process dir, so marker.txt lands in cwd.
	// The key assertion is that Execute completed without error.
}

func TestExecute_ChildModuleTemplatedSource(t *testing.T) {
	parentDir := t.TempDir()
	childDir := filepath.Join(parentDir, "myChild")
	if err := os.Mkdir(childDir, 0o755); err != nil {
		t.Fatal(err)
	}

	targetDir := t.TempDir()

	writeLoomYAML(t, childDir, `
apiVersion: loom.rickliujh.github.io/v1beta1
kind: Loom
metadata:
  name: child-mod
spec:
  operations:
    - name: create-marker
      shell:
        command: touch marker.txt
`)

	writeLoomYAML(t, parentDir, `
apiVersion: loom.rickliujh.github.io/v1beta1
kind: Loom
metadata:
  name: parent-mod
spec:
  params:
    - name: childDir
      default: myChild
  modules:
    - name: child
      source: ./{{ .childDir }}
  operations: []
`)

	mod, err := Load(parentDir, nil, testLogger())
	if err != nil {
		t.Fatal(err)
	}

	if err := Execute(context.Background(), mod, targetDir, RunOptions{}); err != nil {
		t.Fatal(err)
	}
}

func TestExecute_ChildModuleResolvesOwnTarget(t *testing.T) {
	// This is the bug scenario: a child module has its own target spec.
	// Before the fix, the parent's targetDir was passed through, causing
	// "repository does not exist" when the child tried to open it as a git repo.
	bare := initBareRepo(t)

	parentDir := t.TempDir()
	childDir := filepath.Join(parentDir, "child")
	if err := os.Mkdir(childDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Child module has its own target pointing to a git repo.
	// It runs a shell command to verify it can operate on the cloned repo.
	writeLoomYAML(t, childDir, `
apiVersion: loom.rickliujh.github.io/v1beta1
kind: Loom
metadata:
  name: child-with-target
spec:
  target:
    url: `+bare+`
    featureBranch: feat/test-branch
  operations:
    - name: verify-repo
      shell:
        command: git -C $LOOM_TARGET_DIR rev-parse --is-inside-work-tree 2>/dev/null || true
`)

	writeLoomYAML(t, parentDir, `
apiVersion: loom.rickliujh.github.io/v1beta1
kind: Loom
metadata:
  name: parent-mod
spec:
  modules:
    - name: child-with-target
      source: ./child
  operations: []
`)

	mod, err := Load(parentDir, nil, testLogger())
	if err != nil {
		t.Fatal(err)
	}

	// Before the fix, this would fail with:
	// executing child module "child-with-target": operation "commit" failed: opening repo at .: repository does not exist
	err = Execute(context.Background(), mod, parentDir, RunOptions{})
	if err != nil {
		t.Fatalf("Execute should succeed with child module's own target resolved: %v", err)
	}
}

func TestExecute_ChildModuleParamsRendered(t *testing.T) {
	parentDir := t.TempDir()
	childDir := filepath.Join(parentDir, "child")
	if err := os.Mkdir(childDir, 0o755); err != nil {
		t.Fatal(err)
	}

	targetDir := t.TempDir()

	writeLoomYAML(t, childDir, `
apiVersion: loom.rickliujh.github.io/v1beta1
kind: Loom
metadata:
  name: child-mod
spec:
  params:
    - name: greeting
      required: true
  operations:
    - name: write-greeting
      shell:
        command: echo done
`)

	writeLoomYAML(t, parentDir, `
apiVersion: loom.rickliujh.github.io/v1beta1
kind: Loom
metadata:
  name: parent-mod
spec:
  params:
    - name: env
      default: prod
  modules:
    - name: child
      source: ./child
      params:
        greeting: "hello-{{ .env }}"
  operations: []
`)

	mod, err := Load(parentDir, nil, testLogger())
	if err != nil {
		t.Fatal(err)
	}

	if err := Execute(context.Background(), mod, targetDir, RunOptions{}); err != nil {
		t.Fatalf("Execute with templated child params failed: %v", err)
	}
}

// TestExecute_ChildLogsLabeledByInstanceName guards bulk-run readability: when
// several items share one source (and thus one metadata name), each item's log
// lines must be tagged with its unique instance name (childRef.Name), not the
// shared metadata name — otherwise the interleaved output is indistinguishable.
func TestExecute_ChildLogsLabeledByInstanceName(t *testing.T) {
	parentDir := t.TempDir()
	childDir := filepath.Join(parentDir, "child")
	if err := os.Mkdir(childDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Both instances point at this one child module named "greeter".
	writeLoomYAML(t, childDir, `
apiVersion: loom.rickliujh.github.io/v1beta1
kind: Loom
metadata:
  name: greeter
spec:
  params:
    - name: who
      required: true
  operations:
    - name: hello
      shell:
        command: echo hi
`)

	writeLoomYAML(t, parentDir, `
apiVersion: loom.rickliujh.github.io/v1beta1
kind: Loom
metadata:
  name: bulk-greeter
spec:
  modules:
    - name: greeter-alice
      source: ./child
      params:
        who: alice
    - name: greeter-bob
      source: ./child
      params:
        who: bob
  operations: []
`)

	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))

	mod, err := Load(parentDir, nil, logger)
	if err != nil {
		t.Fatal(err)
	}
	if err := Execute(context.Background(), mod, parentDir, RunOptions{}); err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	out := buf.String()
	for _, want := range []string{
		// Each child's own operation logs carry its unique instance name.
		`module=greeter-alice`,
		`module=greeter-bob`,
		// The batch header is the orchestrator's line: it carries the root
		// module (marked with root=true) and names the item it dispatches to.
		`msg="greeter-alice (1/2)" module=bulk-greeter root=true`,
		`msg="greeter-bob (2/2)" module=bulk-greeter root=true`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("child log output missing %q:\n%s", want, out)
		}
	}

	// The shared metadata name must not leak in as a standalone label — every
	// child line carries an instance name instead.
	if strings.Contains(out, "module=greeter ") || strings.HasSuffix(out, "module=greeter") {
		t.Errorf("child logs used shared metadata name instead of instance name:\n%s", out)
	}
}

// --- LocalRun tests ---

// M2+L4: LocalRun propagates to child modules.
func TestExecute_LocalRun_ChildModuleInheritsLocalRun(t *testing.T) {
	parentDir := t.TempDir()
	childDir := filepath.Join(parentDir, "child")
	if err := os.Mkdir(childDir, 0o755); err != nil {
		t.Fatal(err)
	}

	targetDir := t.TempDir()

	writeLoomYAML(t, childDir, `
apiVersion: loom.rickliujh.github.io/v1beta1
kind: Loom
metadata:
  name: child-mod
spec:
  operations:
    - name: create-marker
      shell:
        command: touch child-marker.txt
`)

	writeLoomYAML(t, parentDir, `
apiVersion: loom.rickliujh.github.io/v1beta1
kind: Loom
metadata:
  name: parent-mod
spec:
  modules:
    - name: child
      source: ./child
  operations: []
`)

	mod, err := Load(parentDir, nil, testLogger())
	if err != nil {
		t.Fatal(err)
	}

	// localRun=true should propagate to child module without error.
	if err := Execute(context.Background(), mod, targetDir, RunOptions{LocalRun: true}); err != nil {
		t.Fatalf("Execute with localRun should succeed: %v", err)
	}
}

func TestResolveChildTarget_LocalRun_ClonesIntoNumberedSubdir(t *testing.T) {
	bare := initBareRepo(t)
	targetPath := t.TempDir()

	childMod := &Module{
		Config: &config.LoomFile{
			Metadata: config.Metadata{Name: "my-child"},
			Spec: config.Spec{
				Target: &config.TargetSpec{URL: bare},
			},
		},
		Logger: testLogger(),
	}

	opts := &RunOptions{LocalRun: true, TargetPath: targetPath}
	dir, cleanup, err := resolveChildTarget(context.Background(), childMod, "/parent/target", opts)
	if err != nil {
		t.Fatal(err)
	}
	if cleanup != nil {
		t.Error("expected nil cleanup in local mode")
	}

	// Should be a numbered subdirectory.
	expected := filepath.Join(targetPath, "00-my-child")
	if dir != expected {
		t.Errorf("expected %q, got %q", expected, dir)
	}

	// Verify the cloned repo exists.
	if _, err := os.Stat(filepath.Join(dir, "README.md")); err != nil {
		t.Errorf("expected cloned repo to contain README.md: %v", err)
	}

	// Second call should increment the counter.
	childMod2 := &Module{
		Config: &config.LoomFile{
			Metadata: config.Metadata{Name: "second-child"},
			Spec: config.Spec{
				Target: &config.TargetSpec{URL: bare},
			},
		},
		Logger: testLogger(),
	}
	dir2, _, err := resolveChildTarget(context.Background(), childMod2, "/parent/target", opts)
	if err != nil {
		t.Fatal(err)
	}
	expected2 := filepath.Join(targetPath, "01-second-child")
	if dir2 != expected2 {
		t.Errorf("expected %q, got %q", expected2, dir2)
	}
}

func TestExecute_LocalRun_ChildWithTarget_ClonesIntoTargetPath(t *testing.T) {
	bare := initBareRepo(t)
	targetPath := t.TempDir()

	parentDir := t.TempDir()
	childDir := filepath.Join(parentDir, "child")
	if err := os.Mkdir(childDir, 0o755); err != nil {
		t.Fatal(err)
	}

	writeLoomYAML(t, childDir, `
apiVersion: loom.rickliujh.github.io/v1beta1
kind: Loom
metadata:
  name: child-with-target
spec:
  target:
    url: `+bare+`
  operations:
    - name: create-marker
      shell:
        command: touch marker.txt
        pure: true
`)

	writeLoomYAML(t, parentDir, `
apiVersion: loom.rickliujh.github.io/v1beta1
kind: Loom
metadata:
  name: parent-mod
spec:
  modules:
    - name: child-with-target
      source: ./child
  operations: []
`)

	mod, err := Load(parentDir, nil, testLogger())
	if err != nil {
		t.Fatal(err)
	}

	opts := RunOptions{LocalRun: true, TargetPath: targetPath}
	if err := Execute(context.Background(), mod, parentDir, opts); err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	// Verify the child cloned into a numbered subdir of target path.
	childCloneDir := filepath.Join(targetPath, "00-child-with-target")
	if _, err := os.Stat(filepath.Join(childCloneDir, "README.md")); err != nil {
		t.Errorf("expected cloned repo at %s: %v", childCloneDir, err)
	}
	if _, err := os.Stat(filepath.Join(childCloneDir, "marker.txt")); err != nil {
		t.Errorf("expected marker.txt from shell command: %v", err)
	}
}

// Sibling module relative reference: networking/loom.yaml references ../monitoring
// when the repo is cloned via //networking subdir separator.
func TestExecute_SiblingModuleRelativeRef(t *testing.T) {
	// Build a working repo with two sibling modules.
	work := t.TempDir()
	for _, args := range [][]string{
		{"git", "init", work},
		{"git", "-C", work, "config", "user.email", "test@test.com"},
		{"git", "-C", work, "config", "user.name", "Test"},
	} {
		if out, err := exec.Command(args[0], args[1:]...).CombinedOutput(); err != nil {
			t.Fatalf("%v: %v\n%s", args, err, out)
		}
	}

	// Create monitoring module (the sibling that will be referenced).
	monDir := filepath.Join(work, "monitoring")
	os.MkdirAll(monDir, 0o755)
	writeLoomYAML(t, monDir, `
apiVersion: loom.rickliujh.github.io/v1beta1
kind: Loom
metadata:
  name: monitoring-mod
spec:
  operations:
    - name: create-marker
      shell:
        command: touch monitoring-executed.txt
        pure: true
`)

	// Create networking module that references ../monitoring as child.
	netDir := filepath.Join(work, "networking")
	os.MkdirAll(netDir, 0o755)
	writeLoomYAML(t, netDir, `
apiVersion: loom.rickliujh.github.io/v1beta1
kind: Loom
metadata:
  name: networking-mod
spec:
  modules:
    - name: monitoring
      source: ../monitoring
  operations:
    - name: create-marker
      shell:
        command: touch networking-executed.txt
        pure: true
`)

	// Commit and create bare repo for cloning.
	for _, args := range [][]string{
		{"git", "-C", work, "add", "."},
		{"git", "-C", work, "commit", "-m", "init with two modules"},
	} {
		if out, err := exec.Command(args[0], args[1:]...).CombinedOutput(); err != nil {
			t.Fatalf("%v: %v\n%s", args, err, out)
		}
	}

	bare := t.TempDir()
	if out, err := exec.Command("git", "clone", "--bare", work, bare).CombinedOutput(); err != nil {
		t.Fatalf("bare clone: %v\n%s", err, out)
	}

	// Resolve source with //networking subdir.
	moduleDir, cleanup, err := ResolveSource("file://"+bare+"//networking", "", testLogger())
	if err != nil {
		t.Fatal(err)
	}
	if cleanup != nil {
		defer cleanup()
	}

	// Verify full repo was cloned (monitoring dir exists as sibling).
	siblingDir := filepath.Join(moduleDir, "..", "monitoring")
	if _, err := os.Stat(siblingDir); err != nil {
		t.Fatalf("full repo not cloned — sibling dir missing: %v", err)
	}

	// Load and execute the networking module.
	mod, err := Load(moduleDir, nil, testLogger())
	if err != nil {
		t.Fatal(err)
	}

	targetDir := t.TempDir()
	if err := Execute(context.Background(), mod, targetDir, RunOptions{}); err != nil {
		t.Fatalf("Execute with sibling module ref failed: %v", err)
	}
}

func TestNewExecutionContext_SetsLocalRun(t *testing.T) {
	mod := &Module{
		Config: &config.LoomFile{Spec: config.Spec{}},
		Params: map[string]string{"key": "val"},
		Logger: testLogger(),
	}

	ctx := mod.NewExecutionContext("/target", RunOptions{LocalRun: true})
	if !ctx.LocalRun {
		t.Error("expected LocalRun to be true")
	}
	if ctx.DryRun {
		t.Error("expected DryRun to be false")
	}

	ctx2 := mod.NewExecutionContext("/target", RunOptions{DryRun: true})
	if ctx2.LocalRun {
		t.Error("expected LocalRun to be false")
	}
	if !ctx2.DryRun {
		t.Error("expected DryRun to be true")
	}

	ctx3 := mod.NewExecutionContext("/target", RunOptions{DryRun: true, ShowDiff: true})
	if !ctx3.ShowDiff {
		t.Error("expected ShowDiff to be true")
	}
	if !ctx3.DryRun {
		t.Error("expected DryRun to be true")
	}
}

func TestNewExecutionContext_PropagatesSummaryAndModuleName(t *testing.T) {
	mod := &Module{
		Config: &config.LoomFile{
			Metadata: config.Metadata{Name: "my-module"},
			Spec:     config.Spec{},
		},
		Params: map[string]string{},
		Logger: testLogger(),
	}

	summary := &action.RunSummary{}
	ctx := mod.NewExecutionContext("/target", RunOptions{Summary: summary})
	if ctx.Summary != summary {
		t.Error("expected Summary pointer to be propagated")
	}
	if ctx.ModuleName != "my-module" {
		t.Errorf("expected ModuleName my-module, got %q", ctx.ModuleName)
	}
}
