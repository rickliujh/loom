package server

import (
	"encoding/json"
	"os"
)

// Run modes.
const (
	ModeExecute = "execute"
	ModeDryRun  = "dry-run"
	ModeLocal   = "local"
)

// JobRequest is the body of POST /jobs and POST /cli: one run, diff,
// generate or bulk scaffold. Which fields apply depends on Kind.
type JobRequest struct {
	Kind string `json:"kind"`

	// run and diff
	Source string `json:"source,omitempty"`
	Params Params `json:"params,omitempty"`
	// ParamsYAML is output only: the YAML form of each structured param, the
	// one that keeps every number's spelling.
	ParamsYAML map[string]string `json:"paramsYaml,omitempty"`
	Author     string            `json:"author,omitempty"`
	Email      string            `json:"email,omitempty"`

	// run
	Mode       string `json:"mode,omitempty"`
	TargetPath string `json:"targetPath,omitempty"`

	// diff
	Quick         bool `json:"quick,omitempty"`
	KeepWorkspace bool `json:"keepWorkspace,omitempty"`

	// generate
	Refs      []string          `json:"refs,omitempty"`
	Values    map[string]string `json:"values,omitempty"`
	TokenEnv  string            `json:"tokenEnv,omitempty"`
	Overwrite bool              `json:"overwrite,omitempty"`

	// generate and bulk
	Name   string `json:"name,omitempty"`
	Output string `json:"output,omitempty"`

	// bulk
	Module    string   `json:"module,omitempty"`
	NameParam string   `json:"nameParam,omitempty"`
	Items     []Params `json:"items,omitempty"`
}

// primarySource is what the job acts on, as the history list names it.
func (r JobRequest) primarySource() string {
	switch r.Kind {
	case KindGenerate:
		if len(r.Refs) > 0 {
			return r.Refs[0]
		}
		return ""
	case KindBulk:
		return r.Module
	}
	return r.Source
}

// summary is the request as the history list shows it.
func (r JobRequest) summary() map[string]string {
	out := map[string]string{"source": r.primarySource()}
	switch r.Kind {
	case KindRun:
		out["mode"] = r.Mode
	case KindDiff:
		out["mode"] = "full"
		if r.Quick {
			out["mode"] = "quick"
		}
	}
	return out
}

// preparedJob is a request that passed every check, with what the checks
// resolved.
type preparedJob struct {
	req    JobRequest
	src    *moduleSource
	module jobModule
	// targetPath is the canonical user-supplied target path, or "".
	targetPath string
	// output is the canonical generate/bulk output directory, or "".
	output string
	// needsWorkspace asks for a server-managed workspace.
	needsWorkspace bool
	keepWorkspace  bool
	// immediate starts the job without queueing: a quick diff.
	immediate bool
	locks     []string
}

// prepare checks a job request: kind, fields, and every path against the
// server's rules. It touches nothing on disk.
func (s *Server) prepare(req JobRequest) (*preparedJob, error) {
	req.ParamsYAML = nil
	p := &preparedJob{req: req}
	switch req.Kind {
	case KindRun:
		return p, s.prepareRun(p)
	case KindDiff:
		return p, s.prepareDiff(p)
	case KindGenerate:
		return p, s.prepareGenerate(p)
	case KindBulk:
		return p, s.prepareBulk(p)
	case "":
		return nil, errInvalid("kind", "kind is required: run, diff, generate or bulk")
	}
	return nil, errInvalid("kind", "unknown kind %q: expected run, diff, generate or bulk", req.Kind)
}

func (s *Server) prepareSource(p *preparedJob, raw, field string) error {
	src, err := s.resolveSource(raw, field)
	if err != nil {
		return err
	}
	p.src = src
	p.module.Dir = src.Dir
	if !src.Local {
		p.module.Dir = src.URL
	}
	return nil
}

func (s *Server) prepareRun(p *preparedJob) error {
	switch p.req.Mode {
	case ModeExecute, ModeDryRun, ModeLocal:
	case "":
		return errInvalid("mode", "mode is required: execute, dry-run or local")
	default:
		return errInvalid("mode", "unknown mode %q: expected execute, dry-run or local", p.req.Mode)
	}
	if err := s.prepareSource(p, p.req.Source, "source"); err != nil {
		return err
	}
	if p.req.TargetPath != "" {
		tp, err := s.resolveOutput(p.req.TargetPath, "targetPath")
		if err != nil {
			return err
		}
		p.targetPath = tp
		p.locks = append(p.locks, tp)
	} else if p.req.Mode == ModeLocal {
		// A local run keeps its clones; they are the result, so they stay
		// until the job is deleted.
		p.needsWorkspace = true
		p.keepWorkspace = true
	}
	return nil
}

func (s *Server) prepareDiff(p *preparedJob) error {
	if err := s.prepareSource(p, p.req.Source, "source"); err != nil {
		return err
	}
	if p.req.Quick {
		p.immediate = true
		return nil
	}
	p.needsWorkspace = true
	p.keepWorkspace = p.req.KeepWorkspace
	return nil
}

func (s *Server) prepareGenerate(p *preparedJob) error {
	switch len(p.req.Refs) {
	case 0:
		return errInvalid("refs", "refs is required: the PR or MR to generate from")
	case 1:
	default:
		return errInvalid("refs", "multi-source generate is not available in this version of loom: give exactly one ref")
	}
	if p.req.Refs[0] == "" {
		return errInvalid("refs", "refs[0] is empty")
	}
	out, err := s.resolveOutput(p.req.Output, "output")
	if err != nil {
		return err
	}
	// generate overwrites whatever it writes without asking, so an existing
	// directory with anything in it is only used when the caller says so.
	// Checked here for an answer before the job queues, and again when it
	// starts (checkOutputEmpty); the output is held from submission, so no
	// other job can write it in between.
	if !p.req.Overwrite {
		if err := checkOutputEmpty(out, p.req.Output); err != nil {
			return err
		}
	}
	p.output = out
	p.locks = append(p.locks, out)
	p.module = jobModule{Name: p.req.Name, Dir: out}
	return nil
}

func (s *Server) prepareBulk(p *preparedJob) error {
	if err := s.prepareSource(p, p.req.Module, "module"); err != nil {
		return err
	}
	out, err := s.resolveOutput(p.req.Output, "output")
	if err != nil {
		return err
	}
	p.output = out
	p.locks = append(p.locks, out)
	return nil
}

// withOverrides is req with the fields in overrides replaced, as rerun takes
// them. A field named in overrides replaces the original whole — params
// included — and the kind cannot change.
func withOverrides(req JobRequest, overrides json.RawMessage) (JobRequest, error) {
	if len(overrides) == 0 || string(overrides) == "null" {
		return req, nil
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(overrides, &fields); err != nil {
		return req, errInvalid("overrides", "overrides must be an object: %v", err)
	}
	if k, ok := fields["kind"]; ok {
		var kind string
		if err := json.Unmarshal(k, &kind); err != nil || kind != req.Kind {
			return req, errInvalid("overrides", "a rerun cannot change the job's kind")
		}
	}
	base, err := marshalJSON(req)
	if err != nil {
		return req, err
	}
	var merged map[string]json.RawMessage
	if err := json.Unmarshal(base, &merged); err != nil {
		return req, err
	}
	for k, v := range fields {
		merged[k] = v
	}
	data, err := marshalJSON(merged)
	if err != nil {
		return req, err
	}
	var out JobRequest
	if err := json.Unmarshal(data, &out); err != nil {
		return req, errInvalid("overrides", "%v", err)
	}
	// Values the overrides left alone are kept exactly, not as a JSON round
	// trip would give them back.
	if _, ok := fields["params"]; !ok {
		out.Params = req.Params
	}
	if _, ok := fields["items"]; !ok {
		out.Items = req.Items
	}
	return out, nil
}

// checkOutputEmpty refuses an output directory that has anything in it.
func checkOutputEmpty(dir, shown string) error {
	if des, err := os.ReadDir(dir); err == nil && len(des) > 0 {
		return errConflict("%s is not empty; set overwrite to write into it anyway", shown).withField("output")
	}
	return nil
}
