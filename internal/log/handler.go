package log

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"
)

// color codes
const (
	colorReset  = "\033[0m"
	colorRed    = "\033[31m"
	colorYellow = "\033[33m"
	colorCyan   = "\033[36m"
	colorGray   = "\033[90m"
)

// PrettyHandler formats log output for human readability.
type PrettyHandler struct {
	w     io.Writer
	opts  slog.HandlerOptions
	mu    sync.Mutex
	attrs []slog.Attr
	group string
	color bool
}

// NewPrettyHandler creates a human-friendly log handler.
func NewPrettyHandler(w io.Writer, opts *slog.HandlerOptions) *PrettyHandler {
	if opts == nil {
		opts = &slog.HandlerOptions{}
	}
	return &PrettyHandler{
		w:     w,
		opts:  *opts,
		color: isTerminal(w),
	}
}

func (h *PrettyHandler) Enabled(_ context.Context, level slog.Level) bool {
	minLevel := slog.LevelInfo
	if h.opts.Level != nil {
		minLevel = h.opts.Level.Level()
	}
	return level >= minLevel
}

func (h *PrettyHandler) Handle(_ context.Context, r slog.Record) error {
	var b strings.Builder

	// Level prefix with color.
	prefix, color := h.levelPrefix(r.Level)
	if h.color && color != "" {
		b.WriteString(color)
	}
	b.WriteString(prefix)
	if h.color && color != "" {
		b.WriteString(colorReset)
	}
	b.WriteString(r.Message)

	// Append pre-set attrs (from With/WithGroup).
	for _, a := range h.attrs {
		h.writeAttr(&b, a)
	}

	// Append record attrs.
	r.Attrs(func(a slog.Attr) bool {
		h.writeAttr(&b, a)
		return true
	})

	b.WriteByte('\n')

	h.mu.Lock()
	defer h.mu.Unlock()
	_, err := io.WriteString(h.w, b.String())
	return err
}

func (h *PrettyHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &PrettyHandler{
		w:     h.w,
		opts:  h.opts,
		attrs: append(append([]slog.Attr{}, h.attrs...), attrs...),
		group: h.group,
		color: h.color,
	}
}

func (h *PrettyHandler) WithGroup(name string) slog.Handler {
	prefix := name
	if h.group != "" {
		prefix = h.group + "." + name
	}
	return &PrettyHandler{
		w:     h.w,
		opts:  h.opts,
		attrs: append([]slog.Attr{}, h.attrs...),
		group: prefix,
		color: h.color,
	}
}

func (h *PrettyHandler) levelPrefix(level slog.Level) (string, string) {
	switch {
	case level >= slog.LevelError:
		return "ERROR ", colorRed
	case level >= slog.LevelWarn:
		return "WARN  ", colorYellow
	case level >= slog.LevelInfo:
		return "", ""
	default:
		return "DEBUG ", colorGray
	}
}

func (h *PrettyHandler) writeAttr(b *strings.Builder, a slog.Attr) {
	if a.Equal(slog.Attr{}) {
		return
	}

	key := a.Key
	if h.group != "" {
		key = h.group + "." + key
	}

	if h.color {
		fmt.Fprintf(b, " %s%s=%s%s", colorGray, key, a.Value.String(), colorReset)
	} else {
		fmt.Fprintf(b, " %s=%s", key, a.Value.String())
	}
}

func isTerminal(w io.Writer) bool {
	if f, ok := w.(*os.File); ok {
		fi, err := f.Stat()
		if err != nil {
			return false
		}
		return (fi.Mode() & os.ModeCharDevice) != 0
	}
	return false
}
