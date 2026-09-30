package server

import (
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

const structuredSpec = `  params:
    - name: serviceName
      required: true
    - name: sources
      type: list
      required: true
    - name: replicas
    - name: region
      default: eu
  operations: []
`

func TestServe_Info(t *testing.T) {
	e := newEnv(t, func(c *Config) { c.MaxConcurrentJobs = 3 })
	var info struct {
		Version           string          `json:"version"`
		Roots             []string        `json:"roots"`
		Workspace         string          `json:"workspace"`
		MaxConcurrentJobs int             `json:"maxConcurrentJobs"`
		Capabilities      map[string]bool `json:"capabilities"`
		Env               map[string]bool `json:"env"`
	}
	e.call(http.MethodGet, "/api/v1/info", nil, http.StatusOK, &info)
	if info.Version != "test" || !slices.Equal(info.Roots, []string{e.root}) || info.MaxConcurrentJobs != 3 || info.Workspace == "" {
		t.Errorf("info = %+v", info)
	}
	for _, k := range []string{"git", "gh", "glab"} {
		if _, ok := info.Capabilities[k]; !ok {
			t.Errorf("capabilities lack %s", k)
		}
	}
}

func TestServe_SV21_EnvIsReportedAsBooleans(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "ghp_supersecretvalue")
	t.Setenv("GITLAB_TOKEN", "")
	e := newEnv(t)
	resp := e.request(http.MethodGet, "/api/v1/info", nil)
	defer resp.Body.Close()
	var raw map[string]json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw["env"]), "supersecret") {
		t.Fatal("an environment value leaked")
	}
	var env map[string]bool
	if err := json.Unmarshal(raw["env"], &env); err != nil {
		t.Fatalf("env is not a map of booleans: %s", raw["env"])
	}
	if !env["GITHUB_TOKEN"] || env["GITLAB_TOKEN"] {
		t.Errorf("env = %v", env)
	}
	for k := range env {
		if !slices.Contains(envReported, k) {
			t.Errorf("unexpected env key %s", k)
		}
	}
}

func TestServe_Discovery(t *testing.T) {
	e := newEnv(t)
	writeModule(t, filepath.Join(e.root, "modules", "onboard"), "onboard-service", structuredSpec+"  target:\n    url: https://example.com/r.git\n")
	writeModule(t, filepath.Join(e.root, "modules", "onboard", "child"), "child", "  operations: []\n")
	writeFile(t, filepath.Join(e.root, "broken", "loom.yaml"), "apiVersion: [\n")
	writeFile(t, filepath.Join(e.root, "js", "loom.jsonnet"), `{apiVersion: 'loom.rickliujh.github.io/v1beta1', kind: 'Loom', metadata: {name: 'js'}, spec: {}}`)
	for _, skipped := range []string{".hidden/m", "node_modules/m", "vendor/m", "x/__functions/m", ".git/m"} {
		writeModule(t, filepath.Join(e.root, skipped), "skipped", "  operations: []\n")
	}
	// A symlinked directory is not followed.
	outside := realTempDir(t)
	writeModule(t, filepath.Join(outside, "m"), "outside", "  operations: []\n")
	if err := os.Symlink(outside, filepath.Join(e.root, "link")); err != nil {
		t.Fatal(err)
	}

	var out struct {
		Modules []moduleEntry `json:"modules"`
	}
	e.call(http.MethodGet, "/api/v1/modules", nil, http.StatusOK, &out)
	byRel := map[string]moduleEntry{}
	for _, m := range out.Modules {
		byRel[m.Rel] = m
	}
	if len(byRel) != 4 {
		t.Fatalf("modules = %+v", out.Modules)
	}
	on := byRel["modules/onboard"]
	if on.Name != "onboard-service" || on.Format != "yaml" || on.Params.Required != 2 || on.Params.Total != 4 || !on.HasTarget || on.Root != e.root || on.Dir != filepath.Join(e.root, "modules/onboard") {
		t.Errorf("onboard = %+v", on)
	}
	if _, ok := byRel["modules/onboard/child"]; !ok {
		t.Error("discovery stopped below a module")
	}
	if b := byRel["broken"]; b.LoadError == "" || b.Name != "" {
		t.Errorf("broken = %+v", b)
	}
	if js := byRel["js"]; js.Format != "jsonnet" || js.Name != "js" {
		t.Errorf("jsonnet = %+v", js)
	}

	// Cached until refresh.
	writeModule(t, filepath.Join(e.root, "late"), "late", "  operations: []\n")
	e.call(http.MethodGet, "/api/v1/modules", nil, http.StatusOK, &out)
	if len(out.Modules) != 4 {
		t.Errorf("the list was not cached: %d modules", len(out.Modules))
	}
	e.call(http.MethodGet, "/api/v1/modules?refresh=1", nil, http.StatusOK, &out)
	if len(out.Modules) != 5 {
		t.Errorf("refresh did not rescan: %d modules", len(out.Modules))
	}
}

func TestServe_CreateModule(t *testing.T) {
	e := newEnv(t)
	dir := filepath.Join(e.root, "modules", "new-service")
	var entry moduleEntry
	e.call(http.MethodPost, "/api/v1/modules", map[string]string{"dir": dir, "name": "new-service"}, http.StatusCreated, &entry)
	if entry.Name != "new-service" || entry.Dir != dir || entry.LoadError != "" || entry.Rel != "modules/new-service" {
		t.Errorf("entry = %+v", entry)
	}
	// The new module validates.
	var v validateOut
	e.call(http.MethodPost, "/api/v1/validate", map[string]any{"source": dir}, http.StatusOK, &v)
	if !v.Valid {
		t.Errorf("new module is invalid: %+v", v)
	}
	// And it is listed.
	var out struct {
		Modules []moduleEntry `json:"modules"`
	}
	e.call(http.MethodGet, "/api/v1/modules", nil, http.StatusOK, &out)
	if len(out.Modules) != 1 {
		t.Errorf("modules = %+v", out.Modules)
	}

	if c := e.errorCode(http.MethodPost, "/api/v1/modules", map[string]string{"dir": dir}, http.StatusConflict); c != codeConflict {
		t.Errorf("existing dir: %s", c)
	}
	if c := e.errorCode(http.MethodPost, "/api/v1/modules", map[string]string{"dir": filepath.Join(realTempDir(t), "x")}, http.StatusForbidden); c != codeForbidden {
		t.Errorf("outside the roots: %s", c)
	}
	if c := e.errorCode(http.MethodPost, "/api/v1/modules", map[string]string{"dir": "relative/x"}, http.StatusBadRequest); c != codeInvalid {
		t.Errorf("relative dir: %s", c)
	}
	if c := e.errorCode(http.MethodPost, "/api/v1/modules", map[string]string{"dir": filepath.Join(e.root, "y"), "name": "bad name: {x}"}, http.StatusBadRequest); c != codeInvalid {
		t.Errorf("bad name: %s", c)
	}
	if _, err := os.Stat(filepath.Join(e.root, "y")); !os.IsNotExist(err) {
		t.Error("a refused module left its directory behind")
	}
}

func TestServe_SV7_SourceRules(t *testing.T) {
	e := newEnv(t)
	mod := writeModule(t, filepath.Join(e.root, "m"), "m", structuredSpec)
	outside := writeModule(t, realTempDir(t), "outside", "  operations: []\n")
	// A symlink inside a root that points outside it is outside.
	if err := os.Symlink(outside, filepath.Join(e.root, "escape")); err != nil {
		t.Fatal(err)
	}

	check := func(e *testEnv, source string, want int, wantCode string) {
		t.Helper()
		body := map[string]any{"source": source}
		if want == http.StatusOK {
			e.call(http.MethodPost, "/api/v1/validate", body, http.StatusOK, nil)
			return
		}
		if c := e.errorCode(http.MethodPost, "/api/v1/validate", body, want); c != wantCode {
			t.Errorf("source %q: code %s, want %s", source, c, wantCode)
		}
	}
	check(e, mod, http.StatusOK, "")
	check(e, "file://"+mod, http.StatusOK, "")
	check(e, "m", http.StatusBadRequest, codeInvalid)
	check(e, "./m", http.StatusBadRequest, codeInvalid)
	check(e, "", http.StatusBadRequest, codeInvalid)
	check(e, outside, http.StatusForbidden, codeForbidden)
	check(e, "file://"+outside, http.StatusForbidden, codeForbidden)
	check(e, filepath.Join(e.root, "escape"), http.StatusForbidden, codeForbidden)
	check(e, filepath.Join(e.root, "missing"), http.StatusNotFound, codeNotFound)

	open := newEnv(t, func(c *Config) { c.AllowOutsideRoots = true })
	check(open, outside, http.StatusOK, "")

	closed := newEnv(t, func(c *Config) { c.NoRemoteModules = true })
	check(closed, "https://github.com/o/r.git//m", http.StatusForbidden, codeForbidden)
	check(closed, "git@github.com:o/r.git", http.StatusForbidden, codeForbidden)
}

// A git URL is accepted as a source when remote modules are allowed and is
// cloned to be read; one that cannot be cloned is unprocessable. The URL
// points at a closed loopback port, so nothing leaves the machine.
func TestServe_RemoteSourceUnreachable(t *testing.T) {
	e := newEnv(t)
	if c := e.errorCode(http.MethodPost, "/api/v1/inspect", map[string]any{"source": "https://127.0.0.1:1/none.git//sub"}, http.StatusUnprocessableEntity); c != codeUnprocessable {
		t.Errorf("code %s", c)
	}
}

func TestServe_Inspect(t *testing.T) {
	e := newEnv(t)
	mod := writeModule(t, filepath.Join(e.root, "m"), "onboard", structuredSpec)
	var raw json.RawMessage
	e.call(http.MethodPost, "/api/v1/inspect", map[string]any{
		"source": mod,
		"params": json.RawMessage(`{"serviceName":"payments","sources":[{"repoURL":"https://charts.example.com","targetRevision":1.10}]}`),
	}, http.StatusOK, &raw)

	var out struct {
		Modules []struct {
			Path   []string `json:"path"`
			Module struct {
				Name   string            `json:"name"`
				Params []json.RawMessage `json:"params"`
			} `json:"module"`
		} `json:"modules"`
		MissingParams []any  `json:"missingParams"`
		CLI           string `json:"cli"`
		ParamsFile    string `json:"paramsFile"`
	}
	data := []byte(raw)
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Modules) != 1 || out.Modules[0].Module.Name != "onboard" {
		t.Fatalf("report = %s", data)
	}
	var sources string
	for _, p := range out.Modules[0].Module.Params {
		if strings.Contains(string(p), `"name":"sources"`) {
			sources = string(p)
		}
	}
	for _, want := range []string{`"type":"list"`, `"state":"provided"`, `"targetRevision":1.10`, `"valueYaml":"- repoURL: https://charts.example.com\n  targetRevision: 1.10\n"`} {
		if !strings.Contains(sources, want) {
			t.Errorf("sources param %s lacks %s", sources, want)
		}
	}
	if !strings.Contains(out.CLI, "loom inspect "+mod) || !strings.Contains(out.CLI, "-p serviceName=payments") || !strings.Contains(out.CLI, "--params-file params.yaml") {
		t.Errorf("cli = %q", out.CLI)
	}
	if !strings.Contains(out.ParamsFile, "targetRevision: 1.10") {
		t.Errorf("paramsFile = %q", out.ParamsFile)
	}

	// Depth 0 describes the tree; a broken module is 422.
	broken := filepath.Join(e.root, "broken")
	writeFile(t, filepath.Join(broken, "loom.yaml"), "kind: [\n")
	if c := e.errorCode(http.MethodPost, "/api/v1/inspect", map[string]any{"source": broken}, http.StatusUnprocessableEntity); c != codeUnprocessable {
		t.Errorf("broken: %s", c)
	}
	if c := e.errorCode(http.MethodPost, "/api/v1/inspect", map[string]any{"source": mod, "depth": -1}, http.StatusBadRequest); c != codeInvalid {
		t.Errorf("negative depth: %s", c)
	}
	if c := e.errorCode(http.MethodPost, "/api/v1/inspect", map[string]any{"source": mod, "modules": []string{"nope"}}, http.StatusUnprocessableEntity); c != codeUnprocessable {
		t.Errorf("unknown submodule: %s", c)
	}
}

func TestServe_Validate(t *testing.T) {
	e := newEnv(t)
	good := writeModule(t, filepath.Join(e.root, "good"), "good", "  params:\n    - name: region\n  operations: []\n")
	var v validateOut
	e.call(http.MethodPost, "/api/v1/validate", map[string]any{"source": good}, http.StatusOK, &v)
	if !v.Valid || v.Count != 1 || len(v.Warnings) != 1 || v.Warnings[0].Module != "" || len(v.Errors) != 0 {
		t.Errorf("good = %+v", v)
	}
	bad := writeModule(t, filepath.Join(e.root, "bad"), "", "  operations: []\n")
	e.call(http.MethodPost, "/api/v1/validate", map[string]any{"source": bad}, http.StatusOK, &v)
	if v.Valid || len(v.Errors) != 1 {
		t.Errorf("bad = %+v", v)
	}
	parent := writeModule(t, filepath.Join(e.root, "parent"), "parent", "  modules:\n    - name: kid\n      source: ./kid\n")
	writeModule(t, filepath.Join(parent, "kid"), "", "  operations: []\n")
	e.call(http.MethodPost, "/api/v1/validate", map[string]any{"source": parent, "recursive": true}, http.StatusOK, &v)
	if v.Valid || v.Count != 2 || len(v.Errors) != 1 || v.Errors[0].Module != "kid" {
		t.Errorf("recursive = %+v", v)
	}
	empty := filepath.Join(e.root, "empty")
	if err := os.MkdirAll(empty, 0o755); err != nil {
		t.Fatal(err)
	}
	if c := e.errorCode(http.MethodPost, "/api/v1/validate", map[string]any{"source": empty}, http.StatusUnprocessableEntity); c != codeUnprocessable {
		t.Errorf("no config: %s", c)
	}
}

func TestServe_ParamsCheck(t *testing.T) {
	e := newEnv(t)
	mod := writeModule(t, filepath.Join(e.root, "m"), "m", structuredSpec)
	var out paramsCheckOut
	e.call(http.MethodPost, "/api/v1/params/check", map[string]any{
		"source": mod,
		"params": map[string]any{"sources": "- repoURL: x\n", "replicas": []any{1}, "typo": "x"},
	}, http.StatusOK, &out)
	want := paramsCheckOut{
		Fields: map[string]fieldCheck{
			"sources":  {OK: true},
			"replicas": {Error: `param "replicas" is declared string, but received a list`},
		},
		Undeclared: []string{"typo"},
		Missing:    []string{"serviceName"},
	}
	if !reflect.DeepEqual(out, want) {
		t.Errorf("check = %+v\nwant %+v", out, want)
	}
	e.call(http.MethodPost, "/api/v1/params/check", map[string]any{"source": mod, "params": map[string]any{"sources": "a: b"}}, http.StatusOK, &out)
	if out.Fields["sources"].OK || !slices.Contains(out.Missing, "serviceName") {
		t.Errorf("check = %+v", out)
	}
}

// SV10: describing a module runs none of it. Every command the module could
// run — a dynamic param, an if predicate, a shell step, a child's — would
// leave a marker file.
func TestServe_SV10_ReadOnlyEndpointsExecuteNothing(t *testing.T) {
	e := newEnv(t)
	marker := filepath.Join(realTempDir(t), "ran")
	touch := "touch '" + marker + "'"
	mod := writeModule(t, filepath.Join(e.root, "m"), "m", `  params:
    - name: env
  dynamicParams:
    - name: dyn
      command: "`+touch+`; echo x"
  modules:
    - name: kid
      source: ./kid
      if: "`+touch+`"
  operations:
    - name: step
      if: "`+touch+`"
      shell:
        command: "`+touch+`"
`)
	writeModule(t, filepath.Join(mod, "kid"), "kid", `  dynamicParams:
    - name: dyn2
      command: "`+touch+`"
  operations: []
`)
	params := map[string]any{"env": "prod"}
	e.call(http.MethodGet, "/api/v1/modules?refresh=1", nil, http.StatusOK, nil)
	e.call(http.MethodPost, "/api/v1/inspect", map[string]any{"source": mod, "params": params, "depth": 0}, http.StatusOK, nil)
	e.call(http.MethodPost, "/api/v1/validate", map[string]any{"source": mod, "recursive": true}, http.StatusOK, nil)
	e.call(http.MethodPost, "/api/v1/params/check", map[string]any{"source": mod, "params": params}, http.StatusOK, nil)
	e.call(http.MethodGet, "/api/v1/files?source="+url.QueryEscape(mod)+"&path=", nil, http.StatusOK, nil)
	e.call(http.MethodGet, "/api/v1/files?source="+url.QueryEscape(mod)+"&path=loom.yaml", nil, http.StatusOK, nil)
	e.call(http.MethodPost, "/api/v1/cli", map[string]any{"kind": "run", "source": mod, "mode": "execute", "params": params}, http.StatusOK, nil)
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("a read-only endpoint executed a module command")
	}
}

func TestServe_YAMLHelpers(t *testing.T) {
	e := newEnv(t)
	var parsed map[string]any
	e.call(http.MethodPost, "/api/v1/yaml/parse", map[string]string{"text": "- a\n- b\n"}, http.StatusOK, &parsed)
	if !reflect.DeepEqual(parsed["value"], []any{"a", "b"}) {
		t.Errorf("parse = %v", parsed)
	}
	resp := e.request(http.MethodPost, "/api/v1/yaml/parse", map[string]string{"text": "v: 1.10\n"})
	var rawBody map[string]json.RawMessage
	_ = json.NewDecoder(resp.Body).Decode(&rawBody)
	resp.Body.Close()
	if string(rawBody["value"]) != `{"v":1.10}` || !strings.Contains(string(rawBody["valueYaml"]), "v: 1.10") {
		t.Errorf("1.10 did not survive: %s", rawBody)
	}

	var failed struct {
		Error struct {
			Line    int    `json:"line"`
			Col     int    `json:"col"`
			Message string `json:"message"`
		} `json:"error"`
	}
	e.call(http.MethodPost, "/api/v1/yaml/parse", map[string]string{"text": "- a\n b: [\n"}, http.StatusOK, &failed)
	if failed.Error.Line == 0 || failed.Error.Message == "" {
		t.Errorf("parse error = %+v", failed)
	}

	var formatted map[string]string
	e.call(http.MethodPost, "/api/v1/yaml/format", json.RawMessage(`{"value":{"a":1,"v":1.10,"l":["x"]}}`), http.StatusOK, &formatted)
	if formatted["text"] != "a: 1\nl:\n  - x\nv: 1.10\n" {
		t.Errorf("format = %q", formatted["text"])
	}
}

func TestServe_Docs(t *testing.T) {
	e := newEnv(t)
	var idx struct {
		Topics []struct{ Name, Title string } `json:"topics"`
	}
	e.call(http.MethodGet, "/api/v1/docs", nil, http.StatusOK, &idx)
	var names []string
	for _, tp := range idx.Topics {
		names = append(names, tp.Name)
	}
	if !slices.Contains(names, "reference/cli-run") || !slices.Contains(names, "reference/serve-api") {
		t.Fatalf("topics = %v", names)
	}
	for _, p := range []string{"/api/v1/docs/reference/cli-run", "/api/v1/docs/reference%2Fcli-run", "/api/v1/docs/cli-run"} {
		resp := e.request(http.MethodGet, p, nil)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != "text/markdown; charset=utf-8" {
			t.Errorf("GET %s: %d %s", p, resp.StatusCode, resp.Header.Get("Content-Type"))
		}
	}
	if c := e.errorCode(http.MethodGet, "/api/v1/docs/nope-nothing", nil, http.StatusNotFound); c != codeNotFound {
		t.Errorf("unknown topic: %s", c)
	}
}
