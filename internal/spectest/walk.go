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
// nested structs, pointers, slice elements, and map[string]string values.
func CollectStringPaths(v reflect.Value, prefix []Step, out *[][]Step) {
	switch v.Kind() {
	case reflect.String:
		cp := make([]Step, len(prefix))
		copy(cp, prefix)
		*out = append(*out, cp)
	case reflect.Pointer:
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
		if v.Type().Key().Kind() != reflect.String || v.Type().Elem().Kind() != reflect.String {
			return
		}
		keys := make([]string, 0, v.Len())
		for _, k := range v.MapKeys() {
			keys = append(keys, k.String())
		}
		sort.Strings(keys)
		for _, k := range keys {
			CollectStringPaths(v.MapIndex(reflect.ValueOf(k)), append(prefix, Step{kind: stepMapKey, key: k, name: fmt.Sprintf("[%s]", k)}), out)
		}
	}
}

// SetByPath sets the string at path (as recorded by CollectStringPaths) to val.
func SetByPath(root reflect.Value, path []Step, val string) {
	v := root
	for i, s := range path {
		for v.Kind() == reflect.Pointer {
			v = v.Elem()
		}
		switch s.kind {
		case stepField:
			v = v.Field(s.index)
		case stepIndex:
			v = v.Index(s.index)
		case stepMapKey:
			// Map values are not addressable, so a map key is always terminal
			// (only map[string]string is walked).
			if i != len(path)-1 {
				panic("spectest: map step must be terminal")
			}
			v.SetMapIndex(reflect.ValueOf(s.key), reflect.ValueOf(val))
			return
		}
	}
	v.SetString(val)
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
