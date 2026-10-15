package server

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"sync"
	"time"

	"github.com/rickliujh/loom/pkg/event"
)

// Server-side event types, added to the ones a run emits.
const (
	// eventJobState reports a job's state change: state, and error when it
	// failed or was interrupted.
	eventJobState event.Type = "job.state"
	// eventEnd is sent on an event stream after the job's terminal state,
	// just before the server closes it. It is not stored and has no id.
	eventEnd event.Type = "end"
)

// maxLogEvents caps the log events kept per job. Lifecycle events — state,
// module, operation, diff and PR events — are always kept, so a chatty shell
// step cannot push the job's structure out of its history.
const maxLogEvents = 50_000

// jobEvent is an event as stored and streamed: a run's event, or one of the
// server's own, which carry State.
type jobEvent struct {
	event.Event
	State string `json:"state,omitempty"`
}

// eventLog is a job's event stream: it numbers events, redacts them, caps
// log events, keeps them for replay and wakes streams waiting on new ones.
type eventLog struct {
	mu sync.Mutex
	// events are the stored events, in seq order. Nil once spilled: the
	// job is over and they are read back from path.
	events  []jobEvent
	spilled bool
	path    string
	file    *os.File

	seq    int64
	logs   int
	capped bool
	// closed is set with the terminal job.state event; nothing follows it.
	closed bool
	// notify is closed and replaced on every append, waking every stream
	// blocked on the old one.
	notify chan struct{}
}

func newEventLog() *eventLog {
	return &eventLog{notify: make(chan struct{})}
}

// persistTo makes the log also append each event to path, a JSON line each.
func (l *eventLog) persistTo(path string) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	l.mu.Lock()
	l.file, l.path = f, path
	l.mu.Unlock()
	return nil
}

// sink is the event.Sink a run emits into.
func (l *eventLog) sink() event.Sink {
	return func(e event.Event) { l.append(jobEvent{Event: e}) }
}

// append stores e with the next sequence number. A log event past the cap
// is dropped without taking a number, so the sequence stays gapless.
func (l *eventLog) append(e jobEvent) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.appendLocked(e)
}

func (l *eventLog) appendLocked(e jobEvent) {
	if l.closed {
		return
	}
	if e.Type == event.Log {
		if l.capped {
			return
		}
		if l.logs >= maxLogEvents {
			l.capped = true
			e = jobEvent{Event: event.Event{Type: event.Log, Time: e.Time, Log: &event.LogEntry{
				Level: "WARN",
				Msg:   "log capped: later lines are left out of the event stream; log.txt has them",
				Attrs: []event.Attr{},
			}}}
		}
		l.logs++
	}
	if e.Time.IsZero() {
		e.Time = time.Now()
	}
	l.seq++
	e.Seq = l.seq
	scrub(&e)
	if !l.spilled {
		l.events = append(l.events, e)
	}
	if l.file != nil {
		if line, err := marshalJSON(e); err == nil {
			_, _ = l.file.Write(append(line, '\n'))
		}
	}
	close(l.notify)
	l.notify = make(chan struct{})
}

// scrub redacts URL credentials from every free-text field. The log handler
// already redacts log records; errors come from anywhere.
func scrub(e *jobEvent) {
	e.Error = redact(e.Error)
	if e.Log != nil {
		e.Log.Msg = redact(e.Log.Msg)
		for i := range e.Log.Attrs {
			e.Log.Attrs[i].Value = redact(e.Log.Attrs[i].Value)
		}
	}
}

// finish appends the terminal job.state event and closes the log.
func (l *eventLog) finish(e jobEvent) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.appendLocked(e)
	l.closed = true
	if l.file != nil {
		_ = l.file.Close()
		l.file = nil
	}
}

// spill drops the in-memory copy of a finished, persisted log; replay reads
// the file instead.
func (l *eventLog) spill() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed && l.path != "" {
		l.events = nil
		l.spilled = true
	}
}

// lastSeq is the sequence number of the last stored event.
func (l *eventLog) lastSeq() int64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.seq
}

// since returns the events after seq, whether the log is closed with all of
// them returned, and a channel closed on the next append.
func (l *eventLog) since(after int64) ([]jobEvent, bool, <-chan struct{}, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.spilled {
		evs, last, err := readEvents(l.path, after)
		l.seq = max(l.seq, last)
		return evs, l.closed, l.notify, err
	}
	var out []jobEvent
	for _, e := range l.events {
		if e.Seq > after {
			out = append(out, e)
		}
	}
	return out, l.closed, l.notify, nil
}

// readEvents reads an events.jsonl file, keeping the events after seq; it
// also returns the last sequence number in the file.
func readEvents(path string, after int64) ([]jobEvent, int64, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errIsNotExist(err) {
			return nil, 0, nil
		}
		return nil, 0, err
	}
	var last int64
	var out []jobEvent
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 0, 64*1024), 16<<20)
	for sc.Scan() {
		var e jobEvent
		if err := json.Unmarshal(sc.Bytes(), &e); err != nil {
			// A line cut short by a crash; everything before it stands.
			break
		}
		last = max(last, e.Seq)
		if e.Seq > after {
			out = append(out, e)
		}
	}
	return out, last, nil
}

// maxTextLog caps a job's plain-text log.
const maxTextLog = 32 << 20

// textLog is a job's plain-text log, as the CLI would print it: the pretty
// handler writes here without colour. Writes are redacted, capped, and — with
// disk history — appended to log.txt as they happen.
type textLog struct {
	mu     sync.Mutex
	buf    bytes.Buffer
	file   *os.File
	path   string
	size   int
	capped bool
	// dropped is set once the job is over and the text lives in path only.
	dropped bool
}

func (t *textLog) persistTo(path string) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	t.mu.Lock()
	t.file, t.path = f, path
	t.mu.Unlock()
	return nil
}

func (t *textLog) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.capped || t.dropped {
		return len(p), nil
	}
	text := redact(string(p))
	if t.size+len(text) > maxTextLog {
		text = "… log capped at 32 MiB\n"
		t.capped = true
	}
	t.size += len(text)
	t.buf.WriteString(text)
	if t.file != nil {
		_, _ = t.file.WriteString(text)
	}
	return len(p), nil
}

// close ends writing; with a file behind it, the memory copy is released.
func (t *textLog) close() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.file != nil {
		_ = t.file.Close()
		t.file = nil
		t.buf = bytes.Buffer{}
		t.dropped = true
	}
}

func (t *textLog) text() ([]byte, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.dropped {
		b, err := os.ReadFile(t.path)
		if errIsNotExist(err) {
			return nil, nil
		}
		return b, err
	}
	return append([]byte(nil), t.buf.Bytes()...), nil
}
