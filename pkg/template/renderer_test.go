package template

import "testing"

func TestRenderString(t *testing.T) {
	tests := []struct {
		name    string
		tmpl    string
		params  map[string]string
		want    string
		wantErr bool
	}{
		{
			name:   "simple substitution",
			tmpl:   "hello {{ .name }}",
			params: map[string]string{"name": "world"},
			want:   "hello world",
		},
		{
			name:   "multiple params",
			tmpl:   "{{ .greeting }}, {{ .name }}!",
			params: map[string]string{"greeting": "Hi", "name": "Alice"},
			want:   "Hi, Alice!",
		},
		{
			name:   "no placeholders",
			tmpl:   "static text",
			params: map[string]string{"unused": "value"},
			want:   "static text",
		},
		{
			name:   "empty params",
			tmpl:   "no params here",
			params: map[string]string{},
			want:   "no params here",
		},
		{
			name:   "nil params",
			tmpl:   "nil params",
			params: nil,
			want:   "nil params",
		},
		{
			name:   "empty template",
			tmpl:   "",
			params: map[string]string{"k": "v"},
			want:   "",
		},
		{
			name:    "invalid template syntax",
			tmpl:    "{{ .name",
			params:  map[string]string{"name": "x"},
			wantErr: true,
		},
		{
			name:   "custom func upper",
			tmpl:   `{{ .name | upper }}`,
			params: map[string]string{"name": "hello"},
			want:   "HELLO",
		},
		{
			name:   "custom func lower",
			tmpl:   `{{ .name | lower }}`,
			params: map[string]string{"name": "HELLO"},
			want:   "hello",
		},
		{
			name:   "custom func default with empty value",
			tmpl:   `{{ default "fallback" .name }}`,
			params: map[string]string{"name": ""},
			want:   "fallback",
		},
		{
			name:   "custom func default with non-empty value",
			tmpl:   `{{ default "fallback" .name }}`,
			params: map[string]string{"name": "actual"},
			want:   "actual",
		},
		{
			name:   "chained funcs",
			tmpl:   `{{ default "fallback" .name | upper }}`,
			params: map[string]string{"name": ""},
			want:   "FALLBACK",
		},
		{
			name:   "multiline template",
			tmpl:   "line1: {{ .a }}\nline2: {{ .b }}",
			params: map[string]string{"a": "x", "b": "y"},
			want:   "line1: x\nline2: y",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := RenderString(tt.tmpl, tt.params)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("RenderString(%q) = %q, want %q", tt.tmpl, got, tt.want)
			}
		})
	}
}

func TestRenderFile(t *testing.T) {
	tests := []struct {
		name    string
		content []byte
		params  map[string]string
		want    []byte
		wantErr bool
	}{
		{
			name:    "simple substitution",
			content: []byte("name: {{ .name }}"),
			params:  map[string]string{"name": "myapp"},
			want:    []byte("name: myapp"),
		},
		{
			name:    "yaml-like content",
			content: []byte("apiVersion: v1\nmetadata:\n  name: {{ .name }}\n  namespace: {{ .namespace }}"),
			params:  map[string]string{"name": "myapp", "namespace": "production"},
			want:    []byte("apiVersion: v1\nmetadata:\n  name: myapp\n  namespace: production"),
		},
		{
			name:    "empty content",
			content: []byte(""),
			params:  map[string]string{"k": "v"},
			want:    []byte(""),
		},
		{
			name:    "nil content",
			content: nil,
			params:  map[string]string{},
			want:    []byte(""),
		},
		{
			name:    "invalid template syntax",
			content: []byte("{{ .name"),
			params:  map[string]string{"name": "x"},
			wantErr: true,
		},
		{
			name:    "custom funcs available",
			content: []byte(`{{ .env | upper }}`),
			params:  map[string]string{"env": "prod"},
			want:    []byte("PROD"),
		},
		{
			name:    "no placeholders",
			content: []byte("static: content\nkey: value"),
			params:  map[string]string{},
			want:    []byte("static: content\nkey: value"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := RenderFile(tt.content, tt.params)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if string(got) != string(tt.want) {
				t.Errorf("RenderFile() = %q, want %q", got, tt.want)
			}
		})
	}
}
