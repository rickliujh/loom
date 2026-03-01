package template

import (
	"bytes"
	"fmt"
	"text/template"
)

// RenderString renders a Go template string with the given params.
func RenderString(tmplStr string, params map[string]string) (string, error) {
	t, err := template.New("").Funcs(FuncMap()).Parse(tmplStr)
	if err != nil {
		return "", fmt.Errorf("parsing template: %w", err)
	}

	var buf bytes.Buffer
	if err := t.Execute(&buf, params); err != nil {
		return "", fmt.Errorf("executing template: %w", err)
	}

	return buf.String(), nil
}

// RenderFile renders a template file's contents with the given params.
func RenderFile(content []byte, params map[string]string) ([]byte, error) {
	t, err := template.New("").Funcs(FuncMap()).Parse(string(content))
	if err != nil {
		return nil, fmt.Errorf("parsing template: %w", err)
	}

	var buf bytes.Buffer
	if err := t.Execute(&buf, params); err != nil {
		return nil, fmt.Errorf("executing template: %w", err)
	}

	return buf.Bytes(), nil
}
