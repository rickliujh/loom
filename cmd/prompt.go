package cmd

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/rickliujh/loom/pkg/config"
)

// stdinPrompter reads values for missing required params from stdin. Prompt
// text goes to stderr so it never pollutes stdout. The bufio.Reader is created
// once so buffered input survives across successive prompts (e.g. multiple
// values piped in). On EOF with no value, Prompt returns an error rather than
// blocking, so `--interactive` in a non-TTY context fails fast.
type stdinPrompter struct {
	r   *bufio.Reader
	out io.Writer
}

func newStdinPrompter() *stdinPrompter {
	return &stdinPrompter{r: bufio.NewReader(os.Stdin), out: os.Stderr}
}

func (p *stdinPrompter) Prompt(param config.ParamDef) (string, error) {
	label := param.Name
	if param.Description != "" {
		label = fmt.Sprintf("%s — %s", param.Name, param.Description)
	}
	fmt.Fprintf(p.out, "Enter value for %s: ", label)

	line, err := p.r.ReadString('\n')
	if err != nil && err != io.EOF {
		return "", err
	}
	line = strings.TrimRight(line, "\r\n")
	if line == "" && err == io.EOF {
		return "", fmt.Errorf("no input for required parameter %q", param.Name)
	}
	return line, nil
}
