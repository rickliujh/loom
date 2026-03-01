package template

import (
	"strings"
	"text/template"
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
		"upper": strings.ToUpper,
		"lower": strings.ToLower,
	}
}
