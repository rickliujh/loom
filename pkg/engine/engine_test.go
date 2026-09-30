package engine

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/rickliujh/loom/pkg/event"
)

func quietLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func writeModule(t *testing.T, dir, yaml string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "loom.yaml"), []byte("apiVersion: loom.rickliujh.github.io/v1beta1\nkind: Loom\n"+yaml), 0o644); err != nil {
		t.Fatal(err)
	}
}

// diffFixture is a parent that composes one child: the child renders a file
// into a subdirectory and patches nothing; a pure shell step of the parent
// edits an upstream file.
func diffFixture(t *testing.T) (moduleDir, upstream string) {
	t.Helper()
	upstream = initRepo(t, map[string]string{"README.md": "readme\n"})
	moduleDir = t.TempDir()
	writeFiles(t, moduleDir, map[string]string{"child/tpl/app.yaml": "name: {{ .app }}\n"})
	writeModule(t, filepath.Join(moduleDir, "child"), `metadata:
  name: renderer
spec:
  params:
    - name: app
      required: true
  excludes: [loom.yaml]
  target:
    url: "file://`+upstream+`"
    branch: main
  operations:
    - name: render
      newFiles:
        source: tpl
        dest: "apps/{{ .app }}"
`)
	writeModule(t, moduleDir, `metadata:
  name: parent
spec:
  excludes: [child]
  target:
    url: "file://`+upstream+`"
    branch: main
  modules:
    - name: web
      source: ./child
      params:
        app: web
  operations:
    - name: edit
      shell:
        pure: true
        command: "printf 'more\n' >> README.md"
`)
	return moduleDir, upstream
}

// Full mode returns every changed clone as data, identified by the breadcrumb
// of the module that made it, with no raw output unless asked for.
func TestDiff_FullStructured(t *testing.T) {
	moduleDir, upstream := diffFixture(t)
	ws := t.TempDir()
	res, err := Diff(context.Background(), DiffRequest{ModuleDir: moduleDir, Workspace: ws, Logger: quietLogger()})
	if err != nil {
		t.Fatal(err)
	}
	if res.Mode != ModeFull || res.ModuleName != "parent" || res.Incomplete {
		t.Fatalf("result = %+v", res)
	}
	if len(res.Targets) != 2 {
		t.Fatalf("got %d targets, want the parent's clone and the child's", len(res.Targets))
	}
	parent, child := res.Targets[0], res.Targets[1]
	if !slices.Equal(parent.Path, []string{"parent"}) || !slices.Equal(child.Path, []string{"parent", "web"}) {
		t.Errorf("paths = %v, %v", parent.Path, child.Path)
	}
	for _, tgt := range res.Targets {
		if tgt.Repo != "file://"+upstream || tgt.Branch != "main" || tgt.Raw != "" {
			t.Errorf("target %v: repo %q branch %q raw %q", tgt.Path, tgt.Repo, tgt.Branch, tgt.Raw)
		}
	}
	wantParent := []FileDiff{{Path: "README.md", Status: event.StatusModified, Unified: "@@ -1 +1,2 @@\n readme\n+more\n"}}
	wantChild := []FileDiff{{Path: "apps/web/app.yaml", Status: event.StatusAdded, Unified: "@@ -0,0 +1 @@\n+name: web\n"}}
	if !slices.Equal(parent.Files, wantParent) || !slices.Equal(child.Files, wantChild) {
		t.Errorf("files:\n parent %+v\n child %+v", parent.Files, child.Files)
	}
}

// Quick mode groups the collected diffs by module and target and reports each
// file by its path from the target's root, as full mode does (DF2).
func TestDiff_QuickStructured(t *testing.T) {
	moduleDir, upstream := diffFixture(t)
	res, err := Diff(context.Background(), DiffRequest{ModuleDir: moduleDir, Quick: true, Logger: quietLogger()})
	if err != nil {
		t.Fatal(err)
	}
	if res.Mode != ModeQuick || len(res.Entries) != 1 || len(res.Targets) != 1 {
		t.Fatalf("result = %+v", res)
	}
	tgt := res.Targets[0]
	if !slices.Equal(tgt.Path, []string{"parent", "web"}) || tgt.Repo != "file://"+upstream || tgt.Branch != "main" ||
		tgt.Label != "file://"+upstream+" (main)" {
		t.Errorf("target = %+v", tgt)
	}
	want := []FileDiff{{Path: "apps/web/app.yaml", Status: event.StatusAdded, Unified: "@@ -1 +1,2 @@\n+name: web\n \n"}}
	if !slices.Equal(tgt.Files, want) {
		t.Errorf("files = %+v, want %+v", tgt.Files, want)
	}
	if !strings.HasPrefix(res.Entries[0].Text, "--- /dev/null\n+++ apps/web/app.yaml\n") {
		t.Errorf("entry text = %q", res.Entries[0].Text)
	}
}

// A failed run reports Incomplete with the run's error. Its diffs are read
// only when asked for: reading a full-mode workspace means staging it.
func TestDiff_FailedRun(t *testing.T) {
	upstream := initRepo(t, map[string]string{"README.md": "readme\n"})
	moduleDir := t.TempDir()
	writeModule(t, moduleDir, `metadata:
  name: fails
spec:
  target:
    url: "file://`+upstream+`"
    branch: main
  operations:
    - name: edit
      shell:
        pure: true
        command: "printf 'more\n' >> README.md"
    - name: boom
      shell:
        pure: true
        command: "exit 3"
`)

	ws := t.TempDir()
	res, err := Diff(context.Background(), DiffRequest{ModuleDir: moduleDir, Workspace: ws, Logger: quietLogger()})
	if err == nil || !res.Incomplete || res.Targets != nil {
		t.Fatalf("err = %v, result = %+v; want the run's error and no targets", err, res)
	}
	if staged := git(t, filepath.Join(ws, "00-fails"), "diff", "--cached", "--name-only"); staged != "" {
		t.Errorf("workspace was staged without CollectOnFailure: %q", staged)
	}

	res, err = Diff(context.Background(), DiffRequest{ModuleDir: moduleDir, CollectOnFailure: true, Logger: quietLogger()})
	if err == nil || !strings.Contains(err.Error(), `operation "boom" failed`) || !res.Incomplete {
		t.Fatalf("err = %v, result = %+v", err, res)
	}
	if len(res.Targets) != 1 || len(res.Targets[0].Files) != 1 || res.Targets[0].Files[0].Path != "README.md" {
		t.Errorf("targets = %+v, want the change made before the failure", res.Targets)
	}
}

// A local run without a TargetPath is refused after the module loads, with
// the module named in the result.
func TestRun_LocalRunNeedsTargetPath(t *testing.T) {
	moduleDir := t.TempDir()
	writeModule(t, moduleDir, "metadata:\n  name: local\nspec:\n  operations: []\n")
	res, err := Run(context.Background(), RunRequest{ModuleDir: moduleDir, LocalRun: true, Logger: quietLogger()})
	if !errors.Is(err, ErrLocalRunWithoutTargetPath) {
		t.Fatalf("err = %v", err)
	}
	if res == nil || res.ModuleName != "local" {
		t.Errorf("result = %+v", res)
	}
}

// Validation reports every warning and every failing module, labelled by
// where it sits in the tree, with warnings delivered as they are found.
func TestValidate_Findings(t *testing.T) {
	root := t.TempDir()
	writeModule(t, root, `metadata:
  name: root
spec:
  params:
    - name: unused
    - name: where
      default: ./good
  modules:
    - name: good
      source: ./good
    - name: bad
      source: ./bad
    - name: later
      source: "{{ .where }}"
    - name: missing
      source: ./nope
  operations: []
`)
	writeModule(t, filepath.Join(root, "good"), `metadata:
  name: good
spec:
  params:
    - name: idle
  operations: []
`)
	writeModule(t, filepath.Join(root, "bad"), `metadata:
  name: bad
spec:
  operations:
    - name: x
      shell:
        command: "echo {{ .undeclared }}"
`)

	var streamed []Finding
	res, err := Validate(context.Background(), ValidateRequest{
		Dir: root, Recursive: true, Logger: quietLogger(),
		OnWarning: func(f Finding) { streamed = append(streamed, f) },
	})
	if err == nil {
		t.Fatal("expected the bad child and the missing source to fail")
	}
	if res.Valid() || res.Count != 3 {
		t.Errorf("valid = %v, count = %d; want invalid, 3 configs", res.Valid(), res.Count)
	}
	var warnings []string
	for _, w := range res.Warnings {
		warnings = append(warnings, w.Module+"|"+w.Message)
	}
	if len(warnings) != 3 || !strings.HasPrefix(warnings[0], `|param "unused"`) ||
		!strings.HasPrefix(warnings[1], `good|param "idle"`) || !strings.HasPrefix(warnings[2], `later|source "{{ .where }}" is templated`) {
		t.Errorf("warnings = %q", warnings)
	}
	if !slices.Equal(streamed, res.Warnings) {
		t.Errorf("streamed %v, want the same warnings in the same order", streamed)
	}
	if len(res.Errors) != 2 || res.Errors[0].Module != "bad" || res.Errors[1].Module != "missing" {
		t.Errorf("errors = %+v", res.Errors)
	}
	// The error reads as the CLI prints it: each failure named by its label.
	for _, want := range []string{"bad: ", "missing: "} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error lacks %q: %v", want, err)
		}
	}
	if got := (Finding{Module: "a › b", Message: "m"}).String(); got != "a › b: m" {
		t.Errorf("String() = %q", got)
	}
}

// Inspect selects subjects in the order asked and rolls them up; a module it
// cannot describe fails the report, not the call.
func TestInspect_ReportAndSubjects(t *testing.T) {
	root := t.TempDir()
	writeModule(t, root, `metadata:
  name: top
spec:
  modules:
    - name: a
      source: ./leaf
    - name: broken
      source: ./nope
  operations: []
`)
	writeModule(t, filepath.Join(root, "leaf"), `metadata:
  name: leaf
spec:
  params:
    - name: needed
      required: true
  operations: []
`)
	res, err := Inspect(context.Background(), InspectRequest{ModuleDir: root, Depth: 0, Logger: quietLogger()})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Subjects) != 1 || !slices.Equal(res.Subjects[0].Path, []string{"top"}) {
		t.Errorf("subjects = %+v", res.Subjects)
	}
	if len(res.Report.MissingParams) != 1 || res.Report.MissingParams[0].Name != "needed" {
		t.Errorf("missing = %+v", res.Report.MissingParams)
	}
	if err := res.Report.Err(); err == nil || err.Error() != "1 module(s) could not be inspected" {
		t.Errorf("Report.Err() = %v", err)
	}

	if _, err := Inspect(context.Background(), InspectRequest{ModuleDir: root, Modules: []string{"zzz"}, Logger: quietLogger()}); err == nil {
		t.Error("a query naming no module must fail")
	}
}

// Both modes report each changed file as a diff.file event carrying the same
// data as the result: quick mode as it computes them, full mode once it has
// read the clones back.
func TestDiff_EmitsFileEvents(t *testing.T) {
	for _, quick := range []bool{true, false} {
		moduleDir, _ := diffFixture(t)
		var files []event.Event
		sink := event.Sink(func(e event.Event) {
			if e.Type == event.DiffFile {
				files = append(files, e)
			}
		})
		res, err := Diff(context.Background(), DiffRequest{ModuleDir: moduleDir, Quick: quick, Logger: quietLogger(), Events: sink})
		if err != nil {
			t.Fatal(err)
		}
		var want []string
		for _, tgt := range res.Targets {
			for _, f := range tgt.Files {
				want = append(want, strings.Join(tgt.Path, "/")+"|"+tgt.Label+"|"+f.Path+"|"+f.Status+"|"+f.Unified)
			}
		}
		var got []string
		for _, e := range files {
			got = append(got, strings.Join(e.Path, "/")+"|"+e.Target+"|"+e.Diff.Path+"|"+e.Diff.Status+"|"+e.Diff.Unified)
		}
		if len(want) == 0 || !slices.Equal(got, want) {
			t.Errorf("quick=%v: events\n %q\nwant\n %q", quick, got, want)
		}
	}
}

// A local run reports the clones it made and whose they are, so a caller can
// read back exactly those — and tell bulk items that share a module apart.
func TestRun_LocalReportsClones(t *testing.T) {
	moduleDir, _ := diffFixture(t)
	ws := t.TempDir()
	res, err := Run(context.Background(), RunRequest{ModuleDir: moduleDir, LocalRun: true, TargetPath: ws, Logger: quietLogger()})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string][]string{
		filepath.Join(ws, "00-parent"):   {"parent"},
		filepath.Join(ws, "01-renderer"): {"parent", "web"},
	}
	if len(res.DirLabels) != len(want) {
		t.Fatalf("DirLabels = %v, want %v", res.DirLabels, want)
	}
	for dir, crumb := range want {
		if !slices.Equal(res.DirLabels[dir], crumb) {
			t.Errorf("DirLabels[%s] = %v, want %v", dir, res.DirLabels[dir], crumb)
		}
	}
	res, err = Run(context.Background(), RunRequest{ModuleDir: moduleDir, DryRun: true, Logger: quietLogger()})
	if err != nil || res.DirLabels != nil {
		t.Errorf("dry run: DirLabels = %v, err %v", res.DirLabels, err)
	}
}
