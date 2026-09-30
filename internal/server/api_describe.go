package server

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sort"

	"github.com/rickliujh/loom/pkg/config"
	"github.com/rickliujh/loom/pkg/engine"
	"github.com/rickliujh/loom/pkg/module"
	"github.com/rickliujh/loom/pkg/params"
	tmpl "github.com/rickliujh/loom/pkg/template"
)

// The describing endpoints — inspect, validate, params/check — execute
// nothing: no operation, no dynamic-param command, no `if` predicate. They
// read configs with config.Load and engine.Inspect/Validate, never
// module.Load, which is where dynamic-param commands run. Cloning a remote
// source to read it is their only side effect.

var discardLogger = slog.New(slog.NewTextHandler(io.Discard, nil))

// openModule is the local directory of a checked source: the directory
// itself, or a temporary clone of a remote one. cleanup is never nil.
func (s *Server) openModule(ctx context.Context, src *moduleSource) (string, func(), error) {
	if src.Local {
		return src.Dir, func() {}, nil
	}
	dir, cleanup, err := module.ResolveSourceContext(ctx, src.URL, "", discardLogger)
	if err != nil {
		return "", nil, errUnprocessable(err)
	}
	if cleanup == nil {
		cleanup = func() {}
	}
	return dir, cleanup, nil
}

// hasConfig reports whether dir holds a module config at all.
func hasConfig(dir string) bool {
	return isFile(filepath.Join(dir, discoveryConfigYAML)) || isFile(filepath.Join(dir, discoveryConfigJsonnet))
}

type inspectRequest struct {
	Source  string   `json:"source"`
	Params  Params   `json:"params"`
	Depth   *int     `json:"depth"`
	Modules []string `json:"modules"`
	NoFetch bool     `json:"noFetch"`
}

func (s *Server) handleInspect(w http.ResponseWriter, r *http.Request) error {
	var req inspectRequest
	if err := decodeBody(r, &req); err != nil {
		return err
	}
	depth := 1
	if req.Depth != nil {
		depth = *req.Depth
	}
	if depth < 0 {
		return errInvalid("depth", "depth must be 0 (every level) or more")
	}
	src, err := s.resolveSource(req.Source, "source")
	if err != nil {
		return err
	}
	dir, cleanup, err := s.openModule(r.Context(), src)
	if err != nil {
		return err
	}
	defer cleanup()

	res, err := engine.Inspect(r.Context(), engine.InspectRequest{
		ModuleDir: dir,
		Params:    req.Params,
		Depth:     depth,
		Modules:   req.Modules,
		NoFetch:   req.NoFetch,
		Logger:    discardLogger,
	})
	if err != nil {
		return errUnprocessable(err)
	}
	cli, _ := inspectCommand(req, depth)
	out := newInspectOut(res.Report)
	out.CLI = cli.Command
	out.ParamsFile = cli.ParamsFile
	writeJSON(w, http.StatusOK, out)
	return nil
}

// inspectOut is the `loom inspect -o json` report with each param's YAML
// form added and the command line that reproduces it.
type inspectOut struct {
	Modules       []subjectOut          `json:"modules"`
	MissingParams []module.MissingParam `json:"missingParams"`
	Problems      []string              `json:"problems"`
	Unexpanded    [][]string            `json:"unexpanded"`
	CLI           string                `json:"cli"`
	ParamsFile    string                `json:"paramsFile,omitempty"`
}

type subjectOut struct {
	Path   []string       `json:"path"`
	Module *inspectionOut `json:"module"`
}

// inspectionOut is a module.Inspection whose params and children are
// replaced by their API form. The outer fields shadow the embedded ones of
// the same JSON name, so everything else marshals exactly as the CLI's.
type inspectionOut struct {
	*module.Inspection
	Params   []paramOut       `json:"params,omitempty"`
	Children []*inspectionOut `json:"modules,omitempty"`
}

// paramOut is module.Param plus the YAML form of its structured value and
// default. It restates the fields rather than embedding Param, whose
// MarshalJSON would be promoted and drop the additions.
type paramOut struct {
	Name        string            `json:"name"`
	Type        config.ParamType  `json:"type"`
	State       module.ParamState `json:"state"`
	Required    bool              `json:"required,omitempty"`
	Value       any               `json:"value,omitempty"`
	ValueYAML   string            `json:"valueYaml,omitempty"`
	Default     any               `json:"default,omitempty"`
	DefaultYAML string            `json:"defaultYaml,omitempty"`
	Command     string            `json:"command,omitempty"`
	From        any               `json:"from,omitempty"`
}

func newInspectOut(r engine.InspectReport) *inspectOut {
	out := &inspectOut{
		Modules:       []subjectOut{},
		MissingParams: r.MissingParams,
		Problems:      r.Problems,
		Unexpanded:    r.Unexpanded,
	}
	for _, sub := range r.Modules {
		out.Modules = append(out.Modules, subjectOut{Path: sub.Path, Module: newInspectionOut(sub.Module)})
	}
	return out
}

func newInspectionOut(in *module.Inspection) *inspectionOut {
	if in == nil {
		return nil
	}
	out := &inspectionOut{Inspection: in}
	for _, p := range in.Params {
		po := paramOut{
			Name: p.Name, Type: p.Type, State: p.State, Required: p.Required,
			Value: emptyToNil(p.Value), Default: emptyToNil(p.Default),
			Command: p.Command, From: emptyToNil(p.From),
		}
		if isStructured(p.Value) {
			po.ValueYAML, _ = yamlText(p.Value)
		}
		if isStructured(p.Default) {
			po.DefaultYAML, _ = yamlText(p.Default)
		}
		out.Params = append(out.Params, po)
	}
	for _, c := range in.Children {
		out.Children = append(out.Children, newInspectionOut(c))
	}
	return out
}

// emptyToNil leaves out an empty string, as module.Param's JSON does.
func emptyToNil(v any) any {
	if s, ok := v.(string); ok && s == "" {
		return nil
	}
	return v
}

type validateRequest struct {
	Source    string `json:"source"`
	Recursive bool   `json:"recursive"`
}

type validateOut struct {
	Valid    bool             `json:"valid"`
	Count    int              `json:"count"`
	Warnings []engine.Finding `json:"warnings"`
	Errors   []engine.Finding `json:"errors"`
}

func (s *Server) handleValidate(w http.ResponseWriter, r *http.Request) error {
	var req validateRequest
	if err := decodeBody(r, &req); err != nil {
		return err
	}
	src, err := s.resolveSource(req.Source, "source")
	if err != nil {
		return err
	}
	dir, cleanup, err := s.openModule(r.Context(), src)
	if err != nil {
		return err
	}
	defer cleanup()
	// A directory with no config at all is not a module that failed
	// validation: there is nothing to validate.
	if !hasConfig(dir) {
		return errUnprocessable(errors.New("no loom.yaml or loom.jsonnet in " + req.Source))
	}

	res, _ := engine.Validate(r.Context(), engine.ValidateRequest{Dir: dir, Recursive: req.Recursive, Logger: discardLogger})
	out := validateOut{Valid: res.Valid(), Count: res.Count, Warnings: res.Warnings, Errors: res.Errors}
	if out.Warnings == nil {
		out.Warnings = []engine.Finding{}
	}
	if out.Errors == nil {
		out.Errors = []engine.Finding{}
	}
	for i := range out.Errors {
		out.Errors[i].Message = redact(out.Errors[i].Message)
	}
	writeJSON(w, http.StatusOK, out)
	return nil
}

type paramsCheckRequest struct {
	Source string `json:"source"`
	Params Params `json:"params"`
}

type fieldCheck struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

type paramsCheckOut struct {
	Fields     map[string]fieldCheck `json:"fields"`
	Undeclared []string              `json:"undeclared"`
	Missing    []string              `json:"missing"`
}

// handleParamsCheck checks values against the declarations alone. It reads
// the config with config.Load and never loads the module: loading runs the
// dynamic-param commands, and this is called as someone types.
func (s *Server) handleParamsCheck(w http.ResponseWriter, r *http.Request) error {
	var req paramsCheckRequest
	if err := decodeBody(r, &req); err != nil {
		return err
	}
	src, err := s.resolveSource(req.Source, "source")
	if err != nil {
		return err
	}
	dir, cleanup, err := s.openModule(r.Context(), src)
	if err != nil {
		return err
	}
	defer cleanup()
	lf, err := config.Load(dir)
	if err != nil {
		return errUnprocessable(err)
	}
	writeJSON(w, http.StatusOK, checkParams(lf, req.Params))
	return nil
}

func checkParams(lf *config.LoomFile, supplied Params) paramsCheckOut {
	out := paramsCheckOut{Fields: map[string]fieldCheck{}, Undeclared: []string{}, Missing: []string{}}
	types := map[string]config.ParamType{}
	for _, p := range lf.Spec.Params {
		types[p.Name] = p.Type
	}
	// A dynamic param given a value takes it instead of running its command.
	for _, dp := range lf.Spec.DynamicParams {
		types[dp.Name] = dp.Type
	}
	coerced := map[string]any{}
	for name, v := range supplied {
		t, ok := types[name]
		if !ok {
			out.Undeclared = append(out.Undeclared, name)
			continue
		}
		cv, err := params.Coerce(name, t, v)
		if err != nil {
			out.Fields[name] = fieldCheck{Error: err.Error()}
			continue
		}
		coerced[name] = cv
		out.Fields[name] = fieldCheck{OK: true}
	}
	for _, p := range lf.Spec.Params {
		if !p.Required || p.HasDefault() {
			continue
		}
		if v, ok := coerced[p.Name]; ok && !tmpl.IsEmpty(v) {
			continue
		}
		if _, failed := out.Fields[p.Name]; failed {
			continue
		}
		out.Missing = append(out.Missing, p.Name)
	}
	sort.Strings(out.Undeclared)
	sort.Strings(out.Missing)
	return out
}

// errIsNotExist reports a missing file behind err.
func errIsNotExist(err error) bool {
	return errors.Is(err, fs.ErrNotExist) || os.IsNotExist(err)
}
