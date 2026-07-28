package config

import (
	"os"
	"path/filepath"
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

func TestValidate_PatchPreserveCommentsInvalid(t *testing.T) {
	lf := validLoomFile()
	lf.Spec.Operations = []Operation{
		{Name: "patch-op", Patch: &Patch{Path: "p.yaml", Target: "t.yaml", PreserveComments: "yes"}},
	}

	err := Validate(lf)
	if err == nil {
		t.Fatal("expected error for invalid preserveComments")
	}
	if !strings.Contains(err.Error(), `invalid patch preserveComments "yes"`) {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidate_PatchPreserveCommentsValid(t *testing.T) {
	for _, v := range []string{"", "true", "false"} {
		lf := validLoomFile()
		lf.Spec.Operations = []Operation{
			{Name: "patch-op", Patch: &Patch{Path: "p.yaml", Target: "t.yaml", PreserveComments: v}},
		}

		if err := Validate(lf); err != nil {
			t.Fatalf("preserveComments=%q: unexpected error: %v", v, err)
		}
	}
}

func TestValidate_PatchPreserveCommentsTemplatedSkipped(t *testing.T) {
	lf := validLoomFile()
	lf.Spec.Params = []ParamDef{{Name: "keep"}}
	lf.Spec.Operations = []Operation{
		{Name: "patch-op", Patch: &Patch{Path: "p.yaml", Target: "t.yaml", PreserveComments: "{{ .keep }}"}},
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
	lf.Spec.Params = []ParamDef{{Name: "svc"}}
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
	lf.Spec.Params = []ParamDef{{Name: "svc"}}
	lf.Spec.Modules = []ModuleRef{
		{Name: "onboard-{{ .svc }}", Source: "../child", Params: map[string]string{"svc": "{{ .svc }}"}},
	}

	if err := Validate(lf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// --- if condition ---

func TestValidate_IfConditionValid(t *testing.T) {
	lf := validLoomFile()
	lf.Spec.Params = []ParamDef{{Name: "env"}}
	lf.Spec.Modules = []ModuleRef{
		{Name: "child", Source: "../child", If: `[ {{ .env }} = prod ]`},
	}
	lf.Spec.Operations = []Operation{
		{Name: "sh", If: `test -f {{ .env }}.yaml`, Shell: &Shell{Command: "echo hi"}},
	}

	if err := Validate(lf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidate_OperationIfUndeclaredParam(t *testing.T) {
	lf := validLoomFile()
	lf.Spec.Params = []ParamDef{{Name: "svc"}}
	lf.Spec.Operations = []Operation{
		{Name: "sh", If: "test {{ .missing }}", Shell: &Shell{Command: "echo hi"}},
	}

	err := Validate(lf)
	if err == nil {
		t.Fatal("expected error for undeclared param in operation if")
	}
	if !strings.Contains(err.Error(), `operation "sh" if: references undeclared param "missing"`) {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidate_ModuleIfUndeclaredParam(t *testing.T) {
	lf := validLoomFile()
	lf.Spec.Params = []ParamDef{{Name: "svc"}}
	lf.Spec.Modules = []ModuleRef{
		{Name: "child", Source: "../child", If: "test {{ .missing }}"},
	}

	err := Validate(lf)
	if err == nil {
		t.Fatal("expected error for undeclared param in module if")
	}
	if !strings.Contains(err.Error(), `module "child" if: references undeclared param "missing"`) {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidate_OperationIfTemplateSyntaxError(t *testing.T) {
	lf := validLoomFile()
	lf.Spec.Operations = []Operation{
		{Name: "sh", If: "test {{ .x", Shell: &Shell{Command: "echo hi"}},
	}

	err := Validate(lf)
	if err == nil {
		t.Fatal("expected error for malformed if template")
	}
	if !strings.Contains(err.Error(), `operation "sh" if: invalid template`) {
		t.Errorf("unexpected error: %v", err)
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
	lf.Spec.Params = []ParamDef{{Name: "env"}}
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

// --- undeclared param references ---

func TestValidate_UndeclaredParamRef(t *testing.T) {
	lf := validLoomFile()
	lf.Spec.Params = []ParamDef{{Name: "svc"}}
	lf.Spec.Operations = []Operation{
		{Name: "sh", Shell: &Shell{Command: "echo {{ .svc }} {{ .env }}"}},
	}

	err := Validate(lf)
	if err == nil {
		t.Fatal("expected error for undeclared param reference")
	}
	if !strings.Contains(err.Error(), `operation "sh" shell.command: references undeclared param "env"`) {
		t.Errorf("unexpected error: %v", err)
	}
	if strings.Contains(err.Error(), `"svc"`) {
		t.Errorf("declared param should not be flagged: %v", err)
	}
}

func TestValidate_UndeclaredParamRefNestedField(t *testing.T) {
	lf := validLoomFile()
	lf.Spec.Operations = []Operation{
		{Name: "sh", Shell: &Shell{Command: "echo {{ .cfg.key }}"}},
	}

	err := Validate(lf)
	if err == nil {
		t.Fatal("expected error for undeclared param reference")
	}
	if !strings.Contains(err.Error(), `references undeclared param "cfg"`) {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidate_UndeclaredParamRefReportedOnce(t *testing.T) {
	lf := validLoomFile()
	lf.Spec.Operations = []Operation{
		{Name: "sh", Shell: &Shell{Command: "echo {{ .env }} {{ .env }}"}},
	}

	err := Validate(lf)
	if err == nil {
		t.Fatal("expected error for undeclared param reference")
	}
	if strings.Count(err.Error(), `undeclared param "env"`) != 1 {
		t.Errorf("expected exactly one report per field, got: %v", err)
	}
}

func TestValidate_DynamicParamCanReferenceEarlierParams(t *testing.T) {
	lf := validLoomFile()
	lf.Spec.Params = []ParamDef{{Name: "branch"}}
	lf.Spec.DynamicParams = []DynamicParamDef{
		{Name: "sha", Command: "git rev-parse {{ .branch }}"},
		{Name: "short", Command: "echo {{ .sha }} | cut -c1-7"},
	}

	if err := Validate(lf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidate_DynamicParamCannotReferenceLaterParam(t *testing.T) {
	lf := validLoomFile()
	lf.Spec.DynamicParams = []DynamicParamDef{
		{Name: "short", Command: "echo {{ .sha }} | cut -c1-7"},
		{Name: "sha", Command: "git rev-parse HEAD"},
	}

	err := Validate(lf)
	if err == nil {
		t.Fatal("expected error for forward reference")
	}
	if !strings.Contains(err.Error(), `dynamicParam "short" command: references undeclared param "sha"`) {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidate_ParamRefCheckSkippedForRange(t *testing.T) {
	lf := validLoomFile()
	lf.Spec.Operations = []Operation{
		{Name: "sh", Shell: &Shell{Command: "echo {{ range . }}{{ .x }}{{ end }}"}},
	}

	if err := Validate(lf); err != nil {
		t.Fatalf("expected dot-rebinding template to be skipped: %v", err)
	}
}

// --- filesystem checks (ValidateInDir) ---

func fsLoomFile(op Operation) *LoomFile {
	lf := validLoomFile()
	lf.Spec.Params = []ParamDef{{Name: "svc"}}
	lf.Spec.Operations = []Operation{op}
	return lf
}

func TestValidateInDir_NewFilesSourceMissing(t *testing.T) {
	dir := t.TempDir()
	lf := fsLoomFile(Operation{Name: "nf", NewFiles: &NewFiles{Source: "templates"}})

	err := ValidateInDir(lf, dir)
	if err == nil {
		t.Fatal("expected error for missing newFiles source directory")
	}
	if !strings.Contains(err.Error(), `newFiles source "templates" not found`) {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidateInDir_NewFilesSourceNotADir(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "templates"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	lf := fsLoomFile(Operation{Name: "nf", NewFiles: &NewFiles{Source: "templates"}})

	err := ValidateInDir(lf, dir)
	if err == nil {
		t.Fatal("expected error for file newFiles source")
	}
	if !strings.Contains(err.Error(), `newFiles source "templates" is not a directory`) {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidateInDir_NewFilesSourceExists(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "templates"), 0o755); err != nil {
		t.Fatal(err)
	}
	lf := fsLoomFile(Operation{Name: "nf", NewFiles: &NewFiles{Source: "templates", Dest: "{{ .svc }}"}})

	if err := ValidateInDir(lf, dir); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateInDir_NewFilesSourceTemplatedSkipped(t *testing.T) {
	dir := t.TempDir()
	lf := fsLoomFile(Operation{Name: "nf", NewFiles: &NewFiles{Source: "templates-{{ .svc }}"}})

	if err := ValidateInDir(lf, dir); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateInDir_PatchFileMissing(t *testing.T) {
	dir := t.TempDir()
	lf := fsLoomFile(Operation{Name: "p", Patch: &Patch{Path: "patch.yaml", Target: "t.yaml"}})

	err := ValidateInDir(lf, dir)
	if err == nil {
		t.Fatal("expected error for missing patch file")
	}
	if !strings.Contains(err.Error(), `patch file "patch.yaml" not found`) {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidateInDir_PatchPathIsDir(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "patch.yaml"), 0o755); err != nil {
		t.Fatal(err)
	}
	lf := fsLoomFile(Operation{Name: "p", Patch: &Patch{Path: "patch.yaml", Target: "t.yaml"}})

	err := ValidateInDir(lf, dir)
	if err == nil {
		t.Fatal("expected error for directory patch path")
	}
	if !strings.Contains(err.Error(), `patch path "patch.yaml" is a directory`) {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidateInDir_PatchFileExists(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "patch.yaml"), []byte("a: {{ .svc }}"), 0o644); err != nil {
		t.Fatal(err)
	}
	lf := fsLoomFile(Operation{Name: "p", Patch: &Patch{Path: "patch.yaml", Target: "t.yaml"}})

	if err := ValidateInDir(lf, dir); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidate_SkipsFilesystemChecks(t *testing.T) {
	lf := fsLoomFile(Operation{Name: "p", Patch: &Patch{Path: "nonexistent.yaml", Target: "t.yaml"}})

	if err := Validate(lf); err != nil {
		t.Fatalf("Validate without a dir must not stat paths: %v", err)
	}
}

// --- templated enum values skipped ---

func TestValidate_PatchEngineTemplatedSkipped(t *testing.T) {
	lf := validLoomFile()
	lf.Spec.Params = []ParamDef{{Name: "engine"}}
	lf.Spec.Operations = []Operation{
		{Name: "patch-op", Patch: &Patch{Path: "p.yaml", Target: "t.yaml", Engine: "{{ .engine }}"}},
	}

	if err := Validate(lf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidate_LLMRetryDelayTemplatedSkipped(t *testing.T) {
	lf := validLoomFile()
	lf.Spec.Params = []ParamDef{{Name: "delay"}}
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
	lf.Spec.Params = []ParamDef{{Name: "mode"}}
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
	lf.Spec.Params = []ParamDef{{Name: "dur"}}
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
	lf.Spec.Params = []ParamDef{{Name: "provider"}}
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

// --- exclude/include glob patterns ---

func TestValidate_ExcludeMalformedGlob(t *testing.T) {
	lf := validLoomFile()
	lf.Spec.Excludes = []string{"__functions["}

	err := Validate(lf)
	if err == nil {
		t.Fatal("expected error for malformed exclude glob")
	}
	if !strings.Contains(err.Error(), `spec.excludes[0]: invalid glob pattern "__functions["`) {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidate_IncludeMalformedGlob(t *testing.T) {
	lf := validLoomFile()
	lf.Spec.Includes = []string{"[a-"}

	err := Validate(lf)
	if err == nil {
		t.Fatal("expected error for malformed include glob")
	}
	if !strings.Contains(err.Error(), `spec.includes[0]: invalid glob pattern "[a-"`) {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidate_ExcludeWithPathSeparator(t *testing.T) {
	lf := validLoomFile()
	lf.Spec.Excludes = []string{"__functions/patches/*.yaml"}

	err := Validate(lf)
	if err == nil {
		t.Fatal("expected error for exclude pattern with a path separator")
	}
	if !strings.Contains(err.Error(), "contains a path separator") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidate_ExcludeEmptyPattern(t *testing.T) {
	lf := validLoomFile()
	lf.Spec.Excludes = []string{""}

	err := Validate(lf)
	if err == nil {
		t.Fatal("expected error for empty exclude pattern")
	}
	if !strings.Contains(err.Error(), "spec.excludes[0]: pattern cannot be empty") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidate_ExcludeValidGlobs(t *testing.T) {
	lf := validLoomFile()
	lf.Spec.Excludes = []string{"__functions", "*.md", "[abc]*.yaml"}
	lf.Spec.Includes = []string{"README.md"}

	if err := Validate(lf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidate_ExcludeTemplatedGlobSkipped(t *testing.T) {
	lf := validLoomFile()
	lf.Spec.Params = []ParamDef{{Name: "svc"}}
	// Would look like a separator violation once rendered — resolved at run time.
	lf.Spec.Excludes = []string{"{{ .svc }}"}

	if err := Validate(lf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// --- destination paths escaping the target dir ---

func TestValidate_PatchTargetEscapesTargetDir(t *testing.T) {
	lf := validLoomFile()
	lf.Spec.Operations = []Operation{
		{Name: "p", Patch: &Patch{Path: "patch.yaml", Target: "../../etc/passwd"}},
	}

	err := Validate(lf)
	if err == nil {
		t.Fatal("expected error for patch target escaping the target dir")
	}
	if !strings.Contains(err.Error(), `patch target "../../etc/passwd" escapes the target directory`) {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidate_NewFilesDestEscapesTargetDir(t *testing.T) {
	lf := validLoomFile()
	lf.Spec.Operations = []Operation{
		{Name: "nf", NewFiles: &NewFiles{Source: "templates", Dest: "sub/../../out"}},
	}

	err := Validate(lf)
	if err == nil {
		t.Fatal("expected error for newFiles dest escaping the target dir")
	}
	if !strings.Contains(err.Error(), `newFiles dest "sub/../../out" escapes the target directory`) {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidate_LLMTargetEscapesTargetDir(t *testing.T) {
	lf := validLoomFile()
	lf.Spec.Operations = []Operation{
		{Name: "gen", LLM: &LLM{
			Provider: "openai",
			Model:    "gpt-4o",
			Prompt:   "hi",
			Target:   "../out.yaml",
		}},
	}

	err := Validate(lf)
	if err == nil {
		t.Fatal("expected error for llm target escaping the target dir")
	}
	if !strings.Contains(err.Error(), `llm target "../out.yaml" escapes the target directory`) {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidate_TargetPathsInsideTargetDirOK(t *testing.T) {
	lf := validLoomFile()
	lf.Spec.Params = []ParamDef{{Name: "svc"}}
	lf.Spec.Operations = []Operation{
		{Name: "p", Patch: &Patch{Path: "patch.yaml", Target: "app/../k8s/deploy.yaml"}},
		{Name: "nf", NewFiles: &NewFiles{Source: "templates", Dest: "charts"}},
		// Templated: resolved at run time, so not checked here.
		{Name: "nf2", NewFiles: &NewFiles{Source: "templates", Dest: "{{ .svc }}/../out"}},
	}

	if err := Validate(lf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// --- llm maxTokens ---

func TestValidate_LLMNegativeMaxTokens(t *testing.T) {
	lf := validLoomFile()
	lf.Spec.Operations = []Operation{
		{Name: "gen", LLM: &LLM{
			Provider:  "openai",
			Model:     "gpt-4o",
			Prompt:    "hi",
			Target:    "out.yaml",
			MaxTokens: -1,
		}},
	}

	err := Validate(lf)
	if err == nil {
		t.Fatal("expected error for negative llm maxTokens")
	}
	if !strings.Contains(err.Error(), "llm maxTokens must be >= 0") {
		t.Errorf("unexpected error: %v", err)
	}
}

// --- patch file leaking into newFiles output (ValidateInDir) ---

// patchLeakModule builds a module dir with a newFiles source directory that
// also holds the patch file, plus the matching config.
func patchLeakModule(t *testing.T, excludes []string) (string, *LoomFile) {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "__functions", "patches"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "__functions", "patches", "deploy.yaml"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	lf := validLoomFile()
	lf.Spec.Excludes = excludes
	lf.Spec.Operations = []Operation{
		{Name: "nf", NewFiles: &NewFiles{Source: "."}},
		{Name: "p", Patch: &Patch{Path: "__functions/patches/deploy.yaml", Target: "deploy.yaml"}},
	}
	return dir, lf
}

func TestValidateInDir_PatchFileRenderedByNewFiles(t *testing.T) {
	dir, lf := patchLeakModule(t, nil)

	err := ValidateInDir(lf, dir)
	if err == nil {
		t.Fatal("expected error for patch file rendered into the target by newFiles")
	}
	if !strings.Contains(err.Error(), `patch file "__functions/patches/deploy.yaml" is also rendered into the target by newFiles operation "nf"`) {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidateInDir_PatchFileExcludedFromNewFiles(t *testing.T) {
	dir, lf := patchLeakModule(t, []string{"__functions"})

	if err := ValidateInDir(lf, dir); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateInDir_PatchLeakSkippedWhenFilterTemplated(t *testing.T) {
	dir, lf := patchLeakModule(t, []string{"{{ .svc }}"})
	lf.Spec.Params = []ParamDef{{Name: "svc"}}

	if err := ValidateInDir(lf, dir); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidate_PatchLeakNotCheckedWithoutModuleDir(t *testing.T) {
	_, lf := patchLeakModule(t, nil)

	if err := Validate(lf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// --- template file param references (ValidateInDir) ---

// tmplModule writes a module directory whose newFiles source "templates"
// contains the given files (relative path -> contents), and returns a config
// declaring params and a newFiles operation over that source.
func tmplModule(t *testing.T, files map[string]string, params ...string) (*LoomFile, string) {
	t.Helper()
	dir := t.TempDir()
	for rel, content := range files {
		path := filepath.Join(dir, "templates", rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(dir, "templates"), 0o755); err != nil {
		t.Fatal(err)
	}

	lf := validLoomFile()
	for _, p := range params {
		lf.Spec.Params = append(lf.Spec.Params, ParamDef{Name: p})
	}
	lf.Spec.Operations = []Operation{
		{Name: "nf", NewFiles: &NewFiles{Source: "templates"}},
	}
	return lf, dir
}

func TestValidateInDir_TemplateFileUndeclaredParam(t *testing.T) {
	lf, dir := tmplModule(t, map[string]string{"app.yaml": "name: {{ .svc }}\n"})

	err := ValidateInDir(lf, dir)
	if err == nil {
		t.Fatal("expected error for undeclared param in template file")
	}
	if !strings.Contains(err.Error(), `template file "app.yaml": references undeclared param "svc"`) {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidateInDir_TemplateFileDeclaredParamOK(t *testing.T) {
	lf, dir := tmplModule(t, map[string]string{"app.yaml": "name: {{ .svc }}\n"}, "svc")

	if err := ValidateInDir(lf, dir); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// A __param__ placeholder in a path is a template reference too.
func TestValidateInDir_T3_TemplateFilePathUndeclaredParam(t *testing.T) {
	lf, dir := tmplModule(t, map[string]string{"__svc__/app.yaml": "a: b\n"})

	err := ValidateInDir(lf, dir)
	if err == nil {
		t.Fatal("expected error for undeclared param in template file path")
	}
	if !strings.Contains(err.Error(), `references undeclared param "svc"`) {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidateInDir_TemplateFileInvalidTemplate(t *testing.T) {
	lf, dir := tmplModule(t, map[string]string{"app.yaml": "name: {{ .svc\n"})

	err := ValidateInDir(lf, dir)
	if err == nil {
		t.Fatal("expected error for unparseable template file")
	}
	if !strings.Contains(err.Error(), `template file "app.yaml": invalid template`) {
		t.Errorf("unexpected error: %v", err)
	}
}

// A file the run never renders is never checked.
func TestValidateInDir_ExcludedTemplateFileSkipped(t *testing.T) {
	lf, dir := tmplModule(t, map[string]string{"skip.yaml": "name: {{ .svc }}\n"})
	lf.Spec.Excludes = []string{"skip.yaml"}

	if err := ValidateInDir(lf, dir); err != nil {
		t.Fatalf("excluded file must not be checked: %v", err)
	}
}

// A templated filter makes the run-time file set unknown, so the walk is skipped.
func TestValidateInDir_TemplatedFilterSkipsTemplateFiles(t *testing.T) {
	lf, dir := tmplModule(t, map[string]string{"app.yaml": "name: {{ .svc }}\n"}, "keep")
	lf.Spec.Excludes = []string{"{{ .keep }}"}

	if err := ValidateInDir(lf, dir); err != nil {
		t.Fatalf("templated filter must skip template file checks: %v", err)
	}
}

func TestValidateInDir_PatchFileUndeclaredParam(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "patch.yaml"), []byte("a: {{ .svc }}"), 0o644); err != nil {
		t.Fatal(err)
	}
	lf := validLoomFile()
	lf.Spec.Operations = []Operation{
		{Name: "p", Patch: &Patch{Path: "patch.yaml", Target: "t.yaml"}},
	}

	err := ValidateInDir(lf, dir)
	if err == nil {
		t.Fatal("expected error for undeclared param in patch file")
	}
	if !strings.Contains(err.Error(), `patch file "patch.yaml": references undeclared param "svc"`) {
		t.Errorf("unexpected error: %v", err)
	}
}

// --- declared but never referenced ---

// warnOnly asserts the config is valid and returns the warnings it produced.
func warnOnly(t *testing.T, lf *LoomFile, dir string) []string {
	t.Helper()
	warnings, err := ValidateInDirWithWarnings(lf, dir)
	if err != nil {
		t.Fatalf("unused params must not make a config invalid: %v", err)
	}
	return warnings
}

func TestValidateInDir_UnusedParamWarns(t *testing.T) {
	lf, dir := tmplModule(t, map[string]string{"app.yaml": "a: b\n"}, "svc")

	warnings := warnOnly(t, lf, dir)
	if len(warnings) != 1 || !strings.Contains(warnings[0], `param "svc" is declared but never referenced by any template`) {
		t.Errorf("unexpected warnings: %v", warnings)
	}
}

func TestValidateInDir_UnusedDynamicParamWarns(t *testing.T) {
	lf, dir := tmplModule(t, map[string]string{"app.yaml": "a: b\n"})
	lf.Spec.DynamicParams = []DynamicParamDef{{Name: "hash", Command: "git rev-parse HEAD"}}

	warnings := warnOnly(t, lf, dir)
	if len(warnings) != 1 || !strings.Contains(warnings[0], `dynamicParam "hash" is declared but never referenced by any template`) {
		t.Errorf("unexpected warnings: %v", warnings)
	}
}

// An undeclared reference stays a violation — that one the run gets wrong.
func TestValidateInDir_UndeclaredRefIsStillAnError(t *testing.T) {
	lf, dir := tmplModule(t, map[string]string{"app.yaml": "name: {{ .svc }}\n"})

	if _, err := ValidateInDirWithWarnings(lf, dir); err == nil {
		t.Fatal("expected undeclared param reference to remain a violation")
	}
}

// A param used only by a rendered file counts as used — the cross-check that
// makes the unused rule safe to enforce.
func TestValidateInDir_ParamUsedOnlyInTemplateFile(t *testing.T) {
	lf, dir := tmplModule(t, map[string]string{"app.yaml": "name: {{ .svc }}\n"}, "svc")

	if w := warnOnly(t, lf, dir); len(w) != 0 {
		t.Errorf("param used in a template file must count as used, got %v", w)
	}
}

func TestValidateInDir_T3_ParamUsedOnlyInTemplateFilePath(t *testing.T) {
	lf, dir := tmplModule(t, map[string]string{"__svc__/app.yaml": "a: b\n"}, "svc")

	if w := warnOnly(t, lf, dir); len(w) != 0 {
		t.Errorf("param used in a template file path must count as used, got %v", w)
	}
}

// A param handed to a submodule is used: children inherit nothing implicitly,
// so spec.modules[].params is the only way a value reaches them.
func TestValidateInDir_ParamUsedOnlyByChildModule(t *testing.T) {
	dir := t.TempDir()
	lf := validLoomFile()
	lf.Spec.Params = []ParamDef{{Name: "svc"}}
	lf.Spec.Modules = []ModuleRef{
		{Name: "child", Source: "./child", Params: map[string]string{"name": "{{ .svc }}"}},
	}

	if w := warnOnly(t, lf, dir); len(w) != 0 {
		t.Errorf("param passed to a child module must count as used, got %v", w)
	}
}

// --- unused check bails when references cannot be seen ---

// A source resolved at run time no longer abandons the whole module: the fixed
// part of the path stands in for it, so a param referenced under there is still
// seen as used.
func TestValidateInDir_TemplatedSourceStillCountsUsage(t *testing.T) {
	lf, dir := tmplModule(t, map[string]string{"app.yaml": "name: {{ .svc }}\n"}, "svc", "which")
	lf.Spec.Operations = []Operation{
		{Name: "nf", NewFiles: &NewFiles{Source: "{{ .which }}"}},
	}

	if w := warnOnly(t, lf, dir); len(w) != 0 {
		t.Errorf("stand-in scan should have found svc, got %v", w)
	}
}

func TestValidateInDir_TemplatedPatchPathStillCountsUsage(t *testing.T) {
	dir := t.TempDir()
	patches := filepath.Join(dir, "__functions", "patches")
	if err := os.MkdirAll(patches, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(patches, "deploy.yaml"), []byte("a: {{ .svc }}"), 0o644); err != nil {
		t.Fatal(err)
	}
	lf := validLoomFile()
	lf.Spec.Params = []ParamDef{{Name: "svc"}, {Name: "which"}}
	lf.Spec.Operations = []Operation{
		{Name: "p", Patch: &Patch{Path: "__functions/patches/{{ .which }}.yaml", Target: "t.yaml"}},
	}

	if w := warnOnly(t, lf, dir); len(w) != 0 {
		t.Errorf("stand-in scan should have found svc, got %v", w)
	}
}

// The stand-in scan only gathers references. Its files may never be rendered,
// so a bad reference in one cannot be blamed on the config.
func TestValidateInDir_StandInScanRaisesNoViolations(t *testing.T) {
	dir := t.TempDir()
	patches := filepath.Join(dir, "__functions", "patches")
	if err := os.MkdirAll(patches, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(patches, "deploy.yaml"), []byte("a: {{ .neverDeclared }}"), 0o644); err != nil {
		t.Fatal(err)
	}
	lf := validLoomFile()
	lf.Spec.Params = []ParamDef{{Name: "which"}}
	lf.Spec.Operations = []Operation{
		{Name: "p", Patch: &Patch{Path: "__functions/patches/{{ .which }}.yaml", Target: "t.yaml"}},
	}

	if _, err := ValidateInDirWithWarnings(lf, dir); err != nil {
		t.Fatalf("stand-in files must not raise violations: %v", err)
	}
}

func TestStaticPrefixDir(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"__functions/patches/{{ .kind }}.yaml", "__functions/patches"},
		{"__functions/{{ .kind }}/p.yaml", "__functions"},
		{"{{ .dir }}/p.yaml", "."},
		{"{{ .whole }}", "."},
		{"patches/p.yaml", "patches"},
	} {
		if got := staticPrefixDir(tc.in); got != tc.want {
			t.Errorf("staticPrefixDir(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// A range body rebinds dot to a string element of a flat map, so nothing in it
// can name a param — only the ranged-over pipeline can. The analysis therefore
// stays on, and still reports the params the template really does not touch.
func TestValidateInDir_RangeDoesNotDisableUnusedCheck(t *testing.T) {
	lf, dir := tmplModule(t, map[string]string{"app.yaml": "{{ range .list }}x{{ end }}\n"}, "list", "svc")

	warnings := warnOnly(t, lf, dir)
	if len(warnings) != 1 || !strings.Contains(warnings[0], `"svc"`) {
		t.Errorf("expected only svc to be unused, got %v", warnings)
	}
}

func TestValidateInDir_WithDoesNotDisableUnusedCheck(t *testing.T) {
	lf, dir := tmplModule(t, map[string]string{"app.yaml": "{{ with .maybe }}{{ . }}{{ end }}\n"}, "maybe", "svc")

	warnings := warnOnly(t, lf, dir)
	if len(warnings) != 1 || !strings.Contains(warnings[0], `"svc"`) {
		t.Errorf("expected only svc to be unused, got %v", warnings)
	}
}

// {{ index . "x" }} is the only way to reach a param whose name is not a valid
// template identifier. Its key is a literal, so the reference is exact.
func TestValidateInDir_IndexOnDotCountsAsUse(t *testing.T) {
	lf, dir := tmplModule(t, map[string]string{"app.yaml": `{{ index . "my-svc" }}`}, "my-svc", "svc")

	warnings := warnOnly(t, lf, dir)
	if len(warnings) != 1 || !strings.Contains(warnings[0], `"svc"`) {
		t.Errorf("expected only svc to be unused, got %v", warnings)
	}
}

// A computed key really is unknowable, so that one still stands down.
func TestValidateInDir_UnusedSkippedForComputedIndexKey(t *testing.T) {
	lf, dir := tmplModule(t, map[string]string{"app.yaml": `{{ index . .which }}`}, "which", "svc")

	if w := warnOnly(t, lf, dir); len(w) != 0 {
		t.Errorf("computed index key must skip the unused check, got %v", w)
	}
}

// Without a module directory the rendered files cannot be read at all.
func TestValidate_UnusedSkippedWithoutModuleDir(t *testing.T) {
	lf := validLoomFile()
	lf.Spec.Params = []ParamDef{{Name: "svc"}}

	if w := warnOnly(t, lf, ""); len(w) != 0 {
		t.Errorf("in-memory validation must not report unused params, got %v", w)
	}
}
