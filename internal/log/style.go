package log

import "io"

// Style applies the pretty handler's palette to text that is not a log record —
// the `loom inspect` tree, for one. Sharing it keeps every surface loom prints
// in one visual language, instead of each command forking its own copy of these
// escape codes and drifting.
//
// The zero Style is the uncolored variant, so output piped to a file or a test
// buffer carries no escapes and stays byte-comparable.
type Style struct {
	on bool
}

// NewStyle returns a Style that colors output only when w is a terminal.
func NewStyle(w io.Writer) Style {
	return Style{on: IsTerminal(w)}
}

// Enabled reports whether this Style emits color, for callers that pick a
// different glyph or layout when they cannot rely on it.
func (s Style) Enabled() bool { return s.on }

func (s Style) wrap(color, v string) string {
	if !s.on || v == "" {
		return v
	}
	return color + v + colorReset
}

// Bold marks the one thing on a line the eye should land on first.
func (s Style) Bold(v string) string { return s.wrap(colorBold, v) }

// Muted recedes: labels, paths, and other supporting detail.
func (s Style) Muted(v string) string { return s.wrap(colorMuted, v) }

// Error marks what is broken or unsatisfied.
func (s Style) Error(v string) string { return s.wrap(colorError, v) }

// Warn marks what still works but deserves a second look.
func (s Style) Warn(v string) string { return s.wrap(colorWarn, v) }

// Success marks a clean result.
func (s Style) Success(v string) string { return s.wrap(colorSuccess, v) }

// Module is the worker (leaf) module hue.
func (s Style) Module(v string) string { return s.wrap(colorModule, v) }

// Root is the orchestrator hue, reserved for the module driving a run.
func (s Style) Root(v string) string { return s.wrap(colorRootModule, v) }

// RootChip renders the run's root as the reserved bold, inverted "≡ name ≡"
// badge the run log and diff headers use, so the same module is recognizable
// across all three surfaces. Without color it degrades to the same bracketed
// form those surfaces fall back to.
func (s Style) RootChip(name string) string {
	if !s.on {
		return "[≡ " + name + " ≡]"
	}
	return colorInvert + colorRootModule + colorBold + " ≡ " + name + " ≡ " + colorReset
}
