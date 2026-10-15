package log

import (
	"bytes"
	"testing"
)

func TestSuccessfAndFailuref(t *testing.T) {
	// A plain buffer is not a terminal, so output is uncolored.
	var buf bytes.Buffer

	Successf(&buf, "run of %q complete", "batch")
	if got, want := buf.String(), "✔ run of \"batch\" complete\n"; got != want {
		t.Errorf("Successf: got %q, want %q", got, want)
	}

	buf.Reset()
	Failuref(&buf, "%v", "operation \"boom\" failed")
	if got, want := buf.String(), "✖ operation \"boom\" failed\n"; got != want {
		t.Errorf("Failuref: got %q, want %q", got, want)
	}
}
