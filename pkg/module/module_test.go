package module

import (
	"bytes"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rickliujh/loom/pkg/config"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
}

// --- resolveParams tests ---

// P1: CLI provided value overrides default.
func TestResolveParams_ProvidedOverridesDefault(t *testing.T) {
	declared := []config.ParamDef{
		{Name: "foo", Default: "default-value"},
	}
	provided := map[string]string{"foo": "provided-value"}

	result, err := resolveParams(declared, nil, provided, testLogger())
	if err != nil {
		t.Fatal(err)
	}
	if result["foo"] != "provided-value" {
		t.Errorf("expected provided-value, got %q", result["foo"])
	}
}

// P1: Default used when no CLI value provided.
func TestResolveParams_DefaultUsedWhenNotProvided(t *testing.T) {
	declared := []config.ParamDef{
		{Name: "foo", Default: "fallback"},
	}

	result, err := resolveParams(declared, nil, nil, testLogger())
	if err != nil {
		t.Fatal(err)
	}
	if result["foo"] != "fallback" {
		t.Errorf("expected fallback, got %q", result["foo"])
	}
}

// P1: Required param with no value and no default errors.
func TestResolveParams_RequiredParamErrors(t *testing.T) {
	declared := []config.ParamDef{
		{Name: "foo", Required: true},
	}

	_, err := resolveParams(declared, nil, nil, testLogger())
	if err == nil {
		t.Fatal("expected error for missing required param")
	}
	if !strings.Contains(err.Error(), `required parameter "foo"`) {
		t.Errorf("unexpected error: %v", err)
	}
}

// P7: Missing required param error includes the declared description.
func TestResolveParams_RequiredParamErrorIncludesDescription(t *testing.T) {
	declared := []config.ParamDef{
		{Name: "env", Required: true, Description: "Target environment (staging|production)"},
	}

	_, err := resolveParams(declared, nil, nil, testLogger())
	if err == nil {
		t.Fatal("expected error for missing required param")
	}
	want := `required parameter "env" not provided: Target environment (staging|production)`
	if err.Error() != want {
		t.Errorf("expected %q, got %q", want, err.Error())
	}
}

// fakePrompter records which params it was asked for and returns canned answers.
type fakePrompter struct {
	answers map[string]string
	err     error
	asked   []string
}

func (f *fakePrompter) Prompt(p config.ParamDef) (string, error) {
	f.asked = append(f.asked, p.Name)
	if f.err != nil {
		return "", f.err
	}
	return f.answers[p.Name], nil
}

// P8: only required params that are missing and have no default are prompted;
// provided/default/optional params are left alone and the caller's map is not
// mutated.
func TestPromptMissingRequired_FillsOnlyMissing(t *testing.T) {
	declared := []config.ParamDef{
		{Name: "provided", Required: true},
		{Name: "withDefault", Required: true, Default: "d"},
		{Name: "optional"},
		{Name: "missing", Required: true, Description: "needs a value"},
	}
	provided := map[string]string{"provided": "x"}
	fp := &fakePrompter{answers: map[string]string{"missing": "answered"}}

	out, err := promptMissingRequired(declared, provided, fp)
	if err != nil {
		t.Fatal(err)
	}
	if out["missing"] != "answered" {
		t.Errorf("expected prompted value, got %q", out["missing"])
	}
	if len(fp.asked) != 1 || fp.asked[0] != "missing" {
		t.Errorf("expected only \"missing\" to be prompted, got %v", fp.asked)
	}
	if _, ok := provided["missing"]; ok {
		t.Error("caller's provided map must not be mutated")
	}
}

// P8: a prompter error aborts the load.
func TestPromptMissingRequired_ErrorPropagates(t *testing.T) {
	declared := []config.ParamDef{{Name: "x", Required: true}}
	fp := &fakePrompter{err: fmt.Errorf("no input")}

	_, err := promptMissingRequired(declared, nil, fp)
	if err == nil {
		t.Fatal("expected error from prompter to propagate")
	}
}

// P3: Undeclared params provided via CLI are rejected.
func TestResolveParams_UndeclaredParamRejected(t *testing.T) {
	declared := []config.ParamDef{
		{Name: "foo", Default: "x"},
	}
	provided := map[string]string{"foo": "x", "bar": "y"}

	_, err := resolveParams(declared, nil, provided, testLogger())
	if err == nil {
		t.Fatal("expected error for undeclared param")
	}
	if !strings.Contains(err.Error(), `undeclared parameter "bar"`) {
		t.Errorf("unexpected error: %v", err)
	}
}

// P3: Params declared in dynamicParams are not rejected.
func TestResolveParams_DynamicParamNotRejected(t *testing.T) {
	declared := []config.ParamDef{
		{Name: "foo", Default: "x"},
	}
	dynamic := []config.DynamicParamDef{
		{Name: "bar", Command: "echo y"},
	}
	provided := map[string]string{"foo": "x", "bar": "override"}

	_, err := resolveParams(declared, dynamic, provided, testLogger())
	if err != nil {
		t.Fatalf("dynamic param should not be rejected: %v", err)
	}
}

// --- resolveDynamicParams tests ---

// P4: Dynamic param command is evaluated.
func TestResolveDynamicParams_CommandEvaluated(t *testing.T) {
	declared := []config.DynamicParamDef{
		{Name: "foo", Command: "echo hello-dynamic"},
	}
	resolved := make(map[string]string)

	err := resolveDynamicParams(declared, resolved, nil, ".", testLogger())
	if err != nil {
		t.Fatal(err)
	}
	if resolved["foo"] != "hello-dynamic" {
		t.Errorf("expected hello-dynamic, got %q", resolved["foo"])
	}
}

// P4: Trailing newlines are trimmed from command output.
func TestResolveDynamicParams_CommandTrimsTrailingNewlines(t *testing.T) {
	declared := []config.DynamicParamDef{
		{Name: "foo", Command: "printf 'value\\n\\n'"},
	}
	resolved := make(map[string]string)

	err := resolveDynamicParams(declared, resolved, nil, ".", testLogger())
	if err != nil {
		t.Fatal(err)
	}
	if resolved["foo"] != "value" {
		t.Errorf("expected %q, got %q", "value", resolved["foo"])
	}
}

// P6: CLI override skips dynamic command and logs warning.
func TestResolveDynamicParams_CLIOverrideSkipsCommandWithWarning(t *testing.T) {
	declared := []config.DynamicParamDef{
		{Name: "foo", Command: "echo should-not-run"},
	}
	resolved := make(map[string]string)
	provided := map[string]string{"foo": "cli-value"}

	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn}))

	err := resolveDynamicParams(declared, resolved, provided, ".", logger)
	if err != nil {
		t.Fatal(err)
	}
	if resolved["foo"] != "cli-value" {
		t.Errorf("expected cli-value, got %q", resolved["foo"])
	}

	logOutput := buf.String()
	if !strings.Contains(logOutput, "CLI override skipping dynamic param") {
		t.Errorf("expected warning about CLI override, got:\n%s", logOutput)
	}
}

// P4: Dynamic param command failure without default returns error.
func TestResolveDynamicParams_CommandFailsNoDefault(t *testing.T) {
	declared := []config.DynamicParamDef{
		{Name: "foo", Command: "exit 1"},
	}
	resolved := make(map[string]string)

	err := resolveDynamicParams(declared, resolved, nil, ".", testLogger())
	if err == nil {
		t.Fatal("expected error for failed command")
	}
}

// P4: Dynamic param command failure with default uses fallback.
func TestResolveDynamicParams_CommandFailsFallsBackToDefault(t *testing.T) {
	declared := []config.DynamicParamDef{
		{Name: "foo", Command: "exit 1", Default: "fallback"},
	}
	resolved := make(map[string]string)

	err := resolveDynamicParams(declared, resolved, nil, ".", testLogger())
	if err != nil {
		t.Fatal(err)
	}
	if resolved["foo"] != "fallback" {
		t.Errorf("expected fallback, got %q", resolved["foo"])
	}
}

// T4: Dynamic param default is templated with already-resolved params.
func TestResolveDynamicParams_DefaultTemplatedWithParams(t *testing.T) {
	declared := []config.DynamicParamDef{
		{Name: "foo", Command: "exit 1", Default: "{{ .env }}-unknown"},
	}
	resolved := map[string]string{"env": "prod"}

	err := resolveDynamicParams(declared, resolved, nil, ".", testLogger())
	if err != nil {
		t.Fatal(err)
	}
	if resolved["foo"] != "prod-unknown" {
		t.Errorf("expected prod-unknown, got %q", resolved["foo"])
	}
}

// T4: Dynamic param default that fails to render returns an error.
func TestResolveDynamicParams_DefaultTemplateError(t *testing.T) {
	declared := []config.DynamicParamDef{
		{Name: "foo", Command: "exit 1", Default: "{{ .env"},
	}
	resolved := make(map[string]string)

	err := resolveDynamicParams(declared, resolved, nil, ".", testLogger())
	if err == nil {
		t.Fatal("expected error for unrenderable default")
	}
	if !strings.Contains(err.Error(), "templating default for dynamic param") {
		t.Errorf("unexpected error: %v", err)
	}
}

// P4: Dynamic param command is templated with already-resolved params.
func TestResolveDynamicParams_CommandTemplatedWithParams(t *testing.T) {
	declared := []config.DynamicParamDef{
		{Name: "greeting", Command: "echo hello-{{ .name }}"},
	}
	resolved := map[string]string{"name": "world"}

	err := resolveDynamicParams(declared, resolved, nil, ".", testLogger())
	if err != nil {
		t.Fatal(err)
	}
	if resolved["greeting"] != "hello-world" {
		t.Errorf("expected hello-world, got %q", resolved["greeting"])
	}
}

// P4: Dynamic params are evaluated after static params are resolved.
func TestResolveDynamicParams_EvaluatedAfterStaticParams(t *testing.T) {
	params := []config.ParamDef{
		{Name: "env", Default: "staging"},
	}
	dynamicParams := []config.DynamicParamDef{
		{Name: "configPath", Command: "echo /configs/{{ .env }}/app.yaml"},
	}

	resolved, err := resolveParams(params, dynamicParams, nil, testLogger())
	if err != nil {
		t.Fatal(err)
	}

	err = resolveDynamicParams(dynamicParams, resolved, nil, ".", testLogger())
	if err != nil {
		t.Fatal(err)
	}

	if resolved["configPath"] != "/configs/staging/app.yaml" {
		t.Errorf("expected /configs/staging/app.yaml, got %q", resolved["configPath"])
	}
}

// P5: Later dynamic params can reference earlier ones.
func TestResolveDynamicParams_ChainedDynamic(t *testing.T) {
	declared := []config.DynamicParamDef{
		{Name: "first", Command: "echo alpha"},
		{Name: "second", Command: "echo {{ .first }}-beta"},
	}
	resolved := make(map[string]string)

	err := resolveDynamicParams(declared, resolved, nil, ".", testLogger())
	if err != nil {
		t.Fatal(err)
	}

	if resolved["first"] != "alpha" {
		t.Errorf("expected alpha, got %q", resolved["first"])
	}
	if resolved["second"] != "alpha-beta" {
		t.Errorf("expected alpha-beta, got %q", resolved["second"])
	}
}

// Dynamic param command runs in module directory, not process cwd.
func TestResolveDynamicParams_CommandRunsInModuleDir(t *testing.T) {
	dir := t.TempDir()
	declared := []config.DynamicParamDef{
		{Name: "cwd", Command: "pwd"},
	}
	resolved := make(map[string]string)

	err := resolveDynamicParams(declared, resolved, nil, dir, testLogger())
	if err != nil {
		t.Fatal(err)
	}
	if resolved["cwd"] != dir {
		t.Errorf("expected command to run in %q, got %q", dir, resolved["cwd"])
	}
}

// Load produces Params containing both static and dynamic resolved values.
func TestLoad_ParamsContainStaticAndDynamic(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "loom.yaml"), []byte(`
apiVersion: loom.rickliujh.github.io/v1beta1
kind: Loom
metadata:
  name: test-both
spec:
  params:
    - name: env
      default: "staging"
  dynamicParams:
    - name: hash
      command: "echo abc123"
`), 0o644); err != nil {
		t.Fatal(err)
	}

	mod, err := Load(dir, nil, testLogger())
	if err != nil {
		t.Fatal(err)
	}

	if mod.Params["env"] != "staging" {
		t.Errorf("expected static param env=staging, got %q", mod.Params["env"])
	}
	if mod.Params["hash"] != "abc123" {
		t.Errorf("expected dynamic param hash=abc123, got %q", mod.Params["hash"])
	}
}
