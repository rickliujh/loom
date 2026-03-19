package generate

import (
	"testing"

	"gopkg.in/yaml.v3"
)

func TestComputeSMP(t *testing.T) {
	tests := []struct {
		name string
		old  string
		new  string
		want string // expected SMP as YAML, or empty if nil expected
	}{
		{
			name: "added field",
			old:  "apiVersion: v1\nkind: Service\n",
			new:  "apiVersion: v1\nkind: Service\nmetadata:\n  name: test\n",
			want: "metadata:\n    name: test\n",
		},
		{
			name: "changed field",
			old:  "name: old\nversion: 1\n",
			new:  "name: new\nversion: 1\n",
			want: "name: new\n",
		},
		{
			name: "no changes",
			old:  "name: same\n",
			new:  "name: same\n",
			want: "",
		},
		{
			name: "nested change",
			old:  "spec:\n  replicas: 1\n  image: nginx\n",
			new:  "spec:\n  replicas: 3\n  image: nginx\n",
			want: "spec:\n    replicas: 3\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ComputeSMP([]byte(tt.old), []byte(tt.new))

			if tt.want == "" {
				if got != nil {
					t.Errorf("expected nil, got %q", string(got))
				}
				return
			}

			if got == nil {
				t.Fatal("expected non-nil SMP")
			}

			// Normalize both for comparison.
			var gotVal, wantVal any
			if err := yaml.Unmarshal(got, &gotVal); err != nil {
				t.Fatalf("cannot parse got: %v", err)
			}
			if err := yaml.Unmarshal([]byte(tt.want), &wantVal); err != nil {
				t.Fatalf("cannot parse want: %v", err)
			}

			gotBytes, _ := yaml.Marshal(gotVal)
			wantBytes, _ := yaml.Marshal(wantVal)
			if string(gotBytes) != string(wantBytes) {
				t.Errorf("SMP mismatch:\ngot:\n%s\nwant:\n%s", string(got), tt.want)
			}
		})
	}
}

func TestComputeSMP_InvalidYAML(t *testing.T) {
	got := ComputeSMP([]byte("not: yaml: valid: {"), []byte("name: test"))
	if got != nil {
		t.Errorf("expected nil for invalid YAML, got %q", string(got))
	}
}
