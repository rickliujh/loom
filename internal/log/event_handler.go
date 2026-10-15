package log

import (
	"context"
	"errors"
	"log/slog"
	"regexp"

	"github.com/rickliujh/loom/pkg/event"
)

// EventHandler turns log records into event.Log events, so a run's logs can
// be streamed as data next to its other events. It reads records the way
// PrettyHandler does — the ordered "module" attributes are the breadcrumb, and
// the "section", "dispatch" and "root" styling attributes become flags — so a
// record's event says what its pretty line shows, without the rendering.
type EventHandler struct {
	sink  event.Sink
	level slog.Leveler
	attrs []slog.Attr
	group string
}

// NewEventHandler returns a handler that emits a log event to sink for every
// record at or above level (Info when level is nil).
func NewEventHandler(sink event.Sink, level slog.Leveler) *EventHandler {
	return &EventHandler{sink: sink, level: level}
}

func (h *EventHandler) Enabled(_ context.Context, level slog.Level) bool {
	minLevel := slog.LevelInfo
	if h.level != nil {
		minLevel = h.level.Level()
	}
	return level >= minLevel
}

func (h *EventHandler) Handle(_ context.Context, r slog.Record) error {
	var path []string
	var orchestrator bool
	entry := &event.LogEntry{Level: r.Level.String(), Msg: RedactURLUserinfo(r.Message), Attrs: []event.Attr{}}

	// The classification mirrors PrettyHandler.Handle, including its choice
	// that under a group no attribute steers rendering.
	classify := func(a slog.Attr) {
		a.Value = a.Value.Resolve()
		switch {
		case a.Equal(slog.Attr{}):
		case a.Key == KeySection && h.group == "":
			entry.Section = a.Value.Kind() != slog.KindBool || a.Value.Bool()
		case a.Key == KeyModule && h.group == "":
			path = append(path, a.Value.String())
		case a.Key == KeyRoot && h.group == "":
			orchestrator = a.Value.Kind() == slog.KindBool && a.Value.Bool()
		case a.Key == KeyDispatch && h.group == "":
			entry.Dispatch = a.Value.Kind() != slog.KindBool || a.Value.Bool()
		default:
			key := a.Key
			if h.group != "" {
				key = h.group + "." + key
			}
			entry.Attrs = append(entry.Attrs, event.Attr{Key: key, Value: RedactURLUserinfo(a.Value.String())})
		}
	}
	for _, a := range h.attrs {
		classify(a)
	}
	r.Attrs(func(a slog.Attr) bool {
		classify(a)
		return true
	})

	// As in PrettyHandler: only the top-level module is the root, since
	// children inherit the attribute from the orchestrator's logger.
	entry.Root = len(path) == 1 && orchestrator

	h.sink.Emit(event.Event{Type: event.Log, Time: r.Time, Path: path, Log: entry})
	return nil
}

func (h *EventHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &EventHandler{
		sink:  h.sink,
		level: h.level,
		attrs: append(append([]slog.Attr{}, h.attrs...), attrs...),
		group: h.group,
	}
}

func (h *EventHandler) WithGroup(name string) slog.Handler {
	prefix := name
	if h.group != "" {
		prefix = h.group + "." + name
	}
	return &EventHandler{
		sink:  h.sink,
		level: h.level,
		attrs: append([]slog.Attr{}, h.attrs...),
		group: prefix,
	}
}

// urlUserinfo matches the userinfo of a URL: the scheme, then everything up
// to the last "@" before the host ends. Greedy, so a password holding an
// unescaped "@" is still covered.
var urlUserinfo = regexp.MustCompile(`(?i)\b([a-z][a-z0-9+.-]*://)[^\s/?#]*@`)

// RedactURLUserinfo replaces the userinfo of every URL in s — a token in
// "https://x-access-token:TOKEN@host" or "https://TOKEN@host" — with "***",
// keeping the rest of the URL readable.
func RedactURLUserinfo(s string) string {
	return urlUserinfo.ReplaceAllString(s, "${1}***@")
}

// MultiHandler hands each record to several handlers — for `loom serve`, the
// event handler and an uncoloured PrettyHandler writing the job's text log.
// (slog.NewMultiHandler does this from Go 1.26; go.mod targets 1.25.)
type MultiHandler struct {
	handlers []slog.Handler
}

// NewMultiHandler returns a handler that forwards to every one of handlers.
func NewMultiHandler(handlers ...slog.Handler) *MultiHandler {
	return &MultiHandler{handlers: append([]slog.Handler(nil), handlers...)}
}

func (m *MultiHandler) Enabled(ctx context.Context, level slog.Level) bool {
	for _, h := range m.handlers {
		if h.Enabled(ctx, level) {
			return true
		}
	}
	return false
}

// Handle forwards r to each handler that is enabled for its level, each with
// its own copy, and joins their errors.
func (m *MultiHandler) Handle(ctx context.Context, r slog.Record) error {
	var errs []error
	for _, h := range m.handlers {
		if h.Enabled(ctx, r.Level) {
			if err := h.Handle(ctx, r.Clone()); err != nil {
				errs = append(errs, err)
			}
		}
	}
	return errors.Join(errs...)
}

func (m *MultiHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	out := make([]slog.Handler, len(m.handlers))
	for i, h := range m.handlers {
		out[i] = h.WithAttrs(attrs)
	}
	return &MultiHandler{handlers: out}
}

func (m *MultiHandler) WithGroup(name string) slog.Handler {
	out := make([]slog.Handler, len(m.handlers))
	for i, h := range m.handlers {
		out[i] = h.WithGroup(name)
	}
	return &MultiHandler{handlers: out}
}
