package template

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"text/template"
)

// noValue is what text/template prints for a missing map key or nil value.
const noValue = "<no value>"

// ErrMissingValue marks a render that printed a missing value. Missing keys
// are deliberately not an execution error — a nil field is falsy and works with
// default, with and if — so the damage only shows in the output, as the literal
// "<no value>". Written into a GitOps repo that is a broken manifest, so it
// fails the render instead (see checkMissing for when the literal is trusted).
var ErrMissingValue = errors.New("template printed a missing value")

// RenderString renders a Go template string with the given params.
func RenderString(tmplStr string, params map[string]any) (string, error) {
	out, err := render(tmplStr, params)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// RenderFile renders a template file's contents with the given params.
func RenderFile(content []byte, params map[string]any) ([]byte, error) {
	return render(string(content), params)
}

func render(src string, params map[string]any) ([]byte, error) {
	t, err := template.New("").Funcs(FuncMap()).Parse(src)
	if err != nil {
		return nil, fmt.Errorf("parsing template: %w", err)
	}

	var buf bytes.Buffer
	if err := t.Execute(&buf, params); err != nil {
		return nil, fmt.Errorf("executing template: %w", err)
	}

	out := buf.Bytes()
	if err := checkMissing(src, params, out); err != nil {
		return nil, err
	}
	return out, nil
}

// checkMissing fails a render whose output carries "<no value>", under one
// predictable rule: the guard stands down for the whole render when the
// literal could have come from somewhere other than a missing value — the
// template source, or any string (or map key) anywhere in the params. A
// template documenting Loom, or a PR body quoting the message, renders as
// written; otherwise any occurrence is a printed missing value.
func checkMissing(src string, params map[string]any, out []byte) error {
	i := bytes.Index(out, []byte(noValue))
	if i < 0 || strings.Contains(src, noValue) || carries(params, noValue) {
		return nil
	}
	line := 1 + bytes.Count(out[:i], []byte("\n"))
	return fmt.Errorf("%w: %q appears on output line %d; guard the field with default, required, if or with",
		ErrMissingValue, noValue, line)
}

// carries reports whether any string in v, at any depth, contains lit.
func carries(v any, lit string) bool {
	switch v := v.(type) {
	case string:
		return strings.Contains(v, lit)
	case []any:
		for _, e := range v {
			if carries(e, lit) {
				return true
			}
		}
	case map[string]any:
		for k, e := range v {
			if strings.Contains(k, lit) || carries(e, lit) {
				return true
			}
		}
	}
	return false
}
