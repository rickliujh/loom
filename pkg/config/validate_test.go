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

// --- apiVersion / kind / metadata ---

func TestValidate_ValidFile(t *testing.T) {
	if err := Validate(validLoomFile()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidate_WrongAPIVersion(t *testing.T) {
	lf := validLoomFile()
	lf.APIVersion = "wrong/v1"

	err := Validate(lf)
	if err == nil {
		t.Fatal("expected error for wrong apiVersion")
	}
	if !strings.Contains(err.Error(), `unsupported apiVersion "wrong/v1"`) {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidate_WrongKind(t *testing.T) {
	lf := validLoomFile()
	lf.Kind = "NotLoom"

	err := Validate(lf)
	if err == nil {
		t.Fatal("expected error for wrong kind")
	}
	if !strings.Contains(err.Error(), `unsupported kind "NotLoom"`) {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidate_MissingMetadataName(t *testing.T) {
	lf := validLoomFile()
	lf.Metadata.Name = ""

	err := Validate(lf)
	if err == nil {
		t.Fatal("expected error for missing metadata.name")
	}
	if !strings.Contains(err.Error(), "metadata.name is required") {
		t.Errorf("unexpected error: %v", err)
	}
}

// --- params ---

func TestValidate_ParamEmptyName(t *testing.T) {
	lf := validLoomFile()
	lf.Spec.Params = []ParamDef{{Name: ""}}

	err := Validate(lf)
	if err == nil {
		t.Fatal("expected error for empty param name")
	}
	if !strings.Contains(err.Error(), "param name cannot be empty") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidate_DuplicateParamName(t *testing.T) {
	lf := validLoomFile()
	lf.Spec.Params = []ParamDef{
		{Name: "foo", Default: "a"},
		{Name: "foo", Default: "b"},
	}

	err := Validate(lf)
	if err == nil {
		t.Fatal("expected error for duplicate param name")
	}
	if !strings.Contains(err.Error(), `duplicate param name "foo"`) {
		t.Errorf("unexpected error: %v", err)
	}
}

// --- dynamicParams ---

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

// --- operations ---

func TestValidate_OperationNameEmpty(t *testing.T) {
	lf := validLoomFile()
	lf.Spec.Operations = []Operation{
		{Name: "", Shell: &Shell{Command: "echo hi"}},
	}

	err := Validate(lf)
	if err == nil {
		t.Fatal("expected error for empty operation name")
	}
	if !strings.Contains(err.Error(), "operation name cannot be empty") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidate_OperationNameDuplicate(t *testing.T) {
	lf := validLoomFile()
	lf.Spec.Operations = []Operation{
		{Name: "dup", Shell: &Shell{Command: "echo a"}},
		{Name: "dup", Shell: &Shell{Command: "echo b"}},
	}

	err := Validate(lf)
	if err == nil {
		t.Fatal("expected error for duplicate operation name")
	}
	if !strings.Contains(err.Error(), `duplicate operation name "dup"`) {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidate_OperationMustHaveExactlyOneAction(t *testing.T) {
	lf := validLoomFile()
	lf.Spec.Operations = []Operation{
		{Name: "none"}, // zero actions
	}

	err := Validate(lf)
	if err == nil {
		t.Fatal("expected error for zero action types")
	}
	if !strings.Contains(err.Error(), "must have exactly one action type, got 0") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidate_OperationMultipleActions(t *testing.T) {
	lf := validLoomFile()
	lf.Spec.Operations = []Operation{
		{
			Name:  "multi",
			Shell: &Shell{Command: "echo"},
			Patch: &Patch{Path: "p.yaml", Target: "t.yaml"},
		},
	}

	err := Validate(lf)
	if err == nil {
		t.Fatal("expected error for multiple action types")
	}
	if !strings.Contains(err.Error(), "must have exactly one action type, got 2") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidate_PatchEngineUnknown(t *testing.T) {
	lf := validLoomFile()
	lf.Spec.Operations = []Operation{
		{Name: "patch-op", Patch: &Patch{Path: "p.yaml", Target: "t.yaml", Engine: "unknown"}},
	}

	err := Validate(lf)
	if err == nil {
		t.Fatal("expected error for unknown patch engine")
	}
	if !strings.Contains(err.Error(), `unknown patch engine "unknown"`) {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidate_PatchEngineSMP(t *testing.T) {
	lf := validLoomFile()
	lf.Spec.Operations = []Operation{
		{Name: "patch-op", Patch: &Patch{Path: "p.yaml", Target: "t.yaml", Engine: "smp"}},
	}

	if err := Validate(lf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidate_PatchEngineJSON6902(t *testing.T) {
	lf := validLoomFile()
	lf.Spec.Operations = []Operation{
		{Name: "patch-op", Patch: &Patch{Path: "p.yaml", Target: "t.yaml", Engine: "json6902"}},
	}

	if err := Validate(lf); err != nil {
		t.Fatalf("unexpected error: %v", err)
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
					Provider: "cohere",
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
					Prompt:   "prompt", Target: "out.yaml",
					Mode: "summarize",
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

func TestValidate_LLMBedrockValid(t *testing.T) {
	lf := &LoomFile{
		APIVersion: ExpectedAPIVersion,
		Kind:       ExpectedKind,
		Metadata:   Metadata{Name: "test"},
		Spec: Spec{
			Operations: []Operation{
				{Name: "gen", LLM: &LLM{
					Provider: "bedrock",
					Model:    "anthropic.claude-sonnet-4-20250514-v1:0",
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
	if !strings.Contains(err.Error(), "providerConfig.project is required") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidate_LLMRetryValid(t *testing.T) {
	lf := &LoomFile{
		APIVersion: ExpectedAPIVersion,
		Kind:       ExpectedKind,
		Metadata:   Metadata{Name: "test"},
		Spec: Spec{
			Operations: []Operation{
				{Name: "gen", LLM: &LLM{
					Provider:   "openai",
					Model:      "gpt-4o",
					Prompt:     "prompt",
					Target:     "out.yaml",
					Retries:    3,
					RetryDelay: "5s",
				}},
			},
		},
	}
	if err := Validate(lf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidate_LLMRetryInvalidDelay(t *testing.T) {
	lf := &LoomFile{
		APIVersion: ExpectedAPIVersion,
		Kind:       ExpectedKind,
		Metadata:   Metadata{Name: "test"},
		Spec: Spec{
			Operations: []Operation{
				{Name: "gen", LLM: &LLM{
					Provider:   "openai",
					Model:      "gpt-4o",
					Prompt:     "prompt",
					Target:     "out.yaml",
					Retries:    3,
					RetryDelay: "notaduration",
				}},
			},
		},
	}
	err := Validate(lf)
	if err == nil {
		t.Fatal("expected error for invalid retryDelay")
	}
	if !strings.Contains(err.Error(), "invalid llm retryDelay") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidate_LLMVertexWithProviderConfig(t *testing.T) {
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
					ProviderConfig: &LLMProviderConfig{
						Project:  "my-project",
						Location: "us-central1",
					},
				}},
			},
		},
	}
	if err := Validate(lf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
