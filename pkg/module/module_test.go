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

func TestResolveParams_ProvidedOverridesCommand(t *testing.T) {
	declared := []config.ParamDef{
		{Name: "foo", Dynamic: "echo command-value"},
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

func TestResolveParams_CommandEvaluated(t *testing.T) {
	declared := []config.ParamDef{
		{Name: "foo", Dynamic: "echo hello-dynamic"},
	}

	result, err := resolveParams(declared, nil, testLogger())
	if err != nil {
		t.Fatal(err)
	}
	if result["foo"] != "hello-dynamic" {
		t.Errorf("expected hello-dynamic, got %q", result["foo"])
	}
}

func TestResolveParams_CommandTrimsTrailingNewlines(t *testing.T) {
	declared := []config.ParamDef{
		{Name: "foo", Dynamic: "printf 'value\\n\\n'"},
	}

	result, err := resolveParams(declared, nil, testLogger())
	if err != nil {
		t.Fatal(err)
	}
	if result["foo"] != "value" {
		t.Errorf("expected %q, got %q", "value", result["foo"])
	}
}

func TestResolveParams_CommandFails(t *testing.T) {
	declared := []config.ParamDef{
		{Name: "foo", Dynamic: "exit 1"},
	}

	_, err := resolveParams(declared, nil, testLogger())
	if err == nil {
		t.Fatal("expected error for failed command")
	}
}

func TestResolveParams_CommandBeforeDefault(t *testing.T) {
	declared := []config.ParamDef{
		{Name: "foo", Dynamic: "echo from-cmd", Default: "from-default"},
	}

	result, err := resolveParams(declared, nil, testLogger())
	if err != nil {
		t.Fatal(err)
	}
	if result["foo"] != "from-cmd" {
		t.Errorf("expected from-cmd, got %q", result["foo"])
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
