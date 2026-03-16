package template

import "testing"

func TestConvertPathTemplate(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "single placeholder in filename",
			in:   "application-__serviceName__.yaml",
			want: "application-{{ .serviceName }}.yaml",
		},
		{
			name: "placeholder in directory name",
			in:   "__env__/config.yaml",
			want: "{{ .env }}/config.yaml",
		},
		{
			name: "multiple placeholders",
			in:   "__namespace__/__serviceName__-deploy.yaml",
			want: "{{ .namespace }}/{{ .serviceName }}-deploy.yaml",
		},
		{
			name: "no placeholders",
			in:   "static/config.yaml",
			want: "static/config.yaml",
		},
		{
			name: "underscore in param name",
			in:   "__service_name__.yaml",
			want: "{{ .service_name }}.yaml",
		},
		{
			name: "existing go template syntax untouched",
			in:   "{{ .name }}/file.yaml",
			want: "{{ .name }}/file.yaml",
		},
		{
			name: "mixed syntax",
			in:   "__dir__/{{ .name }}.yaml",
			want: "{{ .dir }}/{{ .name }}.yaml",
		},
		{
			name: "single underscore not matched",
			in:   "_partial.yaml",
			want: "_partial.yaml",
		},
		{
			name: "placeholder starting with number not matched",
			in:   "__1bad__.yaml",
			want: "__1bad__.yaml",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ConvertPathTemplate(tt.in)
			if got != tt.want {
				t.Errorf("ConvertPathTemplate(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestConvertPathTemplate_RenderIntegration(t *testing.T) {
	params := map[string]string{
		"serviceName": "payments",
		"env":         "prod",
	}

	path := "__env__/application-__serviceName__.yaml"
	converted := ConvertPathTemplate(path)
	rendered, err := RenderString(converted, params)
	if err != nil {
		t.Fatal(err)
	}

	want := "prod/application-payments.yaml"
	if rendered != want {
		t.Errorf("got %q, want %q", rendered, want)
	}
}
