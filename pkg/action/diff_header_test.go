package action

import (
	"bytes"
	"strings"
	"testing"
)

func TestDiffHeader_SingleModuleIsPlainChip(t *testing.T) {
	// A non-bulk run has a one-segment breadcrumb: no "≡" markers, target on its
	// own line beneath the chip.
	got := DiffHeader([]string{"demo"}, "github.com/acme/demo (main)", false)
	want := "\n[demo]\ngithub.com/acme/demo (main)\n"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestDiffHeader_BulkItemShowsRootMarkerAndCrumb(t *testing.T) {
	// Two segments = the orchestrator fanned out: the root is wrapped in "≡ … ≡"
	// and the item's instance name trails after it, so two items with the same
	// metadata name are told apart.
	got := DiffHeader([]string{"deps-bump", "patch-go-mod-service-a"}, "github.com/acme/service-a (main)", false)
	want := "\n[≡ deps-bump ≡] › patch-go-mod-service-a\ngithub.com/acme/service-a (main)\n"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestDiffHeader_ColorRootChipThenMutedCrumb(t *testing.T) {
	got := DiffHeader([]string{"deps-bump", "service-a"}, "acme/service-a", true)
	want := "\n" +
		diffColorInvert + " ≡ deps-bump ≡ " + diffColorReset +
		diffColorMuted + " › service-a" + diffColorReset +
		"\n" + diffColorMuted + "acme/service-a" + diffColorReset + "\n"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestDiffHeader_EmptyIsBlankLine(t *testing.T) {
	if got := DiffHeader(nil, "", false); got != "\n" {
		t.Errorf("got %q, want %q", got, "\n")
	}
}

func TestDiffHeader_SkipsEmptySegments(t *testing.T) {
	// Defensive: empty breadcrumb segments must not produce empty "› " steps.
	got := DiffHeader([]string{"root", "", "leaf"}, "", false)
	want := "\n[≡ root ≡] › leaf\n"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestDiffCollector_HeaderDedupedPerBreadcrumbAndTarget(t *testing.T) {
	c := &DiffCollector{}
	c.Add([]string{"bulk", "item-a"}, "repo-a", "--- a\n+++ b\n")
	c.Add([]string{"bulk", "item-a"}, "repo-a", "--- c\n+++ d\n") // same header, no repeat
	c.Add([]string{"bulk", "item-b"}, "repo-b", "--- e\n+++ f\n") // new header

	var b bytes.Buffer
	c.Print(&b)
	out := b.String()

	if n := strings.Count(out, "item-a"); n != 1 {
		t.Errorf("expected item-a header once, got %d:\n%s", n, out)
	}
	if !strings.Contains(out, "item-b") || !strings.Contains(out, "repo-b") {
		t.Errorf("expected item-b header, got:\n%s", out)
	}
}
