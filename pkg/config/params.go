package config

import (
	"fmt"

	"gopkg.in/yaml.v3"

	tmpl "github.com/rickliujh/loom/pkg/template"
)

// ParamType is the shape a param's value takes. It is part of the param's
// definition, so like the rest of spec.params it is a literal, never a
// template (T4).
type ParamType string

const (
	// ParamString is the default: a scalar, used as its text.
	ParamString ParamType = "string"
	// ParamList is a YAML sequence of any values.
	ParamList ParamType = "list"
	// ParamMap is a YAML mapping of any values.
	ParamMap ParamType = "map"
)

// Effective is the type a param resolves as: an omitted type is string.
func (t ParamType) Effective() ParamType {
	if t == "" {
		return ParamString
	}
	return t
}

// Valid reports whether t is a known type, or omitted.
func (t ParamType) Valid() bool {
	switch t {
	case "", ParamString, ParamList, ParamMap:
		return true
	}
	return false
}

// HasDefault reports whether the definition carries a usable default. An
// empty one ("", [], {}) is no default, as an empty string always was.
func (p ParamDef) HasDefault() bool {
	return !tmpl.IsEmpty(p.Default)
}

// paramDefFields are the keys a param definition may carry. ParamDef decodes
// itself, which bypasses the loader's strict mode, so it enforces the same
// rule on its own.
var paramDefFields = map[string]bool{"name": true, "type": true, "required": true, "default": true}

// UnmarshalYAML decodes a param definition, reading default through the
// literal-preserving decoder. A scalar default of a string param is kept as
// the exact text written, so "default: 1.10" is "1.10", as it always was.
func (p *ParamDef) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind != yaml.MappingNode {
		return fmt.Errorf("line %d: cannot unmarshal %s into config.ParamDef", n.Line, n.ShortTag())
	}
	var def *yaml.Node
	seen := make(map[string]int, len(n.Content)/2)
	for i := 0; i+1 < len(n.Content); i += 2 {
		k, v := n.Content[i], n.Content[i+1]
		// Decoding field by field bypasses the decoder's own check.
		if first, dup := seen[k.Value]; dup {
			return fmt.Errorf("line %d: mapping key %q already defined at line %d", k.Line, k.Value, first)
		}
		seen[k.Value] = k.Line
		var err error
		switch k.Value {
		case "name":
			err = v.Decode(&p.Name)
		case "type":
			err = v.Decode(&p.Type)
		case "required":
			err = v.Decode(&p.Required)
		case "default":
			def = v
		default:
			return fmt.Errorf("line %d: field %s not found in type config.ParamDef", k.Line, k.Value)
		}
		if err != nil {
			return err
		}
	}
	if def == nil {
		return nil
	}
	if def.Kind == yaml.ScalarNode && p.Type.Effective() == ParamString {
		if def.ShortTag() != "!!null" {
			p.Default = def.Value
		}
		return nil
	}
	v, err := tmpl.DecodeNode(def)
	if err != nil {
		return fmt.Errorf("param %q default: %w", p.Name, err)
	}
	p.Default = v
	return nil
}

// ParamValues is a set of param values keyed by name: strings, lists or maps.
// It decodes with DecodeParamValues.
type ParamValues map[string]any

// UnmarshalYAML decodes the mapping with DecodeParamValues.
func (pv *ParamValues) UnmarshalYAML(n *yaml.Node) error {
	m, err := DecodeParamValues(n)
	if err != nil {
		return err
	}
	*pv = m
	return nil
}

// DecodeParamValues decodes a YAML mapping of param name to value, the shape
// of a params file and of spec.modules[].params.
//
// A top-level scalar is kept as the exact text written — "True", "010" and
// "1.10" stay as typed — which is what these values have always been, and
// what a string param receives. Lists and maps decode with
// template.DecodeNode, so their scalars keep their written form too. A null
// value is nil. An empty document is an empty (nil) map.
func DecodeParamValues(n *yaml.Node) (map[string]any, error) {
	if n.Kind == yaml.DocumentNode {
		if len(n.Content) == 0 {
			return nil, nil
		}
		n = n.Content[0]
	}
	if n.Kind == 0 || (n.Kind == yaml.ScalarNode && n.ShortTag() == "!!null") {
		return nil, nil
	}
	if n.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("line %d: expected a mapping of param names to values, got %s", n.Line, kindName(n))
	}
	entries, err := tmpl.MappingEntries(n)
	if err != nil {
		return nil, err
	}
	m := make(map[string]any, len(entries))
	for k, v := range entries {
		// Whether written directly or merged in through <<, a top-level
		// scalar is the text written.
		if r := resolveAlias(v); r.Kind == yaml.ScalarNode {
			if r.ShortTag() != "!!null" {
				m[k] = r.Value
			} else {
				m[k] = nil
			}
			continue
		}
		val, err := tmpl.DecodeNode(v)
		if err != nil {
			return nil, err
		}
		m[k] = val
	}
	return m, nil
}

func resolveAlias(n *yaml.Node) *yaml.Node {
	for n.Kind == yaml.AliasNode {
		n = n.Alias
	}
	return n
}

// kindName describes a YAML node for an error message.
func kindName(n *yaml.Node) string {
	switch n.Kind {
	case yaml.SequenceNode:
		return "a list"
	case yaml.MappingNode:
		return "a map"
	}
	return "a scalar"
}

// ShapeOf names the shape of a param value as the type that would hold it:
// "list", "map", or "string" for any scalar. Nil is "null".
func ShapeOf(v any) string {
	switch v.(type) {
	case nil:
		return "null"
	case []any:
		return string(ParamList)
	case map[string]any:
		return string(ParamMap)
	}
	return string(ParamString)
}
