package log

import (
	"fmt"
	"io"
)

// Successf writes a "✔"-prefixed status line to w, colored green when w is
// a terminal. Used for closing status output that is not a log record.
func Successf(w io.Writer, format string, args ...any) {
	check := "✔"
	if IsTerminal(w) {
		check = colorSuccess + "✔" + colorReset
	}
	fmt.Fprintf(w, "%s %s\n", check, fmt.Sprintf(format, args...))
}

// Failuref writes a "✖"-prefixed error line to w, colored red when w is a
// terminal. Used for the top-level command error, which is returned up to
// main and so never flows through the log handler.
func Failuref(w io.Writer, format string, args ...any) {
	cross := "✖"
	if IsTerminal(w) {
		cross = colorError + "✖" + colorReset
	}
	fmt.Fprintf(w, "%s %s\n", cross, fmt.Sprintf(format, args...))
}

// Warningf writes a "⚠"-prefixed warning line to w, colored amber when w is a
// terminal. Used for standalone notices that are not log records.
func Warningf(w io.Writer, format string, args ...any) {
	warn := "⚠"
	if IsTerminal(w) {
		warn = colorWarn + "⚠" + colorReset
	}
	fmt.Fprintf(w, "%s %s\n", warn, fmt.Sprintf(format, args...))
}
