package template

import (
	"strconv"
	"strings"
	"text/template"

	"gopkg.in/yaml.v3"
)

// FuncMap returns custom template functions available in loom templates.
func FuncMap() template.FuncMap {
	return template.FuncMap{
		"default": func(def, val string) string {
			if val == "" {
				return def
			}
			return val
		},
		"upper":   strings.ToUpper,
		"lower":   strings.ToLower,
		"indent":  indent,
		"nindent": nindent,
		"quote":   strconv.Quote,
		"toYaml":  toYaml,
	}
}

// indent prefixes every line of s with n spaces.
func indent(n int, s string) string {
	pad := strings.Repeat(" ", n)
	return pad + strings.ReplaceAll(s, "\n", "\n"+pad)
}

// nindent is indent with a leading newline, so a value can be placed
// after a key on the same line: "config: {{ .param | nindent 4 }}".
func nindent(n int, s string) string {
	return "\n" + indent(n, s)
}

// toYaml marshals a value to 2-space-indented YAML, without a trailing newline.
func toYaml(v any) (string, error) {
	var buf strings.Builder
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(v); err != nil {
		return "", err
	}
	if err := enc.Close(); err != nil {
		return "", err
	}
	return strings.TrimSuffix(buf.String(), "\n"), nil
}
