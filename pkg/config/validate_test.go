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

// --- error collection ---

func TestValidate_CollectsAllErrors(t *testing.T) {
	lf := validLoomFile()
	lf.Metadata.Name = ""
	lf.Spec.Params = []ParamDef{{Name: "dup"}, {Name: "dup"}}
	lf.Spec.Operations = []Operation{
		{Name: "sh", Shell: &Shell{}},
	}

	err := Validate(lf)
	if err == nil {
		t.Fatal("expected errors")
	}
	for _, want := range []string{
		"metadata.name is required",
		`duplicate param name "dup"`,
		"shell command is required",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("expected error to contain %q, got: %v", want, err)
		}
	}
}

// --- target ---

func TestValidate_TargetURLRequired(t *testing.T) {
	lf := validLoomFile()
	lf.Spec.Target = &TargetSpec{Branch: "main"}

	err := Validate(lf)
	if err == nil {
		t.Fatal("expected error for missing target url")
	}
	if !strings.Contains(err.Error(), "spec.target.url is required") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidate_TargetValid(t *testing.T) {
	lf := validLoomFile()
	lf.Spec.Target = &TargetSpec{URL: "https://github.com/org/repo.git", FeatureBranch: "loom/{{ .svc }}"}

	if err := Validate(lf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// --- child module refs ---

func TestValidate_ModuleNameEmpty(t *testing.T) {
	lf := validLoomFile()
	lf.Spec.Modules = []ModuleRef{{Name: "", Source: "../child"}}

	err := Validate(lf)
	if err == nil {
		t.Fatal("expected error for empty module name")
	}
	if !strings.Contains(err.Error(), "module name cannot be empty") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidate_ModuleNameDuplicate(t *testing.T) {
	lf := validLoomFile()
	lf.Spec.Modules = []ModuleRef{
		{Name: "child", Source: "../a"},
		{Name: "child", Source: "../b"},
	}

	err := Validate(lf)
	if err == nil {
		t.Fatal("expected error for duplicate module name")
	}
	if !strings.Contains(err.Error(), `duplicate module name "child"`) {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidate_ModuleSourceRequired(t *testing.T) {
	lf := validLoomFile()
	lf.Spec.Modules = []ModuleRef{{Name: "child"}}

	err := Validate(lf)
	if err == nil {
		t.Fatal("expected error for missing module source")
	}
	if !strings.Contains(err.Error(), `module "child": source is required`) {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidate_ModuleValid(t *testing.T) {
	lf := validLoomFile()
	lf.Spec.Modules = []ModuleRef{
		{Name: "onboard-{{ .svc }}", Source: "../child", Params: map[string]string{"svc": "{{ .svc }}"}},
	}

	if err := Validate(lf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// --- template syntax ---

func TestValidate_TemplateSyntaxErrorInShellCommand(t *testing.T) {
	lf := validLoomFile()
	lf.Spec.Operations = []Operation{
		{Name: "sh", Shell: &Shell{Command: "echo {{ .svc"}},
	}

	err := Validate(lf)
	if err == nil {
		t.Fatal("expected error for unclosed template action")
	}
	if !strings.Contains(err.Error(), `operation "sh" shell.command: invalid template`) {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidate_TemplateSyntaxErrorInExcludes(t *testing.T) {
	lf := validLoomFile()
	lf.Spec.Excludes = []string{"{{ .dir }/**"}

	err := Validate(lf)
	if err == nil {
		t.Fatal("expected error for malformed template")
	}
	if !strings.Contains(err.Error(), "spec.excludes[0]: invalid template") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidate_TemplateSyntaxErrorInDynamicParamCommand(t *testing.T) {
	lf := validLoomFile()
	lf.Spec.DynamicParams = []DynamicParamDef{
		{Name: "sha", Command: "git rev-parse {{ .branch"},
	}

	err := Validate(lf)
	if err == nil {
		t.Fatal("expected error for unclosed template action")
	}
	if !strings.Contains(err.Error(), `dynamicParam "sha" command: invalid template`) {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidate_TemplateSyntaxErrorInModuleSource(t *testing.T) {
	lf := validLoomFile()
	lf.Spec.Modules = []ModuleRef{
		{Name: "child", Source: "{{ if .x }../a"},
	}

	err := Validate(lf)
	if err == nil {
		t.Fatal("expected error for malformed template")
	}
	if !strings.Contains(err.Error(), `module "child" source: invalid template`) {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidate_TemplateWithFuncsValid(t *testing.T) {
	lf := validLoomFile()
	lf.Spec.Operations = []Operation{
		{Name: "sh", Shell: &Shell{Command: `echo {{ default "dev" .env | upper }}`}},
	}

	if err := Validate(lf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidate_TemplateUnknownFuncRejected(t *testing.T) {
	lf := validLoomFile()
	lf.Spec.Operations = []Operation{
		{Name: "sh", Shell: &Shell{Command: "echo {{ nosuchfunc .env }}"}},
	}

	err := Validate(lf)
	if err == nil {
		t.Fatal("expected error for unknown template function")
	}
	if !strings.Contains(err.Error(), `operation "sh" shell.command: invalid template`) {
		t.Errorf("unexpected error: %v", err)
	}
}

// --- templated enum values skipped ---

func TestValidate_PatchEngineTemplatedSkipped(t *testing.T) {
	lf := validLoomFile()
	lf.Spec.Operations = []Operation{
		{Name: "patch-op", Patch: &Patch{Path: "p.yaml", Target: "t.yaml", Engine: "{{ .engine }}"}},
	}

	if err := Validate(lf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidate_LLMRetryDelayTemplatedSkipped(t *testing.T) {
	lf := validLoomFile()
	lf.Spec.Operations = []Operation{
		{Name: "gen", LLM: &LLM{
			Provider:   "openai",
			Model:      "gpt-4o",
			Prompt:     "prompt",
			Target:     "out.yaml",
			RetryDelay: "{{ .delay }}",
		}},
	}

	if err := Validate(lf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidate_LLMModeTemplatedSkipped(t *testing.T) {
	lf := validLoomFile()
	lf.Spec.Operations = []Operation{
		{Name: "gen", LLM: &LLM{
			Provider: "openai",
			Model:    "gpt-4o",
			Prompt:   "prompt",
			Target:   "out.yaml",
			Mode:     "{{ .mode }}",
		}},
	}

	if err := Validate(lf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// --- per-action required fields ---

func TestValidate_NewFilesSourceRequired(t *testing.T) {
	lf := validLoomFile()
	lf.Spec.Operations = []Operation{
		{Name: "nf", NewFiles: &NewFiles{Dest: "out/"}},
	}

	err := Validate(lf)
	if err == nil {
		t.Fatal("expected error for missing newFiles source")
	}
	if !strings.Contains(err.Error(), "newFiles source is required") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidate_PatchPathRequired(t *testing.T) {
	lf := validLoomFile()
	lf.Spec.Operations = []Operation{
		{Name: "patch-op", Patch: &Patch{Target: "t.yaml"}},
	}

	err := Validate(lf)
	if err == nil {
		t.Fatal("expected error for missing patch path")
	}
	if !strings.Contains(err.Error(), "patch path is required") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidate_PatchTargetRequired(t *testing.T) {
	lf := validLoomFile()
	lf.Spec.Operations = []Operation{
		{Name: "patch-op", Patch: &Patch{Path: "p.yaml"}},
	}

	err := Validate(lf)
	if err == nil {
		t.Fatal("expected error for missing patch target")
	}
	if !strings.Contains(err.Error(), "patch target is required") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidate_ShellCommandRequired(t *testing.T) {
	lf := validLoomFile()
	lf.Spec.Operations = []Operation{
		{Name: "sh", Shell: &Shell{}},
	}

	err := Validate(lf)
	if err == nil {
		t.Fatal("expected error for missing shell command")
	}
	if !strings.Contains(err.Error(), "shell command is required") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidate_ShellTimeoutInvalid(t *testing.T) {
	lf := validLoomFile()
	lf.Spec.Operations = []Operation{
		{Name: "sh", Shell: &Shell{Command: "echo hi", Timeout: "not-a-duration"}},
	}

	err := Validate(lf)
	if err == nil {
		t.Fatal("expected error for invalid shell timeout")
	}
	if !strings.Contains(err.Error(), "invalid shell timeout") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidate_ShellTimeoutTemplatedSkipped(t *testing.T) {
	lf := validLoomFile()
	lf.Spec.Operations = []Operation{
		{Name: "sh", Shell: &Shell{Command: "echo hi", Timeout: "{{ .dur }}"}},
	}

	if err := Validate(lf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidate_ShellTimeoutValid(t *testing.T) {
	lf := validLoomFile()
	lf.Spec.Operations = []Operation{
		{Name: "sh", Shell: &Shell{Command: "echo hi", Timeout: "30s"}},
	}

	if err := Validate(lf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidate_CommitPushMessageRequired(t *testing.T) {
	lf := validLoomFile()
	lf.Spec.Operations = []Operation{
		{Name: "cp", CommitPush: &CommitPush{Author: "loom"}},
	}

	err := Validate(lf)
	if err == nil {
		t.Fatal("expected error for missing commitPush message")
	}
	if !strings.Contains(err.Error(), "commitPush message is required") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidate_PRProviderRequired(t *testing.T) {
	lf := validLoomFile()
	lf.Spec.Operations = []Operation{
		{Name: "pr", PR: &PR{Title: "title"}},
	}

	err := Validate(lf)
	if err == nil {
		t.Fatal("expected error for missing pr provider")
	}
	if !strings.Contains(err.Error(), "pr provider is required") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidate_PRProviderUnknown(t *testing.T) {
	lf := validLoomFile()
	lf.Spec.Operations = []Operation{
		{Name: "pr", PR: &PR{Provider: "bitbucket", Title: "title"}},
	}

	err := Validate(lf)
	if err == nil {
		t.Fatal("expected error for unknown pr provider")
	}
	if !strings.Contains(err.Error(), `unknown pr provider "bitbucket"`) {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidate_PRProviderTemplatedSkipped(t *testing.T) {
	lf := validLoomFile()
	lf.Spec.Operations = []Operation{
		{Name: "pr", PR: &PR{Provider: "{{ .provider }}", Title: "title"}},
	}

	if err := Validate(lf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidate_PRTitleRequired(t *testing.T) {
	lf := validLoomFile()
	lf.Spec.Operations = []Operation{
		{Name: "pr", PR: &PR{Provider: "github"}},
	}

	err := Validate(lf)
	if err == nil {
		t.Fatal("expected error for missing pr title")
	}
	if !strings.Contains(err.Error(), "pr title is required") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidate_PRValid(t *testing.T) {
	lf := validLoomFile()
	lf.Spec.Operations = []Operation{
		{Name: "pr", PR: &PR{Provider: "gitlab", Title: "Add config"}},
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
