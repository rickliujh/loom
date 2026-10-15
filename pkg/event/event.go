// Package event is the vocabulary a run reports its progress in, as data
// rather than rendered text: which module and operation started, skipped or
// ended, what was logged, which files changed, which PRs were opened. The CLI
// renders none of it; `loom serve` streams it to the browser.
//
// It is a leaf: the executor, the actions, the engine and the log handler all
// speak it, and it depends on none of them.
package event

import (
	"encoding/json"
	"fmt"
	"time"
)

// Type names an event.
type Type string

// The events a run emits. The server adds its own job-level events.
//
// A module or operation that runs yields a start and an end; one skipped by
// its `if` yields a single skip instead of either. A module's start comes once
// its source, params and target are resolved, so the logs of that setup
// precede it, carrying its path.
const (
	Log         Type = "log"
	ModuleStart Type = "module.start"
	ModuleSkip  Type = "module.skip"
	ModuleEnd   Type = "module.end"
	OpStart     Type = "op.start"
	OpSkip      Type = "op.skip"
	OpEnd       Type = "op.end"
	DiffFile    Type = "diff.file"
	PRCreated   Type = "pr.created"
)

// ReasonIfFalse is the Reason of a skip caused by an `if` predicate that
// exited non-zero.
const ReasonIfFalse = "if condition false"

// Event is one thing that happened during a run. Which fields are set depends
// on Type; unset ones are omitted from JSON.
type Event struct {
	// Seq orders events within a job. The emitter leaves it zero; whoever
	// stores the stream assigns it.
	Seq  int64     `json:"seq"`
	Type Type      `json:"type"`
	Time time.Time `json:"time"`
	// Path is the module breadcrumb, root first: instance names, so bulk
	// items that share one module are told apart.
	Path []string `json:"path,omitempty"`
	// Module is the module's metadata.name (module.* and pr.created).
	Module string `json:"module,omitempty"`
	// Op, Kind, Index and Total identify an operation: its name, its action
	// kind ("newFiles", "shell", …), and its 1-based position among the
	// module's operations.
	Op    string `json:"op,omitempty"`
	Kind  string `json:"kind,omitempty"`
	Index int    `json:"index,omitempty"`
	Total int    `json:"total,omitempty"`
	// Target is the display label of the target a diff.file applies to.
	Target string `json:"target,omitempty"`
	// Reason says why a module or operation was skipped.
	Reason string `json:"reason,omitempty"`
	// Error is set on a module.end or op.end that failed.
	Error string `json:"error,omitempty"`
	// DurationMS is how long a module or operation ran, on its end event;
	// absent means under a millisecond.
	DurationMS int64     `json:"durationMs,omitempty"`
	Log        *LogEntry `json:"log,omitempty"`
	Diff       *FileDiff `json:"diff,omitempty"`
	PR         *PR       `json:"pr,omitempty"`
}

// LogEntry is one log record.
type LogEntry struct {
	// Level is the slog level name: DEBUG, INFO, WARN or ERROR.
	Level string `json:"level"`
	Msg   string `json:"msg"`
	// Attrs are the record's attributes in order, less the ones that only
	// steer the CLI's rendering, which become the flags below.
	Attrs []Attr `json:"attrs"`
	// Section marks a header line: an operation's or a dispatch's.
	Section bool `json:"section,omitempty"`
	// Dispatch marks a parent's header announcing the child it hands off to.
	Dispatch bool `json:"dispatch,omitempty"`
	// Root marks a line of the run's root module when that module
	// orchestrates children — the CLI's "≡ … ≡" chip.
	Root bool `json:"root,omitempty"`
}

// Attr is one log attribute. It marshals as a two-element JSON array,
// ["key","value"], so order survives every JSON reader.
type Attr struct {
	Key, Value string
}

func (a Attr) MarshalJSON() ([]byte, error) {
	return json.Marshal([2]string{a.Key, a.Value})
}

func (a *Attr) UnmarshalJSON(data []byte) error {
	var kv [2]string
	if err := json.Unmarshal(data, &kv); err != nil {
		return fmt.Errorf("log attribute: %w", err)
	}
	a.Key, a.Value = kv[0], kv[1]
	return nil
}

// PR is a pull or merge request a run opened.
type PR struct {
	// Module is the metadata.name of the module whose pr operation opened it.
	Module string `json:"module"`
	Title  string `json:"title"`
	URL    string `json:"url"`
}

// File diff statuses. Quick and full mode report the same four.
const (
	StatusAdded    = "added"
	StatusModified = "modified"
	StatusDeleted  = "deleted"
	StatusRenamed  = "renamed"
)

// FileDiff is one changed file in a target.
type FileDiff struct {
	// Path is the file's path from the target repository's root, with "/"
	// separators — its new path when renamed.
	Path string `json:"path"`
	// OldPath is the path the file was renamed from; empty otherwise.
	OldPath string `json:"oldPath"`
	// Status is StatusAdded, StatusModified, StatusDeleted or StatusRenamed.
	Status string `json:"status"`
	// Binary reports a change git could not show as text; Unified is empty.
	Binary bool `json:"binary"`
	// Unified holds the change's hunks, from the first "@@" line on, without
	// colour and without the "---"/"+++" header lines Path already says.
	Unified string `json:"unified"`
}

// Sink receives events. A run calls it from the goroutine executing the run,
// one event at a time; a Sink shared by concurrent runs must synchronise
// itself. A nil Sink discards everything, which is what the CLI passes.
type Sink func(Event)

// Emit hands e to the sink, stamping Time if it is unset. Safe on a nil Sink.
func (s Sink) Emit(e Event) {
	if s == nil {
		return
	}
	if e.Time.IsZero() {
		e.Time = time.Now()
	}
	s(e)
}

// Since is the DurationMS of something that began at start.
func Since(start time.Time) int64 {
	return time.Since(start).Milliseconds()
}

// ErrorText is the Error field for err: its message, or empty for nil.
func ErrorText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
