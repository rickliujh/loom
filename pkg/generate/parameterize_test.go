package generate

import "testing"

func TestParameterize(t *testing.T) {
	tests := []struct {
		name    string
		content string
		params  map[string]string
		want    string
	}{
		{
			name:    "single param",
			content: "name: payments",
			params:  map[string]string{"serviceName": "payments"},
			want:    "name: {{ .serviceName }}",
		},
		{
			name:    "multiple params",
			content: "name: payments\nnamespace: fintech",
			params:  map[string]string{"serviceName": "payments", "namespace": "fintech"},
			want:    "name: {{ .serviceName }}\nnamespace: {{ .namespace }}",
		},
		{
			name:    "longer value replaced first",
			content: "path: prod-payments",
			params:  map[string]string{"service": "prod-payments", "env": "prod"},
			want:    "path: {{ .service }}",
		},
		{
			name:    "empty value skipped",
			content: "name: payments",
			params:  map[string]string{"empty": "", "serviceName": "payments"},
			want:    "name: {{ .serviceName }}",
		},
		{
			name:    "no params",
			content: "name: payments",
			params:  map[string]string{},
			want:    "name: payments",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Parameterize(tt.content, tt.params)
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParameterizePath(t *testing.T) {
	tests := []struct {
		name   string
		path   string
		params map[string]string
		want   string
	}{
		{
			name:   "param in filename",
			path:   "argocd/application-payments.yaml",
			params: map[string]string{"serviceName": "payments"},
			want:   "argocd/application-__serviceName__.yaml",
		},
		{
			name:   "param in directory",
			path:   "prod/config.yaml",
			params: map[string]string{"env": "prod"},
			want:   "__env__/config.yaml",
		},
		{
			name:   "multiple params in path",
			path:   "prod/payments-deploy.yaml",
			params: map[string]string{"env": "prod", "serviceName": "payments"},
			want:   "__env__/__serviceName__-deploy.yaml",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParameterizePath(tt.path, tt.params)
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}
