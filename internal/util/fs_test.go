package util

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
)

// setupTestDir creates a temporary module directory with the given files.
func setupTestDir(t *testing.T, files []string) string {
	t.Helper()
	dir := t.TempDir()
	for _, f := range files {
		p := filepath.Join(dir, f)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("test"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func sorted(s []string) []string {
	sort.Strings(s)
	return s
}

func equalSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestWalkTemplateFiles_ImplicitExcludes(t *testing.T) {
	dir := setupTestDir(t, []string{
		"loom.yaml",
		"loom.jsonnet",
		"app.yaml",
		"README.md",
		"readme.md",
		".git/config",
		".git/HEAD",
		"__functions/patch.yaml",
		"sub/deploy.yaml",
	})

	got, err := WalkTemplateFiles(dir, nil)
	if err != nil {
		t.Fatal(err)
	}

	// __functions is NOT implicitly excluded — must be listed in excludes.
	// .git and README.md ARE implicitly excluded.
	want := []string{"__functions/patch.yaml", "app.yaml", "sub/deploy.yaml"}
	if !equalSlices(sorted(got), sorted(want)) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestWalkTemplateFiles_ExplicitExcludeFunctions(t *testing.T) {
	dir := setupTestDir(t, []string{
		"app.yaml",
		"__functions/patch.yaml",
		"__functions/scripts/build.sh",
	})

	opts := &FilterOptions{Excludes: []string{"__functions"}}
	got, err := WalkTemplateFiles(dir, opts)
	if err != nil {
		t.Fatal(err)
	}

	want := []string{"app.yaml"}
	if !equalSlices(sorted(got), sorted(want)) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestWalkTemplateFiles_UserExcludes(t *testing.T) {
	dir := setupTestDir(t, []string{
		"app.yaml",
		"secret.env",
		"sub/deploy.yaml",
	})

	opts := &FilterOptions{Excludes: []string{"*.env"}}
	got, err := WalkTemplateFiles(dir, opts)
	if err != nil {
		t.Fatal(err)
	}

	want := []string{"app.yaml", "sub/deploy.yaml"}
	if !equalSlices(sorted(got), sorted(want)) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestWalkTemplateFiles_IncludesOverrideImplicitExcludes(t *testing.T) {
	dir := setupTestDir(t, []string{
		"app.yaml",
		"README.md",
		".git/config",
	})

	opts := &FilterOptions{Includes: []string{"README.md", ".git"}}
	got, err := WalkTemplateFiles(dir, opts)
	if err != nil {
		t.Fatal(err)
	}

	want := []string{".git/config", "README.md", "app.yaml"}
	if !equalSlices(sorted(got), sorted(want)) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestWalkTemplateFiles_IncludesOverrideUserExcludes(t *testing.T) {
	dir := setupTestDir(t, []string{
		"app.yaml",
		"keep.env",
		"drop.log",
	})

	opts := &FilterOptions{
		Excludes: []string{"*.env", "*.log"},
		Includes: []string{"keep.env"},
	}
	got, err := WalkTemplateFiles(dir, opts)
	if err != nil {
		t.Fatal(err)
	}

	want := []string{"app.yaml", "keep.env"}
	if !equalSlices(sorted(got), sorted(want)) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestWalkTemplateFiles_ConfigAlwaysExcluded(t *testing.T) {
	dir := setupTestDir(t, []string{
		"loom.yaml",
		"loom.jsonnet",
		"app.yaml",
	})

	// Even with includes, config files cannot be overridden.
	opts := &FilterOptions{Includes: []string{"loom.yaml", "loom.jsonnet"}}
	got, err := WalkTemplateFiles(dir, opts)
	if err != nil {
		t.Fatal(err)
	}

	want := []string{"app.yaml"}
	if !equalSlices(sorted(got), sorted(want)) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestWalkTemplateFiles_ExcludeDir(t *testing.T) {
	dir := setupTestDir(t, []string{
		"app.yaml",
		"scripts/build.sh",
		"scripts/deploy.sh",
	})

	opts := &FilterOptions{Excludes: []string{"scripts"}}
	got, err := WalkTemplateFiles(dir, opts)
	if err != nil {
		t.Fatal(err)
	}

	want := []string{"app.yaml"}
	if !equalSlices(sorted(got), sorted(want)) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestWalkTemplateFiles_NilOpts(t *testing.T) {
	dir := setupTestDir(t, []string{
		"app.yaml",
		"sub/deploy.yaml",
	})

	got, err := WalkTemplateFiles(dir, nil)
	if err != nil {
		t.Fatal(err)
	}

	want := []string{"app.yaml", "sub/deploy.yaml"}
	if !equalSlices(sorted(got), sorted(want)) {
		t.Errorf("got %v, want %v", got, want)
	}
}
