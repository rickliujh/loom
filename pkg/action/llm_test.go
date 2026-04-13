package action

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rickliujh/loom/pkg/config"
	"github.com/rickliujh/loom/pkg/llm"
)

// ---------------------------------------------------------------------------
// L1: Template rendering — all string fields are rendered as Go templates
//     against the module's resolved parameter map before any other step.
// ---------------------------------------------------------------------------

// L1: mode field is templated. Prove by templating mode to "generate" and
// triggering the generate-mode target-exists guard.
func TestLLMAction_L1_ModeTemplated(t *testing.T) {
	targetDir := t.TempDir()
	targetFile := filepath.Join(targetDir, "out.txt")
	os.WriteFile(targetFile, []byte("exists"), 0o644)

	action := &LLMAction{Config: config.LLM{
		Provider: "openai",
		Model:    "m",
		Prompt:   "p",
		Target:   "out.txt",
		Mode:     "{{ .m }}",
	}}

	execCtx := testExecCtx(t, t.TempDir(), targetDir)
	execCtx.Params = map[string]string{"m": "generate"}

	err := action.Execute(context.Background(), execCtx)
	if err == nil {
		t.Fatal("expected 'target already exists' error proving mode was rendered")
	}
	if !strings.Contains(err.Error(), "target already exists") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// L1: retryDelay field is templated. Prove by templating to an invalid
// duration and checking the error contains the rendered value.
func TestLLMAction_L1_RetryDelayTemplated(t *testing.T) {
	targetDir := t.TempDir()

	action := &LLMAction{Config: config.LLM{
		Provider:   "openai",
		Model:      "m",
		Prompt:     "p",
		Target:     "out.txt",
		Mode:       "generate",
		RetryDelay: "{{ .delay }}",
	}}

	execCtx := testExecCtx(t, t.TempDir(), targetDir)
	execCtx.Params = map[string]string{"delay": "bad-duration"}

	err := action.Execute(context.Background(), execCtx)
	if err == nil {
		t.Fatal("expected retryDelay parse error")
	}
	if !strings.Contains(err.Error(), `parsing retryDelay "bad-duration"`) {
		t.Fatalf("expected rendered value in error, got: %v", err)
	}
}

// L1: providerConfig.tokenEnv is templated. Prove by templating to a known
// env var name. We can't easily verify the resolved value without calling
// the LLM, but we verify the render path doesn't error and execution
// proceeds past rendering (hits the provider client creation, which proves
// tokenEnv was rendered without error).
func TestLLMAction_L1_TokenEnvTemplated(t *testing.T) {
	targetDir := t.TempDir()

	action := &LLMAction{Config: config.LLM{
		Provider: "openai",
		Model:    "m",
		Prompt:   "p",
		Target:   "out.txt",
		Mode:     "generate",
		ProviderConfig: &config.LLMProviderConfig{
			TokenEnv: "{{ .envVar }}",
		},
	}}

	execCtx := testExecCtx(t, t.TempDir(), targetDir)
	execCtx.Params = map[string]string{"envVar": "MY_CUSTOM_TOKEN"}

	err := action.Execute(context.Background(), execCtx)
	// Should fail at LLM client creation (no real API), not at template rendering.
	if err == nil {
		t.Fatal("expected error (no real provider), but should not be a template error")
	}
	if strings.Contains(err.Error(), "rendering providerConfig.tokenEnv") {
		t.Fatalf("tokenEnv template rendering failed: %v", err)
	}
}

// L1: provider, model, prompt, systemPrompt, target are templated.
// Prove by using template expressions that resolve, then checking the
// target path in the generate-mode exists error.
func TestLLMAction_L1_TargetTemplated(t *testing.T) {
	targetDir := t.TempDir()
	os.WriteFile(filepath.Join(targetDir, "payments.yaml"), []byte("x"), 0o644)

	action := &LLMAction{Config: config.LLM{
		Provider: "openai",
		Model:    "m",
		Prompt:   "generate {{ .svc }}",
		Target:   "{{ .svc }}.yaml",
		Mode:     "generate",
	}}

	execCtx := testExecCtx(t, t.TempDir(), targetDir)
	execCtx.Params = map[string]string{"svc": "payments"}

	err := action.Execute(context.Background(), execCtx)
	if err == nil {
		t.Fatal("expected target-exists error")
	}
	if !strings.Contains(err.Error(), "payments.yaml") {
		t.Fatalf("expected rendered target in error, got: %v", err)
	}
}

// L1: providerConfig.project and providerConfig.location are templated.
func TestLLMAction_L1_ProviderConfigTemplated(t *testing.T) {
	targetDir := t.TempDir()

	action := &LLMAction{Config: config.LLM{
		Provider: "vertex",
		Model:    "m",
		Prompt:   "p",
		Target:   "out.txt",
		Mode:     "generate",
		ProviderConfig: &config.LLMProviderConfig{
			Project:  "{{ .proj }}",
			Location: "{{ .loc }}",
		},
	}}

	execCtx := testExecCtx(t, t.TempDir(), targetDir)
	execCtx.Params = map[string]string{"proj": "my-project", "loc": "europe-west1"}

	err := action.Execute(context.Background(), execCtx)
	// Will fail at vertex client creation (no ADC), not at template rendering.
	if err != nil && strings.Contains(err.Error(), "rendering providerConfig") {
		t.Fatalf("providerConfig template rendering failed: %v", err)
	}
}

// L1: Bad template expression in any field produces a rendering error.
func TestLLMAction_L1_BadTemplateErrors(t *testing.T) {
	tests := []struct {
		name   string
		config config.LLM
		field  string
	}{
		{
			name:  "provider",
			field: "provider",
			config: config.LLM{
				Provider: "{{ .missing }", // malformed
				Model:    "m",
				Prompt:   "p",
				Target:   "t",
			},
		},
		{
			name:  "model",
			field: "model",
			config: config.LLM{
				Provider: "openai",
				Model:    "{{ .missing }",
				Prompt:   "p",
				Target:   "t",
			},
		},
		{
			name:  "prompt",
			field: "prompt",
			config: config.LLM{
				Provider: "openai",
				Model:    "m",
				Prompt:   "{{ .missing }",
				Target:   "t",
			},
		},
		{
			name:  "systemPrompt",
			field: "systemPrompt",
			config: config.LLM{
				Provider:     "openai",
				Model:        "m",
				Prompt:       "p",
				SystemPrompt: "{{ .missing }",
				Target:       "t",
			},
		},
		{
			name:  "target",
			field: "target",
			config: config.LLM{
				Provider: "openai",
				Model:    "m",
				Prompt:   "p",
				Target:   "{{ .missing }",
			},
		},
		{
			name:  "mode",
			field: "mode",
			config: config.LLM{
				Provider: "openai",
				Model:    "m",
				Prompt:   "p",
				Target:   "t",
				Mode:     "{{ .missing }",
			},
		},
		{
			name:  "retryDelay",
			field: "retryDelay",
			config: config.LLM{
				Provider:   "openai",
				Model:      "m",
				Prompt:     "p",
				Target:     "t",
				RetryDelay: "{{ .missing }",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			action := &LLMAction{Config: tc.config}
			execCtx := testExecCtx(t, t.TempDir(), t.TempDir())

			err := action.Execute(context.Background(), execCtx)
			if err == nil {
				t.Fatalf("expected template error for field %s", tc.field)
			}
			if !strings.Contains(err.Error(), "rendering "+tc.field) {
				t.Fatalf("expected 'rendering %s' in error, got: %v", tc.field, err)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// L2: Generate mode — target must NOT already exist.
// ---------------------------------------------------------------------------

func TestLLMAction_L2_GenerateFailsIfTargetExists(t *testing.T) {
	targetDir := t.TempDir()
	os.WriteFile(filepath.Join(targetDir, "existing.txt"), []byte("data"), 0o644)

	action := &LLMAction{Config: config.LLM{
		Provider: "openai",
		Model:    "m",
		Prompt:   "p",
		Target:   "existing.txt",
		Mode:     "generate",
	}}

	execCtx := testExecCtx(t, t.TempDir(), targetDir)
	err := action.Execute(context.Background(), execCtx)
	if err == nil {
		t.Fatal("expected error when target already exists in generate mode")
	}
	if !strings.Contains(err.Error(), "target already exists") {
		t.Fatalf("expected 'target already exists' error, got: %v", err)
	}
}

// L2: Generate mode is the default when mode is omitted.
func TestLLMAction_L2_GenerateIsDefault(t *testing.T) {
	targetDir := t.TempDir()
	os.WriteFile(filepath.Join(targetDir, "existing.txt"), []byte("data"), 0o644)

	action := &LLMAction{Config: config.LLM{
		Provider: "openai",
		Model:    "m",
		Prompt:   "p",
		Target:   "existing.txt",
		// Mode omitted — should default to "generate"
	}}

	execCtx := testExecCtx(t, t.TempDir(), targetDir)
	err := action.Execute(context.Background(), execCtx)
	if err == nil {
		t.Fatal("expected error: default mode should be generate, which fails on existing target")
	}
	if !strings.Contains(err.Error(), "target already exists") {
		t.Fatalf("expected 'target already exists' error, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// L3: Modify mode — target MUST exist; content prepended to prompt.
// ---------------------------------------------------------------------------

func TestLLMAction_L3_ModifyFailsIfTargetMissing(t *testing.T) {
	targetDir := t.TempDir()

	action := &LLMAction{Config: config.LLM{
		Provider: "openai",
		Model:    "m",
		Prompt:   "p",
		Target:   "nonexistent.txt",
		Mode:     "modify",
	}}

	execCtx := testExecCtx(t, t.TempDir(), targetDir)
	err := action.Execute(context.Background(), execCtx)
	if err == nil {
		t.Fatal("expected error when target missing in modify mode")
	}
	if !strings.Contains(err.Error(), "reading target for modify") {
		t.Fatalf("expected 'reading target for modify' error, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// L8: Dry run — LLM is NOT called and no file is written.
// ---------------------------------------------------------------------------

func TestLLMAction_L8_DryRunNoFileWritten(t *testing.T) {
	targetDir := t.TempDir()
	outPath := filepath.Join(targetDir, "output.txt")

	action := &LLMAction{Config: config.LLM{
		Provider: "openai",
		Model:    "m",
		Prompt:   "p",
		Target:   "output.txt",
		Mode:     "generate",
	}}

	execCtx := testExecCtx(t, t.TempDir(), targetDir)
	execCtx.DryRun = true

	err := action.Execute(context.Background(), execCtx)
	if err != nil {
		t.Fatalf("dry-run should not error: %v", err)
	}

	if _, err := os.Stat(outPath); !os.IsNotExist(err) {
		t.Error("dry-run should not create the output file")
	}
}

// L8: Dry run does not fail even when target exists in generate mode
// (the existence check happens after dry-run returns).
func TestLLMAction_L8_DryRunSkipsExistCheck(t *testing.T) {
	targetDir := t.TempDir()
	os.WriteFile(filepath.Join(targetDir, "exists.txt"), []byte("x"), 0o644)

	action := &LLMAction{Config: config.LLM{
		Provider: "openai",
		Model:    "m",
		Prompt:   "p",
		Target:   "exists.txt",
		Mode:     "generate",
	}}

	execCtx := testExecCtx(t, t.TempDir(), targetDir)
	execCtx.DryRun = true

	err := action.Execute(context.Background(), execCtx)
	if err != nil {
		t.Fatalf("dry-run should not error even if target exists: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Error conditions from spec
// ---------------------------------------------------------------------------

// retryDelay parse error includes the value.
func TestLLMAction_Error_InvalidRetryDelay(t *testing.T) {
	targetDir := t.TempDir()

	action := &LLMAction{Config: config.LLM{
		Provider:   "openai",
		Model:      "m",
		Prompt:     "p",
		Target:     "out.txt",
		Mode:       "generate",
		RetryDelay: "not-a-duration",
	}}

	execCtx := testExecCtx(t, t.TempDir(), targetDir)
	err := action.Execute(context.Background(), execCtx)
	if err == nil {
		t.Fatal("expected error for invalid retryDelay")
	}
	if !strings.Contains(err.Error(), `parsing retryDelay "not-a-duration"`) {
		t.Fatalf("expected retryDelay value in error, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Prompt rendering — verify exact prompts sent to the LLM via mock Infer.
// ---------------------------------------------------------------------------

// fakeInfer returns an InferFunc that captures the InferenceOptions and
// returns a fixed response.
func fakeInfer(captured *llm.InferenceOptions, response string) InferFunc {
	return func(_ context.Context, opts llm.InferenceOptions) (string, error) {
		*captured = opts
		return response, nil
	}
}

// L1+L3: In generate mode, the rendered prompt is sent as-is.
func TestLLMAction_Prompt_GenerateMode(t *testing.T) {
	targetDir := t.TempDir()
	var captured llm.InferenceOptions

	action := &LLMAction{
		Config: config.LLM{
			Provider: "openai",
			Model:    "gpt-4o",
			Prompt:   "Generate a deployment for {{ .svc }} in {{ .env }}.",
			Target:   "deploy/{{ .svc }}.yaml",
			Mode:     "generate",
		},
		Infer: fakeInfer(&captured, "apiVersion: apps/v1"),
	}

	execCtx := testExecCtx(t, t.TempDir(), targetDir)
	execCtx.Params = map[string]string{"svc": "payments", "env": "prod"}

	if err := action.Execute(context.Background(), execCtx); err != nil {
		t.Fatal(err)
	}

	want := "Generate a deployment for payments in prod."
	if captured.Prompt != want {
		t.Errorf("prompt mismatch\ngot:  %q\nwant: %q", captured.Prompt, want)
	}
}

// L3: In modify mode, existing file content is prepended to the rendered
// prompt in the exact format specified by the spec.
func TestLLMAction_Prompt_ModifyModePrepend(t *testing.T) {
	targetDir := t.TempDir()
	existingContent := "# README\nThis is the payments service."
	os.WriteFile(filepath.Join(targetDir, "README.md"), []byte(existingContent), 0o644)

	var captured llm.InferenceOptions

	action := &LLMAction{
		Config: config.LLM{
			Provider: "openai",
			Model:    "gpt-4o",
			Prompt:   "Add a ## {{ .svc }} section describing this service.",
			Target:   "README.md",
			Mode:     "modify",
		},
		Infer: fakeInfer(&captured, "updated content"),
	}

	execCtx := testExecCtx(t, t.TempDir(), targetDir)
	execCtx.Params = map[string]string{"svc": "payments"}

	if err := action.Execute(context.Background(), execCtx); err != nil {
		t.Fatal(err)
	}

	// Spec L3 format:
	//   Here is the existing file content:
	//
	//   ```
	//   <existing file content>
	//   ```
	//
	//   <rendered prompt>
	wantPrompt := fmt.Sprintf(
		"Here is the existing file content:\n\n```\n%s\n```\n\nAdd a ## payments section describing this service.",
		existingContent,
	)
	if captured.Prompt != wantPrompt {
		t.Errorf("modify-mode prompt mismatch\ngot:\n%s\n\nwant:\n%s", captured.Prompt, wantPrompt)
	}
}

// L5: systemPrompt is rendered and passed to inference when non-empty.
func TestLLMAction_Prompt_SystemPromptRendered(t *testing.T) {
	targetDir := t.TempDir()
	var captured llm.InferenceOptions

	action := &LLMAction{
		Config: config.LLM{
			Provider:     "openai",
			Model:        "gpt-4o",
			Prompt:       "Generate config.",
			SystemPrompt: "Output only valid {{ .format }}. No explanation.",
			Target:       "out.yaml",
			Mode:         "generate",
		},
		Infer: fakeInfer(&captured, "key: value"),
	}

	execCtx := testExecCtx(t, t.TempDir(), targetDir)
	execCtx.Params = map[string]string{"format": "YAML"}

	if err := action.Execute(context.Background(), execCtx); err != nil {
		t.Fatal(err)
	}

	wantSys := "Output only valid YAML. No explanation."
	if captured.SystemPrompt != wantSys {
		t.Errorf("systemPrompt mismatch\ngot:  %q\nwant: %q", captured.SystemPrompt, wantSys)
	}
}

// L5: When systemPrompt is empty, it remains empty (no system message sent).
func TestLLMAction_Prompt_EmptySystemPrompt(t *testing.T) {
	targetDir := t.TempDir()
	var captured llm.InferenceOptions

	action := &LLMAction{
		Config: config.LLM{
			Provider: "openai",
			Model:    "gpt-4o",
			Prompt:   "hello",
			Target:   "out.txt",
			Mode:     "generate",
		},
		Infer: fakeInfer(&captured, "world"),
	}

	execCtx := testExecCtx(t, t.TempDir(), targetDir)

	if err := action.Execute(context.Background(), execCtx); err != nil {
		t.Fatal(err)
	}

	if captured.SystemPrompt != "" {
		t.Errorf("expected empty systemPrompt, got: %q", captured.SystemPrompt)
	}
}

// L1: All fields rendered end-to-end — provider, model, prompt, systemPrompt,
// target, mode, retryDelay, tokenEnv, project, location are all templated
// and the correct resolved values reach the inference function.
func TestLLMAction_Prompt_AllFieldsRendered(t *testing.T) {
	targetDir := t.TempDir()
	var captured llm.InferenceOptions

	action := &LLMAction{
		Config: config.LLM{
			Provider:     "{{ .provider }}",
			Model:        "{{ .model }}",
			Prompt:       "deploy {{ .svc }}",
			SystemPrompt: "output {{ .fmt }}",
			Target:       "{{ .svc }}.yaml",
			Mode:         "{{ .mode }}",
			MaxTokens:    1024,
			Retries:      2,
			RetryDelay:   "{{ .delay }}",
			ProviderConfig: &config.LLMProviderConfig{
				TokenEnv: "{{ .tokenVar }}",
				Project:  "{{ .proj }}",
				Location: "{{ .loc }}",
			},
		},
		Infer: fakeInfer(&captured, "result"),
	}

	execCtx := testExecCtx(t, t.TempDir(), targetDir)
	execCtx.Params = map[string]string{
		"provider": "openai",
		"model":    "gpt-4o",
		"svc":      "auth",
		"fmt":      "JSON",
		"mode":     "generate",
		"delay":    "500ms",
		"tokenVar": "MY_KEY",
		"proj":     "my-proj",
		"loc":      "us-east1",
	}

	if err := action.Execute(context.Background(), execCtx); err != nil {
		t.Fatal(err)
	}

	if captured.Provider != "openai" {
		t.Errorf("provider: got %q, want %q", captured.Provider, "openai")
	}
	if captured.Model != "gpt-4o" {
		t.Errorf("model: got %q, want %q", captured.Model, "gpt-4o")
	}
	if captured.Prompt != "deploy auth" {
		t.Errorf("prompt: got %q, want %q", captured.Prompt, "deploy auth")
	}
	if captured.SystemPrompt != "output JSON" {
		t.Errorf("systemPrompt: got %q, want %q", captured.SystemPrompt, "output JSON")
	}
	if captured.MaxTokens != 1024 {
		t.Errorf("maxTokens: got %d, want %d", captured.MaxTokens, 1024)
	}
	if captured.Retries != 2 {
		t.Errorf("retries: got %d, want %d", captured.Retries, 2)
	}
	if captured.RetryDelay != 500*time.Millisecond {
		t.Errorf("retryDelay: got %v, want %v", captured.RetryDelay, 500*time.Millisecond)
	}
	if captured.TokenEnv != "MY_KEY" {
		t.Errorf("tokenEnv: got %q, want %q", captured.TokenEnv, "MY_KEY")
	}
	if captured.Project != "my-proj" {
		t.Errorf("project: got %q, want %q", captured.Project, "my-proj")
	}
	if captured.Location != "us-east1" {
		t.Errorf("location: got %q, want %q", captured.Location, "us-east1")
	}
}

// L3: Modify mode with multiline file content — verify exact format.
func TestLLMAction_Prompt_ModifyModeMultilineContent(t *testing.T) {
	targetDir := t.TempDir()
	existing := "line1\nline2\nline3\n"
	os.WriteFile(filepath.Join(targetDir, "data.txt"), []byte(existing), 0o644)

	var captured llm.InferenceOptions

	action := &LLMAction{
		Config: config.LLM{
			Provider: "openai",
			Model:    "m",
			Prompt:   "Append a fourth line.",
			Target:   "data.txt",
			Mode:     "modify",
		},
		Infer: fakeInfer(&captured, "updated"),
	}

	execCtx := testExecCtx(t, t.TempDir(), targetDir)

	if err := action.Execute(context.Background(), execCtx); err != nil {
		t.Fatal(err)
	}

	// Verify the prepend wrapper format exactly.
	if !strings.HasPrefix(captured.Prompt, "Here is the existing file content:\n\n```\n") {
		t.Error("prompt missing spec-required prefix")
	}
	if !strings.Contains(captured.Prompt, existing) {
		t.Error("prompt does not contain existing file content")
	}
	if !strings.HasSuffix(captured.Prompt, "\n```\n\nAppend a fourth line.") {
		t.Errorf("prompt has wrong suffix:\n%s", captured.Prompt)
	}
}

// Verify LLM output is written to the correct rendered target path.
func TestLLMAction_Prompt_OutputWrittenToTarget(t *testing.T) {
	targetDir := t.TempDir()

	action := &LLMAction{
		Config: config.LLM{
			Provider: "openai",
			Model:    "m",
			Prompt:   "generate",
			Target:   "sub/{{ .name }}.txt",
			Mode:     "generate",
		},
		Infer: fakeInfer(&llm.InferenceOptions{}, "file-content-here"),
	}

	execCtx := testExecCtx(t, t.TempDir(), targetDir)
	execCtx.Params = map[string]string{"name": "output"}

	if err := action.Execute(context.Background(), execCtx); err != nil {
		t.Fatal(err)
	}

	outPath := filepath.Join(targetDir, "sub", "output.txt")
	got, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("expected output at %s: %v", outPath, err)
	}
	if string(got) != "file-content-here" {
		t.Errorf("output content mismatch\ngot:  %q\nwant: %q", string(got), "file-content-here")
	}
}

// Modify mode: old content is prepended to prompt AND response overwrites target.
func TestLLMAction_Prompt_ModifyOverwritesTarget(t *testing.T) {
	targetDir := t.TempDir()
	oldContent := "old content"
	os.WriteFile(filepath.Join(targetDir, "doc.md"), []byte(oldContent), 0o644)

	var captured llm.InferenceOptions

	action := &LLMAction{
		Config: config.LLM{
			Provider: "openai",
			Model:    "m",
			Prompt:   "update this",
			Target:   "doc.md",
			Mode:     "modify",
		},
		Infer: fakeInfer(&captured, "new content from LLM"),
	}

	execCtx := testExecCtx(t, t.TempDir(), targetDir)

	if err := action.Execute(context.Background(), execCtx); err != nil {
		t.Fatal(err)
	}

	// Verify old content was prepended to the prompt sent to LLM.
	wantPrompt := fmt.Sprintf("Here is the existing file content:\n\n```\n%s\n```\n\nupdate this", oldContent)
	if captured.Prompt != wantPrompt {
		t.Errorf("old content not prepended to prompt\ngot:\n%s\n\nwant:\n%s", captured.Prompt, wantPrompt)
	}

	// Verify target overwritten with LLM response.
	got, _ := os.ReadFile(filepath.Join(targetDir, "doc.md"))
	if string(got) != "new content from LLM" {
		t.Errorf("target not overwritten\ngot:  %q\nwant: %q", string(got), "new content from LLM")
	}
}

// L8: Dry run does NOT call the inference function.
func TestLLMAction_Prompt_DryRunNoInferCall(t *testing.T) {
	targetDir := t.TempDir()
	called := false

	action := &LLMAction{
		Config: config.LLM{
			Provider: "openai",
			Model:    "m",
			Prompt:   "p",
			Target:   "out.txt",
			Mode:     "generate",
		},
		Infer: func(_ context.Context, _ llm.InferenceOptions) (string, error) {
			called = true
			return "should not happen", nil
		},
	}

	execCtx := testExecCtx(t, t.TempDir(), targetDir)
	execCtx.DryRun = true

	if err := action.Execute(context.Background(), execCtx); err != nil {
		t.Fatal(err)
	}
	if called {
		t.Error("dry-run should not call the inference function")
	}
}
