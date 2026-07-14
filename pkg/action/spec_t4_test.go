package action

import (
	"context"
	"log/slog"
	"os"
	"reflect"
	"testing"

	"github.com/rickliujh/loom/internal/spectest"
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
// Module-level loom.yaml fields (params, dynamicParams, excludes, includes,
// target, modules) are covered by the companion TestSpecT4 in pkg/module.
//
// Fields that are intentionally NOT templated must be listed in t4Exempt with
// a reason.

// t4Exempt maps "<case>/<field path>" to the reason the field is allowed to
// bypass template rendering. Keep this empty unless the spec says otherwise.
var t4Exempt = map[string]string{}

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

			var paths [][]spectest.Step
			spectest.CollectStringPaths(reflect.ValueOf(tc.cfg()), nil, &paths)
			if len(paths) == 0 {
				t.Fatal("no string fields found — reflection walk is broken")
			}

			for _, path := range paths {
				t.Run(spectest.PathName(path), func(t *testing.T) {
					if reason, ok := t4Exempt[tc.name+"/"+spectest.PathName(path)]; ok {
						t.Skipf("exempt from T4: %s", reason)
					}
					cfg := tc.cfg()
					spectest.SetByPath(reflect.ValueOf(cfg), path, spectest.BadTmpl)
					spectest.AssertTemplateError(t, tc.run(t, cfg))
				})
			}
		})
	}
}
