package config

import (
	"strings"
	"testing"
)

func validLoomFile() *LoomFile {
	return &LoomFile{
		APIVersion: ExpectedAPIVersion,
		Kind:       ExpectedKind,
		Metadata:   Metadata{Name: "test"},
		Spec: Spec{
			Operations: []Operation{
				{Name: "op1", Shell: &Shell{Command: "echo hi"}},
			},
		},
	}
}

func TestValidate_DynamicParamIsValid(t *testing.T) {
	lf := validLoomFile()
	lf.Spec.DynamicParams = []DynamicParamDef{
		{Name: "foo", Command: "echo val"},
	}

	if err := Validate(lf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidate_DynamicParamRequiresCommand(t *testing.T) {
	lf := validLoomFile()
	lf.Spec.DynamicParams = []DynamicParamDef{
		{Name: "foo", Command: ""},
	}

	err := Validate(lf)
	if err == nil {
		t.Fatal("expected error for missing command")
	}
	if !strings.Contains(err.Error(), "command is required") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidate_DuplicateNameAcrossParamsAndDynamic(t *testing.T) {
	lf := validLoomFile()
	lf.Spec.Params = []ParamDef{
		{Name: "foo", Default: "bar"},
	}
	lf.Spec.DynamicParams = []DynamicParamDef{
		{Name: "foo", Command: "echo val"},
	}

	err := Validate(lf)
	if err == nil {
		t.Fatal("expected error for duplicate name")
	}
	if !strings.Contains(err.Error(), "duplicate param name") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidate_DynamicParamEmptyName(t *testing.T) {
	lf := validLoomFile()
	lf.Spec.DynamicParams = []DynamicParamDef{
		{Name: "", Command: "echo val"},
	}

	err := Validate(lf)
	if err == nil {
		t.Fatal("expected error for empty name")
	}
	if !strings.Contains(err.Error(), "name cannot be empty") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidate_LLMOperationValid(t *testing.T) {
	lf := &LoomFile{
		APIVersion: ExpectedAPIVersion,
		Kind:       ExpectedKind,
		Metadata:   Metadata{Name: "test"},
		Spec: Spec{
			Operations: []Operation{
				{Name: "gen", LLM: &LLM{
					Provider: "openai",
					Model:    "gpt-4o",
					Prompt:   "Generate a config file",
					Target:   "output.yaml",
				}},
			},
		},
	}
	if err := Validate(lf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidate_LLMInvalidProvider(t *testing.T) {
	lf := &LoomFile{
		APIVersion: ExpectedAPIVersion,
		Kind:       ExpectedKind,
		Metadata:   Metadata{Name: "test"},
		Spec: Spec{
			Operations: []Operation{
				{Name: "gen", LLM: &LLM{
					Provider: "bedrock",
					Model:    "model",
					Prompt:   "prompt",
					Target:   "out.yaml",
				}},
			},
		},
	}
	err := Validate(lf)
	if err == nil {
		t.Fatal("expected error for unsupported provider")
	}
	if !strings.Contains(err.Error(), "unknown llm provider") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidate_LLMMissingModel(t *testing.T) {
	lf := &LoomFile{
		APIVersion: ExpectedAPIVersion,
		Kind:       ExpectedKind,
		Metadata:   Metadata{Name: "test"},
		Spec: Spec{
			Operations: []Operation{
				{Name: "gen", LLM: &LLM{
					Provider: "openai",
					Prompt:   "prompt",
					Target:   "out.yaml",
				}},
			},
		},
	}
	err := Validate(lf)
	if err == nil {
		t.Fatal("expected error for missing model")
	}
	if !strings.Contains(err.Error(), "llm model is required") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidate_LLMInvalidMode(t *testing.T) {
	lf := &LoomFile{
		APIVersion: ExpectedAPIVersion,
		Kind:       ExpectedKind,
		Metadata:   Metadata{Name: "test"},
		Spec: Spec{
			Operations: []Operation{
				{Name: "gen", LLM: &LLM{
					Provider: "anthropic",
					Model:    "claude-sonnet-4-20250514",
					Prompt:   "prompt",
					Target:   "out.yaml",
					Mode:     "summarize",
				}},
			},
		},
	}
	err := Validate(lf)
	if err == nil {
		t.Fatal("expected error for invalid mode")
	}
	if !strings.Contains(err.Error(), "unknown llm mode") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidate_LLMOpenRouterValid(t *testing.T) {
	lf := &LoomFile{
		APIVersion: ExpectedAPIVersion,
		Kind:       ExpectedKind,
		Metadata:   Metadata{Name: "test"},
		Spec: Spec{
			Operations: []Operation{
				{Name: "gen", LLM: &LLM{
					Provider: "openrouter",
					Model:    "anthropic/claude-sonnet-4-20250514",
					Prompt:   "Generate a config file",
					Target:   "output.yaml",
				}},
			},
		},
	}
	if err := Validate(lf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidate_LLMVertexRequiresProject(t *testing.T) {
	lf := &LoomFile{
		APIVersion: ExpectedAPIVersion,
		Kind:       ExpectedKind,
		Metadata:   Metadata{Name: "test"},
		Spec: Spec{
			Operations: []Operation{
				{Name: "gen", LLM: &LLM{
					Provider: "vertex",
					Model:    "gemini-2.5-flash",
					Prompt:   "prompt",
					Target:   "out.yaml",
				}},
			},
		},
	}
	err := Validate(lf)
	if err == nil {
		t.Fatal("expected error for missing project")
	}
	if !strings.Contains(err.Error(), "project is required") {
		t.Errorf("unexpected error: %v", err)
	}
}
