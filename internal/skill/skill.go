// Package skill serves loom's own documentation to an AI agent straight from
// the binary, so an agent can learn the tool without anything being installed
// and always reads the docs that match the version it is about to run.
package skill

import (
	"bufio"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"sort"
	"strings"
)

// GuideTopic is the entry point: the document printed when no topic is named.
const GuideTopic = "guide/ai-agents"

// sections maps a directory in the documentation tree to the topic namespace
// it is published under. The namespaces mirror the docs site's URLs, which is
// what lets a site link be rewritten into a topic name (SK5).
var sections = []struct{ dir, namespace string }{
	{"docs/guide", "guide"},
	{"docs/reference", "reference"},
	{"specs", "spec"},
}

// Topic is one document an agent can ask for by name.
type Topic struct {
	// Name is the namespaced identifier, e.g. "reference/op-patch".
	Name string
	// Title is the document's first heading.
	Title string

	file string
}

// Library is the set of documents read from a documentation tree.
type Library struct {
	fsys   fs.FS
	topics []Topic
	byName map[string]Topic
}

// Load indexes the documentation tree rooted at fsys, which holds docs/guide,
// docs/reference and specs as they sit in the repository.
func Load(fsys fs.FS) (*Library, error) {
	lib := &Library{fsys: fsys, byName: map[string]Topic{}}
	for _, s := range sections {
		entries, err := fs.ReadDir(fsys, s.dir)
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", s.dir, err)
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
				continue
			}
			file := path.Join(s.dir, e.Name())
			raw, err := fs.ReadFile(fsys, file)
			if err != nil {
				return nil, fmt.Errorf("reading %s: %w", file, err)
			}
			t := Topic{
				Name:  s.namespace + "/" + strings.TrimSuffix(e.Name(), ".md"),
				Title: title(string(raw)),
				file:  file,
			}
			lib.topics = append(lib.topics, t)
			lib.byName[t.Name] = t
		}
	}
	if _, ok := lib.byName[GuideTopic]; !ok {
		return nil, fmt.Errorf("documentation is missing the agent guide %q", GuideTopic)
	}
	sort.Slice(lib.topics, func(i, j int) bool { return lib.topics[i].Name < lib.topics[j].Name })
	return lib, nil
}

// Topics returns every topic, sorted by name.
func (l *Library) Topics() []Topic {
	return append([]Topic(nil), l.topics...)
}

// Resolve finds the topic a name refers to. A bare name ("op-patch") is
// accepted when exactly one namespace holds it; agents drop the namespace
// often enough that refusing would cost a round trip for no safety gained.
func (l *Library) Resolve(name string) (Topic, error) {
	name = strings.TrimSuffix(strings.Trim(name, "/"), ".md")
	if t, ok := l.byName[name]; ok {
		return t, nil
	}
	var matches []string
	for _, t := range l.topics {
		if path.Base(t.Name) == name {
			matches = append(matches, t.Name)
		}
	}
	switch len(matches) {
	case 1:
		return l.byName[matches[0]], nil
	case 0:
		return Topic{}, fmt.Errorf("unknown topic %q: run \"loom skill list\" to see the topics", name)
	default:
		return Topic{}, fmt.Errorf("topic %q is ambiguous: use one of %s", name, strings.Join(matches, ", "))
	}
}

// Read returns a topic's document, rendered for a reader that has the binary
// but not the docs site (SK5).
func (l *Library) Read(name string) (string, error) {
	t, err := l.Resolve(name)
	if err != nil {
		return "", err
	}
	raw, err := fs.ReadFile(l.fsys, t.file)
	if err != nil {
		return "", fmt.Errorf("reading %s: %w", t.file, err)
	}
	return l.render(string(raw), t), nil
}

// Guide returns the agent guide followed by the index of every other topic,
// so a single call tells an agent both how to work and what else it can read.
func (l *Library) Guide(version string) (string, error) {
	body, err := l.Read(GuideTopic)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "<!-- loom %s — this guide ships inside the binary and describes this version -->\n\n", version)
	b.WriteString(strings.TrimRight(body, "\n"))
	b.WriteString("\n\n## More Topics\n\n")
	b.WriteString("Each of these prints with `loom skill <topic>`. Read one when the task needs it rather than all of them up front.\n\n")
	b.WriteString(l.Index(GuideTopic))
	return b.String(), nil
}

// Index lists the topics as a Markdown list, leaving out skip.
func (l *Library) Index(skip string) string {
	var b strings.Builder
	for _, t := range l.topics {
		if t.Name == skip {
			continue
		}
		fmt.Fprintf(&b, "- `%s` — %s\n", t.Name, t.Title)
	}
	return b.String()
}

var (
	headingRe = regexp.MustCompile(`(?m)^#\s+(.+?)\s*$`)
	// vPreRe matches the wrapper the docs site needs around template braces in
	// prose, so its renderer does not try to evaluate them.
	vPreRe = regexp.MustCompile(`<code v-pre>(.*?)</code>`)
	// anchorRe matches an explicit heading id, e.g. "## JSON6902 {#json6902}".
	anchorRe = regexp.MustCompile(`\s*\{#[A-Za-z0-9_-]+\}\s*$`)
	// linkRe matches a Markdown link. Images are excluded by the caller.
	linkRe = regexp.MustCompile(`\[([^\]]*)\]\(([^)\s]+)\)`)
)

// title returns a document's first level-one heading, or "" if it has none.
func title(doc string) string {
	m := headingRe.FindStringSubmatch(stripFences(doc))
	if m == nil {
		return ""
	}
	return strings.TrimSpace(anchorRe.ReplaceAllString(m[1], ""))
}

// stripFences blanks out fenced code blocks, where a "# comment" line would
// otherwise read as a heading.
func stripFences(doc string) string {
	var b strings.Builder
	eachLine(doc, func(line string, fenced bool) {
		if !fenced {
			b.WriteString(line)
		}
		b.WriteByte('\n')
	})
	return b.String()
}

// render rewrites what only makes sense on the docs site. Fenced code blocks
// pass through untouched: they are examples to copy, and an example may
// legitimately contain anything the rewrites look for.
func (l *Library) render(doc string, from Topic) string {
	var b strings.Builder
	eachLine(doc, func(line string, fenced bool) {
		if !fenced {
			line = vPreRe.ReplaceAllString(line, "`$1`")
			if strings.HasPrefix(line, "#") {
				line = anchorRe.ReplaceAllString(line, "")
			}
			line = linkRe.ReplaceAllStringFunc(line, func(m string) string {
				return l.rewriteLink(m, from)
			})
		}
		b.WriteString(line)
		b.WriteByte('\n')
	})
	out := b.String()
	if !strings.HasSuffix(doc, "\n") {
		out = strings.TrimSuffix(out, "\n")
	}
	return out
}

// rewriteLink turns a link to another document into the command that prints
// it. A link that leaves the documentation — a URL, a same-page anchor — is
// returned unchanged, and so is one whose destination is not a topic: a wrong
// command would be worse than a link the reader cannot follow.
func (l *Library) rewriteLink(link string, from Topic) string {
	m := linkRe.FindStringSubmatch(link)
	text, dest := m[1], m[2]
	if strings.Contains(dest, "://") || strings.HasPrefix(dest, "#") || strings.HasPrefix(dest, "mailto:") {
		return link
	}
	dest, _, _ = strings.Cut(dest, "#")

	var name string
	if strings.HasPrefix(dest, "/") {
		// Site-absolute: the URL path is the topic name.
		name = strings.Trim(dest, "/")
	} else {
		// Relative: a sibling document in the same namespace.
		name = path.Join(path.Dir(from.Name), strings.TrimSuffix(dest, ".md"))
	}
	t, ok := l.byName[strings.TrimSuffix(name, ".md")]
	if !ok {
		return link
	}
	return fmt.Sprintf("%s (`loom skill %s`)", text, t.Name)
}

// eachLine calls fn for every line of doc, reporting whether the line belongs
// to a fenced code block. The fence lines themselves count as fenced.
func eachLine(doc string, fn func(line string, fenced bool)) {
	sc := bufio.NewScanner(strings.NewReader(doc))
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	var fence string
	for sc.Scan() {
		line := sc.Text()
		trimmed := strings.TrimLeft(line, " ")
		switch {
		case fence == "" && (strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~")):
			fence = trimmed[:3]
			fn(line, true)
		case fence != "":
			fn(line, true)
			if strings.HasPrefix(trimmed, fence) {
				fence = ""
			}
		default:
			fn(line, false)
		}
	}
}
