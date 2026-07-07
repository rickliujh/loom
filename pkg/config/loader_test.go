package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoad_YAML(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "loom.yaml", `
apiVersion: loom.rickliujh.github.io/v1beta1
kind: Loom
metadata:
  name: yaml-module
spec:
  params:
    - name: serviceName
      required: true
`)

	lf, err := Load(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if lf.Metadata.Name != "yaml-module" {
		t.Errorf("unexpected name: %q", lf.Metadata.Name)
	}
	if len(lf.Spec.Params) != 1 || lf.Spec.Params[0].Name != "serviceName" {
		t.Errorf("unexpected params: %+v", lf.Spec.Params)
	}
}

func TestLoad_Jsonnet(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "loom.jsonnet", `
local services = ['payments', 'billing', 'auth'];
{
  apiVersion: 'loom.rickliujh.github.io/v1beta1',
  kind: 'Loom',
  metadata: { name: 'bulk-onboard' },
  spec: {
    modules: [
      {
        name: 'onboard-' + svc,
        source: '../onboard-service',
        params: { serviceName: svc },
      }
      for svc in services
    ],
  },
}
`)

	lf, err := Load(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if lf.Metadata.Name != "bulk-onboard" {
		t.Errorf("unexpected name: %q", lf.Metadata.Name)
	}
	if len(lf.Spec.Modules) != 3 {
		t.Fatalf("expected 3 child modules, got %d", len(lf.Spec.Modules))
	}
	if lf.Spec.Modules[1].Name != "onboard-billing" {
		t.Errorf("unexpected child name: %q", lf.Spec.Modules[1].Name)
	}
	if lf.Spec.Modules[2].Params["serviceName"] != "auth" {
		t.Errorf("unexpected child params: %+v", lf.Spec.Modules[2].Params)
	}
}

func TestLoad_JsonnetImport(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "lib.libsonnet", `
{
  child(svc):: {
    name: 'onboard-' + svc,
    source: '../onboard-service',
    params: { serviceName: svc },
  },
}
`)
	writeFile(t, dir, "loom.jsonnet", `
local lib = import 'lib.libsonnet';
{
  apiVersion: 'loom.rickliujh.github.io/v1beta1',
  kind: 'Loom',
  metadata: { name: 'with-import' },
  spec: {
    modules: [lib.child('payments')],
  },
}
`)

	lf, err := Load(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(lf.Spec.Modules) != 1 || lf.Spec.Modules[0].Name != "onboard-payments" {
		t.Errorf("unexpected modules: %+v", lf.Spec.Modules)
	}
}

func TestLoad_BothConfigsRejected(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "loom.yaml", "kind: Loom")
	writeFile(t, dir, "loom.jsonnet", "{}")

	_, err := Load(dir)
	if err == nil {
		t.Fatal("expected error when both config files exist")
	}
	if !strings.Contains(err.Error(), "both loom.yaml and loom.jsonnet") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestLoad_MissingConfig(t *testing.T) {
	_, err := Load(t.TempDir())
	if err == nil {
		t.Fatal("expected error when no config file exists")
	}
	if !strings.Contains(err.Error(), "reading loom.yaml") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestLoad_JsonnetEvalError(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "loom.jsonnet", `{ broken: undefined_var }`)

	_, err := Load(dir)
	if err == nil {
		t.Fatal("expected error for invalid jsonnet")
	}
	if !strings.Contains(err.Error(), "evaluating loom.jsonnet") {
		t.Errorf("unexpected error: %v", err)
	}
}
