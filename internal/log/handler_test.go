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

func TestRootChipMarkedWhenModuleIsOrchestrator(t *testing.T) {
	logger, buf := newTestLogger()
	// The executor flags an orchestrator's logger with KeyRoot; its depth-1
	// lines render with "≡ … ≡" markers regardless of log order.
	logger.With("module", "bulk-onboard").With(KeyRoot, true).
		Info("writing file", "path", "argocd/app.yaml")

	got := buf.String()
	want := "[≡ bulk-onboard ≡] writing file\n  path  argocd/app.yaml\n"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestRootMarkerOnFirstLineNeedsNoPriorNesting(t *testing.T) {
	logger, buf := newTestLogger()
	// The very first line an orchestrator emits (a batch header, before any
	// child has logged) is already marked — root-ness is structural, not
	// inferred from having seen a nested record first.
	logger.With("module", "bulk-onboard").With(KeyRoot, true).
		Info("onboard-0 (1/2)", KeySection, true)

	got := buf.String()
	want := "\n[≡ bulk-onboard ≡] onboard-0 (1/2)\n"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestOrchestratorFlagIgnoredForNestedChild(t *testing.T) {
	logger, buf := newTestLogger()
	// A child inherits the orchestrator's KeyRoot attr but stays plain: root
	// marking is gated on module depth == 1.
	logger.With("module", "bulk-onboard").With(KeyRoot, true).
		With("module", "onboard-0").Info("writing file")

	got := buf.String()
	// Depth 2 (two module attrs) indents one level beneath its parent.
	want := "  [onboard-0] writing file\n"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestFlatModuleChipHasNoRootMarker(t *testing.T) {
	logger, buf := newTestLogger()
	// A single module run never sees nesting, so its chip stays plain — no
	// "≡" markers cluttering an ordinary run.
	logger.With("module", "onboard").Info("writing file")

	got := buf.String()
	want := "[onboard] writing file\n"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestNestedModuleChipHasNoRootMarker(t *testing.T) {
	logger, buf := newTestLogger()
	// Two module attrs = a nested child: plain chip, no "≡" markers, indented
	// one level beneath its parent.
	logger.With("module", "bulk-onboard").With("module", "onboard-0").Info("writing file")

	got := buf.String()
	want := "  [onboard-0] writing file\n"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestNestingIndentsByDepth(t *testing.T) {
	logger, buf := newTestLogger()
	// Three module attrs = two levels of nesting below the root: two indent
	// steps. Attribute bullets carry the same indent as the message.
	logger.With("module", "root").With("module", "mid").With("module", "leaf").
		Info("writing file", "path", "app.yaml")

	got := buf.String()
	want := "    [leaf] writing file\n      path  app.yaml\n"
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

func TestAttrsRenderedAsAlignedBullets(t *testing.T) {
	logger, buf := newTestLogger()
	// Values are alone on their line, so spaces need no quoting; keys align.
	logger.Info("committing", "message", "feat: onboard payments", "author", "loom-bot")

	got := buf.String()
	want := "committing\n  message  feat: onboard payments\n  author   loom-bot\n"
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

func TestColorRootChipInvertedBoldModuleColor(t *testing.T) {
	logger, buf := newColorTestLogger()
	// An orchestrator's logger carries KeyRoot, so its depth-1 line is marked.
	logger.With("module", "onboard").With(KeyRoot, true).Info("writing file")

	got := buf.String()
	// Root chip: invert + the reserved root color + bold, wrapped in "≡ … ≡".
	want := "\033[7m" + colorRootModule + "\033[1m" + " ≡ onboard ≡ " + "\033[0m" + " " + "writing file\n"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestColorChipSharesOneColorAcrossModules(t *testing.T) {
	logger, buf := newColorTestLogger()
	logger.With("module", "greeter-alice").Info("a")
	logger.With("module", "greeter-bob").Info("b")

	got := buf.String()
	// Both chips use the same (module) color; only the name differs.
	for _, name := range []string{"greeter-alice", "greeter-bob"} {
		want := "\033[7m" + colorModule + " " + name + " " + "\033[0m"
		if !strings.Contains(got, want) {
			t.Errorf("expected chip %q for %s in:\n%q", want, name, got)
		}
	}
}

func TestColorChildChipRecoloredBySeverity(t *testing.T) {
	logger, buf := newColorTestLogger()
	// A nested child logging a warning: chip is yellow (severity), not the
	// module's palette color, and carries no root markers.
	logger.With("module", "bulk").With("module", "child-0").Warn("push rejected")

	got := buf.String()
	// Depth 2: indented one level, then the severity-colored chip and message.
	want := "  " + "\033[7m" + colorWarn + " child-0 " + "\033[0m" + " " + colorWarn + "warning: " + "\033[0m" + "push rejected\n"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestColorSectionWithChipIsBoldNotInverted(t *testing.T) {
	logger, buf := newColorTestLogger()
	// With a module chip present, the header is bold only — the inverted chip
	// is the anchor, so a second reverse-video bar would be noise.
	logger.With("module", "child-0").With("module", "child-0").
		Info("operation deploy (1/2)", KeySection, true)

	got := buf.String()
	chip := "\033[7m" + colorModule + " child-0 " + "\033[0m" + " "
	// Depth 2: the section's leading blank stays flush; the chip line indents.
	want := "\n" + "  " + chip + "\033[1m" + "operation deploy (1/2)" + "\033[0m" + "\n"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestColorModeMarkerHighlighted(t *testing.T) {
	logger, buf := newColorTestLogger()
	logger.Info("dry-run: would write file")
	logger.Info("local-run: skipping PR creation")

	got := buf.String()
	for _, want := range []string{
		colorWarn + "dry-run:" + "\033[0m would write file\n",
		colorLocalRun + "local-run:" + "\033[0m skipping PR creation\n",
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
	want := "writing file\n  " + colorMuted + "path\033[0m  argocd/app.yaml\n"
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
