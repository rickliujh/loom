package module

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEvalCondition_IF1_EmptyRuns(t *testing.T) {
	for _, raw := range []string{"", "   ", "\t\n"} {
		run, err := evalCondition(raw, nil, t.TempDir())
		if err != nil {
			t.Fatalf("raw %q: unexpected error: %v", raw, err)
		}
		if !run {
			t.Errorf("raw %q: expected run=true for empty predicate", raw)
		}
	}
}

func TestEvalCondition_IF3_ExitCodeSemantics(t *testing.T) {
	cases := []struct {
		name    string
		cmd     string
		wantRun bool
	}{
		{"exit zero runs", "true", true},
		{"exit nonzero skips", "false", false},
		{"explicit exit 1 skips", "exit 1", false},
		{"explicit exit 3 skips", "exit 3", false},
		{"test success runs", "[ 1 = 1 ]", true},
		{"test failure skips", "[ 1 = 2 ]", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			run, err := evalCondition(tc.cmd, nil, t.TempDir())
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if run != tc.wantRun {
				t.Errorf("cmd %q: run = %v, want %v", tc.cmd, run, tc.wantRun)
			}
		})
	}
}

func TestEvalCondition_IF2_Templated(t *testing.T) {
	params := map[string]string{"env": "prod"}
	// Rendered to `[ prod = prod ]`, which succeeds.
	run, err := evalCondition(`[ {{ .env }} = prod ]`, params, t.TempDir())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !run {
		t.Error("expected run=true when templated predicate succeeds")
	}

	// Rendered to `[ prod = dev ]`, which fails.
	run, err = evalCondition(`[ {{ .env }} = dev ]`, params, t.TempDir())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if run {
		t.Error("expected run=false when templated predicate fails")
	}
}

func TestEvalCondition_IF4_RunsInWorkDir(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "marker"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	// The file exists only in dir, so the predicate is true only when run there.
	run, err := evalCondition("test -f marker", nil, dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !run {
		t.Error("expected predicate to run in workDir where marker exists")
	}

	run, err = evalCondition("test -f marker", nil, t.TempDir())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if run {
		t.Error("expected predicate false in a dir without marker")
	}
}

func TestEvalCondition_TemplateErrorSurfaces(t *testing.T) {
	_, err := evalCondition("{{ .unterminated", nil, t.TempDir())
	if err == nil {
		t.Fatal("expected a template render error")
	}
	if !strings.Contains(err.Error(), "template") {
		t.Errorf("expected template error, got: %v", err)
	}
}
