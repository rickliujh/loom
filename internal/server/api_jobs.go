package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func (s *Server) handleJobCreate(w http.ResponseWriter, r *http.Request) error {
	var req JobRequest
	if err := decodeBody(r, &req); err != nil {
		return err
	}
	return s.submitJob(w, req)
}

func (s *Server) submitJob(w http.ResponseWriter, req JobRequest) error {
	p, err := s.prepare(req)
	if err != nil {
		return err
	}
	j, err := s.jobs.submit(p)
	if err != nil {
		return err
	}
	s.logger.Info("job queued", "job", j.ID, "kind", j.Kind)
	writeJSON(w, http.StatusAccepted, map[string]string{"id": j.ID, "state": StateQueued})
	return nil
}

func (s *Server) handleJobList(w http.ResponseWriter, r *http.Request) error {
	q := r.URL.Query()
	f := jobFilter{module: q.Get("module"), kind: q.Get("kind"), state: q.Get("state"), limit: 50}
	if l := q.Get("limit"); l != "" {
		n, err := strconv.Atoi(l)
		if err != nil || n < 1 {
			return errInvalid("limit", "limit must be a positive number")
		}
		f.limit = min(n, historyMaxJobs)
	}
	writeJSON(w, http.StatusOK, map[string]any{"jobs": s.jobs.list(f)})
	return nil
}

func (s *Server) lookupJob(r *http.Request) (*job, error) {
	id := r.PathValue("id")
	j, ok := s.jobs.get(id)
	if !ok {
		return nil, errNotFound("no job %s", id)
	}
	return j, nil
}

func (s *Server) handleJobGet(w http.ResponseWriter, r *http.Request) error {
	j, err := s.lookupJob(r)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, s.jobs.snapshot(j, true))
	return nil
}

func (s *Server) handleJobDiff(w http.ResponseWriter, r *http.Request) error {
	j, err := s.lookupJob(r)
	if err != nil {
		return err
	}
	s.jobs.mu.Lock()
	d, state, kind, mode := j.diff, j.State, j.Kind, j.Request.Mode
	s.jobs.mu.Unlock()
	if kind != KindDiff && !(kind == KindRun && mode == ModeLocal) {
		return errNotFound("job %s has no diff: only diff jobs and local runs do", j.ID)
	}
	if !isTerminal(state) {
		return errConflict("job %s is %s; its diff is ready when it ends", j.ID, state).with("state", state)
	}
	if d == nil {
		// Ended before any diff was read — failed while loading, say.
		d = &diffOut{Mode: "full", Incomplete: state != StateSucceeded, Targets: nonNilTargets(nil)}
		if kind == KindDiff && j.Request.Quick {
			d.Mode = "quick"
		}
	}
	writeJSON(w, http.StatusOK, d)
	return nil
}

func (s *Server) handleJobLog(w http.ResponseWriter, r *http.Request) error {
	j, err := s.lookupJob(r)
	if err != nil {
		return err
	}
	text, err := j.text.text()
	if err != nil {
		return err
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(text)
	return nil
}

func (s *Server) handleJobCancel(w http.ResponseWriter, r *http.Request) error {
	j, err := s.jobs.cancelJob(r.PathValue("id"))
	if err != nil {
		return err
	}
	rec := s.jobs.snapshot(j, false)
	writeJSON(w, http.StatusAccepted, map[string]string{"id": rec.ID, "state": rec.State})
	return nil
}

func (s *Server) handleJobRerun(w http.ResponseWriter, r *http.Request) error {
	j, err := s.lookupJob(r)
	if err != nil {
		return err
	}
	var body struct {
		Overrides json.RawMessage `json:"overrides"`
	}
	if err := decodeBody(r, &body); err != nil {
		return err
	}
	s.jobs.mu.Lock()
	orig := j.Request
	s.jobs.mu.Unlock()
	req, err := withOverrides(orig, body.Overrides)
	if err != nil {
		return err
	}
	return s.submitJob(w, req)
}

func (s *Server) handleJobDelete(w http.ResponseWriter, r *http.Request) error {
	if err := s.jobs.deleteJob(r.PathValue("id")); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// pingInterval is how often an idle event stream gets a comment line, so
// proxies and browsers do not time it out.
const pingInterval = 15 * time.Second

// handleJobEvents streams a job's events as Server-Sent Events: everything
// after Last-Event-ID (or ?after=), then new events as they happen, then an
// end event once the job is over, and the stream closes.
func (s *Server) handleJobEvents(w http.ResponseWriter, r *http.Request) error {
	j, err := s.lookupJob(r)
	if err != nil {
		return err
	}
	var after int64
	cursor := r.Header.Get("Last-Event-ID")
	if cursor == "" {
		cursor = r.URL.Query().Get("after")
	}
	if cursor != "" {
		n, err := strconv.ParseInt(strings.TrimSpace(cursor), 10, 64)
		if err != nil || n < 0 {
			return errInvalid("after", "the resume point must be an event sequence number")
		}
		after = n
	}

	rc := http.NewResponseController(w)
	// The stream outlives the server's write timeout by design.
	_ = rc.SetWriteDeadline(time.Time{})
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	if err := rc.Flush(); err != nil {
		return nil
	}

	interval := s.pingEvery
	if interval <= 0 {
		interval = pingInterval
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	stopping := false
	for {
		evs, closed, notify, err := j.events.since(after)
		if err != nil {
			return nil
		}
		for _, e := range evs {
			data, err := marshalJSON(e)
			if err != nil {
				continue
			}
			if _, err := fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", e.Seq, e.Type, data); err != nil {
				return nil
			}
			after = e.Seq
		}
		if closed {
			// EventSource dispatches no event without a data line. The id
			// is one past the last stored event; end itself is not stored,
			// so resuming from it replays nothing and ends again.
			_, _ = fmt.Fprintf(w, "id: %d\nevent: %s\ndata: {}\n\n", j.events.lastSeq()+1, eventEnd)
			_ = rc.Flush()
			return nil
		}
		if len(evs) > 0 {
			if err := rc.Flush(); err != nil {
				return nil
			}
		}
		if stopping {
			return nil
		}
		select {
		case <-notify:
		case <-ticker.C:
			if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil {
				return nil
			}
			if err := rc.Flush(); err != nil {
				return nil
			}
		case <-r.Context().Done():
			return nil
		case <-s.stopping:
			// Shutdown has settled every job; one more pass sends what
			// that produced, the end included.
			stopping = true
		}
	}
}
