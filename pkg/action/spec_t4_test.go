package action

import (
	"context"
	"log/slog"
	"os"
	"strings"
	"testing"

	"github.com/rickliujh/loom/pkg/config"
)

// TestSpecT4 verifies that every string field in every action config is passed
// through tmpl.RenderString before use, per spec rule T4: "everything except
// params is templatable."
//
// Method: inject a malformed template into each field one at a time. If the
// field is templated, RenderString returns a parse error. If it's not, the
// malformed string passes through silently — and the test fails.
//
// When adding a new string field to any action config, add a corresponding
// subtest here. A missing subtest won't break the build, but the next person
// to read this file will notice the gap.

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
		t.Fatal("expected template parse error, got nil — field is not templated")
	}
	// template parse errors contain "template" or "unclosed action"
	msg := err.Error()
	if !strings.Contains(msg, "template") && !strings.Contains(msg, "unclosed action") {
		t.Fatalf("expected template parse error, got: %v", err)
	}
}

const badTmpl = "{{ .unterminated"

func TestSpecT4_CommitPush(t *testing.T) {
	base := func() config.CommitPush {
		return config.CommitPush{Message: "msg", Author: "author", Email: "e@x.com"}
	}

	tests := []struct {
		name   string
		mutate func(*config.CommitPush)
	}{
		{"Message", func(c *config.CommitPush) { c.Message = badTmpl }},
		{"Author", func(c *config.CommitPush) { c.Author = badTmpl }},
		{"Email", func(c *config.CommitPush) { c.Email = badTmpl }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := base()
			tt.mutate(&cfg)
			action := &CommitPushAction{Config: cfg}
			err := action.Execute(context.Background(), dryRunCtx(t))
			assertTemplateError(t, err)
		})
	}
}

func TestSpecT4_PR(t *testing.T) {
	base := func() config.PR {
		return config.PR{
			Provider:   "github",
			Title:      "title",
			Body:       "body",
			BaseBranch: "main",
			TokenEnv:   "TOKEN",
			Labels:     []string{"label"},
		}
	}

	tests := []struct {
		name   string
		mutate func(*config.PR)
	}{
		{"Title", func(c *config.PR) { c.Title = badTmpl }},
		{"Body", func(c *config.PR) { c.Body = badTmpl }},
		{"BaseBranch", func(c *config.PR) { c.BaseBranch = badTmpl }},
		{"Provider", func(c *config.PR) { c.Provider = badTmpl }},
		{"TokenEnv", func(c *config.PR) { c.TokenEnv = badTmpl }},
		{"Labels", func(c *config.PR) { c.Labels = []string{badTmpl} }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := base()
			tt.mutate(&cfg)
			action := &PRAction{Config: cfg}
			err := action.Execute(context.Background(), dryRunCtx(t))
			assertTemplateError(t, err)
		})
	}
}

func TestSpecT4_Patch(t *testing.T) {
	// Engine is the only field templated at the action level.
	// Path and Target are file paths resolved by ExpandPath/filepath.Join.
	action := &PatchAction{
		Config: config.Patch{Engine: badTmpl, Path: "p.yaml", Target: "t.yaml"},
	}
	err := action.Execute(context.Background(), dryRunCtx(t))
	assertTemplateError(t, err)
}

func TestSpecT4_Shell(t *testing.T) {
	base := func() config.Shell {
		return config.Shell{Command: "echo ok", Timeout: "1s"}
	}

	tests := []struct {
		name   string
		mutate func(*config.Shell)
	}{
		{"Command", func(c *config.Shell) { c.Command = badTmpl }},
		{"Timeout", func(c *config.Shell) { c.Timeout = badTmpl }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := base()
			tt.mutate(&cfg)
			action := &ShellAction{Config: cfg}
			err := action.Execute(context.Background(), dryRunCtx(t))
			assertTemplateError(t, err)
		})
	}
}
