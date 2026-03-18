package module

import (
	"log/slog"
	"os"
	"testing"

	"github.com/rickliujh/loom/pkg/config"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
}

// --- resolveParams tests ---

func TestResolveParams_ProvidedOverridesDefault(t *testing.T) {
	declared := []config.ParamDef{
		{Name: "foo", Default: "default-value"},
	}
	provided := map[string]string{"foo": "provided-value"}

	result, err := resolveParams(declared, provided, testLogger())
	if err != nil {
		t.Fatal(err)
	}
	if result["foo"] != "provided-value" {
		t.Errorf("expected provided-value, got %q", result["foo"])
	}
}

func TestResolveParams_DefaultStillWorks(t *testing.T) {
	declared := []config.ParamDef{
		{Name: "foo", Default: "fallback"},
	}

	result, err := resolveParams(declared, nil, testLogger())
	if err != nil {
		t.Fatal(err)
	}
	if result["foo"] != "fallback" {
		t.Errorf("expected fallback, got %q", result["foo"])
	}
}

func TestResolveParams_RequiredStillWorks(t *testing.T) {
	declared := []config.ParamDef{
		{Name: "foo", Required: true},
	}

	_, err := resolveParams(declared, nil, testLogger())
	if err == nil {
		t.Fatal("expected error for missing required param")
	}
}

// --- resolveDynamicParams tests ---

func TestResolveDynamicParams_CommandEvaluated(t *testing.T) {
	declared := []config.DynamicParamDef{
		{Name: "foo", Command: "echo hello-dynamic"},
	}
	resolved := make(map[string]string)

	err := resolveDynamicParams(declared, resolved, nil, testLogger())
	if err != nil {
		t.Fatal(err)
	}
	if resolved["foo"] != "hello-dynamic" {
		t.Errorf("expected hello-dynamic, got %q", resolved["foo"])
	}
}

func TestResolveDynamicParams_CommandTrimsTrailingNewlines(t *testing.T) {
	declared := []config.DynamicParamDef{
		{Name: "foo", Command: "printf 'value\\n\\n'"},
	}
	resolved := make(map[string]string)

	err := resolveDynamicParams(declared, resolved, nil, testLogger())
	if err != nil {
		t.Fatal(err)
	}
	if resolved["foo"] != "value" {
		t.Errorf("expected %q, got %q", "value", resolved["foo"])
	}
}

func TestResolveDynamicParams_ProvidedOverridesCommand(t *testing.T) {
	declared := []config.DynamicParamDef{
		{Name: "foo", Command: "echo command-value"},
	}
	resolved := make(map[string]string)
	provided := map[string]string{"foo": "provided-value"}

	err := resolveDynamicParams(declared, resolved, provided, testLogger())
	if err != nil {
		t.Fatal(err)
	}
	if resolved["foo"] != "provided-value" {
		t.Errorf("expected provided-value, got %q", resolved["foo"])
	}
}

func TestResolveDynamicParams_CommandFails(t *testing.T) {
	declared := []config.DynamicParamDef{
		{Name: "foo", Command: "exit 1"},
	}
	resolved := make(map[string]string)

	err := resolveDynamicParams(declared, resolved, nil, testLogger())
	if err == nil {
		t.Fatal("expected error for failed command")
	}
}

func TestResolveDynamicParams_CommandFailsFallsBackToDefault(t *testing.T) {
	declared := []config.DynamicParamDef{
		{Name: "foo", Command: "exit 1", Default: "fallback"},
	}
	resolved := make(map[string]string)

	err := resolveDynamicParams(declared, resolved, nil, testLogger())
	if err != nil {
		t.Fatal(err)
	}
	if resolved["foo"] != "fallback" {
		t.Errorf("expected fallback, got %q", resolved["foo"])
	}
}

func TestResolveDynamicParams_CommandTemplatedWithParams(t *testing.T) {
	declared := []config.DynamicParamDef{
		{Name: "greeting", Command: "echo hello-{{ .name }}"},
	}
	resolved := map[string]string{"name": "world"}

	err := resolveDynamicParams(declared, resolved, nil, testLogger())
	if err != nil {
		t.Fatal(err)
	}
	if resolved["greeting"] != "hello-world" {
		t.Errorf("expected hello-world, got %q", resolved["greeting"])
	}
}

func TestResolveDynamicParams_EvaluatedAfterParams(t *testing.T) {
	// Simulate the full flow: resolve params first, then dynamic params reference them.
	params := []config.ParamDef{
		{Name: "env", Default: "staging"},
	}
	dynamicParams := []config.DynamicParamDef{
		{Name: "configPath", Command: "echo /configs/{{ .env }}/app.yaml"},
	}

	resolved, err := resolveParams(params, nil, testLogger())
	if err != nil {
		t.Fatal(err)
	}

	err = resolveDynamicParams(dynamicParams, resolved, nil, testLogger())
	if err != nil {
		t.Fatal(err)
	}

	if resolved["configPath"] != "/configs/staging/app.yaml" {
		t.Errorf("expected /configs/staging/app.yaml, got %q", resolved["configPath"])
	}
}

func TestResolveDynamicParams_ChainedDynamic(t *testing.T) {
	// Dynamic params are evaluated in order; later ones can reference earlier ones.
	declared := []config.DynamicParamDef{
		{Name: "first", Command: "echo alpha"},
		{Name: "second", Command: "echo {{ .first }}-beta"},
	}
	resolved := make(map[string]string)

	err := resolveDynamicParams(declared, resolved, nil, testLogger())
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
