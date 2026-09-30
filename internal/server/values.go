package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	tmpl "github.com/rickliujh/loom/pkg/template"
	"gopkg.in/yaml.v3"
)

// Param values cross the API as JSON and become the values a module sees the
// same way a params file's do:
//
//   - A top-level scalar is text: "3", "true", "1.10" — what -p and a params
//     file give a string param.
//   - Inside a list or map, a number becomes an int or float64 when it prints
//     back as written, and a template.Number keeping its text otherwise, as
//     YAML decoding does. So targetRevision: 1.10 survives.
//   - null is no value: a top-level null is dropped, as if not sent.
//
// On the way out, a template.Number marshals as a JSON number with its
// original text when that text is a JSON number (1.10), and as a string
// otherwise (010, 0x1f). Every response that carries a structured value also
// gives it as YAML (valueYaml, paramsYaml), the one form in which every
// spelling survives a browser's JSON.parse.

// Params is a request's param values.
type Params map[string]any

// UnmarshalJSON decodes params with numbers kept as written.
func (p *Params) UnmarshalJSON(data []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("params must be an object: %w", err)
	}
	if raw == nil {
		*p = nil
		return nil
	}
	out := make(Params, len(raw))
	for k, r := range raw {
		v, err := decodeJSONValue(r)
		if err != nil {
			return fmt.Errorf("param %q: %w", k, err)
		}
		if v = topLevel(v); v != nil {
			out[k] = v
		}
	}
	*p = out
	return nil
}

// topLevel turns a top-level scalar into its text, as a params file reads it.
func topLevel(v any) any {
	switch x := v.(type) {
	case nil:
		return nil
	case bool:
		return strconv.FormatBool(x)
	case tmpl.Number:
		return string(x)
	case int:
		return strconv.Itoa(x)
	case float64:
		return strconv.FormatFloat(x, 'g', -1, 64)
	}
	return v
}

// decodeJSONValue decodes one JSON value, numbers kept as written.
func decodeJSONValue(data []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	return fromJSON(v), nil
}

// fromJSON converts json.Number, at any depth, the way YAML decoding treats a
// number: native when it prints back as written, template.Number otherwise.
func fromJSON(v any) any {
	switch x := v.(type) {
	case json.Number:
		return jsonNumber(string(x))
	case []any:
		for i := range x {
			x[i] = fromJSON(x[i])
		}
		return x
	case map[string]any:
		for k := range x {
			x[k] = fromJSON(x[k])
		}
		return x
	}
	return v
}

func jsonNumber(text string) any {
	if i, err := strconv.Atoi(text); err == nil && strconv.Itoa(i) == text {
		return i
	}
	if f, err := strconv.ParseFloat(text, 64); err == nil && strconv.FormatFloat(f, 'g', -1, 64) == text {
		return f
	}
	return tmpl.Number(text)
}

// isStructured reports a list or map value.
func isStructured(v any) bool {
	switch v.(type) {
	case []any, map[string]any:
		return true
	}
	return false
}

// yamlText renders v as a YAML document the way toYaml does — two-space
// indent, numbers kept as written — with a final newline.
func yamlText(v any) (string, error) {
	var buf strings.Builder
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(v); err != nil {
		return "", err
	}
	if err := enc.Close(); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// paramsYAML is the YAML form of each structured value in p.
func paramsYAML(p map[string]any) map[string]string {
	var out map[string]string
	for k, v := range p {
		if !isStructured(v) {
			continue
		}
		text, err := yamlText(v)
		if err != nil {
			continue
		}
		if out == nil {
			out = map[string]string{}
		}
		out[k] = text
	}
	return out
}

// marshalJSON is json.Marshal without HTML escaping, which would rewrite
// "<" in a value the UI shows as text.
func marshalJSON(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n")), nil
}
