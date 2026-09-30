package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/rickliujh/loom/pkg/event"
	tmpl "github.com/rickliujh/loom/pkg/template"
)

// historyStore keeps jobs on disk, one directory each:
//
//	<state>/jobs/<id>/job.json      the record, rewritten on every change
//	<state>/jobs/<id>/events.jsonl  the event stream, appended as it happens
//	<state>/jobs/<id>/log.txt       the plain-text log, appended likewise
//	<state>/jobs/<id>/diff.json     the diff, for diff jobs and local runs
//
// Directories are 0700 and files 0600: logs can carry whatever a module's
// commands printed.
type historyStore struct {
	dir string
}

// jobIDRe matches the ids the server issues; nothing else is ever joined
// onto a history path.
var jobIDRe = regexp.MustCompile(`^j_[0-9]{8}T[0-9]{6}_[0-9a-f]{4}$`)

// jobFile is job.json.
type jobFile struct {
	jobRecord
	// Request is the full request with ParamsYAML, which reloads params
	// with every number spelled as it was sent.
	Request       JobRequest `json:"request"`
	Workspace     string     `json:"workspace,omitempty"`
	KeepWorkspace bool       `json:"keepWorkspace,omitempty"`
}

func (h *historyStore) jobDir(id string) string { return filepath.Join(h.dir, id) }

// create makes the job's directory and points its logs at it.
func (h *historyStore) create(j *job) error {
	dir := h.jobDir(j.ID)
	if err := mkdirPrivate(dir); err != nil {
		return err
	}
	if err := j.events.persistTo(filepath.Join(dir, "events.jsonl")); err != nil {
		return err
	}
	return j.text.persistTo(filepath.Join(dir, "log.txt"))
}

// save rewrites job.json. The caller holds the manager's lock.
func (h *historyStore) save(j *job) error {
	f := jobFile{jobRecord: j.record(false), Request: j.Request, Workspace: j.Workspace, KeepWorkspace: j.KeepWorkspace}
	f.Request.ParamsYAML = paramsYAML(f.Request.Params)
	data, err := marshalJSON(f)
	if err != nil {
		return err
	}
	return writePrivate(filepath.Join(h.jobDir(j.ID), "job.json"), data)
}

func (h *historyStore) saveDiff(id string, d *diffOut) error {
	data, err := marshalJSON(d)
	if err != nil {
		return err
	}
	return writePrivate(filepath.Join(h.jobDir(id), "diff.json"), data)
}

func (h *historyStore) remove(id string) {
	if jobIDRe.MatchString(id) {
		_ = os.RemoveAll(h.jobDir(id))
	}
}

// writePrivate replaces p atomically with a 0600 file.
func writePrivate(p string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(p), ".tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), p)
}

// loadAll reads every job in the store. A job that was not terminal when
// the server stopped is recorded as interrupted — its stream is closed with
// that state — since nothing will ever finish it now.
func (h *historyStore) loadAll() ([]*job, error) {
	if err := mkdirPrivate(h.dir); err != nil {
		return nil, err
	}
	des, err := os.ReadDir(h.dir)
	if err != nil {
		return nil, err
	}
	var out []*job
	for _, de := range des {
		if !de.IsDir() || !jobIDRe.MatchString(de.Name()) {
			continue
		}
		j, err := h.load(de.Name())
		if err != nil {
			continue
		}
		out = append(out, j)
	}
	return out, nil
}

func (h *historyStore) load(id string) (*job, error) {
	dir := h.jobDir(id)
	data, err := os.ReadFile(filepath.Join(dir, "job.json"))
	if err != nil {
		return nil, err
	}
	var f jobFile
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, err
	}
	if f.ID != id {
		return nil, os.ErrInvalid
	}
	req := f.Request
	for name, text := range req.ParamsYAML {
		if v, err := tmpl.DecodeYAML([]byte(text)); err == nil && req.Params != nil {
			req.Params[name] = v
		}
	}
	req.ParamsYAML = nil

	j := &job{
		ID: f.ID, Kind: f.Kind, State: f.State, Request: req,
		CreatedAt: f.CreatedAt, Module: f.Module, Result: f.Result, Error: f.Error,
		CLI:           CLI{Command: f.CLI, ParamsFile: f.ParamsFile, ItemsFile: f.ItemsFile},
		Workspace:     f.Workspace,
		KeepWorkspace: f.KeepWorkspace,
		events:        newEventLog(),
		text:          &textLog{path: filepath.Join(dir, "log.txt"), dropped: true},
	}
	if f.StartedAt != nil {
		j.StartedAt = *f.StartedAt
	}
	if f.EndedAt != nil {
		j.EndedAt = *f.EndedAt
	}
	eventsPath := filepath.Join(dir, "events.jsonl")
	if raw, err := os.ReadFile(filepath.Join(dir, "diff.json")); err == nil {
		var d diffOut
		if json.Unmarshal(raw, &d) == nil {
			j.diff = &d
		}
	}

	if !isTerminal(j.State) {
		_, last, _ := readEvents(eventsPath, 0)
		j.events.seq = last
		j.State = StateInterrupted
		j.Error = "the server stopped while the job was " + f.State
		j.EndedAt = time.Now()
		if err := j.events.persistTo(eventsPath); err == nil {
			j.events.finish(jobEvent{Event: event.Event{Type: eventJobState, Error: j.Error}, State: j.State})
		}
		if err := h.save(j); err != nil {
			return nil, err
		}
	}
	j.events.path = eventsPath
	j.events.closed = true
	j.events.spilled = true
	j.events.events = nil
	return j, nil
}
