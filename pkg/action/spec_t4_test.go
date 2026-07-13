package action

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/rickliujh/loom/pkg/config"
	"github.com/rickliujh/loom/pkg/llm"
)

// TestSpecT4 verifies spec rule T4: "everything except params is templatable."
//
// Instead of hand-listing fields, it walks every action config struct with
// reflection and, for each reachable string field (including nested structs,
// pointers, and slice elements), injects a malformed template and asserts
// Execute returns a template parse error. A string field that is added to any
// action config but never passed through tmpl.RenderString fails this test
// automatically — no subtest needs to be written.
//
// Fields that are intentionally NOT templated must be listed in t4Exempt with
// a reason.

const badTmpl = "{{ .unterminated"

// t4Exempt maps "<case>/<field path>" to the reason the field is allowed to
// bypass template rendering. Keep this empty unless the spec says otherwise.
var t4Exempt = map[string]string{}

type t4Step struct {
	field bool // true: struct field index; false: slice index
	index int
	name  string
}

// collectStringPaths records the path of every settable string reachable from v.
func collectStringPaths(v reflect.Value, prefix []t4Step, out *[][]t4Step) {
	switch v.Kind() {
	case reflect.String:
		cp := make([]t4Step, len(prefix))
		copy(cp, prefix)
		*out = append(*out, cp)
	case reflect.Pointer:
		if !v.IsNil() {
			collectStringPaths(v.Elem(), prefix, out)
		}
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			f := v.Type().Field(i)
			if !f.IsExported() {
				continue
			}
			collectStringPaths(v.Field(i), append(prefix, t4Step{true, i, f.Name}), out)
		}
	case reflect.Slice:
		for i := 0; i < v.Len(); i++ {
			collectStringPaths(v.Index(i), append(prefix, t4Step{false, i, fmt.Sprintf("[%d]", i)}), out)
		}
	}
}

func setByPath(root reflect.Value, path []t4Step, val string) {
	v := root
	for _, s := range path {
		for v.Kind() == reflect.Pointer {
			v = v.Elem()
		}
		if s.field {
			v = v.Field(s.index)
		} else {
			v = v.Index(s.index)
		}
	}
	v.SetString(val)
}

func pathName(path []t4Step) string {
	parts := make([]string, 0, len(path))
	for _, s := range path {
		parts = append(parts, s.name)
	}
	return strings.Join(parts, ".")
}

func dryRunCtx(t *testing.T) *ExecutionContext {
	t.Helper()
	return &ExecutionContext{
		ModuleDir: t.TempDir(),
		TargetDir: t.TempDir(),
		Params:    map[string]string{},
		DryRun:    true,
		Logger:    slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})),
	}
}

func assertTemplateError(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("expected template parse error, got nil — field is not templated (spec T4 violation)")
	}
	msg := err.Error()
	if !strings.Contains(msg, "template") && !strings.Contains(msg, "unclosed action") {
		t.Fatalf("expected template parse error, got: %v", err)
	}
}

func TestSpecT4(t *testing.T) {
	cases := []struct {
		name string
		// cfg returns a fresh baseline config whose Execute path reaches
		// the render of every string field.
		cfg func() any
		// run wraps the config in its action and executes it.
		run func(t *testing.T, cfg any) error
	}{
		{
			name: "newFiles",
			cfg:  func() any { return &config.NewFiles{Source: ".", Dest: "out"} },
			run: func(t *testing.T, cfg any) error {
				a := &NewFilesAction{Config: *cfg.(*config.NewFiles)}
				return a.Execute(context.Background(), dryRunCtx(t))
			},
		},
		{
			name: "patch",
			cfg:  func() any { return &config.Patch{Engine: "smp", Path: "patch.yaml", Target: "target.yaml"} },
			run: func(t *testing.T, cfg any) error {
				a := &PatchAction{Config: *cfg.(*config.Patch)}
				return a.Execute(context.Background(), dryRunCtx(t))
			},
		},
		{
			name: "shell",
			cfg:  func() any { return &config.Shell{Command: "true", Timeout: "1s"} },
			run: func(t *testing.T, cfg any) error {
				a := &ShellAction{Config: *cfg.(*config.Shell)}
				return a.Execute(context.Background(), dryRunCtx(t))
			},
		},
		{
			name: "commitPush",
			cfg:  func() any { return &config.CommitPush{Message: "msg", Author: "author", Email: "e@x.com"} },
			run: func(t *testing.T, cfg any) error {
				a := &CommitPushAction{Config: *cfg.(*config.CommitPush)}
				return a.Execute(context.Background(), dryRunCtx(t))
			},
		},
		{
			name: "pr",
			cfg: func() any {
				return &config.PR{
					Provider:   "github",
					Title:      "title",
					Body:       "body",
					BaseBranch: "main",
					Labels:     []string{"label"},
					TokenEnv:   "TOKEN_ENV",
				}
			},
			run: func(t *testing.T, cfg any) error {
				a := &PRAction{Config: *cfg.(*config.PR)}
				return a.Execute(context.Background(), dryRunCtx(t))
			},
		},
		{
			// llm renders retryDelay after the dry-run early return, so it
			// runs for real with a stubbed inference function.
			name: "llm",
			cfg: func() any {
				return &config.LLM{
					Provider:     "anthropic",
					Model:        "model",
					Prompt:       "prompt",
					SystemPrompt: "system",
					Target:       "out.txt",
					Mode:         "generate",
					RetryDelay:   "1ms",
					ProviderConfig: &config.LLMProviderConfig{
						TokenEnv: "TOKEN_ENV",
						Project:  "project",
						Location: "location",
					},
				}
			},
			run: func(t *testing.T, cfg any) error {
				a := &LLMAction{
					Config: *cfg.(*config.LLM),
					Infer: func(ctx context.Context, opts llm.InferenceOptions) (string, error) {
						return "generated", nil
					},
				}
				execCtx := dryRunCtx(t)
				execCtx.DryRun = false
				return a.Execute(context.Background(), execCtx)
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Baseline must execute cleanly, otherwise a field's render
			// might never be reached and its subtest would be vacuous.
			if err := tc.run(t, tc.cfg()); err != nil {
				t.Fatalf("baseline config failed to execute: %v", err)
			}

			var paths [][]t4Step
			collectStringPaths(reflect.ValueOf(tc.cfg()), nil, &paths)
			if len(paths) == 0 {
				t.Fatal("no string fields found — reflection walk is broken")
			}

			for _, path := range paths {
				t.Run(pathName(path), func(t *testing.T) {
					if reason, ok := t4Exempt[tc.name+"/"+pathName(path)]; ok {
						t.Skipf("exempt from T4: %s", reason)
					}
					cfg := tc.cfg()
					setByPath(reflect.ValueOf(cfg), path, badTmpl)
					assertTemplateError(t, tc.run(t, cfg))
				})
			}
		})
	}
}
