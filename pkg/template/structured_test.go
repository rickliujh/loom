package template

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestDecodeYAML_SP1_LiteralPreservation(t *testing.T) {
	tests := []struct {
		in   string
		want any
	}{
		{"1.10", Number("1.10")},
		{"010", Number("010")},
		{"1e3", Number("1e3")},
		{"0x1F", Number("0x1F")},
		{"+3", Number("+3")},
		{"99999999999999999999", Number("99999999999999999999")},
		{".inf", Number(".inf")},
		{"1.0", Number("1.0")},
		{"0.0", Number("0.0")},
		{"-0", Number("-0")},
		{"0o17", Number("0o17")},
		{"1e20", Number("1e20")},
		{"9223372036854775807", 9223372036854775807},
		{"18446744073709551615", uint64(18446744073709551615)},
		{"0.1", 0.1},
		{"1e+21", 1e+21},
		{"3", 3},
		{"-7", -7},
		{"1.5", 1.5},
		{`"3"`, "3"},
		{`"1.10"`, "1.10"},
		{"true", true},
		{"false", false},
		{`"true"`, "true"},
		{"null", nil},
		{"~", nil},
		{"", nil},
		{"plain", "plain"},
		{"[a, 1.10, 2]", []any{"a", Number("1.10"), 2}},
		{"{k: 1.10, n: 3}", map[string]any{"k": Number("1.10"), "n": 3}},
	}
	for _, tt := range tests {
		got, err := DecodeYAML([]byte(tt.in))
		if err != nil {
			t.Errorf("DecodeYAML(%q) error: %v", tt.in, err)
			continue
		}
		if !reflect.DeepEqual(got, tt.want) {
			t.Errorf("DecodeYAML(%q) = %#v, want %#v", tt.in, got, tt.want)
		}
	}
}

func TestDecodeYAML_SP1_AnchorsAndMergeKeys(t *testing.T) {
	src := "base: &b {repoURL: x, targetRevision: 1.10}\nitem:\n  <<: *b\n  targetRevision: main\nref: *b\n"
	got, err := DecodeYAML([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	m := got.(map[string]any)
	item := m["item"].(map[string]any)
	if item["repoURL"] != "x" || item["targetRevision"] != "main" {
		t.Errorf("merged item = %#v, want repoURL from the anchor and targetRevision overridden", item)
	}
	if ref := m["ref"].(map[string]any); ref["targetRevision"] != Number("1.10") {
		t.Errorf("alias = %#v, want the anchored mapping", ref)
	}
}

func TestDecodeYAML_SP1_DuplicateKeyRejected(t *testing.T) {
	if _, err := DecodeYAML([]byte("a: 1\na: 2\n")); err == nil {
		t.Error("duplicate mapping key should be an error")
	}
}

func TestToYaml_SP1_RoundTripKeepsScalars(t *testing.T) {
	src := "targetRevision: 1.10\nvalue: \"3\"\nflag: true\nreplicas: 3\nversion: 010\n"
	v, err := DecodeYAML([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	got, err := toYaml(v)
	if err != nil {
		t.Fatal(err)
	}
	// Keys come out sorted: a Go map carries no order.
	want := "flag: true\nreplicas: 3\ntargetRevision: 1.10\nvalue: \"3\"\nversion: 010"
	if got != want {
		t.Errorf("toYaml round trip =\n%s\nwant\n%s", got, want)
	}
}

func TestRenderString_SP1_NumberPrintsSourceText(t *testing.T) {
	params := map[string]any{"v": Number("1.10"), "n": 3}
	got, err := RenderString(`{{ .v }} {{ eq .n 3 }} {{ eq .v "1.10" }} {{ .v | quote }}`, params)
	if err != nil {
		t.Fatal(err)
	}
	if want := `1.10 true true "1.10"`; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestRenderString_SP1_FromYamlKeepsLiteral(t *testing.T) {
	params := map[string]any{"sources": "- chart: nginx\n  targetRevision: 1.10\n"}
	got, err := RenderString(`{{ .sources | fromYaml | toYaml }}`, params)
	if err != nil {
		t.Fatal(err)
	}
	if want := "- chart: nginx\n  targetRevision: 1.10"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestToJson_SP4(t *testing.T) {
	tests := []struct {
		in   any
		want string
	}{
		{map[string]any{"b": 1, "a": "x"}, `{"a":"x","b":1}`},
		{[]any{"a", Number("1.10"), true, nil}, `["a",1.10,true,null]`},
		{Number("010"), `"010"`},
		{Number("0x1F"), `"0x1F"`},
		{"s", `"s"`},
	}
	for _, tt := range tests {
		got, err := toJson(tt.in)
		if err != nil || got != tt.want {
			t.Errorf("toJson(%#v) = %q, %v; want %q", tt.in, got, err, tt.want)
		}
	}
}

func TestDefault_SP2_EmptinessRule(t *testing.T) {
	tests := []struct {
		name string
		val  any
		want any
	}{
		{"nil", nil, "fb"},
		{"empty string", "", "fb"},
		{"empty list", []any{}, "fb"},
		{"empty map", map[string]any{}, "fb"},
		{"zero is a value", 0, 0},
		{"false is a value", false, false},
		{"string", "x", "x"},
		{"number", Number("1.10"), Number("1.10")},
		{"list", []any{"a"}, []any{"a"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := defaultFn("fb", tt.val); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("default(fb, %#v) = %#v, want %#v", tt.val, got, tt.want)
			}
		})
	}
}

func TestRenderString_SP2_DefaultOnMissingField(t *testing.T) {
	params := map[string]any{"sources": []any{
		map[string]any{"repoURL": "a"},
		map[string]any{"repoURL": "b", "targetRevision": Number("1.10")},
	}}
	got, err := RenderString(`{{ range .sources }}{{ default "HEAD" .targetRevision }};{{ end }}`, params)
	if err != nil {
		t.Fatal(err)
	}
	if want := "HEAD;1.10;"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestRequired_SP3(t *testing.T) {
	if v, err := required("need it", "x"); err != nil || v != "x" {
		t.Errorf("required with a value = %v, %v", v, err)
	}
	for _, empty := range []any{nil, "", []any{}, map[string]any{}} {
		if _, err := required("need it", empty); err == nil || err.Error() != "need it" {
			t.Errorf("required(%#v) error = %v, want the message", empty, err)
		}
	}
	if v, err := required("need it", 0); err != nil || v != 0 {
		t.Errorf("required(0) = %v, %v; zero is a value", v, err)
	}
}

func TestRenderString_SP3_RequiredNamesTheItem(t *testing.T) {
	params := map[string]any{"sources": []any{
		map[string]any{"repoURL": "a"},
		map[string]any{"chart": "b"},
	}}
	tmpl := `{{ range $i, $s := .sources }}{{ required (printf "sources[%d].repoURL is required" $i) $s.repoURL }}{{ end }}`
	_, err := RenderString(tmpl, params)
	if err == nil || !strings.Contains(err.Error(), "sources[1].repoURL is required") {
		t.Errorf("err = %v, want the required message naming sources[1]", err)
	}
}

func TestHasKey_SP4(t *testing.T) {
	m := map[string]any{"a": nil, "b": 1}
	if !hasKey(m, "a") || !hasKey(m, "b") || hasKey(m, "c") {
		t.Error("hasKey should report present keys, including ones set to null")
	}
	if hasKey(nil, "a") || hasKey("str", "a") || hasKey([]any{"a"}, "a") {
		t.Error("hasKey on a non-map should be false")
	}
	got, err := RenderString(`{{ if hasKey .m "b" }}yes{{ end }}`, map[string]any{"m": m})
	if err != nil || got != "yes" {
		t.Errorf("render = %q, %v", got, err)
	}
}

func TestJoin_SP4(t *testing.T) {
	tests := []struct {
		list any
		want string
	}{
		{[]any{"a", "b"}, "a,b"},
		{[]string{"a", "b"}, "a,b"},
		{[]any{"a", 3, Number("1.10")}, "a,3,1.10"},
		{[]any{}, ""},
		{nil, ""},
	}
	for _, tt := range tests {
		got, err := join(",", tt.list)
		if err != nil || got != tt.want {
			t.Errorf("join(%#v) = %q, %v; want %q", tt.list, got, err, tt.want)
		}
	}
	if _, err := join(",", "abc"); err == nil {
		t.Error("join of a non-list should error")
	}
	got, err := RenderString(`{{ .regions | join "," }}`, map[string]any{"regions": []any{"us", "eu"}})
	if err != nil || got != "us,eu" {
		t.Errorf("pipe form = %q, %v", got, err)
	}
}

func TestStringFuncs_SP4_RejectMissingValue(t *testing.T) {
	for _, fn := range []string{"upper", "lower", "quote"} {
		_, err := RenderString(`{{ `+fn+` .m.missing }}`, map[string]any{"m": map[string]any{}})
		if err == nil || !strings.Contains(err.Error(), "value is missing") {
			t.Errorf("%s of a missing field: err = %v, want a missing-value error", fn, err)
		}
	}
}

func TestRender_SP5_NoValueGuard(t *testing.T) {
	params := map[string]any{"src": map[string]any{"repoURL": "x"}}

	_, err := RenderString("repoURL: {{ .src.repoURL }}\ntargetRevision: {{ .src.targetRevision }}\n", params)
	if !errors.Is(err, ErrMissingValue) {
		t.Fatalf("err = %v, want ErrMissingValue", err)
	}
	for _, want := range []string{"line 2", "default", "required", "if", "with"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q should mention %q", err, want)
		}
	}

	if _, err := RenderFile([]byte("rev: {{ .src.targetRevision }}"), params); !errors.Is(err, ErrMissingValue) {
		t.Errorf("RenderFile err = %v, want ErrMissingValue", err)
	}

	// Guarded forms are fine: a missing field is nil, which is falsy.
	for _, ok := range []string{
		`{{ default "HEAD" .src.targetRevision }}`,
		`{{ with .src.targetRevision }}{{ . }}{{ end }}`,
		`{{ if .src.targetRevision }}{{ .src.targetRevision }}{{ end }}`,
	} {
		if _, err := RenderString(ok, params); err != nil {
			t.Errorf("%s: unexpected error %v", ok, err)
		}
	}

	// A template that writes the literal itself is not flagged.
	if got, err := RenderString("docs mention <no value> {{ .src.repoURL }}", params); err != nil || !strings.Contains(got, "<no value>") {
		t.Errorf("literal in template: got %q, %v", got, err)
	}
}

func TestFuncMap_SP4_NewFunctionsRegistered(t *testing.T) {
	fm := FuncMap()
	for _, name := range []string{"required", "hasKey", "toJson", "join"} {
		if _, ok := fm[name]; !ok {
			t.Errorf("FuncMap() missing %q", name)
		}
	}
}

// A list or map has no text form but Go's own ([a b], map[k:v]); text
// functions refuse it rather than write that into a target file.
func TestStringFuncs_SP4_RejectListsAndMaps(t *testing.T) {
	params := map[string]any{
		"cfg":  map[string]any{"a": "b"},
		"list": []any{"a", "b"},
		"srcs": []any{map[string]any{"k": "v"}},
	}
	for _, tt := range []struct{ tmpl, want string }{
		{`values: {{ .cfg | nindent 2 }}`, "nindent: value is a map, not text; render it with toYaml or toJson"},
		{`{{ indent 2 .cfg }}`, "indent: value is a map"},
		{`{{ .list | upper }}`, "upper: value is a list, not text; render it with toYaml, toJson or join"},
		{`{{ lower .list }}`, "lower: value is a list"},
		{`{{ quote .cfg }}`, "quote: value is a map"},
		{`{{ .list | split "," }}`, "split: value is a list"},
		{`{{ .srcs | join "," }}`, "join: element 0: join: value is a map"},
	} {
		got, err := RenderString(tt.tmpl, params)
		if err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%s: got %q, err %v; want error %q", tt.tmpl, got, err, tt.want)
		}
	}
}

// Text that a param value carries is never mistaken for a missing value: the
// guard stands down for a render whose params contain the literal anywhere.
func TestRender_SP5_LiteralFromParamValueIsNotMissing(t *testing.T) {
	body := "fix: stop printing <no value> in manifests"
	got, err := RenderString(`{{ .body }}`, map[string]any{"body": body})
	if err != nil || got != body {
		t.Errorf("param value carrying the literal: got %q, %v", got, err)
	}

	nested := map[string]any{"notes": []any{map[string]any{"text": "says <no value>"}}}
	if _, err := RenderString(`{{ range .notes }}{{ .text }}{{ end }}`, nested); err != nil {
		t.Errorf("nested param value carrying the literal: %v", err)
	}

	// Params that do not carry it keep the guard on.
	_, err = RenderString(`{{ .body }} {{ .m.missing }}`, map[string]any{"body": "ok", "m": map[string]any{}})
	if !errors.Is(err, ErrMissingValue) {
		t.Errorf("err = %v, want ErrMissingValue", err)
	}
}

func TestMapStrings_SP13_PathsOrderAndShape(t *testing.T) {
	in := map[string]any{
		"b": []any{"x", map[string]any{"k": "y"}},
		"a": "z",
		"n": 3,
	}
	var visited []string
	got, err := MapStrings(in, "p", func(path, s string) (string, error) {
		visited = append(visited, path+"="+s)
		return strings.ToUpper(s), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"p.a=z", "p.b[0]=x", "p.b[1].k=y"}; !reflect.DeepEqual(visited, want) {
		t.Errorf("visited %v, want %v", visited, want)
	}
	want := map[string]any{"a": "Z", "b": []any{"X", map[string]any{"k": "Y"}}, "n": 3}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("MapStrings = %#v, want %#v", got, want)
	}
	if in["a"] != "z" {
		t.Error("MapStrings modified its input")
	}
	_, err = MapStrings([]any{"a", "b"}, "", func(path, s string) (string, error) {
		if s == "a" {
			return "", errors.New("stop at " + path)
		}
		t.Error("walk continued past an error")
		return s, nil
	})
	if err == nil || err.Error() != "stop at [0]" {
		t.Errorf("err = %v", err)
	}
}
