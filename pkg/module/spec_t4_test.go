package module

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/rickliujh/loom/internal/spectest"
	"github.com/rickliujh/loom/pkg/config"
)

// TestSpecT4 verifies spec rule T4 for the module-level loom.yaml fields:
// spec.dynamicParams, spec.excludes/includes, spec.target, and spec.modules.
// It is the companion of pkg/action's TestSpecT4, which covers the operation
// configs. Like that test, it walks each config struct with reflection,
// injects a malformed template into every reachable string, and asserts the
// real execute path (Load, Execute, or resolveChildTarget) returns a template
// parse error.
//
// T4's one exception — spec.params definitions are the source of template
// values and are never rendered — is encoded in t4Exempt, along with param
// names, which are identifiers rather than values.
//
// Not covered here: cmd/run.go renders the root module's spec.target with its
// own copies of these render calls; this harness only reaches the child-module
// path (resolveChildTarget).

// t4Exempt maps "<case>/<field path>" to the reason the field is allowed to
// bypass template rendering.
var t4Exempt = map[string]string{
	"load/Params.[0].Name":        "T4 exception: static param definitions are the source of template values",
	"load/Params.[0].Default":     "T4 exception: static param definitions are the source of template values",
	"load/DynamicParams.[0].Name": "param names are identifiers, not templatable values",
}

// writeLoomSpec marshals a LoomFile around spec and writes it as loom.yaml in dir.
func writeLoomSpec(t *testing.T, dir string, spec config.Spec) {
	t.Helper()
	lf := config.LoomFile{
		APIVersion: config.ExpectedAPIVersion,
		Kind:       config.ExpectedKind,
		Metadata:   config.Metadata{Name: "t4"},
		Spec:       spec,
	}
	data, err := yaml.Marshal(&lf)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "loom.yaml"), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestSpecT4(t *testing.T) {
	cases := []struct {
		name string
		// cfg returns a fresh baseline config whose run path reaches the
		// render of every string field.
		cfg func(t *testing.T) any
		// run drives the config through its real execute path.
		run func(t *testing.T, cfg any) error
	}{
		{
			// Load renders dynamicParams (command always; default on command
			// failure — hence the failing baseline command) and the
			// exclude/include patterns.
			name: "load",
			cfg: func(t *testing.T) any {
				// p and dp are referenced downstream so the baseline has no
				// declared-but-unreferenced param.
				return &config.Spec{
					Params:        []config.ParamDef{{Name: "p", Default: "v"}},
					DynamicParams: []config.DynamicParamDef{{Name: "dp", Command: "exit 1 {{ .p }}", Default: "fallback"}},
					Excludes:      []string{"*.tmp{{ .dp }}"},
					Includes:      []string{"*.keep"},
				}
			},
			run: func(t *testing.T, cfg any) error {
				dir := t.TempDir()
				writeLoomSpec(t, dir, *cfg.(*config.Spec))
				_, err := Load(dir, nil, testLogger())
				return err
			},
		},
		{
			// Execute renders a child ref's name, source, and param values.
			name: "modules",
			cfg: func(t *testing.T) any {
				childDir := t.TempDir()
				writeLoomSpec(t, childDir, config.Spec{
					Params: []config.ParamDef{{Name: "k", Default: "x"}},
					// Referenced so the child has no unreferenced param; the
					// harness walks the ModuleRef below, not this spec.
					Excludes: []string{"*.{{ .k }}"},
				})
				return &config.ModuleRef{
					Name:   "child",
					Source: childDir,
					Params: map[string]string{"k": "v"},
				}
			},
			run: func(t *testing.T, cfg any) error {
				parent := &Module{
					Dir: t.TempDir(),
					Config: &config.LoomFile{
						Metadata: config.Metadata{Name: "t4-parent"},
						Spec:     config.Spec{Modules: []config.ModuleRef{*cfg.(*config.ModuleRef)}},
					},
					Params: map[string]string{},
					Logger: testLogger(),
				}
				return Execute(context.Background(), parent, t.TempDir(), RunOptions{})
			},
		},
		{
			// resolveChildTarget renders url, branch, and featureBranch.
			name: "target",
			cfg: func(t *testing.T) any {
				return &config.TargetSpec{URL: initBareRepo(t), FeatureBranch: "t4-branch"}
			},
			run: func(t *testing.T, cfg any) error {
				childMod := &Module{
					Config: &config.LoomFile{Spec: config.Spec{Target: cfg.(*config.TargetSpec)}},
					Params: map[string]string{},
					Logger: testLogger(),
				}
				_, cleanup, err := resolveChildTarget(context.Background(), childMod, "/unused", &RunOptions{}, nil)
				if cleanup != nil {
					cleanup()
				}
				return err
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Baseline must execute cleanly, otherwise a field's render
			// might never be reached and its subtest would be vacuous.
			if err := tc.run(t, tc.cfg(t)); err != nil {
				t.Fatalf("baseline config failed to execute: %v", err)
			}

			var paths [][]spectest.Step
			spectest.CollectStringPaths(reflect.ValueOf(tc.cfg(t)), nil, &paths)
			if len(paths) == 0 {
				t.Fatal("no string fields found — reflection walk is broken")
			}

			for _, path := range paths {
				t.Run(spectest.PathName(path), func(t *testing.T) {
					if reason, ok := t4Exempt[tc.name+"/"+spectest.PathName(path)]; ok {
						t.Skipf("exempt from T4: %s", reason)
					}
					cfg := tc.cfg(t)
					spectest.SetByPath(reflect.ValueOf(cfg), path, spectest.BadTmpl)
					spectest.AssertTemplateError(t, tc.run(t, cfg))
				})
			}
		})
	}
}
