package server

import (
	"errors"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/rickliujh/loom/internal/skill"
	"github.com/rickliujh/loom/pkg/config"
	tmpl "github.com/rickliujh/loom/pkg/template"
	"gopkg.in/yaml.v3"
)

// capabilities reports which CLIs are installed, looked up once: /info is
// how the UI probes for a session, so it stays cheap.
func (s *Server) capabilities() map[string]bool {
	s.capsOnce.Do(func() {
		s.caps = map[string]bool{}
		for _, bin := range []string{"git", "gh", "glab"} {
			_, err := exec.LookPath(bin)
			s.caps[bin] = err == nil
		}
	})
	return s.caps
}

// envReported are the variables /info reports the presence of. Only whether
// each is set leaves the server, never a value.
var envReported = []string{"GITHUB_TOKEN", "GITLAB_TOKEN", "LOOM_GIT_TOKEN"}

func (s *Server) handleInfo(w http.ResponseWriter, r *http.Request) error {
	caps := s.capabilities()
	env := map[string]bool{}
	for _, k := range envReported {
		env[k] = os.Getenv(k) != ""
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"version":           s.cfg.Version,
		"roots":             s.roots,
		"workspace":         s.stateDir,
		"maxConcurrentJobs": s.cfg.MaxConcurrentJobs,
		"capabilities":      caps,
		"env":               env,
	})
	return nil
}

func (s *Server) handleModulesList(w http.ResponseWriter, r *http.Request) error {
	refresh := r.URL.Query().Get("refresh")
	mods, truncated := s.listModules(refresh != "" && refresh != "0" && refresh != "false")
	if mods == nil {
		mods = []moduleEntry{}
	}
	out := map[string]any{"modules": mods}
	if truncated {
		out["truncated"] = true
	}
	writeJSON(w, http.StatusOK, out)
	return nil
}

type createModuleRequest struct {
	Dir  string `json:"dir"`
	Name string `json:"name"`
}

// moduleNameRe keeps new module names to what reads well as a directory,
// an instance name and a breadcrumb segment.
var moduleNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

func (s *Server) handleModuleCreate(w http.ResponseWriter, r *http.Request) error {
	var req createModuleRequest
	if err := decodeBody(r, &req); err != nil {
		return err
	}
	dir, err := s.resolveOutput(req.Dir, "dir")
	if err != nil {
		return err
	}
	root := s.rootOf(dir)
	if root == "" {
		return errForbidden("%s is outside the server's roots", req.Dir).withField("dir")
	}
	if _, err := os.Lstat(dir); err == nil {
		return errConflict("%s already exists", req.Dir).withField("dir")
	}
	name := req.Name
	if name == "" {
		name = filepath.Base(dir)
	}
	if !moduleNameRe.MatchString(name) {
		return errInvalid("name", "name %q must start with a letter or digit and hold only letters, digits, '.', '_' and '-'", name)
	}
	content, err := minimalConfig(name)
	if err != nil {
		return err
	}
	// Check the config before anything touches the disk.
	var lf config.LoomFile
	if err := yaml.Unmarshal(content, &lf); err != nil {
		return err
	}
	if err := config.Validate(&lf); err != nil {
		return errInvalid("name", "the new module would be invalid: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		return err
	}
	if err := os.Mkdir(dir, 0o755); err != nil {
		if errors.Is(err, os.ErrExist) {
			return errConflict("%s already exists", req.Dir).withField("dir")
		}
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, discoveryConfigYAML), content, 0o644); err != nil {
		_ = os.RemoveAll(dir)
		return err
	}
	s.invalidateModules()
	entry, _ := describeModuleDir(root, dir)
	writeJSON(w, http.StatusCreated, entry)
	return nil
}

// minimalConfig is the loom.yaml of a new, empty module. The name goes
// through the YAML encoder, so no spelling of it can inject structure.
func minimalConfig(name string) ([]byte, error) {
	quoted, err := yaml.Marshal(name)
	if err != nil {
		return nil, err
	}
	return []byte("apiVersion: " + config.ExpectedAPIVersion + "\n" +
		"kind: " + config.ExpectedKind + "\n" +
		"metadata:\n" +
		"  name: " + strings.TrimSpace(string(quoted)) + "\n" +
		"spec:\n" +
		"  params: []\n" +
		"  operations: []\n"), nil
}

type yamlParseRequest struct {
	Text string `json:"text"`
}

// yamlErrLine finds the line yaml.v3 reports; it reports no column.
var yamlErrLine = regexp.MustCompile(`line (\d+)`)

// handleYAMLParse parses YAML for the UI, which has no parser of its own.
// A parse failure is the helper's answer, not a failed request: 200 with
// {"error": {line, col, message}}.
func (s *Server) handleYAMLParse(w http.ResponseWriter, r *http.Request) error {
	var req yamlParseRequest
	if err := decodeBody(r, &req); err != nil {
		return err
	}
	v, err := tmpl.DecodeYAML([]byte(req.Text))
	if err != nil {
		msg := strings.TrimPrefix(err.Error(), "yaml: ")
		line := 0
		if m := yamlErrLine.FindStringSubmatch(msg); m != nil {
			line, _ = strconv.Atoi(m[1])
		}
		writeJSON(w, http.StatusOK, map[string]any{"error": map[string]any{"line": line, "col": 0, "message": msg}})
		return nil
	}
	resp := map[string]any{"value": v}
	if isStructured(v) {
		resp["valueYaml"], _ = yamlText(v)
	}
	writeJSON(w, http.StatusOK, resp)
	return nil
}

type yamlFormatRequest struct {
	Value jsonValue `json:"value"`
}

func (s *Server) handleYAMLFormat(w http.ResponseWriter, r *http.Request) error {
	var req yamlFormatRequest
	if err := decodeBody(r, &req); err != nil {
		return err
	}
	text, err := yamlText(req.Value.v)
	if err != nil {
		return errInvalid("value", "cannot format value: %v", err)
	}
	writeJSON(w, http.StatusOK, map[string]string{"text": text})
	return nil
}

// jsonValue is any JSON value decoded with numbers kept as written.
type jsonValue struct{ v any }

func (j *jsonValue) UnmarshalJSON(data []byte) error {
	v, err := decodeJSONValue(data)
	if err != nil {
		return err
	}
	j.v = v
	return nil
}

func (j jsonValue) MarshalJSON() ([]byte, error) { return marshalJSON(j.v) }

// handleDocs lists the `loom skill` topics for the UI's help panel.
func (s *Server) handleDocs(w http.ResponseWriter, r *http.Request) error {
	lib, err := s.docLibrary()
	if err != nil {
		return err
	}
	topics := []map[string]string{}
	for _, t := range lib.Topics() {
		topics = append(topics, map[string]string{"name": t.Name, "title": t.Title})
	}
	writeJSON(w, http.StatusOK, map[string]any{"topics": topics})
	return nil
}

func (s *Server) handleDocTopic(w http.ResponseWriter, r *http.Request) error {
	lib, err := s.docLibrary()
	if err != nil {
		return err
	}
	// The topic arrives with its "/" as is or escaped as %2F, depending on
	// how the client built the URL; both name the same topic.
	topic, err := url.PathUnescape(r.PathValue("topic"))
	if err != nil {
		return errInvalid("topic", "malformed topic name")
	}
	text, err := lib.Read(topic)
	if err != nil {
		return errNotFound("%v", err)
	}
	writeMarkdown(w, text)
	return nil
}

func (s *Server) docLibrary() (*skill.Library, error) {
	if s.docsErr != nil {
		return nil, s.docsErr
	}
	if s.docs == nil {
		return nil, errNotFound("this build of loom does not include its documentation")
	}
	return s.docs, nil
}

func writeMarkdown(w http.ResponseWriter, text string) {
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(text))
}
