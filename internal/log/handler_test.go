package log

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

func newTestLogger() (*slog.Logger, *bytes.Buffer) {
	var buf bytes.Buffer
	return slog.New(NewPrettyHandler(&buf, nil)), &buf
}

func TestModuleAttrRenderedAsPrefix(t *testing.T) {
	logger, buf := newTestLogger()
	logger.With("module", "onboard").Info("writing file", "path", "argocd/app.yaml")

	got := buf.String()
	want := "[onboard] writing file path=argocd/app.yaml\n"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestSectionAttrAddsBlankLineAndIsDropped(t *testing.T) {
	logger, buf := newTestLogger()
	logger.Info("operation create-files (1/6)", KeySection, true)

	got := buf.String()
	want := "\noperation create-files (1/6)\n"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestAttrValuesWithSpacesAreQuoted(t *testing.T) {
	logger, buf := newTestLogger()
	logger.Info("committing", "message", "feat: onboard payments")

	got := buf.String()
	want := `committing message="feat: onboard payments"` + "\n"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestMultilineAttrRenderedAsBlock(t *testing.T) {
	logger, buf := newTestLogger()
	logger.Info("shell output", "output", "line one\nline two\n")

	got := buf.String()
	want := "shell output\n  output:\n    line one\n    line two\n"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func newColorTestLogger() (*slog.Logger, *bytes.Buffer) {
	var buf bytes.Buffer
	h := NewPrettyHandler(&buf, nil)
	h.color = true
	return slog.New(h), &buf
}

func TestColorSectionRenderedInverted(t *testing.T) {
	logger, buf := newColorTestLogger()
	logger.Info("operation create-files (1/6)", KeySection, true)

	got := buf.String()
	want := "\n\033[7m\033[1m operation create-files (1/6) \033[0m\n"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestColorModulePrefixUsesStablePaletteColor(t *testing.T) {
	logger, buf := newColorTestLogger()
	logger.With("module", "onboard").Info("writing file")

	got := buf.String()
	want := moduleColor("onboard") + "[onboard] " + "\033[0m" + "writing file\n"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if moduleColor("onboard") == moduleColor("payments-child") &&
		moduleColor("onboard") == moduleColor("другой") {
		t.Error("expected palette to vary across module names")
	}
}

func TestColorModeMarkerHighlighted(t *testing.T) {
	logger, buf := newColorTestLogger()
	logger.Info("dry-run: would write file")
	logger.Info("local-run: skipping PR creation")

	got := buf.String()
	for _, want := range []string{
		"\033[33mdry-run:\033[0m would write file\n",
		"\033[35mlocal-run:\033[0m skipping PR creation\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q:\n%q", want, got)
		}
	}
}

func TestColorAttrKeyGrayValuePlain(t *testing.T) {
	logger, buf := newColorTestLogger()
	logger.Info("writing file", "path", "argocd/app.yaml")

	got := buf.String()
	want := "writing file \033[90mpath=\033[0margocd/app.yaml\n"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestLevelPrefixes(t *testing.T) {
	logger, buf := newTestLogger()
	logger.Warn("something odd")
	logger.Error("something broke")

	got := buf.String()
	for _, want := range []string{"warning: something odd\n", "error: something broke\n"} {
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q:\n%s", want, got)
		}
	}
}
