package template

import (
	"reflect"
	"testing"
)

func TestFuncMap_Default(t *testing.T) {
	tests := []struct {
		name string
		def  string
		val  string
		want string
	}{
		{
			name: "empty value returns default",
			def:  "fallback",
			val:  "",
			want: "fallback",
		},
		{
			name: "non-empty value returns value",
			def:  "fallback",
			val:  "actual",
			want: "actual",
		},
		{
			name: "both empty",
			def:  "",
			val:  "",
			want: "",
		},
		{
			name: "empty default with non-empty value",
			def:  "",
			val:  "actual",
			want: "actual",
		},
	}

	fm := FuncMap()
	defaultFn := fm["default"].(func(string, string) string)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := defaultFn(tt.def, tt.val)
			if got != tt.want {
				t.Errorf("default(%q, %q) = %q, want %q", tt.def, tt.val, got, tt.want)
			}
		})
	}
}

func TestFuncMap_Upper(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "lowercase", in: "hello", want: "HELLO"},
		{name: "mixed case", in: "Hello World", want: "HELLO WORLD"},
		{name: "already upper", in: "HELLO", want: "HELLO"},
		{name: "empty", in: "", want: ""},
	}

	fm := FuncMap()
	upperFn := fm["upper"].(func(string) string)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := upperFn(tt.in)
			if got != tt.want {
				t.Errorf("upper(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestFuncMap_Lower(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "uppercase", in: "HELLO", want: "hello"},
		{name: "mixed case", in: "Hello World", want: "hello world"},
		{name: "already lower", in: "hello", want: "hello"},
		{name: "empty", in: "", want: ""},
	}

	fm := FuncMap()
	lowerFn := fm["lower"].(func(string) string)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := lowerFn(tt.in)
			if got != tt.want {
				t.Errorf("lower(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestFuncMap_Indent(t *testing.T) {
	tests := []struct {
		name string
		n    int
		in   string
		want string
	}{
		{name: "single line", n: 2, in: "hello", want: "  hello"},
		{name: "multi line", n: 4, in: "line1\nline2", want: "    line1\n    line2"},
		{name: "zero indent", n: 0, in: "line1\nline2", want: "line1\nline2"},
		{name: "empty string", n: 2, in: "", want: "  "},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := indent(tt.n, tt.in)
			if got != tt.want {
				t.Errorf("indent(%d, %q) = %q, want %q", tt.n, tt.in, got, tt.want)
			}
		})
	}
}

func TestFuncMap_Nindent(t *testing.T) {
	got := nindent(4, "line1\nline2")
	want := "\n    line1\n    line2"
	if got != want {
		t.Errorf("nindent(4, ...) = %q, want %q", got, want)
	}
}

func TestFuncMap_Quote(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "plain", in: "hello", want: `"hello"`},
		{name: "with quotes", in: `say "hi"`, want: `"say \"hi\""`},
		{name: "with newline", in: "a\nb", want: `"a\nb"`},
		{name: "empty", in: "", want: `""`},
	}

	fm := FuncMap()
	quoteFn := fm["quote"].(func(string) string)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := quoteFn(tt.in)
			if got != tt.want {
				t.Errorf("quote(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestFuncMap_ToYaml(t *testing.T) {
	tests := []struct {
		name string
		in   any
		want string
	}{
		{name: "plain string", in: "hello", want: "hello"},
		{name: "string needing quotes", in: "yes", want: `"yes"`},
		{name: "multi line string", in: "line1\nline2", want: "|-\n  line1\n  line2"},
		{name: "map", in: map[string]string{"key": "val"}, want: "key: val"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := toYaml(tt.in)
			if err != nil {
				t.Fatalf("toYaml(%v) returned error: %v", tt.in, err)
			}
			if got != tt.want {
				t.Errorf("toYaml(%v) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestFuncMap_FromYaml(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want any
	}{
		{name: "scalar", in: "hello", want: "hello"},
		{name: "block list", in: "- a\n- b", want: []any{"a", "b"}},
		{name: "flow list", in: "[a, b]", want: []any{"a", "b"}},
		{name: "map", in: "key: val", want: map[string]any{"key": "val"}},
		{
			name: "list of maps",
			in:   "- name: DB_HOST\n  value: pg.internal",
			want: []any{map[string]any{"name": "DB_HOST", "value": "pg.internal"}},
		},
		{name: "empty string", in: "", want: nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := fromYaml(tt.in)
			if err != nil {
				t.Fatalf("fromYaml(%q) returned error: %v", tt.in, err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("fromYaml(%q) = %#v, want %#v", tt.in, got, tt.want)
			}
		})
	}
}

func TestFuncMap_FromYaml_Invalid(t *testing.T) {
	if _, err := fromYaml("key: [unclosed"); err == nil {
		t.Error("fromYaml with invalid YAML should return an error")
	}
}

func TestRenderString_FromYamlRange(t *testing.T) {
	params := map[string]string{"regions": "[us-east-1, eu-west-1]"}
	got, err := RenderString("regions:{{ range .regions | fromYaml }}\n  - {{ . }}{{ end }}", params)
	if err != nil {
		t.Fatalf("RenderString returned error: %v", err)
	}
	want := "regions:\n  - us-east-1\n  - eu-west-1"
	if got != want {
		t.Errorf("RenderString = %q, want %q", got, want)
	}
}

func TestRenderString_FromYamlToYamlRoundTrip(t *testing.T) {
	params := map[string]string{"extraEnv": "- name: DB_HOST\n  value: pg.internal"}
	got, err := RenderString("env:{{ .extraEnv | fromYaml | toYaml | nindent 2 }}", params)
	if err != nil {
		t.Fatalf("RenderString returned error: %v", err)
	}
	want := "env:\n  - name: DB_HOST\n    value: pg.internal"
	if got != want {
		t.Errorf("RenderString = %q, want %q", got, want)
	}
}

func TestRenderString_MultilineIndent(t *testing.T) {
	params := map[string]string{"config": "key1: val1\nkey2: val2"}
	got, err := RenderString("data:{{ .config | nindent 2 }}", params)
	if err != nil {
		t.Fatalf("RenderString returned error: %v", err)
	}
	want := "data:\n  key1: val1\n  key2: val2"
	if got != want {
		t.Errorf("RenderString = %q, want %q", got, want)
	}
}

func TestFuncMap_ContainsExpectedFunctions(t *testing.T) {
	fm := FuncMap()
	expected := []string{"default", "upper", "lower", "indent", "nindent", "quote", "toYaml", "fromYaml"}
	for _, name := range expected {
		if _, ok := fm[name]; !ok {
			t.Errorf("FuncMap() missing expected function %q", name)
		}
	}
}
