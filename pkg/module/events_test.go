package module

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	prettylog "github.com/rickliujh/loom/internal/log"
	"github.com/rickliujh/loom/pkg/action"
	"github.com/rickliujh/loom/pkg/event"
)

// eventFixture is a parent composing one child module twice (the second time
// behind a false `if`), with an operation skipped by its `if` and one that
// fails. The child renders one file, so a quick diff emits a diff.file.
func eventFixture(t *testing.T) string {
	t.Helper()
	parent := t.TempDir()
	child := filepath.Join(parent, "child")
	if err := os.MkdirAll(filepath.Join(child, "tpl"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(child, "tpl", "app.txt"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeLoomYAML(t, child, `
apiVersion: loom.rickliujh.github.io/v1beta1
kind: Loom
metadata:
  name: child-mod
spec:
  operations:
    - name: render
      newFiles:
        source: tpl
        dest: out
`)
	if err := os.MkdirAll(filepath.Join(parent, "patches"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(parent, "patches", "p.yaml"), []byte("a: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeLoomYAML(t, parent, `
apiVersion: loom.rickliujh.github.io/v1beta1
kind: Loom
metadata:
  name: parent
spec:
  excludes: [child, patches]
  modules:
    - name: kid
      source: ./child
    - name: quiet-kid
      source: ./child
      if: "false"
  operations:
    - name: maybe
      if: "false"
      shell:
        command: "echo never"
    - name: broken
      patch:
        path: patches/p.yaml
        target: missing.yaml
`)
	return parent
}

// describe renders an event as one comparable line: its type, path, and the
// fields that event type carries (durations and times excluded).
func describe(e event.Event) string {
	s := fmt.Sprintf("%s %s", e.Type, strings.Join(e.Path, "/"))
	switch e.Type {
	case event.ModuleStart, event.ModuleEnd, event.ModuleSkip:
		s += " module=" + e.Module
	case event.OpStart, event.OpEnd, event.OpSkip:
		s += fmt.Sprintf(" op=%s kind=%s %d/%d", e.Op, e.Kind, e.Index, e.Total)
	case event.DiffFile:
		s += fmt.Sprintf(" target=%s file=%s status=%s", e.Target, e.Diff.Path, e.Diff.Status)
	case event.Log:
		s += " " + e.Log.Level + " " + e.Log.Msg
		for _, f := range []struct {
			on   bool
			name string
		}{{e.Log.Section, "section"}, {e.Log.Dispatch, "dispatch"}, {e.Log.Root, "root"}} {
			if f.on {
				s += " +" + f.name
			}
		}
	}
	if e.Reason != "" {
		s += " reason=" + e.Reason
	}
	if e.Error != "" {
		s += " error"
	}
	return s
}

// A quick-diff run of the fixture emits exactly this sequence: each module and
// operation that runs is bracketed by start and end, a skipped one yields a
// lone skip, the failing operation's end carries its error up through every
// module's end, and every log event is attributed to the module breadcrumb the
// executor was running — the same path its lifecycle events carry.
func TestExecute_EventSequence(t *testing.T) {
	var events []event.Event
	sink := event.Sink(func(e event.Event) { events = append(events, e) })
	logger := slog.New(prettylog.NewEventHandler(sink, slog.LevelInfo))

	mod, err := Load(eventFixture(t), nil, logger)
	if err != nil {
		t.Fatal(err)
	}
	target := t.TempDir()
	err = Execute(context.Background(), mod, target, RunOptions{
		DryRun: true, ShowDiff: true, Diffs: &action.DiffCollector{}, Events: sink,
	})
	if err == nil || !strings.Contains(err.Error(), `operation "broken" failed`) {
		t.Fatalf("err = %v, want the broken operation's failure", err)
	}

	var got []string
	for _, e := range events {
		got = append(got, describe(e))
	}
	want := []string{
		"module.start parent module=parent",
		"log parent INFO kid (1/2) +section +dispatch +root",
		"module.start parent/kid module=child-mod",
		"log parent/kid INFO operation render (1/1) +section",
		"op.start parent/kid op=render kind=newFiles 1/1",
		"log parent/kid INFO dry-run: would write file",
		"diff.file parent/kid target=" + target + " file=out/app.txt status=added",
		"op.end parent/kid op=render kind=newFiles 1/1",
		"module.end parent/kid module=child-mod",
		"log parent INFO quiet-kid (2/2) +section +dispatch +root",
		"log parent/quiet-kid INFO skipping module (if condition false)",
		"module.skip parent/quiet-kid module=child-mod reason=if condition false",
		"log parent INFO operation maybe (1/2) +section +root",
		"log parent INFO skipping operation (if condition false) +root",
		"op.skip parent op=maybe kind=shell 1/2 reason=if condition false",
		"log parent INFO operation broken (2/2) +section +root",
		"op.start parent op=broken kind=patch 2/2",
		"log parent INFO dry-run: would apply patch +root",
		"op.end parent op=broken kind=patch 2/2 error",
		"module.end parent module=parent error",
	}
	if !slices.Equal(got, want) {
		t.Errorf("event sequence:\n got:\n  %s\nwant:\n  %s", strings.Join(got, "\n  "), strings.Join(want, "\n  "))
	}

	// Every event of a module that ran carries the path the executor gave it
	// (its ModulePath), and that module's actions log through a logger whose
	// breadcrumb the log handler reads back identically.
	for _, e := range events {
		if e.Type == event.Log && e.Log.Msg == "dry-run: would write file" {
			if !slices.Equal(e.Path, []string{"parent", "kid"}) {
				t.Errorf("action log path = %v, want [parent kid]", e.Path)
			}
			if !slices.Equal(e.Log.Attrs, []event.Attr{{Key: "path", Value: "out/app.txt"}, {Key: "bytes", Value: "6"}}) {
				t.Errorf("action log attrs = %+v", e.Log.Attrs)
			}
		}
		if e.Time.IsZero() {
			t.Errorf("%s has no time", describe(e))
		}
	}
	end := events[len(events)-1]
	if !strings.Contains(end.Error, `operation "broken" failed`) {
		t.Errorf("root module.end error = %q", end.Error)
	}
}
