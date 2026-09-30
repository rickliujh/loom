package config

import (
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	tmpl "github.com/rickliujh/loom/pkg/template"
)

const paramsHeader = `
apiVersion: loom.rickliujh.github.io/v1beta1
kind: Loom
metadata:
  name: typed
spec:
`

func loadSpec(t *testing.T, spec string) *LoomFile {
	t.Helper()
	dir := t.TempDir()
	writeFile(t, dir, "loom.yaml", paramsHeader+spec)
	lf, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return lf
}

func TestLoad_SP7_TypedParamsAndStructuredDefaults(t *testing.T) {
	lf := loadSpec(t, `
  params:
    - name: version
      default: 1.10
    - name: replicas
      default: 3
    - name: sources
      type: list
      default:
        - repoURL: https://charts.example.com
          targetRevision: 1.10
    - name: values
      type: map
      default: {replicas: 3, enabled: true}
    - name: none
      type: list
  dynamicParams:
    - name: tags
      type: list
      command: echo '[a, b]'
`)
	p := lf.Spec.Params
	// A string param's default is the exact text written, as it always was.
	if p[0].Default != "1.10" || p[1].Default != "3" {
		t.Errorf("string defaults = %#v, %#v; want the literal text", p[0].Default, p[1].Default)
	}
	wantSources := []any{map[string]any{"repoURL": "https://charts.example.com", "targetRevision": tmpl.Number("1.10")}}
	if p[2].Type != ParamList || !reflect.DeepEqual(p[2].Default, wantSources) {
		t.Errorf("sources = %s %#v, want list %#v", p[2].Type, p[2].Default, wantSources)
	}
	wantValues := map[string]any{"replicas": 3, "enabled": true}
	if p[3].Type != ParamMap || !reflect.DeepEqual(p[3].Default, wantValues) {
		t.Errorf("values = %s %#v, want map %#v", p[3].Type, p[3].Default, wantValues)
	}
	if p[4].HasDefault() || p[4].Type.Effective() != ParamList {
		t.Errorf("none = %#v, want a list param without default", p[4])
	}
	if lf.Spec.DynamicParams[0].Type != ParamList {
		t.Errorf("dynamic type = %q, want list", lf.Spec.DynamicParams[0].Type)
	}
	if err := Validate(lf); err != nil {
		t.Errorf("valid typed params rejected: %v", err)
	}
}

func TestLoad_SP7_OmittedTypeIsString(t *testing.T) {
	lf := loadSpec(t, `
  params:
    - name: env
`)
	if got := lf.Spec.Params[0].Type.Effective(); got != ParamString {
		t.Errorf("effective type = %q, want string", got)
	}
}

// ParamDef decodes itself; it must stay as strict as the rest of the schema.
func TestLoad_SP7_UnknownParamFieldRejected(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "loom.yaml", paramsHeader+`
  params:
    - name: env
      defualt: prod
`)
	_, err := Load(dir)
	if err == nil || !strings.Contains(err.Error(), "field defualt not found in type config.ParamDef") {
		t.Errorf("err = %v, want an unknown-field error", err)
	}
}

func TestValidate_SP8_TypeAndDefaultShape(t *testing.T) {
	tests := []struct {
		name string
		spec string
		want string
	}{
		{"unknown type", `
  params:
    - name: a
      type: array`, `param "a": unknown type "array" (supported: string, list, map)`},
		{"templated type", `
  params:
    - name: a
      type: "{{ .t }}"`, `param "a": unknown type "{{ .t }}"`},
		{"list default on string param", `
  params:
    - name: a
      default: [x]`, `param "a": default is a list, but the param's type is string`},
		{"string default on list param", `
  params:
    - name: a
      type: list
      default: "[x]"`, `param "a": default is a string, but the param's type is list`},
		{"map default on list param", `
  params:
    - name: a
      type: list
      default: {k: v}`, `param "a": default is a map, but the param's type is list`},
		{"dynamic unknown type", `
  dynamicParams:
    - name: a
      type: dict
      command: echo`, `dynamicParam "a": unknown type "dict"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lf := loadSpec(t, tt.spec)
			err := Validate(lf)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("err = %v, want %q", err, tt.want)
			}
		})
	}
}

// $ is the param map everywhere, so $.name inside a range body is a param
// reference and is checked; a field of the ranged item is not.
func TestValidateInDir_SP9_DollarInRangeBodyIsAParamReference(t *testing.T) {
	body := "{{ range .sources }}{{ .repoURL }} {{ $.env }}{{ end }}\n"

	lf, dir := tmplModule(t, map[string]string{"app.yaml": body}, "sources")
	err := ValidateInDir(lf, dir)
	if err == nil || !strings.Contains(err.Error(), `references undeclared param "env"`) {
		t.Errorf("err = %v, want $.env reported as undeclared", err)
	}
	if err != nil && strings.Contains(err.Error(), `"repoURL"`) {
		t.Errorf("an item field was read as a param reference: %v", err)
	}

	lf, dir = tmplModule(t, map[string]string{"app.yaml": body}, "sources", "env", "svc")
	warnings := warnOnly(t, lf, dir)
	if len(warnings) != 1 || !strings.Contains(warnings[0], `"svc"`) {
		t.Errorf("expected only svc to be unused ($.env counts as a use), got %v", warnings)
	}
}

func TestValidateInDir_SP9_NestedAccessCountsAsReference(t *testing.T) {
	body := "{{ .values.image.tag }} {{ index .labels \"team\" }} {{ index $ \"my-app\" }}\n"
	lf, dir := tmplModule(t, map[string]string{"app.yaml": body}, "values", "labels", "my-app", "svc")
	warnings := warnOnly(t, lf, dir)
	if len(warnings) != 1 || !strings.Contains(warnings[0], `"svc"`) {
		t.Errorf("expected only svc to be unused, got %v", warnings)
	}
}

// The else branch of range/with runs with dot unchanged, so it is read as
// top-level template text.
func TestValidateInDir_SP9_ElseBranchKeepsDot(t *testing.T) {
	body := "{{ range .sources }}x{{ else }}{{ .fallback }}{{ end }}\n"
	lf, dir := tmplModule(t, map[string]string{"app.yaml": body}, "sources")
	err := ValidateInDir(lf, dir)
	if err == nil || !strings.Contains(err.Error(), `references undeclared param "fallback"`) {
		t.Errorf("err = %v, want the else branch's reference checked", err)
	}
}

// A bare $ handed to a function reads the param map opaquely, like dot.
func TestValidateInDir_SP9_BareDollarDisablesUnusedCheck(t *testing.T) {
	lf, dir := tmplModule(t, map[string]string{"app.yaml": "{{ range .sources }}{{ toYaml $ }}{{ end }}\n"}, "sources", "svc")
	if w := warnOnly(t, lf, dir); len(w) != 0 {
		t.Errorf("an opaque $ must skip the unused check, got %v", w)
	}
}

// Every string leaf of a structured child param is a template (T4): it is
// syntax-checked, its references are checked, and it counts as a use.
func TestValidate_SP9_StructuredChildParamLeavesChecked(t *testing.T) {
	lf := loadSpec(t, `
  params:
    - name: repo
  modules:
    - name: child
      source: ./child
      params:
        sources:
          - repoURL: "{{ .repo }}"
            helm:
              valueFiles: ["{{ .envFile }}"]
          - path: "{{ .bad"
`)
	err := Validate(lf)
	if err == nil {
		t.Fatal("expected violations")
	}
	for _, want := range []string{
		`module "child" param "sources[0].helm.valueFiles[0]": references undeclared param "envFile"`,
		`module "child" param "sources[1].path": invalid template`,
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("err = %v, want %q", err, want)
		}
	}
	if strings.Contains(err.Error(), `"repo"`) {
		t.Errorf("declared param reported: %v", err)
	}
}

// Printing a list or map param bare writes Go's own syntax into the target.
// The run does what the template says, so it is a warning, not a violation.
func TestValidateInDir_SP9_PrintingStructuredParamBareWarns(t *testing.T) {
	lf, dir := tmplModule(t, map[string]string{
		"app.yaml": "a: {{ .sources }}\nb: {{ .sources | toYaml }}\n{{ range .sources }}{{ $.sources }}{{ end }}\n",
	})
	lf.Spec.Params = []ParamDef{{Name: "sources", Type: ParamList}}
	warnings := warnOnly(t, lf, dir)
	if len(warnings) != 1 || !strings.Contains(warnings[0], `prints list param "sources" directly`) {
		t.Errorf("warnings = %v, want one printed-directly warning", warnings)
	}
}

// Handing a list or map param to a function that takes text fails the render
// (or, for the print builtins, writes Go's formatting); validate warns about
// both the argument and the pipe form.
func TestValidateInDir_SP9_StructuredParamIntoTextFuncWarns(t *testing.T) {
	lf, dir := tmplModule(t, map[string]string{
		"app.yaml": "a: {{ .cfg | nindent 2 }}\nb: {{ indent 2 .cfg }}\nc: {{ printf \"%v\" .sources }}\n" +
			"d: {{ .cfg | toYaml | nindent 2 }}\ne: {{ .sources | join \",\" | upper }}\n{{ range .sources }}{{ $.cfg | quote }}{{ end }}\n",
	})
	lf.Spec.Params = []ParamDef{{Name: "cfg", Type: ParamMap}, {Name: "sources", Type: ParamList}}
	warnings := warnOnly(t, lf, dir)
	want := []string{
		`passes map param "cfg" to nindent, which takes text`,
		`passes map param "cfg" to indent, which takes text`,
		`passes list param "sources" to printf, which takes text`,
		`passes map param "cfg" to quote, which takes text`,
	}
	if len(warnings) != len(want) {
		t.Errorf("warnings = %q, want %d", warnings, len(want))
	}
	for _, w := range want {
		found := false
		for _, got := range warnings {
			found = found || strings.Contains(got, w)
		}
		if !found {
			t.Errorf("missing warning %q in %q", w, warnings)
		}
	}
}

// ParamDef decodes itself field by field; a repeated key must still fail, as
// it does everywhere else in the schema.
func TestLoad_SP7_DuplicateParamFieldRejected(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "loom.yaml", paramsHeader+`
  params:
    - {name: v, default: 1.10, name: w}
`)
	_, err := Load(dir)
	if err == nil || !strings.Contains(err.Error(), `mapping key "name" already defined at line`) {
		t.Errorf("err = %v, want a duplicate-key error", err)
	}
}

// A top-level scalar is the text written whether it is written in the
// mapping or merged in through <<.
func TestDecodeParamValues_SP11_MergedScalarsKeepText(t *testing.T) {
	var doc yaml.Node
	src := "base: &b {enabled: True, version: 1.10, list: [1.10]}\nitem:\n  <<: *b\n  direct: True\n"
	if err := yaml.Unmarshal([]byte(src), &doc); err != nil {
		t.Fatal(err)
	}
	item := doc.Content[0].Content[3]
	got, err := DecodeParamValues(item)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"enabled": "True", "version": "1.10", "direct": "True", "list": []any{tmpl.Number("1.10")}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("DecodeParamValues = %#v, want %#v", got, want)
	}
}
