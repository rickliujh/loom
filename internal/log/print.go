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
