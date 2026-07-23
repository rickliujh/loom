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

func TestDiffHeader_BulkTurnBannerNamesRootAndItem(t *testing.T) {
	// Two segments = the orchestrator fanned out: the root becomes an inverted
	// "≡ … ≡" turn banner and the item's instance name follows it, so two items
	// with the same metadata name are told apart. No submodule, so no hand-off.
	got := DiffHeader([]string{"deps-bump", "patch-go-mod-service-a"}, "github.com/acme/service-a (main)", false)
	want := "\n[≡ deps-bump ≡] patch-go-mod-service-a\ngithub.com/acme/service-a (main)\n"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestDiffHeader_SubmoduleGetsRootFreeHandoff(t *testing.T) {
	// Three segments = a submodule under a bulk turn: the turn banner names the
	// root and item, then a "▸ item › submodule" hand-off with no root chip.
	got := DiffHeader([]string{"bulk-deploy-k8s", "deploy-k8s-2", "autocert"}, "acme/cluster-2 (main)", false)
	want := "\n[≡ bulk-deploy-k8s ≡] deploy-k8s-2\n" +
		"▸ deploy-k8s-2 › autocert\n" +
		"acme/cluster-2 (main)\n"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestDiffHeader_ColorTurnBannerAndHandoff(t *testing.T) {
	got := DiffHeader([]string{"deps-bump", "service-a", "autocert"}, "acme/service-a", true)
	want := "\n" +
		diffColorInvert + diffColorRoot + diffColorBold + " ≡ deps-bump ≡ " + diffColorReset +
		" " + diffColorBold + "service-a" + diffColorReset + "\n" +
		diffColorWorker + "▸ service-a › " + diffColorReset +
		diffColorBold + "autocert" + diffColorReset + "\n" +
		diffColorMuted + "acme/service-a" + diffColorReset + "\n"
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
	// Defensive: empty breadcrumb segments must not produce an empty chip or a
	// stray "▸ › " step. ["root", "", "leaf"] collapses to a two-segment turn.
	got := DiffHeader([]string{"root", "", "leaf"}, "", false)
	want := "\n[≡ root ≡] leaf\n"
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

func TestDiffCollector_BannerOncePerTurnHandoffPerSubmodule(t *testing.T) {
	// One turn with two submodules, then a second turn: the root chip banner
	// prints once per turn, the "▸" hand-off once per submodule, and the root is
	// never repeated inside a turn.
	c := &DiffCollector{}
	c.Add([]string{"bulk-deploy", "deploy-1", "autocert"}, "cluster-1", "--- a\n+++ b\n")
	c.Add([]string{"bulk-deploy", "deploy-1", "ingress"}, "cluster-1", "--- c\n+++ d\n")
	c.Add([]string{"bulk-deploy", "deploy-2", "autocert"}, "cluster-2", "--- e\n+++ f\n")

	var b bytes.Buffer
	c.Print(&b)
	out := b.String()

	if n := strings.Count(out, "≡ bulk-deploy ≡"); n != 2 {
		t.Errorf("expected root banner once per turn (2), got %d:\n%s", n, out)
	}
	if n := strings.Count(out, "deploy-1"); n != 3 { // banner + two hand-offs name the turn
		t.Errorf("expected deploy-1 named 3 times, got %d:\n%s", n, out)
	}
	for _, want := range []string{
		"▸ deploy-1 › autocert", "▸ deploy-1 › ingress", "▸ deploy-2 › autocert",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("expected hand-off %q, got:\n%s", want, out)
		}
	}
	// Every header block is set off from the diff above it by a blank line, so a
	// breadcrumb between two diffs stays visible — the within-turn submodule too.
	if !strings.Contains(out, "\n\n▸ deploy-1 › ingress") {
		t.Errorf("expected a blank line before the within-turn submodule header, got:\n%s", out)
	}
}
