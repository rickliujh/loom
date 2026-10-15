package module

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/rickliujh/loom/pkg/config"
	"github.com/rickliujh/loom/pkg/params"
	tmpl "github.com/rickliujh/loom/pkg/template"
)

// An optional param nobody sets is present in the resolved map, so templates
// print nothing for it rather than "<no value>" (which the render guard would
// now reject).
func TestLoad_SP6_UnsetOptionalParamIsPresentAndEmpty(t *testing.T) {
	dir := t.TempDir()
	writeLoomSpec(t, dir, config.Spec{
		Params: []config.ParamDef{{Name: "opt"}, {Name: "set", Default: "x"}},
	})
	mod, err := Load(dir, nil, testLogger())
	if err != nil {
		t.Fatal(err)
	}
	v, ok := mod.Params["opt"]
	if !ok || v != "" {
		t.Fatalf("opt = %#v (present %v), want present and \"\"", v, ok)
	}
	got, err := tmpl.RenderString(`[{{ .opt }}][{{ if .opt }}set{{ end }}][{{ default "d" .opt }}]`, mod.Params)
	if err != nil {
		t.Fatal(err)
	}
	if want := "[][][d]"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// writeTypedChild writes a child module that declares a list param and
// renders it into <out>/app.yaml, so a test can read back what it received.
func writeTypedChild(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, "templates"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeLoomYAML(t, dir, `
apiVersion: loom.rickliujh.github.io/v1beta1
kind: Loom
metadata:
  name: typed-child
spec:
  params:
    - name: out
      required: true
    - name: sources
      type: list
      required: true
  operations:
    - name: render
      newFiles:
        source: templates
        dest: "{{ .out }}"
`)
	if err := os.WriteFile(filepath.Join(dir, "templates", "app.yaml"),
		[]byte("sources:{{ .sources | toYaml | nindent 2 }}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

const wantSources = `sources:
  - chart: nginx
    repoURL: https://charts.example.com
    targetRevision: 1.10
  - path: apps/web
    repoURL: https://git.example.com/web.git
    targetRevision: "3"
`

// A parent hands a child a list two ways: as a literal list (its string
// leaves templated), and forwarded from its own list param through toYaml,
// which the child's declared type parses back into a list.
func TestExecute_SP13_ParentPassesListToChild(t *testing.T) {
	parentDir := t.TempDir()
	writeTypedChild(t, filepath.Join(parentDir, "child"))
	writeLoomYAML(t, parentDir, `
apiVersion: loom.rickliujh.github.io/v1beta1
kind: Loom
metadata:
  name: parent
spec:
  params:
    - name: chartRepo
      default: https://charts.example.com
    - name: sources
      type: list
      default:
        - repoURL: https://charts.example.com
          chart: nginx
          targetRevision: 1.10
        - repoURL: https://git.example.com/web.git
          path: apps/web
          targetRevision: "3"
  modules:
    - name: literal
      source: ./child
      params:
        out: literal
        sources:
          - repoURL: "{{ .chartRepo }}"
            chart: nginx
            targetRevision: 1.10
          - repoURL: https://git.example.com/web.git
            path: apps/web
            targetRevision: "3"
    - name: forwarded
      source: ./child
      params:
        out: forwarded
        sources: "{{ .sources | toYaml }}"
`)
	mod, err := Load(parentDir, nil, testLogger())
	if err != nil {
		t.Fatal(err)
	}
	target := t.TempDir()
	if err := Execute(context.Background(), mod, target, RunOptions{}); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	for _, out := range []string{"literal", "forwarded"} {
		got, err := os.ReadFile(filepath.Join(target, out, "app.yaml"))
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != wantSources {
			t.Errorf("%s child rendered:\n%s\nwant:\n%s", out, got, wantSources)
		}
	}
}

// loom.jsonnet evaluates to the same schema, so a jsonnet parent passes a real
// list too.
func TestExecute_SP13_JsonnetParentPassesList(t *testing.T) {
	parentDir := t.TempDir()
	writeTypedChild(t, filepath.Join(parentDir, "child"))
	jsonnet := `
local sources = [
  { repoURL: 'https://charts.example.com', chart: 'nginx', targetRevision: '1.10' },
];
{
  apiVersion: 'loom.rickliujh.github.io/v1beta1',
  kind: 'Loom',
  metadata: { name: 'jsonnet-parent' },
  spec: {
    modules: [
      { name: 'child', source: './child', params: { out: 'out', sources: sources } },
    ],
  },
}
`
	if err := os.WriteFile(filepath.Join(parentDir, "loom.jsonnet"), []byte(jsonnet), 0o644); err != nil {
		t.Fatal(err)
	}
	mod, err := Load(parentDir, nil, testLogger())
	if err != nil {
		t.Fatal(err)
	}
	target := t.TempDir()
	if err := Execute(context.Background(), mod, target, RunOptions{}); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(target, "out", "app.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	// A jsonnet string stays a string, so YAML quotes the version.
	want := "sources:\n  - chart: nginx\n    repoURL: https://charts.example.com\n    targetRevision: \"1.10\"\n"
	if string(got) != want {
		t.Errorf("rendered:\n%s\nwant:\n%s", got, want)
	}
}

func TestExecute_SP13_ChildTypeMismatchNamesParam(t *testing.T) {
	parentDir := t.TempDir()
	writeTypedChild(t, filepath.Join(parentDir, "child"))
	writeLoomYAML(t, parentDir, `
apiVersion: loom.rickliujh.github.io/v1beta1
kind: Loom
metadata:
  name: parent
spec:
  modules:
    - name: child
      source: ./child
      params:
        out: x
        sources: {repoURL: x}
`)
	mod, err := Load(parentDir, nil, testLogger())
	if err != nil {
		t.Fatal(err)
	}
	err = Execute(context.Background(), mod, t.TempDir(), RunOptions{})
	if err == nil || !strings.Contains(err.Error(), `param "sources" is declared list, but received a map`) {
		t.Errorf("err = %v, want a type mismatch naming sources", err)
	}
}

func TestLoad_SP12_CLIStringParsedForListParam(t *testing.T) {
	dir := t.TempDir()
	writeTypedChild(t, dir)

	mod, err := Load(dir, map[string]any{
		"out":     "x",
		"sources": "[{repoURL: https://x, chart: y, targetRevision: 1.0.0}]",
	}, testLogger())
	if err != nil {
		t.Fatal(err)
	}
	want := []any{map[string]any{"repoURL": "https://x", "chart": "y", "targetRevision": "1.0.0"}}
	if !reflect.DeepEqual(mod.Params["sources"], want) {
		t.Errorf("sources = %#v, want %#v", mod.Params["sources"], want)
	}

	_, err = Load(dir, map[string]any{"out": "x", "sources": "just-a-string"}, testLogger())
	if err == nil || !strings.Contains(err.Error(), `param "sources" is declared list, but received a string that parses as a string, not a list`) {
		t.Errorf("err = %v, want a clear type error", err)
	}
}

func TestLoad_SP14_TypedDynamicParam(t *testing.T) {
	dir := t.TempDir()
	writeLoomYAML(t, dir, `
apiVersion: loom.rickliujh.github.io/v1beta1
kind: Loom
metadata:
  name: dyn
spec:
  dynamicParams:
    - name: regions
      type: list
      command: "printf -- '- us-east-1\n- eu-west-1\n'"
    - name: labels
      type: map
      command: "exit 1"
      default: "{team: {{ index .regions 0 }}}"
    - name: broken
      type: list
      command: "echo not-a-list"
      default: "[]"
  operations: []
`)
	_, err := Load(dir, nil, testLogger())
	if err == nil || !strings.Contains(err.Error(), `dynamic param "broken": param "broken" is declared list`) {
		t.Fatalf("err = %v, want the non-list output rejected", err)
	}

	// Drop the broken one and the rest resolves to typed values.
	cfg, _ := os.ReadFile(filepath.Join(dir, "loom.yaml"))
	trimmed := strings.Split(string(cfg), "    - name: broken")[0] + "  operations: []\n"
	writeLoomYAML(t, dir, trimmed)
	mod, err := Load(dir, nil, testLogger())
	if err != nil {
		t.Fatal(err)
	}
	if want := []any{"us-east-1", "eu-west-1"}; !reflect.DeepEqual(mod.Params["regions"], want) {
		t.Errorf("regions = %#v, want %#v", mod.Params["regions"], want)
	}
	if want := map[string]any{"team": "us-east-1"}; !reflect.DeepEqual(mod.Params["labels"], want) {
		t.Errorf("labels (from fallback default) = %#v, want %#v", mod.Params["labels"], want)
	}
}

// The shipped structured example must keep loading and rendering: it is the
// worked example the structured-params guide is built on.
func TestExampleStructured_RendersGuestbook(t *testing.T) {
	root := filepath.Join("..", "..", "example-structured")
	provided, err := params.ParseFile(filepath.Join(root, "guestbook.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	mod, err := Load(filepath.Join(root, "argocd-app"), provided, testLogger())
	if err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(root, "argocd-app", "templates", "apps", "{{ .appName }}.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	got, err := tmpl.RenderFile(body, mod.Params)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"  sources:\n    - repoURL: https://charts.example.com\n      targetRevision: 1.10\n      chart: guestbook\n",
		"      helm:\n        releaseName: guestbook\n        valueFiles:\n          - $values/guestbook/values-prod.yaml\n",
		"    - repoURL: https://git.example.com/guestbook-values.git\n      targetRevision: HEAD\n      ref: values\n",
	} {
		if !strings.Contains(string(got), want) {
			t.Errorf("rendered example missing %q:\n%s", want, got)
		}
	}

	parent, err := Load(filepath.Join(root, "platform"), map[string]any{"redisSources": "[{repoURL: https://x, chart: redis}]"}, testLogger())
	if err != nil {
		t.Fatalf("platform example: %v", err)
	}
	if len(parent.Config.Spec.Modules) != 2 {
		t.Errorf("platform example composes %d modules, want 2", len(parent.Config.Spec.Modules))
	}
}
