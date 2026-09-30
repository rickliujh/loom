// Package spectest holds reflection helpers shared by the spec rule T4
// conformance tests in pkg/action and pkg/module.
//
// T4 says every string field in loom.yaml is templatable except spec.params
// definitions. Rather than hand-listing fields, each test walks its config
// structs with CollectStringPaths, injects a malformed template into every
// reachable string with SetByPath, and asserts the execute path returns a
// template parse error. A string field added to any config struct but never
// passed through tmpl.RenderString fails those tests automatically.
package spectest

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// BadTmpl is a malformed Go template that fails to parse.
const BadTmpl = "{{ .unterminated"

type stepKind int

const (
	stepField stepKind = iota
	stepIndex
	stepMapKey
)

// Step is one hop in a path from a root struct to a reachable string.
type Step struct {
	kind  stepKind
	index int    // struct field or slice index
	key   string // map key (stepMapKey only)
	name  string // display name for PathName
}

// CollectStringPaths records the path of every string reachable from v:
// nested structs, pointers, interfaces, slice elements, and the values of
// string-keyed maps — including the []any and map[string]any trees a
// structured param value is made of.
func CollectStringPaths(v reflect.Value, prefix []Step, out *[][]Step) {
	switch v.Kind() {
	case reflect.String:
		cp := make([]Step, len(prefix))
		copy(cp, prefix)
		*out = append(*out, cp)
	case reflect.Pointer, reflect.Interface:
		if !v.IsNil() {
			CollectStringPaths(v.Elem(), prefix, out)
		}
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			f := v.Type().Field(i)
			if !f.IsExported() {
				continue
			}
			CollectStringPaths(v.Field(i), append(prefix, Step{kind: stepField, index: i, name: f.Name}), out)
		}
	case reflect.Slice:
		for i := 0; i < v.Len(); i++ {
			CollectStringPaths(v.Index(i), append(prefix, Step{kind: stepIndex, index: i, name: fmt.Sprintf("[%d]", i)}), out)
		}
	case reflect.Map:
		if v.Type().Key().Kind() != reflect.String {
			return
		}
		keys := make([]string, 0, v.Len())
		for _, k := range v.MapKeys() {
			keys = append(keys, k.String())
		}
		sort.Strings(keys)
		for _, k := range keys {
			CollectStringPaths(v.MapIndex(reflect.ValueOf(k).Convert(v.Type().Key())), append(prefix, Step{kind: stepMapKey, key: k, name: fmt.Sprintf("[%s]", k)}), out)
		}
	}
}

// SetByPath sets the string at path (as recorded by CollectStringPaths) to val.
func SetByPath(root reflect.Value, path []Step, val string) {
	for root.Kind() == reflect.Pointer {
		root = root.Elem()
	}
	root.Set(with(root, path, val))
}

// with returns v with the string at path replaced by val. Map values and
// interface contents are not addressable, so rather than setting in place it
// rebuilds the value along the path and each level stores the rebuilt child
// back — which reaches strings nested any depth inside map[string]any.
func with(v reflect.Value, path []Step, val string) reflect.Value {
	switch v.Kind() {
	case reflect.Pointer:
		v.Elem().Set(with(v.Elem(), path, val))
		return v
	case reflect.Interface:
		nv := reflect.New(v.Type()).Elem()
		nv.Set(with(v.Elem(), path, val))
		return nv
	}
	if len(path) == 0 {
		nv := reflect.New(v.Type()).Elem()
		nv.SetString(val)
		return nv
	}
	s, rest := path[0], path[1:]
	switch s.kind {
	case stepField:
		nv := reflect.New(v.Type()).Elem()
		nv.Set(v)
		nv.Field(s.index).Set(with(nv.Field(s.index), rest, val))
		return nv
	case stepIndex:
		// Slice elements are addressable and shared with the caller's slice.
		v.Index(s.index).Set(with(v.Index(s.index), rest, val))
		return v
	case stepMapKey:
		key := reflect.ValueOf(s.key).Convert(v.Type().Key())
		v.SetMapIndex(key, with(v.MapIndex(key), rest, val))
		return v
	}
	panic("spectest: unknown step")
}

// PathName renders a path as a dotted field trail, e.g. "ProviderConfig.TokenEnv".
func PathName(path []Step) string {
	parts := make([]string, 0, len(path))
	for _, s := range path {
		parts = append(parts, s.name)
	}
	return strings.Join(parts, ".")
}

// AssertTemplateError fails the test unless err is a template parse error.
func AssertTemplateError(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("expected template parse error, got nil — field is not templated (spec T4 violation)")
	}
	msg := err.Error()
	if !strings.Contains(msg, "template") && !strings.Contains(msg, "unclosed action") {
		t.Fatalf("expected template parse error, got: %v", err)
	}
}
