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

// SMP3: nil on invalid new content too.
func TestComputeSMP_InvalidNewYAML(t *testing.T) {
	got := ComputeSMP([]byte("name: old"), []byte("invalid: yaml: {{{"))
	if got != nil {
		t.Errorf("expected nil for invalid new YAML, got %q", string(got))
	}
}

// Scalar list diff — only added items are included (expandScalarLists
// prepends old values on apply).
func TestComputeSMP_ScalarListDiff_OnlyAdded(t *testing.T) {
	old := "items:\n  - a\n  - b\n"
	new := "items:\n  - a\n  - b\n  - c\n"
	got := ComputeSMP([]byte(old), []byte(new))
	if got == nil {
		t.Fatal("expected non-nil SMP for list diff")
	}

	var result map[string]any
	if err := yaml.Unmarshal(got, &result); err != nil {
		t.Fatalf("cannot parse SMP result: %v", err)
	}
	items, ok := result["items"].([]any)
	if !ok {
		t.Fatalf("expected items to be a list, got %T", result["items"])
	}
	if len(items) != 1 || items[0] != "c" {
		t.Errorf("expected [c], got %v", items)
	}
}

// Scalar list unchanged — nil.
func TestComputeSMP_ScalarListUnchanged(t *testing.T) {
	old := "items:\n  - a\n  - b\n"
	got := ComputeSMP([]byte(old), []byte(old))
	if got != nil {
		t.Errorf("expected nil for identical scalar lists, got %q", string(got))
	}
}

// Map-list diff — only changed/added items by merge key.
func TestComputeSMP_MapListDiff_ByKey(t *testing.T) {
	old := `
stores:
  - name: vault-1
    namespaces:
      - ns-a
  - name: vault-2
    namespaces:
      - ns-b
`
	new := `
stores:
  - name: vault-1
    namespaces:
      - ns-a
  - name: vault-2
    namespaces:
      - ns-b
      - ns-c
  - name: vault-3
    namespaces:
      - ns-d
`
	got := ComputeSMP([]byte(old), []byte(new))
	if got == nil {
		t.Fatal("expected non-nil SMP for map-list diff")
	}

	var result map[string]any
	if err := yaml.Unmarshal(got, &result); err != nil {
		t.Fatalf("parse error: %v", err)
	}
	stores, ok := result["stores"].([]any)
	if !ok {
		t.Fatalf("expected stores list, got %T", result["stores"])
	}
	// vault-1 is unchanged → excluded. vault-2 changed, vault-3 is new.
	if len(stores) != 2 {
		t.Fatalf("expected 2 items in SMP diff, got %d: %v", len(stores), stores)
	}

	// Check that vault-2 only has the merge key + changed field.
	s0 := stores[0].(map[string]any)
	if s0["name"] != "vault-2" {
		t.Errorf("expected first diff item to be vault-2, got %v", s0["name"])
	}
	ns, ok := s0["namespaces"].([]any)
	if !ok {
		t.Fatalf("expected namespaces list in vault-2 diff")
	}
	// Only the added namespace (ns-c) should be in the diff.
	if len(ns) != 1 || ns[0] != "ns-c" {
		t.Errorf("expected [ns-c] in vault-2 diff, got %v", ns)
	}

	// vault-3 is entirely new.
	s1 := stores[1].(map[string]any)
	if s1["name"] != "vault-3" {
		t.Errorf("expected second diff item to be vault-3, got %v", s1["name"])
	}
}

// Map-list unchanged — nil.
func TestComputeSMP_MapListUnchanged(t *testing.T) {
	doc := `
items:
  - name: a
    value: 1
  - name: b
    value: 2
`
	got := ComputeSMP([]byte(doc), []byte(doc))
	if got != nil {
		t.Errorf("expected nil for identical map-lists, got %q", string(got))
	}
}

// SMP1: deleted keys are NOT included in the SMP output.
func TestComputeSMP_DeletedKeysNotIncluded(t *testing.T) {
	old := "name: test\nversion: 1\nobsolete: true\n"
	new := "name: test\nversion: 1\n"
	got := ComputeSMP([]byte(old), []byte(new))

	// No changes from new's perspective (it's a subset), so SMP should be nil.
	if got != nil {
		t.Errorf("expected nil SMP when only deletions occurred, got %q", string(got))
	}
}

// SMP1: deeply nested map changes produce minimal patch.
func TestComputeSMP_DeeplyNested(t *testing.T) {
	old := `
a:
  b:
    c:
      d: 1
      e: 2
    f: 3
`
	new := `
a:
  b:
    c:
      d: 1
      e: 99
    f: 3
`
	got := ComputeSMP([]byte(old), []byte(new))
	if got == nil {
		t.Fatal("expected non-nil SMP")
	}

	var result map[string]any
	if err := yaml.Unmarshal(got, &result); err != nil {
		t.Fatalf("parse error: %v", err)
	}

	// Should only contain the path a.b.c.e=99, not a.b.c.d or a.b.f.
	aMap := result["a"].(map[string]any)
	bMap := aMap["b"].(map[string]any)
	cMap := bMap["c"].(map[string]any)

	if _, hasd := cMap["d"]; hasd {
		t.Error("SMP should not include unchanged key 'd'")
	}
	if cMap["e"] != 99 {
		t.Errorf("expected e=99 in SMP, got %v", cMap["e"])
	}
	if _, hasF := bMap["f"]; hasF {
		t.Error("SMP should not include unchanged key 'f'")
	}
}

// SMP1: new key added to existing map.
func TestComputeSMP_NewKeyInMap(t *testing.T) {
	old := "metadata:\n  name: app\n"
	new := "metadata:\n  name: app\n  labels:\n    managed-by: loom\n"
	got := ComputeSMP([]byte(old), []byte(new))
	if got == nil {
		t.Fatal("expected non-nil SMP for new key")
	}

	var result map[string]any
	if err := yaml.Unmarshal(got, &result); err != nil {
		t.Fatalf("parse error: %v", err)
	}

	meta := result["metadata"].(map[string]any)
	if _, hasName := meta["name"]; hasName {
		t.Error("SMP should not include unchanged key 'name'")
	}
	labels, ok := meta["labels"].(map[string]any)
	if !ok {
		t.Fatal("expected labels map in SMP")
	}
	if labels["managed-by"] != "loom" {
		t.Errorf("expected managed-by=loom, got %v", labels["managed-by"])
	}
}

// SMP2: identical documents → nil.
func TestComputeSMP_IdenticalComplex(t *testing.T) {
	doc := `
apiVersion: v1
kind: ConfigMap
metadata:
  name: test
data:
  key1: value1
  key2: value2
`
	got := ComputeSMP([]byte(doc), []byte(doc))
	if got != nil {
		t.Errorf("expected nil for identical complex docs, got %q", string(got))
	}
}

// SMP1: scalar value type change (string → int).
func TestComputeSMP_TypeChange(t *testing.T) {
	old := "value: \"3\"\n"
	new := "value: 3\n"
	got := ComputeSMP([]byte(old), []byte(new))
	if got == nil {
		t.Fatal("expected non-nil SMP for type change")
	}

	var result map[string]any
	if err := yaml.Unmarshal(got, &result); err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if result["value"] != 3 {
		t.Errorf("expected value=3 (int), got %v (%T)", result["value"], result["value"])
	}
}
