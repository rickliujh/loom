package cmd

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

// runSkillCmd runs `loom skill <args>` against the repository's own
// documentation and returns what it wrote to stdout and stderr.
func runSkillCmd(t *testing.T, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	resetFlags()

	prev := Docs
	Docs = os.DirFS("..")
	t.Cleanup(func() { Docs = prev })

	var out, errOut bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&errOut)
	t.Cleanup(func() {
		rootCmd.SetOut(nil)
		rootCmd.SetErr(nil)
	})

	rootCmd.SetArgs(append([]string{"skill"}, args...))
	err = rootCmd.Execute()
	return out.String(), errOut.String(), err
}

func TestSkill_SK1_PrintsGuideToStdout(t *testing.T) {
	stdout, stderr, err := runSkillCmd(t)
	if err != nil {
		t.Fatal(err)
	}
	if stderr != "" {
		t.Errorf("expected nothing on stderr, got: %q", stderr)
	}
	for _, want := range []string{"# Using Loom with AI Agents", "## Golden Workflow", "## More Topics", "`reference/loom-yaml`"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("output is missing %q", want)
		}
	}
	if !strings.HasSuffix(stdout, "\n") || strings.HasSuffix(stdout, "\n\n") {
		t.Errorf("output should end with exactly one newline, got %q", stdout[len(stdout)-10:])
	}
}

// The command must work with no module, no network and no files of its own:
// everything it prints comes from the documentation it was given.
func TestSkill_SK2_NeedsNothingFromTheWorkingDirectory(t *testing.T) {
	want, _, err := runSkillCmd(t)
	if err != nil {
		t.Fatal(err)
	}

	// Docs was resolved relative to the package directory above; pin it to an
	// absolute path before moving, so only the working directory changes.
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Chdir(t.TempDir())

	resetFlags()
	prev := Docs
	Docs = os.DirFS(wd + "/..")
	t.Cleanup(func() { Docs = prev })

	var out bytes.Buffer
	rootCmd.SetOut(&out)
	t.Cleanup(func() { rootCmd.SetOut(nil) })
	rootCmd.SetArgs([]string{"skill"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if out.String() != want {
		t.Error("output changed with the working directory")
	}
}

func TestSkill_SK2_BuildWithoutDocsSaysSo(t *testing.T) {
	resetFlags()
	prev := Docs
	Docs = nil
	t.Cleanup(func() { Docs = prev })

	rootCmd.SetArgs([]string{"skill"})
	err := rootCmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "does not include its documentation") {
		t.Fatalf("expected an error about the missing documentation, got: %v", err)
	}
}

func TestSkill_SK3_List(t *testing.T) {
	stdout, _, err := runSkillCmd(t, "list")
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(stdout, "\n"), "\n")
	for _, line := range lines {
		if !strings.HasPrefix(line, "- `") {
			t.Fatalf("list should print topics only, got line: %q", line)
		}
	}
	for _, want := range []string{"- `guide/ai-agents` — ", "- `reference/op-patch` — ", "- `spec/module` — "} {
		if !strings.Contains(stdout, want) {
			t.Errorf("list is missing %q", want)
		}
	}
}

func TestSkill_SK4_Topic(t *testing.T) {
	full, _, err := runSkillCmd(t, "reference/op-patch")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(full, "# patch\n") {
		t.Errorf("expected the patch reference, got: %q", strings.SplitN(full, "\n", 2)[0])
	}

	bare, _, err := runSkillCmd(t, "op-patch")
	if err != nil {
		t.Fatal(err)
	}
	if bare != full {
		t.Error("a bare topic name should print the same document as its full name")
	}
}

func TestSkill_SK4_UnknownTopic(t *testing.T) {
	stdout, _, err := runSkillCmd(t, "no-such-topic")
	if err == nil || !strings.Contains(err.Error(), `unknown topic "no-such-topic"`) {
		t.Fatalf("expected an unknown-topic error, got: %v", err)
	}
	if stdout != "" {
		t.Errorf("nothing should be printed for an unknown topic, got: %q", stdout)
	}
}
