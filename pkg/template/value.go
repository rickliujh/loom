package template

import (
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"
	"sort"
	"strconv"

	"gopkg.in/yaml.v3"
)

// Number is a YAML numeric scalar kept as the text it was written with.
//
// Loom emits text, and a number that goes through a float64 or int on its way
// to the output loses its spelling: a chart version written 1.10 comes back as
// 1.1, 010 as 8, 1e3 as 1000. Decoding keeps such a scalar as its source text
// instead. It prints that text in templates and marshals through toYaml as an
// unquoted scalar with that text, so it re-emits exactly as authored. Numbers
// whose text survives a round trip (3, 1.5) stay native ints and floats, which
// keeps comparisons like {{ if eq .replicas 3 }} working.
type Number string

// String returns the number's source text; text/template prints through it.
func (n Number) String() string { return string(n) }

// MarshalYAML emits the source text as a plain scalar. Every Number came from
// a scalar YAML itself resolved as a number, so the plain form reads back as
// one — it never needs quoting.
func (n Number) MarshalYAML() (any, error) {
	return &yaml.Node{Kind: yaml.ScalarNode, Value: string(n)}, nil
}

// jsonNumberRe is the JSON number grammar; text outside it (010, 0x1f, .inf)
// has no JSON number spelling.
var jsonNumberRe = regexp.MustCompile(`^-?(0|[1-9][0-9]*)(\.[0-9]+)?([eE][+-]?[0-9]+)?$`)

// MarshalJSON emits the source text as a JSON number when it is one, and as a
// string otherwise, so the output is always valid JSON and never a rewritten
// number.
func (n Number) MarshalJSON() ([]byte, error) {
	if jsonNumberRe.MatchString(string(n)) {
		return []byte(n), nil
	}
	return json.Marshal(string(n))
}

// DecodeYAML parses one YAML document into a param value: strings, bools,
// nil, []any and map[string]any, with numbers kept as Number wherever their
// native form would re-serialize differently. An empty document is nil.
func DecodeYAML(data []byte) (any, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	return DecodeNode(&doc)
}

// DecodeNode converts an already-parsed YAML node the same way DecodeYAML
// does. Config types use it from their UnmarshalYAML so values nested inside
// loom.yaml keep their literal text too.
func DecodeNode(n *yaml.Node) (any, error) {
	switch n.Kind {
	case 0:
		return nil, nil
	case yaml.DocumentNode:
		if len(n.Content) == 0 {
			return nil, nil
		}
		return DecodeNode(n.Content[0])
	case yaml.AliasNode:
		return DecodeNode(n.Alias)
	case yaml.SequenceNode:
		list := make([]any, 0, len(n.Content))
		for _, c := range n.Content {
			v, err := DecodeNode(c)
			if err != nil {
				return nil, err
			}
			list = append(list, v)
		}
		return list, nil
	case yaml.MappingNode:
		return decodeMapping(n)
	case yaml.ScalarNode:
		return decodeScalar(n)
	}
	return nil, fmt.Errorf("line %d: unsupported YAML node", n.Line)
}

// decodeMapping builds a map from a mapping node's entries.
func decodeMapping(n *yaml.Node) (map[string]any, error) {
	entries, err := MappingEntries(n)
	if err != nil {
		return nil, err
	}
	m := make(map[string]any, len(entries))
	for k, v := range entries {
		val, err := DecodeNode(v)
		if err != nil {
			return nil, err
		}
		m[k] = val
	}
	return m, nil
}

// MappingEntries returns a mapping's entries as key → value node, with merge
// keys (<<) resolved as YAML defines them: keys written in the mapping itself
// win over merged ones, and among merged mappings the earlier one wins. A key
// written twice in the mapping is an error, as it is for the strict decoder.
// Value nodes are returned as written, aliases unresolved.
func MappingEntries(n *yaml.Node) (map[string]*yaml.Node, error) {
	entries := make(map[string]*yaml.Node, len(n.Content)/2)
	lines := make(map[string]int, len(n.Content)/2)
	var merges []*yaml.Node
	for i := 0; i+1 < len(n.Content); i += 2 {
		k, v := n.Content[i], n.Content[i+1]
		if k.Kind == yaml.ScalarNode && k.ShortTag() == "!!merge" {
			merges = append(merges, v)
			continue
		}
		if k.Kind != yaml.ScalarNode {
			return nil, fmt.Errorf("line %d: only scalar mapping keys are supported", k.Line)
		}
		if first, dup := lines[k.Value]; dup {
			return nil, fmt.Errorf("line %d: mapping key %q already defined at line %d", k.Line, k.Value, first)
		}
		lines[k.Value] = k.Line
		entries[k.Value] = v
	}
	for _, src := range merges {
		src = resolveAlias(src)
		sources := []*yaml.Node{src}
		if src.Kind == yaml.SequenceNode {
			sources = src.Content
		}
		for _, s := range sources {
			s = resolveAlias(s)
			if s.Kind != yaml.MappingNode {
				return nil, fmt.Errorf("line %d: merge key value is not a mapping", s.Line)
			}
			merged, err := MappingEntries(s)
			if err != nil {
				return nil, err
			}
			for k, v := range merged {
				if _, set := entries[k]; !set {
					entries[k] = v
				}
			}
		}
	}
	return entries, nil
}

func resolveAlias(n *yaml.Node) *yaml.Node {
	for n.Kind == yaml.AliasNode {
		n = n.Alias
	}
	return n
}

func decodeScalar(n *yaml.Node) (any, error) {
	switch n.ShortTag() {
	case "!!null":
		return nil, nil
	case "!!bool":
		var b bool
		if err := n.Decode(&b); err != nil {
			return nil, err
		}
		return b, nil
	case "!!int", "!!float":
		var v any
		if err := n.Decode(&v); err != nil {
			return nil, err
		}
		// Keep the native value only when it prints back as written. These
		// are the forms yaml.v3 decodes numbers into; each is formatted the
		// way both yaml.v3's encoder and text/template's printing format it
		// (non-finite floats aside, which yaml spells .inf and so never match).
		var canonical string
		switch x := v.(type) {
		case int:
			canonical = strconv.Itoa(x)
		case int64:
			canonical = strconv.FormatInt(x, 10)
		case uint64:
			canonical = strconv.FormatUint(x, 10)
		case float64:
			canonical = strconv.FormatFloat(x, 'g', -1, 64)
		default:
			return Number(n.Value), nil
		}
		if canonical == n.Value {
			return v, nil
		}
		return Number(n.Value), nil
	}
	// Strings, and anything carrying a tag Loom has no type for (timestamps,
	// custom tags), are kept as their text.
	return n.Value, nil
}

// MapStrings returns v with every string in it, at any depth of []any and
// map[string]any, replaced by fn's result; other values are kept as they are.
// path names each string's place below root — "sources[0].repoURL" — and map
// keys, which are not passed to fn, are visited in sorted order so errors and
// callbacks come in a stable order. The first error stops the walk.
func MapStrings(v any, root string, fn func(path, s string) (string, error)) (any, error) {
	switch v := v.(type) {
	case string:
		return fn(root, v)
	case []any:
		out := make([]any, len(v))
		for i, e := range v {
			r, err := MapStrings(e, fmt.Sprintf("%s[%d]", root, i), fn)
			if err != nil {
				return nil, err
			}
			out[i] = r
		}
		return out, nil
	case map[string]any:
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		out := make(map[string]any, len(v))
		for _, k := range keys {
			path := k
			if root != "" {
				path = root + "." + k
			}
			r, err := MapStrings(v[k], path, fn)
			if err != nil {
				return nil, err
			}
			out[k] = r
		}
		return out, nil
	}
	return v, nil
}

// IsEmpty reports whether v counts as "not set" for default and required:
// nil, the empty string, an empty list, or an empty map. Zero and false are
// values, not absences — a replica count of 0 or a flag set to false is
// deliberate, so neither is replaced by a default.
func IsEmpty(v any) bool {
	if v == nil {
		return true
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.String, reflect.Slice, reflect.Map, reflect.Array:
		return rv.Len() == 0
	case reflect.Pointer, reflect.Interface:
		return rv.IsNil()
	}
	return false
}
