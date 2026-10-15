package template

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"text/template"

	"gopkg.in/yaml.v3"
)

// FuncMap returns custom template functions available in loom templates.
func FuncMap() template.FuncMap {
	return template.FuncMap{
		"default":  defaultFn,
		"required": required,
		"hasKey":   hasKey,
		"upper":    upper,
		"lower":    lower,
		"indent":   indent,
		"nindent":  nindent,
		"quote":    quote,
		"toYaml":   toYaml,
		"toJson":   toJson,
		"fromYaml": fromYaml,
		"split":    split,
		"join":     join,
	}
}

// defaultFn returns val unless it is empty (see IsEmpty), in which case it
// returns def. A missing map key arrives as nil, so {{ default "HEAD" .rev }}
// covers both an absent field and one set to "".
func defaultFn(def, val any) any {
	if IsEmpty(val) {
		return def
	}
	return val
}

// required returns val, or fails the render with msg when val is empty by the
// same rule default uses. The message is the author's, so it can name the
// field and the item it belongs to.
func required(msg string, val any) (any, error) {
	if IsEmpty(val) {
		return nil, errors.New(msg)
	}
	return val, nil
}

// hasKey reports whether m is a map holding key. Anything that is not a map —
// including nil — has no keys, so the function is safe on optional fields.
func hasKey(m any, key string) bool {
	rv := reflect.ValueOf(m)
	if rv.Kind() != reflect.Map || rv.Type().Key().Kind() != reflect.String {
		return false
	}
	return rv.MapIndex(reflect.ValueOf(key).Convert(rv.Type().Key())).IsValid()
}

// text converts a scalar to the text a template would print for it. A nil
// value is refused rather than turned into "": it is a missing field, and
// quietly rendering nothing for it is exactly what the <no value> guard
// exists to prevent. A list or map is refused too: its only text form here
// would be Go's own formatting ([a b], map[k:v]), which no target file wants.
func text(fn string, v any) (string, error) {
	switch s := v.(type) {
	case nil:
		return "", fmt.Errorf("%s: value is missing; guard it with default, required, if or with", fn)
	case string:
		return s, nil
	case fmt.Stringer:
		return s.String(), nil
	}
	switch reflect.ValueOf(v).Kind() {
	case reflect.Slice, reflect.Array:
		return "", fmt.Errorf("%s: value is a list, not text; render it with toYaml, toJson or join", fn)
	case reflect.Map:
		return "", fmt.Errorf("%s: value is a map, not text; render it with toYaml or toJson", fn)
	}
	return fmt.Sprint(v), nil
}

func upper(v any) (string, error) {
	s, err := text("upper", v)
	return strings.ToUpper(s), err
}

func lower(v any) (string, error) {
	s, err := text("lower", v)
	return strings.ToLower(s), err
}

func quote(v any) (string, error) {
	s, err := text("quote", v)
	return strconv.Quote(s), err
}

// split divides s around sep, dropping empty elements so both "" and
// trailing separators don't produce blank items. The separator comes
// first to keep the function pipe-friendly: {{ .regions | split "," }}.
func split(sep string, v any) ([]string, error) {
	s, err := text("split", v)
	if err != nil {
		return nil, err
	}
	parts := strings.Split(s, sep)
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	return out, nil
}

// join is split's inverse: the list's elements, as text, separated by sep.
// The separator comes first for the same reason: {{ .regions | join "," }}.
// A nil list joins to "", so an optional list param needs no guard.
func join(sep string, list any) (string, error) {
	if list == nil {
		return "", nil
	}
	rv := reflect.ValueOf(list)
	if rv.Kind() != reflect.Slice && rv.Kind() != reflect.Array {
		return "", fmt.Errorf("join: expected a list, got %T", list)
	}
	parts := make([]string, rv.Len())
	for i := range parts {
		s, err := text("join", rv.Index(i).Interface())
		if err != nil {
			return "", fmt.Errorf("join: element %d: %w", i, err)
		}
		parts[i] = s
	}
	return strings.Join(parts, sep), nil
}

// indent prefixes every line of s with n spaces.
func indent(n int, v any) (string, error) {
	return indentAs("indent", n, v)
}

// nindent is indent with a leading newline, so a value can be placed
// after a key on the same line: "config: {{ .param | nindent 4 }}".
func nindent(n int, v any) (string, error) {
	s, err := indentAs("nindent", n, v)
	if err != nil {
		return "", err
	}
	return "\n" + s, nil
}

// indentAs is indent, naming fn in its errors.
func indentAs(fn string, n int, v any) (string, error) {
	s, err := text(fn, v)
	if err != nil {
		return "", err
	}
	pad := strings.Repeat(" ", n)
	return pad + strings.ReplaceAll(s, "\n", "\n"+pad), nil
}

// fromYaml parses a YAML string into a value (list, map, or scalar), so
// string params can be ranged over or re-serialized with toYaml. It decodes
// exactly like structured params do, so numbers keep their written form.
func fromYaml(s string) (any, error) {
	return DecodeYAML([]byte(s))
}

// toYaml marshals a value to 2-space-indented YAML, without a trailing
// newline. Map keys come out sorted: Go maps carry no order.
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

// toJson marshals a value to compact JSON. Map keys come out sorted.
func toJson(v any) (string, error) {
	out, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(out), nil
}
