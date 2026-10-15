package skill

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"testing/fstest"
)

const fence = "```"

func fixture() fstest.MapFS {
	return fstest.MapFS{
		"docs/guide/ai-agents.md": {Data: []byte(strings.Join([]string{
			"# Using Loom with AI Agents",
			"",
			"See the [patch reference](/reference/op-patch#smp) and [Bulk Runs](/guide/bulk-runs).",
			"Spec on [GitHub](https://github.com/rickliujh/loom/blob/main/specs/module.md).",
			"A [dead link](/reference/nope) and a [same-page link](#golden-workflow).",
			"",
			"| Syntax | Meaning |",
			"|---|---|",
			"| <code v-pre>{{ .name }}</code> | a param |",
			"",
			fence + "yaml",
			"# not a heading",
			"link: [kept](/reference/op-patch)",
			"raw: <code v-pre>{{ .kept }}</code>",
			fence,
			"",
		}, "\n"))},
		"docs/guide/bulk-runs.md":    {Data: []byte("# Bulk Runs\n\nbody\n")},
		"docs/guide/llm.md":          {Data: []byte("# LLM-Powered Operations\n")},
		"docs/guide/notes.txt":       {Data: []byte("not markdown")},
		"docs/reference/op-patch.md": {Data: []byte("# patch\n\n## Strategic Merge Patch {#smp}\n\nSee [newFiles](/reference/op-newfiles).\n")},
		"docs/reference/op-newfiles.md": {Data: []byte(
			fence + "bash\n# a comment, not the title\n" + fence + "\n\n# newFiles\n")},
		"specs/llm.md":    {Data: []byte("# LLM Spec\n")},
		"specs/module.md": {Data: []byte("# Module Spec\n\nMerge rules live in [smp.md](smp.md) and [missing](gone.md).\n")},
		"specs/smp.md":    {Data: []byte("# SMP Spec\n")},
	}
}

func load(t *testing.T) *Library {
	t.Helper()
	lib, err := Load(fixture())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return lib
}

func TestGuide_SK1_GuideThenIndex(t *testing.T) {
	out, err := load(t).Guide("v1.2.3")
	if err != nil {
		t.Fatal(err)
	}

	if !strings.HasPrefix(out, "<!-- loom v1.2.3 ") {
		t.Errorf("guide should open by naming the version it describes, got: %q", firstLine(out))
	}
	guideAt := strings.Index(out, "# Using Loom with AI Agents")
	indexAt := strings.Index(out, "## More Topics")
	if guideAt < 0 || indexAt < 0 || guideAt > indexAt {
		t.Fatalf("expected the guide followed by the topic index, got:\n%s", out)
	}

	index := out[indexAt:]
	for _, want := range []string{"- `guide/bulk-runs` — Bulk Runs", "- `reference/op-patch` — patch", "- `spec/smp` — SMP Spec"} {
		if !strings.Contains(index, want) {
			t.Errorf("index is missing %q", want)
		}
	}
	if strings.Contains(index, "`guide/ai-agents`") {
		t.Error("index should not list the guide it is appended to")
	}
}

func TestLoad_SK1_MissingGuideIsAnError(t *testing.T) {
	fsys := fixture()
	delete(fsys, "docs/guide/ai-agents.md")
	if _, err := Load(fsys); err == nil || !strings.Contains(err.Error(), GuideTopic) {
		t.Fatalf("expected an error naming the missing guide, got: %v", err)
	}
}

func TestTopics_SK3_NamespacedSortedAndTitled(t *testing.T) {
	got := load(t).Topics()

	var names []string
	for _, tp := range got {
		names = append(names, tp.Name)
	}
	want := []string{
		"guide/ai-agents", "guide/bulk-runs", "guide/llm",
		"reference/op-newfiles", "reference/op-patch",
		"spec/llm", "spec/module", "spec/smp",
	}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Fatalf("topics:\n got %v\nwant %v", names, want)
	}

	titles := map[string]string{}
	for _, tp := range got {
		titles[tp.Name] = tp.Title
	}
	// The explicit heading id is site markup, not part of the title.
	if titles["reference/op-patch"] != "patch" {
		t.Errorf("title of op-patch: got %q", titles["reference/op-patch"])
	}
	// A "# comment" inside a code block is not the document's heading.
	if titles["reference/op-newfiles"] != "newFiles" {
		t.Errorf("title of op-newfiles: got %q", titles["reference/op-newfiles"])
	}
}

func TestResolve_SK4_Names(t *testing.T) {
	lib := load(t)

	for in, want := range map[string]string{
		"reference/op-patch":    "reference/op-patch",
		"op-patch":              "reference/op-patch",
		"reference/op-patch.md": "reference/op-patch",
		"/spec/smp/":            "spec/smp",
		"bulk-runs":             "guide/bulk-runs",
	} {
		got, err := lib.Resolve(in)
		if err != nil {
			t.Errorf("Resolve(%q): %v", in, err)
			continue
		}
		if got.Name != want {
			t.Errorf("Resolve(%q) = %q, want %q", in, got.Name, want)
		}
	}
}

func TestResolve_SK4_AmbiguousAndUnknown(t *testing.T) {
	lib := load(t)

	_, err := lib.Resolve("llm")
	if err == nil || !strings.Contains(err.Error(), `topic "llm" is ambiguous: use one of guide/llm, spec/llm`) {
		t.Errorf("ambiguous name: got %v", err)
	}

	_, err = lib.Resolve("nope")
	if err == nil || !strings.Contains(err.Error(), `unknown topic "nope"`) || !strings.Contains(err.Error(), "loom skill list") {
		t.Errorf("unknown name: got %v", err)
	}
}

func TestRead_SK5_RewritesSiteMarkup(t *testing.T) {
	lib := load(t)
	out, err := lib.Read(GuideTopic)
	if err != nil {
		t.Fatal(err)
	}

	for _, want := range []string{
		// A site link becomes the command that prints its destination; the
		// anchor is dropped, since a topic prints whole.
		"See the patch reference (`loom skill reference/op-patch`) and Bulk Runs (`loom skill guide/bulk-runs`).",
		// A link out of the documentation is left alone.
		"[GitHub](https://github.com/rickliujh/loom/blob/main/specs/module.md)",
		// So is one with no topic behind it, and a same-page anchor.
		"[dead link](/reference/nope)",
		"[same-page link](#golden-workflow)",
		// The site's brace-escaping wrapper becomes plain inline code.
		"| `{{ .name }}` | a param |",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("rendered guide is missing %q\n---\n%s", want, out)
		}
	}
}

func TestRead_SK5_CodeBlocksUntouched(t *testing.T) {
	out, err := load(t).Read(GuideTopic)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"link: [kept](/reference/op-patch)",
		"raw: <code v-pre>{{ .kept }}</code>",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("code block content was rewritten; missing %q", want)
		}
	}
}

func TestRead_SK5_HeadingAnchorsAndRelativeLinks(t *testing.T) {
	lib := load(t)

	patch, err := lib.Read("reference/op-patch")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(patch, "## Strategic Merge Patch\n") || strings.Contains(patch, "{#smp}") {
		t.Errorf("heading id should be removed, got:\n%s", patch)
	}

	module, err := lib.Read("spec/module")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(module, "smp.md (`loom skill spec/smp`)") {
		t.Errorf("a relative link should resolve within its own namespace, got:\n%s", module)
	}
	if !strings.Contains(module, "[missing](gone.md)") {
		t.Errorf("a relative link with no topic behind it should be left alone, got:\n%s", module)
	}
}

// ---------------------------------------------------------------------------
// The real documentation.
// ---------------------------------------------------------------------------

func repoDocs(t *testing.T) *Library {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate source file")
	}
	root := filepath.Dir(filepath.Dir(filepath.Dir(file)))
	lib, err := Load(os.DirFS(root))
	if err != nil {
		t.Fatalf("loading the repository's documentation: %v", err)
	}
	return lib
}

// Every topic must render free of site-only markup, and every link between
// documents must lead to a topic — a link left as "/reference/x" is a
// reference the reader of the binary cannot follow.
func TestRepoDocs_SK5_EveryTopicRendersClean(t *testing.T) {
	lib := repoDocs(t)
	siteLink := regexp.MustCompile(`\]\(/[^)]*\)`)

	for _, tp := range lib.Topics() {
		if tp.Title == "" {
			t.Errorf("%s: no level-one heading to use as a title", tp.Name)
		}
		out, err := lib.Read(tp.Name)
		if err != nil {
			t.Errorf("%s: %v", tp.Name, err)
			continue
		}
		eachLine(out, func(line string, fenced bool) {
			if fenced {
				return
			}
			if strings.Contains(line, "<code v-pre>") {
				t.Errorf("%s: site markup left in: %s", tp.Name, line)
			}
			if m := siteLink.FindString(line); m != "" {
				t.Errorf("%s: link to a page that is not a topic: %s", tp.Name, m)
			}
		})
	}
}

func TestRepoDocs_SK1_GuideNamesTheCommand(t *testing.T) {
	out, err := repoDocs(t).Guide("test")
	if err != nil {
		t.Fatal(err)
	}
	// The guide is where an agent learns the workflow; it must at least hold
	// the steps that keep a run from being the first thing tried.
	for _, want := range []string{"loom validate", "loom diff", "--local-run", "loom skill"} {
		if !strings.Contains(out, want) {
			t.Errorf("agent guide does not mention %q", want)
		}
	}
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return line
}
