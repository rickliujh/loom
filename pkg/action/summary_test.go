package action

import (
	"strings"
	"testing"
)

func TestRunSummary_AddAndPrint(t *testing.T) {
	s := &RunSummary{}
	s.AddPR("onboard-payments", "Onboard payments", "https://github.com/org/repo/pull/12")
	s.AddPR("onboard-auth", "Onboard auth", "https://gitlab.com/org/repo/-/merge_requests/3")

	var b strings.Builder
	s.Print(&b)
	out := b.String()

	if !strings.Contains(out, "Pull/merge requests created (2):") {
		t.Errorf("missing header:\n%s", out)
	}
	if !strings.Contains(out, "  - Onboard payments (onboard-payments): https://github.com/org/repo/pull/12") {
		t.Errorf("missing github entry:\n%s", out)
	}
	if !strings.Contains(out, "  - Onboard auth (onboard-auth): https://gitlab.com/org/repo/-/merge_requests/3") {
		t.Errorf("missing gitlab entry:\n%s", out)
	}
}

func TestRunSummary_EmptyPrintsNothing(t *testing.T) {
	var b strings.Builder
	(&RunSummary{}).Print(&b)
	if b.Len() != 0 {
		t.Errorf("expected no output, got %q", b.String())
	}
}

func TestRunSummary_NilSafe(t *testing.T) {
	var s *RunSummary
	s.AddPR("m", "t", "u") // must not panic
	var b strings.Builder
	s.Print(&b)
	if b.Len() != 0 {
		t.Errorf("expected no output, got %q", b.String())
	}
}
