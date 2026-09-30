package params

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/rickliujh/loom/pkg/config"
	tmpl "github.com/rickliujh/loom/pkg/template"
)

func TestCoerce_SP10_DeclaredTypeDrivesParsing(t *testing.T) {
	list := []any{map[string]any{"repoURL": "x"}}
	m := map[string]any{"k": "v"}
	tests := []struct {
		name string
		typ  config.ParamType
		in   any
		want any
	}{
		{"list as is", config.ParamList, list, list},
		{"map as is", config.ParamMap, m, m},
		{"flow list string", config.ParamList, "[{repoURL: x}]", list},
		{"block list string", config.ParamList, "- repoURL: x\n", list},
		{"flow map string", config.ParamMap, "{k: v}", m},
		{"block map string", config.ParamMap, "k: v", m},
		{"empty string is empty list", config.ParamList, "", []any{}},
		{"null is empty map", config.ParamMap, nil, map[string]any{}},
		{"literal kept in parsed list", config.ParamList, "[1.10]", []any{tmpl.Number("1.10")}},
		{"string as is", config.ParamString, "prod", "prod"},
		{"omitted type is string", "", "prod", "prod"},
		{"number text kept", config.ParamString, tmpl.Number("1.10"), "1.10"},
		{"int as text", config.ParamString, 3, "3"},
		{"bool as text", config.ParamString, true, "true"},
		{"null string is empty", config.ParamString, nil, ""},
		{"string that looks like yaml stays a string", config.ParamString, "[a, b]", "[a, b]"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Coerce("p", tt.typ, tt.in)
			if err != nil {
				t.Fatalf("Coerce: %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Coerce(%s, %#v) = %#v, want %#v", tt.typ, tt.in, got, tt.want)
			}
		})
	}
}

func TestCoerce_SP10_MismatchNamesParamTypeAndValue(t *testing.T) {
	tests := []struct {
		typ  config.ParamType
		in   any
		want string
	}{
		{config.ParamList, map[string]any{"k": "v"}, `param "sources" is declared list, but received a map`},
		{config.ParamMap, []any{"a"}, `param "sources" is declared map, but received a list`},
		{config.ParamList, 3, `param "sources" is declared list, but received a scalar (3)`},
		{config.ParamList, "abc", `param "sources" is declared list, but received a string that parses as a string, not a list: "abc"`},
		{config.ParamList, "{k: v}", `param "sources" is declared list, but received a string that parses as a map, not a list`},
		{config.ParamList, "[unclosed", `param "sources" is declared list, but received a string that is not valid YAML`},
		{config.ParamString, []any{"a"}, `param "sources" is declared string, but received a list`},
		{config.ParamString, map[string]any{}, `param "sources" is declared string, but received a map`},
	}
	for _, tt := range tests {
		_, err := Coerce("sources", tt.typ, tt.in)
		if err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("Coerce(%s, %#v) err = %v, want %q", tt.typ, tt.in, err, tt.want)
		}
	}
}

func TestParseYAML_SP11_NestedParamsFile(t *testing.T) {
	got, err := ParseYAML([]byte(`
env: prod
replicas: 3
version: 1.10
enabled: True
octal: 010
empty:
sources:
  - repoURL: https://charts.example.com
    chart: nginx
    targetRevision: 1.10
    value: "3"
    enabled: true
`))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{
		// Top-level scalars are the text written, as a flat file always read.
		"env": "prod", "replicas": "3", "version": "1.10", "enabled": "True", "octal": "010",
		"empty": nil,
		"sources": []any{map[string]any{
			"repoURL": "https://charts.example.com", "chart": "nginx",
			"targetRevision": tmpl.Number("1.10"), "value": "3", "enabled": true,
		}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ParseYAML =\n%#v\nwant\n%#v", got, want)
	}
}

func TestParseYAML_SP11_Errors(t *testing.T) {
	for in, want := range map[string]string{
		"- a\n- b\n":   "parsing params file: line 1: expected a mapping of param names to values, got a list",
		"a: [unclosed": "parsing params file: yaml:",
		"a: 1\na: 2\n": "parsing params file: line 2: mapping key \"a\" already defined",
	} {
		if _, err := ParseYAML([]byte(in)); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("ParseYAML(%q) err = %v, want %q", in, err, want)
		}
	}
	if got, err := ParseYAML(nil); err != nil || len(got) != 0 {
		t.Errorf("empty file = %v, %v; want no params", got, err)
	}
}

func TestParse_SP12_CLIOverridesFileAndStaysText(t *testing.T) {
	file := filepath.Join(t.TempDir(), "params.yaml")
	if err := os.WriteFile(file, []byte("env: prod\nsources: [{repoURL: a}]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := Parse([]string{"env=staging", "sources=[{repoURL: b}]", "eq=a=b"}, file)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"env": "staging", "sources": "[{repoURL: b}]", "eq": "a=b"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Parse = %#v, want %#v", got, want)
	}
	if _, err := ParseCLI([]string{"novalue"}); err == nil || !strings.Contains(err.Error(), `invalid param format "novalue", expected key=value`) {
		t.Errorf("ParseCLI err = %v", err)
	}
	if _, err := ParseFile(filepath.Join(t.TempDir(), "missing.yaml")); err == nil || !strings.Contains(err.Error(), "reading params file") {
		t.Errorf("ParseFile missing err = %v", err)
	}
}

func TestRenderLeaves_SP13_EveryStringAtAnyDepth(t *testing.T) {
	in := map[string]any{
		"repoURL": "{{ .repo }}",
		"helm":    map[string]any{"valueFiles": []any{"values-{{ .env }}.yaml"}},
		"weight":  3,
		"version": tmpl.Number("1.10"),
	}
	got, err := RenderLeaves(in, map[string]any{"repo": "https://x", "env": "prod"})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{
		"repoURL": "https://x",
		"helm":    map[string]any{"valueFiles": []any{"values-prod.yaml"}},
		"weight":  3,
		"version": tmpl.Number("1.10"),
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("RenderLeaves = %#v, want %#v", got, want)
	}
	_, err = RenderLeaves(map[string]any{"a": []any{"{{ .missing.x }}"}}, map[string]any{"missing": map[string]any{}})
	if err == nil || !strings.Contains(err.Error(), "a[0]:") {
		t.Errorf("err = %v, want the failing leaf's path", err)
	}
}

func TestParseYAML_SP11_MergeKeyScalarsKeepText(t *testing.T) {
	got, err := ParseYAML([]byte("defaults: &d\n  enabled: True\n  version: 010\n<<: *d\nenv: prod\n"))
	if err != nil {
		t.Fatal(err)
	}
	if got["enabled"] != "True" || got["version"] != "010" || got["env"] != "prod" {
		t.Errorf("ParseYAML = %#v, want merged scalars as the text written", got)
	}
}
