package bulk

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rickliujh/loom/pkg/config"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

// writeChildModule creates a child module with required, defaulted, and
// dynamic params under dir.
func writeChildModule(t *testing.T, dir, extra string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := `
apiVersion: loom.rickliujh.github.io/v1beta1
kind: Loom
metadata:
  name: onboard-service
spec:
  params:
    - name: serviceName
      required: true
    - name: namespace
      default: "default"
  dynamicParams:
    - name: commitHash
      command: "echo abc"
` + extra + `
  operations:
    - name: announce
      shell:
        command: "echo {{ .serviceName }}"
`
	if err := os.WriteFile(filepath.Join(dir, "loom.yaml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// generate runs bulk generation and returns the generated jsonnet source
// and the wrapper evaluated through the standard loader.
func generate(t *testing.T, opts Options) (string, *config.LoomFile) {
	t.Helper()
	if err := Run(opts, testLogger()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(opts.OutputDir, "loom.jsonnet"))
	if err != nil {
		t.Fatal(err)
	}
	lf, err := config.Load(opts.OutputDir)
	if err != nil {
		t.Fatalf("loading generated wrapper: %v", err)
	}
	return string(raw), lf
}

func TestRun_Placeholder(t *testing.T) {
	tmp := t.TempDir()
	child := filepath.Join(tmp, "onboard-service")
	writeChildModule(t, child, "")
	out := filepath.Join(tmp, "bulk")

	raw, lf := generate(t, Options{ModuleRef: child, OutputDir: out})

	if !strings.Contains(raw, "serviceName: 'CHANGEME',  // required") {
		t.Errorf("expected required placeholder, got:\n%s", raw)
	}
	if !strings.Contains(raw, "namespace: 'default',") {
		t.Errorf("expected default value, got:\n%s", raw)
	}
	if strings.Contains(raw, "commitHash") {
		t.Errorf("dynamic param must not appear in placeholder:\n%s", raw)
	}

	if lf.Metadata.Name != "bulk-onboard-service" {
		t.Errorf("unexpected wrapper name %q", lf.Metadata.Name)
	}
	if len(lf.Spec.Modules) != 1 {
		t.Fatalf("expected 1 child entry, got %d", len(lf.Spec.Modules))
	}
	m := lf.Spec.Modules[0]
	if m.Name != "onboard-service-0" {
		t.Errorf("unexpected entry name %q", m.Name)
	}
	if m.Params["serviceName"] != "CHANGEME" || m.Params["namespace"] != "default" {
		t.Errorf("unexpected entry params: %+v", m.Params)
	}
}

func TestRun_ItemsFileAndNameParam(t *testing.T) {
	tmp := t.TempDir()
	child := filepath.Join(tmp, "onboard-service")
	writeChildModule(t, child, "")
	itemsFile := filepath.Join(tmp, "items.yaml")
	items := `
- serviceName: payments
  namespace: fintech
- serviceName: auth
`
	if err := os.WriteFile(itemsFile, []byte(items), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(tmp, "bulk")

	_, lf := generate(t, Options{
		ModuleRef: child,
		OutputDir: out,
		Name:      "custom-name",
		ItemsFile: itemsFile,
		NameParam: "serviceName",
	})

	if lf.Metadata.Name != "custom-name" {
		t.Errorf("unexpected wrapper name %q", lf.Metadata.Name)
	}
	if len(lf.Spec.Modules) != 2 {
		t.Fatalf("expected 2 child entries, got %d", len(lf.Spec.Modules))
	}
	if lf.Spec.Modules[0].Name != "onboard-service-payments" {
		t.Errorf("unexpected entry name %q", lf.Spec.Modules[0].Name)
	}
	if lf.Spec.Modules[1].Name != "onboard-service-auth" {
		t.Errorf("unexpected entry name %q", lf.Spec.Modules[1].Name)
	}
	if lf.Spec.Modules[0].Params["namespace"] != "fintech" {
		t.Errorf("unexpected params: %+v", lf.Spec.Modules[0].Params)
	}
	if _, ok := lf.Spec.Modules[1].Params["namespace"]; ok {
		t.Errorf("absent item key must not be emitted: %+v", lf.Spec.Modules[1].Params)
	}
}

func TestRun_RelativeSource(t *testing.T) {
	tmp := t.TempDir()
	child := filepath.Join(tmp, "modules", "onboard-service")
	writeChildModule(t, child, "")
	out := filepath.Join(tmp, "bulk")

	_, lf := generate(t, Options{ModuleRef: child, OutputDir: out})

	if lf.Spec.Modules[0].Source != "../modules/onboard-service" {
		t.Errorf("unexpected source %q", lf.Spec.Modules[0].Source)
	}
}

func TestRun_TargetNote(t *testing.T) {
	tmp := t.TempDir()
	child := filepath.Join(tmp, "onboard-service")
	writeChildModule(t, child, `
  target:
    url: "https://github.com/org/repo.git"
`)
	out := filepath.Join(tmp, "bulk")

	raw, _ := generate(t, Options{ModuleRef: child, OutputDir: out})
	if !strings.Contains(raw, "opens its own PR") {
		t.Errorf("expected target topology note, got:\n%s", raw)
	}
}

func TestRun_UndeclaredItemKey(t *testing.T) {
	tmp := t.TempDir()
	child := filepath.Join(tmp, "onboard-service")
	writeChildModule(t, child, "")
	itemsFile := filepath.Join(tmp, "items.yaml")
	if err := os.WriteFile(itemsFile, []byte("- serviceName: a\n  bogus: x\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := Run(Options{ModuleRef: child, OutputDir: filepath.Join(tmp, "bulk"), ItemsFile: itemsFile}, testLogger())
	if err == nil || !strings.Contains(err.Error(), `undeclared parameter "bogus"`) {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestRun_MissingRequiredInItem(t *testing.T) {
	tmp := t.TempDir()
	child := filepath.Join(tmp, "onboard-service")
	writeChildModule(t, child, "")
	itemsFile := filepath.Join(tmp, "items.yaml")
	if err := os.WriteFile(itemsFile, []byte("- namespace: x\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := Run(Options{ModuleRef: child, OutputDir: filepath.Join(tmp, "bulk"), ItemsFile: itemsFile}, testLogger())
	if err == nil || !strings.Contains(err.Error(), `required parameter "serviceName" not provided`) {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestRun_BadNameParam(t *testing.T) {
	tmp := t.TempDir()
	child := filepath.Join(tmp, "onboard-service")
	writeChildModule(t, child, "")

	err := Run(Options{ModuleRef: child, OutputDir: filepath.Join(tmp, "bulk"), NameParam: "nope"}, testLogger())
	if err == nil || !strings.Contains(err.Error(), `--name-param "nope"`) {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestRun_RefusesOverwrite(t *testing.T) {
	tmp := t.TempDir()
	child := filepath.Join(tmp, "onboard-service")
	writeChildModule(t, child, "")
	out := filepath.Join(tmp, "bulk")
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(out, "loom.yaml"), []byte("kind: Loom"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := Run(Options{ModuleRef: child, OutputDir: out}, testLogger())
	if err == nil || !strings.Contains(err.Error(), "refusing to overwrite") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestJsonnetQuoting(t *testing.T) {
	if got := jsonnetString(`it's a \ test`); got != `'it\'s a \\ test'` {
		t.Errorf("jsonnetString: %s", got)
	}
	if got := jsonnetField("serviceName"); got != "serviceName" {
		t.Errorf("jsonnetField ident: %s", got)
	}
	if got := jsonnetField("service-name"); got != "'service-name'" {
		t.Errorf("jsonnetField quoted: %s", got)
	}
	if got := jsonnetField("local"); got != "'local'" {
		t.Errorf("jsonnetField keyword: %s", got)
	}
	if got := jsonnetAccess("service-name"); got != "['service-name']" {
		t.Errorf("jsonnetAccess quoted: %s", got)
	}
}

func TestRun_QuotedParamName(t *testing.T) {
	tmp := t.TempDir()
	child := filepath.Join(tmp, "child")
	if err := os.MkdirAll(child, 0o755); err != nil {
		t.Fatal(err)
	}
	content := `
apiVersion: loom.rickliujh.github.io/v1beta1
kind: Loom
metadata:
  name: quoted
spec:
  params:
    - name: service-name
      required: true
  operations:
    - name: noop
      shell:
        command: "echo {{ index . \"service-name\" }}"
`
	if err := os.WriteFile(filepath.Join(child, "loom.yaml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(tmp, "bulk")

	_, lf := generate(t, Options{ModuleRef: child, OutputDir: out, NameParam: "service-name"})
	if lf.Spec.Modules[0].Params["service-name"] != "CHANGEME" {
		t.Errorf("unexpected params: %+v", lf.Spec.Modules[0].Params)
	}
	if lf.Spec.Modules[0].Name != "quoted-CHANGEME" {
		t.Errorf("unexpected entry name %q", lf.Spec.Modules[0].Name)
	}
}

const typedParams = `
    - name: sources
      type: list
    - name: labels
      type: map
      default: {team: platform}
`

// writeTypedChildModule is writeChildModule plus a list and a map param.
func writeTypedChildModule(t *testing.T, dir string) {
	t.Helper()
	writeChildModule(t, dir, "")
	raw, err := os.ReadFile(filepath.Join(dir, "loom.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	content := strings.Replace(string(raw), `      default: "default"
`, `      default: "default"
`+typedParams, 1)
	if err := os.WriteFile(filepath.Join(dir, "loom.yaml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestRun_SP16_StructuredItemsBecomeJsonnetValues(t *testing.T) {
	tmp := t.TempDir()
	child := filepath.Join(tmp, "onboard-service")
	writeTypedChildModule(t, child)
	itemsFile := filepath.Join(tmp, "items.yaml")
	items := `
- serviceName: payments
  sources:
    - repoURL: https://charts.example.com
      chart: payments
      targetRevision: 1.10
      weight: 3
      enabled: true
  labels: {team: fintech}
`
	if err := os.WriteFile(itemsFile, []byte(items), 0o644); err != nil {
		t.Fatal(err)
	}
	raw, lf := generate(t, Options{ModuleRef: child, OutputDir: filepath.Join(tmp, "bulk"), ItemsFile: itemsFile})

	for _, want := range []string{
		"    sources: [\n      {\n        chart: 'payments',\n        enabled: true,\n        repoURL: 'https://charts.example.com',\n",
		// A number kept as its written text goes in as a string: as a jsonnet
		// number, 1.10 would evaluate to 1.1.
		"        targetRevision: '1.10',\n        weight: 3,\n      },\n    ],\n",
		"    labels: {\n      team: 'fintech',\n    },\n",
	} {
		if !strings.Contains(raw, want) {
			t.Errorf("generated jsonnet missing %q:\n%s", want, raw)
		}
	}
	sources, ok := lf.Spec.Modules[0].Params["sources"].([]any)
	if !ok || len(sources) != 1 {
		t.Fatalf("sources = %#v, want a real list in the evaluated wrapper", lf.Spec.Modules[0].Params["sources"])
	}
	if src := sources[0].(map[string]any); src["targetRevision"] != "1.10" || src["weight"] != 3 {
		t.Errorf("source = %#v", src)
	}
}

func TestRun_SP16_PlaceholderUsesTypedEmptyValues(t *testing.T) {
	tmp := t.TempDir()
	child := filepath.Join(tmp, "onboard-service")
	writeTypedChildModule(t, child)
	raw, _ := generate(t, Options{ModuleRef: child, OutputDir: filepath.Join(tmp, "bulk")})
	for _, want := range []string{"    sources: [],\n", "    labels: {\n      team: 'platform',\n    },\n"} {
		if !strings.Contains(raw, want) {
			t.Errorf("placeholder missing %q:\n%s", want, raw)
		}
	}
}

func TestRun_SP16_ItemTypeMismatchFailsEarly(t *testing.T) {
	tmp := t.TempDir()
	child := filepath.Join(tmp, "onboard-service")
	writeTypedChildModule(t, child)
	itemsFile := filepath.Join(tmp, "items.yaml")
	if err := os.WriteFile(itemsFile, []byte("- serviceName: [a, b]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := Run(Options{ModuleRef: child, OutputDir: filepath.Join(tmp, "bulk"), ItemsFile: itemsFile}, testLogger())
	if err == nil || !strings.Contains(err.Error(), `item 0: param "serviceName" is declared string, but received a list`) {
		t.Errorf("err = %v, want the type mismatch reported for the item", err)
	}

	err = Run(Options{ModuleRef: child, OutputDir: filepath.Join(tmp, "bulk2"), NameParam: "sources"}, testLogger())
	if err == nil || !strings.Contains(err.Error(), `--name-param "sources" is a list parameter; it must be a string`) {
		t.Errorf("err = %v, want --name-param on a list rejected", err)
	}
}
