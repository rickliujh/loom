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

// Text attributes (SGR) — structural, not hues.
const (
	colorReset  = "\033[0m"
	colorBold   = "\033[1m"
	colorInvert = "\033[7m"
)

// Palette. A restrained, desaturated 256-color scheme: hue carries meaning
// (which module, what severity) without the loud primary tones a plain 16-color
// palette produces. Every value is a muted mid-tone, so a busy run reads as one
// calm, serious surface rather than a string of alerts.
//
// colorModule is the single color every worker (leaf) module chip shares.
// Modules are told apart by the chip's inverted shape and its name, not by hue
// — one consistent color reads calmer than a per-module palette, and leaves
// color free to mean severity (a chip only changes hue for a warning/error).
//
// colorRootModule sets the run's root/orchestrator apart with its own reserved
// hue, so the module that fans work out reads distinctly from the workers it
// drives — reinforcing the bold "≡ … ≡" chip.
const (
	colorModule     = "\033[38;5;66m"  // slate teal — worker chips
	colorRootModule = "\033[38;5;61m"  // muted indigo — the orchestrator
	colorError      = "\033[38;5;131m" // brick red — errors, failure line
	colorWarn       = "\033[38;5;137m" // amber — warnings, dry-run marker
	colorSuccess    = "\033[38;5;65m"  // sage — the success line
	colorMuted      = "\033[38;5;244m" // mid gray — attr keys, debug
	colorLocalRun   = "\033[38;5;96m"  // mauve — the local-run mode marker
)

// modePrefixes are message prefixes that mark an execution mode; the pretty
// handler highlights them so skipped/simulated steps stand out from real ones.
var modePrefixes = []struct {
	prefix string
	color  string
}{
	{"dry-run:", colorWarn},
	{"local-run:", colorLocalRun},
}

// KeySection marks a log record as a section header. The pretty handler
// renders it with a leading blank line and bold text; structured handlers
// keep it as a regular attribute.
const KeySection = "section"

// KeyModule is the attribute carrying the executing module's name (set via
// Logger.With in module.Load, or the per-instance name in the executor). The
// pretty handler renders it as a colored "[name]" prefix instead of a trailing
// key=value pair, so interleaved output from different modules stays separable.
const KeyModule = "module"

// KeyRoot marks a logger as the run's root/orchestrator — a module that fans
// work out to child modules. The executor sets it (Logger.With(KeyRoot, true))
// on any module that has children, so the top-level module's own lines render
// with the reserved "≡ … ≡" chip. It is a structural signal, not inferred from
// log order, so it holds even for the very first line an orchestrator emits and
// for a pure orchestrator that never runs operations of its own. Children
// inherit the attr but stay plain: root marking is gated on module depth == 1.
const KeyRoot = "root"

// sharedState is shared by a handler and every handler derived from it via
// WithAttrs/WithGroup, so the output mutex survives the per-logger cloning that
// slog does.
type sharedState struct {
	mu sync.Mutex
	w  io.Writer
}

// PrettyHandler formats log output for human readability.
type PrettyHandler struct {
	st    *sharedState
	opts  slog.HandlerOptions
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
		st:    &sharedState{w: w},
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
	var moduleDepth int // count of module attrs: 1 = root, >1 = nested child
	var orchestrator bool
	var section bool
	var inline []slog.Attr // rendered as aligned bullets below the message
	var blocks []slog.Attr // multi-line values, rendered as indented blocks

	classify := func(a slog.Attr) {
		switch {
		case a.Equal(slog.Attr{}):
		case a.Key == KeySection && h.group == "":
			section = a.Value.Kind() != slog.KindBool || a.Value.Bool()
		case a.Key == KeyModule && h.group == "":
			moduleName = a.Value.String()
			moduleDepth++
		case a.Key == KeyRoot && h.group == "":
			orchestrator = a.Value.Bool()
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

	// The run's root/orchestrator is the top-level module (depth 1) that the
	// executor flagged as having children. Depth gates the flag so nested
	// children — which inherit the attr — stay plain, and a flat single-module
	// run (no children, no flag) keeps an undecorated chip.
	root := moduleDepth == 1 && orchestrator

	h.st.mu.Lock()
	defer h.st.mu.Unlock()

	var b strings.Builder

	if section {
		b.WriteByte('\n')
	}

	// Module chip: the leftmost gutter, so the executing module is the first
	// thing the eye lands on. Inverted and colored by the module (severity
	// overrides on warn/error); the root module is marked distinctly.
	if moduleName != "" {
		h.writeChip(&b, moduleName, root, r.Level)
	}

	// Level prefix.
	prefix, color := h.levelPrefix(r.Level)
	if h.color && color != "" {
		b.WriteString(color)
	}
	b.WriteString(prefix)
	if h.color && color != "" {
		b.WriteString(colorReset)
	}

	// Message. Section headers are emphasized; but when a chip is present it is
	// already an inverted anchor, so bold alone avoids a second reverse-video
	// bar on the same line. Headerless sections keep the full inverted bar.
	switch {
	case section && h.color && moduleName != "":
		b.WriteString(colorBold + r.Message + colorReset)
	case section && h.color:
		b.WriteString(colorInvert + colorBold + " " + r.Message + " " + colorReset)
	default:
		h.writeMessage(&b, r.Message)
	}
	b.WriteByte('\n')

	// Attributes render as aligned, dim bullets below the message.
	h.writeAttrs(&b, inline)
	for _, a := range blocks {
		h.writeBlock(&b, a)
	}

	_, err := io.WriteString(h.st.w, b.String())
	return err
}

func (h *PrettyHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &PrettyHandler{
		st:    h.st,
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
		st:    h.st,
		opts:  h.opts,
		attrs: append([]slog.Attr{}, h.attrs...),
		group: prefix,
		color: h.color,
	}
}

func (h *PrettyHandler) levelPrefix(level slog.Level) (string, string) {
	switch {
	case level >= slog.LevelError:
		return "error: ", colorError
	case level >= slog.LevelWarn:
		return "warning: ", colorWarn
	case level >= slog.LevelInfo:
		return "", ""
	default:
		return "debug: ", colorMuted
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

// writeChip renders the module gutter: an inverted, colored "[name]" chip.
// The color is the module's stable palette color, overridden by red/yellow on
// error/warning so severity reads at a glance. Root modules (a single module
// attr on the logger) are wrapped in "≡ … ≡" and bolded to stand apart from
// their nested children.
func (h *PrettyHandler) writeChip(b *strings.Builder, name string, root bool, level slog.Level) {
	label := name
	if root {
		label = "≡ " + name + " ≡"
	}

	if !h.color {
		b.WriteString("[" + label + "] ")
		return
	}

	color := colorModule
	if root {
		color = colorRootModule
	}
	switch {
	case level >= slog.LevelError:
		color = colorError
	case level >= slog.LevelWarn:
		color = colorWarn
	}

	b.WriteString(colorInvert + color)
	if root {
		b.WriteString(colorBold)
	}
	b.WriteString(" " + label + " " + colorReset + " ")
}

// writeAttrs renders inline attributes as aligned, dim-key bullets beneath the
// message. Keys are padded to a common width within the record so values form
// a readable column; multi-line values are handled separately by writeBlock.
func (h *PrettyHandler) writeAttrs(b *strings.Builder, attrs []slog.Attr) {
	if len(attrs) == 0 {
		return
	}

	keys := make([]string, len(attrs))
	maxKey := 0
	for i, a := range attrs {
		k := a.Key
		if h.group != "" {
			k = h.group + "." + k
		}
		keys[i] = k
		if len(k) > maxKey {
			maxKey = len(k)
		}
	}

	for i, a := range attrs {
		key := keys[i]
		pad := strings.Repeat(" ", maxKey-len(key))
		val := a.Value.String()
		if val == "" {
			val = `""`
		}
		if h.color {
			fmt.Fprintf(b, "  %s%s%s%s  %s\n", colorMuted, key, colorReset, pad, val)
		} else {
			fmt.Fprintf(b, "  %s%s  %s\n", key, pad, val)
		}
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
		fmt.Fprintf(b, "  %s%s:%s\n", colorMuted, key, colorReset)
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
