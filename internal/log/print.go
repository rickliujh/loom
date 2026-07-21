package log

import (
	"fmt"
	"io"
)

// Successf writes a "✔"-prefixed status line to w, colored green when w is
// a terminal. Used for closing status output that is not a log record.
func Successf(w io.Writer, format string, args ...any) {
	check := "✔"
	if isTerminal(w) {
		check = colorGreen + "✔" + colorReset
	}
	fmt.Fprintf(w, "%s %s\n", check, fmt.Sprintf(format, args...))
}

// Failuref writes a "✖"-prefixed error line to w, colored red when w is a
// terminal. Used for the top-level command error, which is returned up to
// main and so never flows through the log handler.
func Failuref(w io.Writer, format string, args ...any) {
	cross := "✖"
	if isTerminal(w) {
		cross = colorRed + "✖" + colorReset
	}
	fmt.Fprintf(w, "%s %s\n", cross, fmt.Sprintf(format, args...))
}
