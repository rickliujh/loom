package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/rickliujh/loom/pkg/action"
	"github.com/rickliujh/loom/pkg/engine"
	"github.com/rickliujh/loom/pkg/event"
)

// Job kinds.
const (
	KindRun      = "run"
	KindDiff     = "diff"
	KindGenerate = "generate"
	KindBulk     = "bulk"
)

// Job states. A job moves queued → running → (cancelling) → succeeded |
// failed | cancelled; one the server stopped before it ended is interrupted.
// The last four are terminal, and terminal states are final.
const (
	StateQueued      = "queued"
	StateRunning     = "running"
	StateCancelling  = "cancelling"
	StateSucceeded   = "succeeded"
	StateFailed      = "failed"
	StateCancelled   = "cancelled"
	StateInterrupted = "interrupted"
)

func isTerminal(state string) bool {
	switch state {
	case StateSucceeded, StateFailed, StateCancelled, StateInterrupted:
		return true
	}
	return false
}

// Causes a job's context is cancelled with.
var (
	errCancelledByUser = errors.New("cancelled")
	errServerStopping  = errors.New("the server stopped before the job finished")
)

// History retention.
const (
	historyMaxJobs = 200
	historyMaxAge  = 30 * 24 * time.Hour
)

// job is one execution. Its fields are guarded by jobManager.mu; the event
// and text logs have locks of their own, so a running job logs without
// contending with requests that read its record.
type job struct {
	ID        string
	Kind      string
	State     string
	Request   JobRequest
	CreatedAt time.Time
	StartedAt time.Time
	EndedAt   time.Time
	Module    jobModule
	Result    jobResult
	Error     string
	CLI       CLI

	// Workspace is the server-managed directory the job ran in, removed
	// with the job; empty when it used none.
	Workspace string
	// KeepWorkspace keeps Workspace once the job ends; otherwise it is
	// removed then.
	KeepWorkspace bool

	events *eventLog
	text   *textLog
	diff   *diffOut

	// Runtime state, meaningless once terminal.
	prepared *preparedJob
	ctx      context.Context
	cancel   context.CancelCauseFunc
	locks    []string
	queued   bool // counts against the concurrency limit
}

type jobModule struct {
	Name string `json:"name"`
	Dir  string `json:"dir"`
}

type jobResult struct {
	PRs       []prOut      `json:"prs"`
	Workspace string       `json:"workspace,omitempty"`
	Output    string       `json:"output,omitempty"`
	Diff      *diffSummary `json:"diff"`
}

type prOut struct {
	Path   []string `json:"path"`
	Module string   `json:"module"`
	Title  string   `json:"title"`
	URL    string   `json:"url"`
}

type diffSummary struct {
	Incomplete bool   `json:"incomplete"`
	Targets    int    `json:"targets"`
	Files      int    `json:"files"`
	Note       string `json:"note,omitempty"`
}

// diffOut is GET /jobs/{id}/diff.
type diffOut struct {
	Mode       string              `json:"mode"`
	Incomplete bool                `json:"incomplete"`
	Targets    []engine.TargetDiff `json:"targets"`
	// Note says why a diff is empty when that is not "nothing changed".
	Note string `json:"note,omitempty"`
}

func (d *diffOut) summary() *diffSummary {
	s := &diffSummary{Incomplete: d.Incomplete, Targets: len(d.Targets), Note: d.Note}
	for _, t := range d.Targets {
		s.Files += len(t.Files)
	}
	return s
}

// jobRecord is a job as the API returns it.
type jobRecord struct {
	ID         string     `json:"id"`
	Kind       string     `json:"kind"`
	State      string     `json:"state"`
	Request    any        `json:"request"`
	CreatedAt  time.Time  `json:"createdAt"`
	StartedAt  *time.Time `json:"startedAt"`
	EndedAt    *time.Time `json:"endedAt"`
	Module     jobModule  `json:"module"`
	Result     jobResult  `json:"result"`
	Error      string     `json:"error"`
	CLI        string     `json:"cli"`
	ParamsFile string     `json:"paramsFile,omitempty"`
	ItemsFile  string     `json:"itemsFile,omitempty"`
}

// record is the job's API form; full includes the whole request, otherwise
// only its source and mode, as the history list shows.
func (j *job) record(full bool) jobRecord {
	rec := jobRecord{
		ID: j.ID, Kind: j.Kind, State: j.State, CreatedAt: j.CreatedAt,
		StartedAt: timePtr(j.StartedAt), EndedAt: timePtr(j.EndedAt),
		Module: j.Module, Result: j.Result, Error: j.Error,
		CLI: j.CLI.Command, ParamsFile: j.CLI.ParamsFile, ItemsFile: j.CLI.ItemsFile,
	}
	if rec.Result.PRs == nil {
		rec.Result.PRs = []prOut{}
	}
	if full {
		req := j.Request
		req.ParamsYAML = paramsYAML(req.Params)
		rec.Request = req
	} else {
		rec.Request = j.Request.summary()
	}
	return rec
}

func timePtr(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

// jobManager owns every job: it schedules executing jobs FIFO, at most
// maxConcurrentJobs at a time, holds the path locks, and keeps history.
type jobManager struct {
	s       *Server
	history *historyStore

	mu      sync.Mutex
	jobs    map[string]*job
	queue   []*job
	running int
	// locks maps a canonical path to the job holding it.
	locks   map[string]string
	stopped bool
	active  sync.WaitGroup
}

func newJobManager(s *Server) (*jobManager, error) {
	m := &jobManager{s: s, jobs: map[string]*job{}, locks: map[string]string{}}
	if s.cfg.History == HistoryDisk {
		m.history = &historyStore{dir: filepath.Join(s.stateDir, "jobs")}
		jobs, err := m.history.loadAll()
		if err != nil {
			return nil, err
		}
		for _, j := range jobs {
			m.jobs[j.ID] = j
		}
		m.prune()
	}
	return m, nil
}

// newJobID is j_<UTC time>_<4 hex>: sortable by creation, unique in
// practice, and re-drawn on the rare collision.
func (m *jobManager) newJobID(now time.Time) string {
	for {
		b := make([]byte, 2)
		_, _ = rand.Read(b)
		id := "j_" + now.UTC().Format("20060102T150405") + "_" + hex.EncodeToString(b)
		if _, taken := m.jobs[id]; !taken {
			return id
		}
	}
}

// submit creates a job from a prepared request and schedules it.
func (m *jobManager) submit(p *preparedJob) (*job, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.stopped {
		return nil, errConflict("the server is shutting down")
	}
	now := time.Now()
	j := &job{
		ID:        m.newJobID(now),
		Kind:      p.req.Kind,
		State:     StateQueued,
		Request:   p.req,
		CreatedAt: now,
		Module:    p.module,
		events:    newEventLog(),
		text:      &textLog{},
		prepared:  p,
	}
	if p.needsWorkspace {
		j.Workspace = filepath.Join(m.s.workspaces, j.ID)
		j.KeepWorkspace = p.keepWorkspace
		if j.KeepWorkspace {
			j.Result.Workspace = j.Workspace
		}
	}
	if p.output != "" {
		j.Result.Output = p.output
	}
	j.CLI, _ = cliFor(p.req, j.Workspace)

	for _, l := range p.locks {
		if holder := m.lockHolder(l); holder != "" {
			return nil, errConflict("%s is in use by job %s", l, holder).with("job", holder)
		}
	}
	for _, l := range p.locks {
		m.locks[l] = j.ID
	}
	j.locks = p.locks

	if m.history != nil {
		if err := m.history.create(j); err != nil {
			m.releaseLocked(j)
			return nil, err
		}
	}
	m.jobs[j.ID] = j
	j.ctx, j.cancel = context.WithCancelCause(context.Background())
	m.emitState(j)
	m.save(j)

	m.active.Add(1)
	if p.immediate {
		// A quick diff executes nothing that needs the queue's protection;
		// it starts at once and never waits behind executing jobs.
		m.startLocked(j)
	} else {
		j.queued = true
		m.queue = append(m.queue, j)
		m.dispatchLocked()
	}
	return j, nil
}

// lockHolder is the job holding a path that overlaps p, if any: the same
// path, or one inside the other.
func (m *jobManager) lockHolder(p string) string {
	for l, id := range m.locks {
		if within(l, p) || within(p, l) {
			return id
		}
	}
	return ""
}

func (m *jobManager) releaseLocked(j *job) {
	for _, l := range j.locks {
		if m.locks[l] == j.ID {
			delete(m.locks, l)
		}
	}
	j.locks = nil
}

// dispatchLocked starts queued jobs while there is room.
func (m *jobManager) dispatchLocked() {
	for !m.stopped && len(m.queue) > 0 && m.running < m.s.cfg.MaxConcurrentJobs {
		j := m.queue[0]
		m.queue = m.queue[1:]
		m.running++
		m.startLocked(j)
	}
}

// startLocked moves j to running and runs it in the background.
func (m *jobManager) startLocked(j *job) {
	j.State = StateRunning
	j.StartedAt = time.Now()
	m.emitState(j)
	m.save(j)
	go m.run(j)
}

// run executes j and records how it ended.
func (m *jobManager) run(j *job) {
	defer m.active.Done()
	err := m.execute(j)

	m.mu.Lock()
	defer m.mu.Unlock()
	if j.queued {
		m.running--
		j.queued = false
	}
	if !isTerminal(j.State) {
		cause := context.Cause(j.ctx)
		switch {
		case err == nil:
			m.finishLocked(j, StateSucceeded, "")
		case errors.Is(cause, errServerStopping):
			m.finishLocked(j, StateInterrupted, errServerStopping.Error())
		case errors.Is(cause, errCancelledByUser):
			m.finishLocked(j, StateCancelled, "")
		default:
			m.finishLocked(j, StateFailed, redact(err.Error()))
		}
	}
	m.dispatchLocked()
	m.prune()
}

// finishLocked moves j to a terminal state: the final event, history, locks
// and workspace are all settled here, once.
func (m *jobManager) finishLocked(j *job, state, errText string) {
	if isTerminal(j.State) {
		return
	}
	j.State = state
	j.Error = errText
	j.EndedAt = time.Now()
	if j.cancel != nil {
		j.cancel(context.Canceled)
	}
	m.releaseLocked(j)
	e := jobEvent{Event: event.Event{Type: eventJobState, Error: errText}, State: state}
	j.events.finish(e)
	j.text.close()
	if j.Workspace != "" && !j.KeepWorkspace {
		_ = os.RemoveAll(j.Workspace)
	}
	m.save(j)
	if m.history != nil {
		j.events.spill()
	}
	j.prepared = nil
}

func (m *jobManager) emitState(j *job) {
	j.events.append(jobEvent{Event: event.Event{Type: eventJobState}, State: j.State})
}

// save writes the job's record to history; a failure is logged, not fatal.
func (m *jobManager) save(j *job) {
	if m.history == nil {
		return
	}
	if err := m.history.save(j); err != nil {
		m.s.logger.Warn("saving job history", "job", j.ID, "error", err)
	}
}

// cancel stops a queued or running job.
func (m *jobManager) cancelJob(id string) (*job, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	j, ok := m.jobs[id]
	if !ok {
		return nil, errNotFound("no job %s", id)
	}
	switch j.State {
	case StateQueued:
		m.removeQueuedLocked(j)
		m.finishLocked(j, StateCancelled, "")
		m.active.Done()
	case StateRunning:
		j.State = StateCancelling
		m.emitState(j)
		m.save(j)
		j.cancel(errCancelledByUser)
	case StateCancelling:
	default:
		return nil, errConflict("job %s is already %s", id, j.State).with("state", j.State)
	}
	return j, nil
}

func (m *jobManager) removeQueuedLocked(j *job) {
	for i, q := range m.queue {
		if q == j {
			m.queue = append(m.queue[:i], m.queue[i+1:]...)
			break
		}
	}
	j.queued = false
}

// deleteJob removes a finished job from history, with its workspace.
func (m *jobManager) deleteJob(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	j, ok := m.jobs[id]
	if !ok {
		return errNotFound("no job %s", id)
	}
	if !isTerminal(j.State) {
		return errConflict("job %s is %s; cancel it first", id, j.State).with("state", j.State)
	}
	m.removeLocked(j)
	return nil
}

func (m *jobManager) removeLocked(j *job) {
	delete(m.jobs, j.ID)
	if j.Workspace != "" && within(m.s.workspaces, j.Workspace) {
		_ = os.RemoveAll(j.Workspace)
	}
	if m.history != nil {
		m.history.remove(j.ID)
	}
}

// prune drops the oldest finished jobs past the retention limits. Called
// with mu held, or before the manager is shared.
func (m *jobManager) prune() {
	var done []*job
	cutoff := time.Now().Add(-historyMaxAge)
	for _, j := range m.jobs {
		if !isTerminal(j.State) {
			continue
		}
		if j.CreatedAt.Before(cutoff) {
			m.removeLocked(j)
			continue
		}
		done = append(done, j)
	}
	if excess := len(m.jobs) - historyMaxJobs; excess > 0 {
		sort.Slice(done, func(a, b int) bool { return done[a].CreatedAt.Before(done[b].CreatedAt) })
		for i := 0; i < excess && i < len(done); i++ {
			m.removeLocked(done[i])
		}
	}
}

func (m *jobManager) get(id string) (*job, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	j, ok := m.jobs[id]
	return j, ok
}

// snapshot returns j's record under the lock.
func (m *jobManager) snapshot(j *job, full bool) jobRecord {
	m.mu.Lock()
	defer m.mu.Unlock()
	return j.record(full)
}

// jobFilter narrows the history list.
type jobFilter struct {
	module, kind, state string
	limit               int
}

func (m *jobManager) list(f jobFilter) []jobRecord {
	m.mu.Lock()
	defer m.mu.Unlock()
	var js []*job
	for _, j := range m.jobs {
		if f.kind != "" && j.Kind != f.kind {
			continue
		}
		if f.state != "" && j.State != f.state {
			continue
		}
		if f.module != "" && f.module != j.Request.primarySource() && f.module != j.Module.Dir {
			continue
		}
		js = append(js, j)
	}
	sort.Slice(js, func(a, b int) bool {
		if !js[a].CreatedAt.Equal(js[b].CreatedAt) {
			return js[a].CreatedAt.After(js[b].CreatedAt)
		}
		return js[a].ID > js[b].ID
	})
	if f.limit > 0 && len(js) > f.limit {
		js = js[:f.limit]
	}
	out := make([]jobRecord, 0, len(js))
	for _, j := range js {
		out = append(out, j.record(false))
	}
	return out
}

// shutdown stops the job system: queued jobs are interrupted, running ones
// cancelled and awaited for up to grace, and any still going recorded as
// interrupted.
func (m *jobManager) shutdown(grace time.Duration) {
	m.mu.Lock()
	m.stopped = true
	for _, j := range append([]*job(nil), m.queue...) {
		m.removeQueuedLocked(j)
		m.finishLocked(j, StateInterrupted, errServerStopping.Error())
		m.active.Done()
	}
	for _, j := range m.jobs {
		if !isTerminal(j.State) && j.cancel != nil {
			j.cancel(errServerStopping)
		}
	}
	m.mu.Unlock()

	waited := make(chan struct{})
	go func() { m.active.Wait(); close(waited) }()
	select {
	case <-waited:
	case <-time.After(grace):
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	for _, j := range m.jobs {
		if !isTerminal(j.State) {
			m.finishLocked(j, StateInterrupted, errServerStopping.Error())
		}
	}
}

// update changes a running job's record under the lock.
func (m *jobManager) update(j *job, fn func(j *job)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	fn(j)
}

func prsOut(prs []action.PRResult) []prOut {
	out := make([]prOut, 0, len(prs))
	for _, p := range prs {
		path := p.Path
		if path == nil {
			path = []string{}
		}
		out = append(out, prOut{Path: path, Module: p.Module, Title: p.Title, URL: p.URL})
	}
	return out
}
