package template

import "testing"

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

func TestFuncMap_ContainsExpectedFunctions(t *testing.T) {
	fm := FuncMap()
	expected := []string{"default", "upper", "lower"}
	for _, name := range expected {
		if _, ok := fm[name]; !ok {
			t.Errorf("FuncMap() missing expected function %q", name)
		}
	}
}
