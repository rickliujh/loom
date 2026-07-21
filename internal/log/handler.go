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
	colorReset   = "\033[0m"
	colorBold    = "\033[1m"
	colorInvert  = "\033[7m"
	colorRed     = "\033[31m"
	colorGreen   = "\033[32m"
	colorYellow  = "\033[33m"
	colorBlue    = "\033[34m"
	colorMagenta = "\033[35m"
	colorCyan    = "\033[36m"
	colorGray    = "\033[90m"
)

// modulePalette assigns each module a stable color so interleaved output
// from parent/child modules stays visually separable.
var modulePalette = [...]string{colorCyan, colorMagenta, colorBlue, colorGreen, colorYellow}

func moduleColor(name string) string {
	var h uint32 = 2166136261
	for i := 0; i < len(name); i++ {
		h ^= uint32(name[i])
		h *= 16777619
	}
	return modulePalette[h%uint32(len(modulePalette))]
}

// modePrefixes are message prefixes that mark an execution mode; the pretty
// handler highlights them so skipped/simulated steps stand out from real ones.
var modePrefixes = []struct {
	prefix string
	color  string
}{
	{"dry-run:", colorYellow},
	{"local-run:", colorMagenta},
}

// KeySection marks a log record as a section header. The pretty handler
// renders it with a leading blank line and bold text; structured handlers
// keep it as a regular attribute.
const KeySection = "section"

// keyModule is the attribute carrying the executing module's name (set via
// Logger.With in module.Load). The pretty handler renders it as a dim
// "[name]" prefix instead of a trailing key=value pair.
const keyModule = "module"

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
	// Split attrs by rendering role before writing anything.
	var moduleName string
	var section bool
	var inline []slog.Attr // rendered as trailing key=value pairs
	var blocks []slog.Attr // multi-line values, rendered as indented blocks

	classify := func(a slog.Attr) {
		switch {
		case a.Equal(slog.Attr{}):
		case a.Key == KeySection && h.group == "":
			section = a.Value.Kind() != slog.KindBool || a.Value.Bool()
		case a.Key == keyModule && h.group == "":
			moduleName = a.Value.String()
		case strings.Contains(a.Value.String(), "\n"):
			blocks = append(blocks, a)
		default:
			inline = append(inline, a)
		}
	}
	for _, a := range h.attrs {
		classify(a)
	}
	r.Attrs(func(a slog.Attr) bool {
		classify(a)
		return true
	})

	var b strings.Builder

	if section {
		b.WriteByte('\n')
	}

	// Level prefix with color.
	prefix, color := h.levelPrefix(r.Level)
	if h.color && color != "" {
		b.WriteString(color)
	}
	b.WriteString(prefix)
	if h.color && color != "" {
		b.WriteString(colorReset)
	}

	if moduleName != "" {
		if h.color {
			b.WriteString(moduleColor(moduleName))
		}
		b.WriteString("[" + moduleName + "] ")
		if h.color {
			b.WriteString(colorReset)
		}
	}

	if section && h.color {
		b.WriteString(colorInvert + colorBold + " " + r.Message + " " + colorReset)
	} else {
		h.writeMessage(&b, r.Message)
	}

	for _, a := range inline {
		h.writeAttr(&b, a)
	}
	b.WriteByte('\n')

	for _, a := range blocks {
		h.writeBlock(&b, a)
	}

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
		return "error: ", colorRed
	case level >= slog.LevelWarn:
		return "warning: ", colorYellow
	case level >= slog.LevelInfo:
		return "", ""
	default:
		return "debug: ", colorGray
	}
}

// writeMessage writes the record message, highlighting a leading mode
// marker ("dry-run:", "local-run:") when color is enabled.
func (h *PrettyHandler) writeMessage(b *strings.Builder, msg string) {
	if h.color {
		for _, m := range modePrefixes {
			if strings.HasPrefix(msg, m.prefix) {
				b.WriteString(m.color + m.prefix + colorReset + msg[len(m.prefix):])
				return
			}
		}
	}
	b.WriteString(msg)
}

func (h *PrettyHandler) writeAttr(b *strings.Builder, a slog.Attr) {
	key := a.Key
	if h.group != "" {
		key = h.group + "." + key
	}

	val := a.Value.String()
	if strings.ContainsAny(val, " \t") || val == "" {
		val = fmt.Sprintf("%q", val)
	}

	if h.color {
		// Gray key, plain value — the values are what the reader scans for.
		fmt.Fprintf(b, " %s%s=%s%s", colorGray, key, colorReset, val)
	} else {
		fmt.Fprintf(b, " %s=%s", key, val)
	}
}

// writeBlock renders a multi-line attribute value as an indented block
// under a dim "key:" label, keeping the value itself readable.
func (h *PrettyHandler) writeBlock(b *strings.Builder, a slog.Attr) {
	key := a.Key
	if h.group != "" {
		key = h.group + "." + key
	}

	if h.color {
		fmt.Fprintf(b, "  %s%s:%s\n", colorGray, key, colorReset)
	} else {
		fmt.Fprintf(b, "  %s:\n", key)
	}
	val := strings.TrimRight(a.Value.String(), "\n")
	for _, line := range strings.Split(val, "\n") {
		b.WriteString("    ")
		b.WriteString(line)
		b.WriteByte('\n')
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
