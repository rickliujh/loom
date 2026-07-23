// Package spectrace enforces traceability between the behavioral spec and
// the test suite: every rule ID declared in specs/*.md (headings of the form
// "#### <ID>: ...") must be referenced by at least one *_test.go file, and
// every rule ID referenced by a test must still exist in the spec.
//
// Rules that predate this check and have no test yet are listed in
// uncovered — a burn-down list, not a licence. Adding a NEW spec rule
// without a test fails this check; writing a test for a listed rule
// requires deleting its entry (the check fails until you do).
package spectrace

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"testing"
)

// uncovered lists spec rule IDs that currently have no referencing test.
// Only ever remove entries. Do not add entries for new rules — write a test.
var uncovered = map[string]bool{
	"A3": true, "C1": true, "C2": true, "C3": true, "C4": true,
	"D1": true, "D2": true, "D3": true, "D4": true, "D5": true,
	"DR1": true, "E1": true,
	"F1": true, "F2": true, "F3": true,
	"GO1": true, "M1": true, "M3": true, "M4": true,
	"P2": true, "PM1": true, "PM2": true, "PM3": true, "PM4": true,
	"T1": true, "T2": true, "T3": true,
	"TG1": true, "TG2": true, "TG3": true, "TG4": true, "TG5": true,
	"CP1": true, "CP2": true, "CP3": true, "CP4": true,
	"J1": true, "J2": true, "L6": true, "L7": true,
	"NF1": true, "NF4": true, "NF5": true, "NF6": true,
	"PR1": true, "PR2": true, "PR3": true, "S3": true,
}

var (
	specIDRe = regexp.MustCompile(`(?m)^#{3,5} ([A-Z]+[0-9]+):`)
	// Rule IDs referenced from test function names, e.g. TestSMP_B4_Foo,
	// TestSpecT4, TestPatch_E2_Bar. Rule numbers are 1-2 digits — the bound
	// keeps engine names like JSON6902 from being misread as IDs.
	testIDRe = regexp.MustCompile(`func Test\w*?_?([A-Z]{1,3}[0-9]{1,2})(?:_\w*)?\(`)
)

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate source file")
	}
	return filepath.Dir(filepath.Dir(filepath.Dir(file)))
}

func specIDs(t *testing.T, root string) map[string]string {
	t.Helper()
	ids := map[string]string{} // id -> spec file
	matches, err := filepath.Glob(filepath.Join(root, "specs", "*.md"))
	if err != nil || len(matches) == 0 {
		t.Fatalf("no spec files found under %s/specs: %v", root, err)
	}
	for _, m := range matches {
		data, err := os.ReadFile(m)
		if err != nil {
			t.Fatal(err)
		}
		for _, g := range specIDRe.FindAllStringSubmatch(string(data), -1) {
			ids[g[1]] = filepath.Base(m)
		}
	}
	return ids
}

// testFiles returns the content of every *_test.go outside this package.
func testFiles(t *testing.T, root string) map[string]string {
	t.Helper()
	files := map[string]string{}
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			name := d.Name()
			if name == ".git" || name == "spectrace" || name == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(path, "_test.go") {
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			rel, _ := filepath.Rel(root, path)
			files[rel] = string(data)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func TestSpecRulesHaveTests(t *testing.T) {
	root := repoRoot(t)
	ids := specIDs(t, root)
	files := testFiles(t, root)

	var missing, staleAllowlist []string
	for id := range ids {
		// Underscores count as delimiters so IDs embedded in test names
		// (TestFoo_PR4_Bar) are found; \b alone treats _ as a word char.
		re := regexp.MustCompile(`(?:\b|_)` + id + `(?:\b|_)`)
		covered := false
		for _, content := range files {
			if re.MatchString(content) {
				covered = true
				break
			}
		}
		switch {
		case !covered && !uncovered[id]:
			missing = append(missing, id)
		case covered && uncovered[id]:
			staleAllowlist = append(staleAllowlist, id)
		}
	}
	sort.Strings(missing)
	sort.Strings(staleAllowlist)

	if len(missing) > 0 {
		t.Errorf("spec rules with no referencing test (write a test that names the ID, e.g. TestFoo_%s_Bar): %v",
			missing[0], missing)
	}
	if len(staleAllowlist) > 0 {
		t.Errorf("rules now covered by tests — remove them from the uncovered list: %v", staleAllowlist)
	}
}

func TestTestsReferenceRealSpecRules(t *testing.T) {
	root := repoRoot(t)
	ids := specIDs(t, root)
	files := testFiles(t, root)

	stale := map[string][]string{} // id -> files
	for rel, content := range files {
		for _, g := range testIDRe.FindAllStringSubmatch(content, -1) {
			id := g[1]
			if _, ok := ids[id]; !ok {
				stale[id] = append(stale[id], rel)
			}
		}
	}
	for id, where := range stale {
		t.Errorf("test references spec rule %q which does not exist in specs/*.md (files: %v)", id, where)
	}
}
