// Package params turns the values a caller supplies — a params file, -p
// flags, a parent's spec.modules[].params, a dynamic param's output — into the
// typed values a module's templates see.
package params

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/rickliujh/loom/pkg/config"
	tmpl "github.com/rickliujh/loom/pkg/template"
)

// Zero is what a declared param of type t resolves to when nothing supplies
// it and it has no default: "", an empty list, or an empty map. Resolution
// always fills it in, so a template reading such a param prints nothing and
// ranges over nothing rather than printing "<no value>".
func Zero(t config.ParamType) any {
	switch t.Effective() {
	case config.ParamList:
		return []any{}
	case config.ParamMap:
		return map[string]any{}
	}
	return ""
}

// RenderLeaves renders every string in v, at any depth, as a template with
// params, and returns the result with the same shape. Map keys are not
// rendered, and scalars that are not strings pass through unchanged.
func RenderLeaves(v any, params map[string]any) (any, error) {
	return tmpl.MapStrings(v, "", func(path, s string) (string, error) {
		out, err := tmpl.RenderString(s, params)
		if err != nil && path != "" {
			return "", fmt.Errorf("%s: %w", path, err)
		}
		return out, err
	})
}

// Coerce converts a value supplied for a param to the param's declared type.
// The declared type drives parsing, so every source of values behaves alike:
//
//   - list or map, given that kind: used as is.
//   - list or map, given a string: the string is parsed as YAML (flow or
//     block) and must yield that kind. This is how -p sources='[...]', a
//     dynamic param's output, and a parent forwarding {{ .x | toYaml }} work.
//     An empty string, like null, is the empty list or map.
//   - string, given a scalar: its text. A number kept as its written form
//     keeps that form, so 1.10 stays 1.10.
//   - string, given a list or map: an error.
//
// Errors name the param, the declared type, and what was received.
func Coerce(name string, t config.ParamType, v any) (any, error) {
	t = t.Effective()
	if v == nil {
		return Zero(t), nil
	}
	if t == config.ParamString {
		if s, ok := scalarText(v); ok {
			return s, nil
		}
		return nil, fmt.Errorf("param %q is declared string, but received a %s", name, config.ShapeOf(v))
	}

	s, isString := v.(string)
	if !isString {
		if config.ShapeOf(v) == string(t) {
			return v, nil
		}
		return nil, fmt.Errorf("param %q is declared %s, but received %s", name, t, describe(v))
	}
	if s == "" {
		return Zero(t), nil
	}
	parsed, err := tmpl.DecodeYAML([]byte(s))
	if err != nil {
		return nil, fmt.Errorf("param %q is declared %s, but received a string that is not valid YAML (%v): %s", name, t, err, preview(s))
	}
	if parsed == nil {
		return Zero(t), nil
	}
	if config.ShapeOf(parsed) != string(t) {
		return nil, fmt.Errorf("param %q is declared %s, but received a string that parses as %s, not a %s: %s", name, t, describe(parsed), t, preview(s))
	}
	return parsed, nil
}

// scalarText is the text of a scalar value, as a template would print it.
func scalarText(v any) (string, bool) {
	switch v := v.(type) {
	case string:
		return v, true
	case tmpl.Number:
		return string(v), true
	case bool, int, int64, uint64, float64:
		return fmt.Sprint(v), true
	}
	return "", false
}

// describe names what a value is, for an error message.
func describe(v any) string {
	switch config.ShapeOf(v) {
	case string(config.ParamList):
		return "a list"
	case string(config.ParamMap):
		return "a map"
	}
	if _, ok := v.(string); ok {
		return "a string"
	}
	return fmt.Sprintf("a scalar (%v)", v)
}

// preview quotes the start of a value for an error message.
func preview(s string) string {
	const limit = 60
	if r := []rune(s); len(r) > limit {
		s = string(r[:limit]) + "…"
	}
	return fmt.Sprintf("%q", s)
}

// Parse merges a params file and key=value pairs into the values supplied
// for a module, the way loom run takes them: the file first, then each pair
// overriding it. Values are not yet coerced — that needs the module's
// declarations and happens when the module is loaded (module.Load).
func Parse(pairs []string, file string) (map[string]any, error) {
	result := make(map[string]any)
	if file != "" {
		fromFile, err := ParseFile(file)
		if err != nil {
			return nil, err
		}
		for k, v := range fromFile {
			result[k] = v
		}
	}
	cli, err := ParseCLI(pairs)
	if err != nil {
		return nil, err
	}
	for k, v := range cli {
		result[k] = v
	}
	return result, nil
}

// ParseFile reads a params file: a YAML mapping of param name to value.
// Values may be nested lists and maps; see ParseYAML.
func ParseFile(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading params file: %w", err)
	}
	return ParseYAML(data)
}

// ParseYAML parses params-file content. A top-level scalar is kept as the text
// written, exactly as a flat params file always read; lists and maps keep
// their scalars' written form (config.DecodeParamValues).
func ParseYAML(data []byte) (map[string]any, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parsing params file: %w", err)
	}
	m, err := config.DecodeParamValues(&doc)
	if err != nil {
		return nil, fmt.Errorf("parsing params file: %w", err)
	}
	return m, nil
}

// ParseCLI splits repeated key=value flags into a map. The value is
// everything after the first "=", kept as text; a list or map param parses it
// as YAML when the module is loaded (Coerce).
func ParseCLI(pairs []string) (map[string]string, error) {
	result := make(map[string]string, len(pairs))
	for _, p := range pairs {
		k, v, ok := strings.Cut(p, "=")
		if !ok {
			return nil, fmt.Errorf("invalid param format %q, expected key=value", p)
		}
		result[k] = v
	}
	return result, nil
}
