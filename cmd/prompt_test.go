package cmd

import (
	"bufio"
	"io"
	"strings"
	"testing"

	"github.com/rickliujh/loom/pkg/config"
)

// P8: a typed value is read and returned.
func TestStdinPrompter_ReadsValue(t *testing.T) {
	p := &stdinPrompter{r: bufio.NewReader(strings.NewReader("staging\n")), out: io.Discard}
	got, err := p.Prompt(config.ParamDef{Name: "env", Description: "Target environment"})
	if err != nil {
		t.Fatal(err)
	}
	if got != "staging" {
		t.Errorf("expected staging, got %q", got)
	}
}

// P8: a value without a trailing newline (EOF right after) is still returned.
func TestStdinPrompter_ValueWithoutTrailingNewline(t *testing.T) {
	p := &stdinPrompter{r: bufio.NewReader(strings.NewReader("prod")), out: io.Discard}
	got, err := p.Prompt(config.ParamDef{Name: "env"})
	if err != nil {
		t.Fatal(err)
	}
	if got != "prod" {
		t.Errorf("expected prod, got %q", got)
	}
}

// P8: EOF with no value fails fast instead of blocking.
func TestStdinPrompter_EOFErrors(t *testing.T) {
	p := &stdinPrompter{r: bufio.NewReader(strings.NewReader("")), out: io.Discard}
	_, err := p.Prompt(config.ParamDef{Name: "env"})
	if err == nil {
		t.Fatal("expected error on EOF with no input")
	}
}

// P8: successive prompts consume input in order from one reader.
func TestStdinPrompter_MultiplePrompts(t *testing.T) {
	p := &stdinPrompter{r: bufio.NewReader(strings.NewReader("one\ntwo\n")), out: io.Discard}
	first, err := p.Prompt(config.ParamDef{Name: "a"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := p.Prompt(config.ParamDef{Name: "b"})
	if err != nil {
		t.Fatal(err)
	}
	if first != "one" || second != "two" {
		t.Errorf("expected one/two, got %q/%q", first, second)
	}
}
