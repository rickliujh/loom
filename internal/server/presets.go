package server

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/rickliujh/loom/pkg/params"
)

// Presets are saved params files for one module, kept in the state
// directory under a hash of the module's source — never in the module,
// where a newFiles operation would render them into the target.
//
//	<state>/presets/<sha256(source)[:16]>/<name>.yaml

var presetNameRe = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

func validPresetName(name string) bool {
	return presetNameRe.MatchString(name) && strings.Trim(name, ".") != ""
}

// presetDir is the directory of a source's presets. A local source is keyed
// by its canonical directory, so every spelling of it shares presets.
func (s *Server) presetDir(src *moduleSource) string {
	key := src.URL
	if src.Local {
		key = src.Dir
	}
	sum := sha256.Sum256([]byte(key))
	return filepath.Join(s.presetsDir, hex.EncodeToString(sum[:8]))
}

func (s *Server) presetSource(r *http.Request) (*moduleSource, error) {
	return s.resolveSource(r.URL.Query().Get("source"), "source")
}

func (s *Server) presetPath(r *http.Request) (*moduleSource, string, string, error) {
	src, err := s.presetSource(r)
	if err != nil {
		return nil, "", "", err
	}
	name := r.PathValue("name")
	if !validPresetName(name) {
		return nil, "", "", errInvalid("name", "a preset name may hold only letters, digits, '.', '_' and '-'")
	}
	return src, name, filepath.Join(s.presetDir(src), name+".yaml"), nil
}

type presetEntry struct {
	Name      string    `json:"name"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func (s *Server) handlePresetList(w http.ResponseWriter, r *http.Request) error {
	src, err := s.presetSource(r)
	if err != nil {
		return err
	}
	out := []presetEntry{}
	des, err := os.ReadDir(s.presetDir(src))
	if err != nil && !errIsNotExist(err) {
		return err
	}
	for _, de := range des {
		name, ok := strings.CutSuffix(de.Name(), ".yaml")
		if !ok || de.IsDir() || !validPresetName(name) {
			continue
		}
		info, err := de.Info()
		if err != nil {
			continue
		}
		out = append(out, presetEntry{Name: name, UpdatedAt: info.ModTime().UTC()})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	writeJSON(w, http.StatusOK, map[string]any{"presets": out})
	return nil
}

func (s *Server) handlePresetGet(w http.ResponseWriter, r *http.Request) error {
	_, name, path, err := s.presetPath(r)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errIsNotExist(err) {
			return errNotFound("no preset %q", name)
		}
		return err
	}
	p, err := params.ParseYAML(data)
	if err != nil {
		return errUnprocessable(err)
	}
	if p == nil {
		p = map[string]any{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"name": name, "params": p, "paramsYaml": paramsYAML(p), "yaml": string(data),
	})
	return nil
}

type presetPutRequest struct {
	Params Params  `json:"params"`
	YAML   *string `json:"yaml"`
}

// handlePresetPut saves a preset from YAML text, kept exactly as written —
// comments, spelling and all — once it parses as a params file, or from
// params, formatted as one. The file is the preset: GET returns its text.
func (s *Server) handlePresetPut(w http.ResponseWriter, r *http.Request) error {
	_, name, path, err := s.presetPath(r)
	if err != nil {
		return err
	}
	var req presetPutRequest
	if err := decodeBody(r, &req); err != nil {
		return err
	}
	var data []byte
	switch {
	case req.YAML != nil && req.Params != nil:
		return errInvalid("", "send params or yaml, not both")
	case req.YAML != nil:
		if _, err := params.ParseYAML([]byte(*req.YAML)); err != nil {
			return errInvalid("yaml", "%v", err)
		}
		data = []byte(*req.YAML)
	default:
		values := map[string]any{}
		for k, v := range req.Params {
			values[k] = v
		}
		text, err := yamlText(values)
		if err != nil {
			return errInvalid("params", "%v", err)
		}
		data = []byte(text)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	if err := writePrivate(path, data); err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, presetEntry{Name: name, UpdatedAt: time.Now().UTC()})
	return nil
}

func (s *Server) handlePresetDelete(w http.ResponseWriter, r *http.Request) error {
	_, name, path, err := s.presetPath(r)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil {
		if errIsNotExist(err) {
			return errNotFound("no preset %q", name)
		}
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}
