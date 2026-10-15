package log

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"slices"
	"strings"
	"testing"

	"github.com/rickliujh/loom/pkg/event"
)

// recorder collects the events a sink receives.
type recorder struct{ events []event.Event }

func (r *recorder) sink() event.Sink { return func(e event.Event) { r.events = append(r.events, e) } }

func (r *recorder) last(t *testing.T) event.Event {
	t.Helper()
	if len(r.events) == 0 {
		t.Fatal("no event emitted")
	}
	return r.events[len(r.events)-1]
}

// A record becomes a log event carrying the module breadcrumb in order, with
// the styling attributes turned into flags and every other attribute kept in
// the order it was given.
func TestEventHandler_RecordToEvent(t *testing.T) {
	var rec recorder
	logger := slog.New(NewEventHandler(rec.sink(), nil)).
		With(KeyModule, "bulk").With(KeyRoot, true).With("run", 7)

	logger.Info("item-a (1/2)", KeySection, true, KeyDispatch, true, "z", "last")
	e := rec.last(t)
	if e.Type != event.Log || !slices.Equal(e.Path, []string{"bulk"}) || e.Time.IsZero() {
		t.Errorf("event = %+v", e)
	}
	want := event.LogEntry{Level: "INFO", Msg: "item-a (1/2)", Section: true, Dispatch: true, Root: true,
		Attrs: []event.Attr{{Key: "run", Value: "7"}, {Key: "z", Value: "last"}}}
	if !logEqual(*e.Log, want) {
		t.Errorf("log = %+v, want %+v", *e.Log, want)
	}

	// A child inherits the root attribute but is not the root; its breadcrumb
	// grows by its instance name.
	logger.With(KeyModule, "item-a").Warn("careful", "err", errors.New("boom"))
	e = rec.last(t)
	if !slices.Equal(e.Path, []string{"bulk", "item-a"}) || e.Log.Root || e.Log.Level != "WARN" {
		t.Errorf("child event = %+v / %+v", e, *e.Log)
	}
	if !slices.Equal(e.Log.Attrs, []event.Attr{{Key: "run", Value: "7"}, {Key: "err", Value: "boom"}}) {
		t.Errorf("child attrs = %+v", e.Log.Attrs)
	}
}

func logEqual(a, b event.LogEntry) bool {
	return a.Level == b.Level && a.Msg == b.Msg && a.Section == b.Section && a.Dispatch == b.Dispatch &&
		a.Root == b.Root && slices.Equal(a.Attrs, b.Attrs)
}

// The breadcrumb and flags are exactly what PrettyHandler renders from the
// same records: the last path element is the chip, the path's length the
// indentation depth, and a lone root module wears the "≡ … ≡" chip.
func TestEventHandler_MatchesPrettyHandler(t *testing.T) {
	var rec recorder
	var buf bytes.Buffer
	logger := slog.New(NewMultiHandler(NewEventHandler(rec.sink(), nil), NewPrettyHandler(&buf, nil)))

	root := logger.With(KeyModule, "root").With(KeyRoot, true)
	loggers := []*slog.Logger{
		logger,
		logger.With(KeyModule, "flat"),
		root,
		root.With(KeyModule, "child"),
		root.With(KeyModule, "child").With(KeyModule, "grandchild"),
	}
	for _, l := range loggers {
		buf.Reset()
		l.Info("line")
		e := rec.last(t)
		depth := len(e.Path)
		wantLine := "line\n"
		if depth > 0 {
			chip := e.Path[depth-1]
			if e.Log.Root {
				chip = "≡ " + chip + " ≡"
			}
			wantLine = strings.Repeat("  ", max(depth-1, 0)) + "[" + chip + "] line\n"
		}
		if buf.String() != wantLine {
			t.Errorf("path %v root %v: pretty printed %q, want %q", e.Path, e.Log.Root, buf.String(), wantLine)
		}
	}

	// Under a group, PrettyHandler steers nothing by attribute — neither does
	// the event handler; keys are qualified by the group.
	buf.Reset()
	logger.With(KeyModule, "m").WithGroup("g").Info("grouped", "k", "v")
	e := rec.last(t)
	if len(e.Path) != 0 || !slices.Equal(e.Log.Attrs, []event.Attr{{Key: "g.module", Value: "m"}, {Key: "g.k", Value: "v"}}) {
		t.Errorf("grouped event = %+v / %+v", e, *e.Log)
	}
	if want := "grouped\n  g.module  m\n  g.k       v\n"; buf.String() != want {
		t.Errorf("pretty grouped = %q, want %q", buf.String(), want)
	}
}

// Credentials in a URL's userinfo never reach an event, whether in the
// message or an attribute, and the rest of the URL stays readable.
func TestEventHandler_RedactsURLUserinfo(t *testing.T) {
	var rec recorder
	logger := slog.New(NewEventHandler(rec.sink(), nil))
	logger.Info("cloning https://x-access-token:ghp_secret@github.com/o/r.git failed",
		"url", "https://ghp_token@github.com/o/r.git",
		"error", "git clone http://user:p@ss@host:8080/p: exit 128",
		"plain", "git@github.com:o/r.git")

	e := rec.last(t)
	if want := "cloning https://***@github.com/o/r.git failed"; e.Log.Msg != want {
		t.Errorf("msg = %q, want %q", e.Log.Msg, want)
	}
	want := []event.Attr{
		{Key: "url", Value: "https://***@github.com/o/r.git"},
		{Key: "error", Value: "git clone http://***@host:8080/p: exit 128"},
		{Key: "plain", Value: "git@github.com:o/r.git"},
	}
	if !slices.Equal(e.Log.Attrs, want) {
		t.Errorf("attrs = %q, want %q", e.Log.Attrs, want)
	}
	data, _ := json.Marshal(e)
	for _, secret := range []string{"ghp_secret", "ghp_token", "p@ss"} {
		if bytes.Contains(data, []byte(secret)) {
			t.Errorf("event JSON leaks %q: %s", secret, data)
		}
	}
	for _, s := range []string{"https://host/path?to=a@b", "mailto:a@b.c", "see https://example.com/x and a@b"} {
		if got := RedactURLUserinfo(s); got != s {
			t.Errorf("RedactURLUserinfo(%q) = %q, want it unchanged", s, got)
		}
	}
}

// Records below the level are dropped; a nil level means Info.
func TestEventHandler_Level(t *testing.T) {
	var rec recorder
	slog.New(NewEventHandler(rec.sink(), nil)).Debug("hidden")
	slog.New(NewEventHandler(rec.sink(), slog.LevelWarn)).Info("hidden")
	if len(rec.events) != 0 {
		t.Fatalf("emitted %d events below the level", len(rec.events))
	}
	slog.New(NewEventHandler(rec.sink(), slog.LevelDebug)).Debug("shown")
	if len(rec.events) != 1 || rec.events[0].Log.Level != "DEBUG" {
		t.Errorf("events = %+v", rec.events)
	}
}

// An attribute marshals as a ["key","value"] pair and reads back.
func TestEventAttrJSON(t *testing.T) {
	data, err := json.Marshal(event.LogEntry{Level: "INFO", Msg: "m", Attrs: []event.Attr{{Key: "path", Value: "a b"}}})
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"level":"INFO","msg":"m","attrs":[["path","a b"]]}`; string(data) != want {
		t.Errorf("json = %s, want %s", data, want)
	}
	var back event.LogEntry
	if err := json.Unmarshal(data, &back); err != nil || !slices.Equal(back.Attrs, []event.Attr{{Key: "path", Value: "a b"}}) {
		t.Errorf("round trip = %+v, %v", back, err)
	}
}

// MultiHandler hands each record to every handler enabled for its level, and
// derived loggers carry their attributes to all of them.
func TestMultiHandler(t *testing.T) {
	var rec recorder
	var buf bytes.Buffer
	h := NewMultiHandler(
		NewEventHandler(rec.sink(), slog.LevelDebug),
		NewPrettyHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}),
	)
	if !h.Enabled(context.Background(), slog.LevelDebug) {
		t.Error("enabled should hold when any handler is enabled")
	}
	logger := slog.New(h).With(KeyModule, "m")
	logger.Debug("only events")
	logger.Info("both")

	if len(rec.events) != 2 {
		t.Fatalf("event handler got %d records, want 2", len(rec.events))
	}
	if buf.String() != "[m] both\n" {
		t.Errorf("pretty handler wrote %q, want only the info line", buf.String())
	}
	if !slices.Equal(rec.events[1].Path, []string{"m"}) {
		t.Errorf("derived attrs did not reach the event handler: %+v", rec.events[1])
	}
}
