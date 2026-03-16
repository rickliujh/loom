package template

import "regexp"

// pathTemplateRe matches __paramName__ placeholders in file/folder names.
// These are converted to Go template expressions before rendering.
var pathTemplateRe = regexp.MustCompile(`__([a-zA-Z_][a-zA-Z0-9_]*)__`)

// ConvertPathTemplate converts filesystem-friendly __paramName__ placeholders
// to Go template syntax {{ .paramName }}. This allows template expressions in
// file and folder names without relying on curly braces which are awkward in
// filesystem paths.
//
// Example: "app/__serviceName__/deploy.yaml" → "app/{{ .serviceName }}/deploy.yaml"
func ConvertPathTemplate(path string) string {
	return pathTemplateRe.ReplaceAllString(path, "{{ .${1} }}")
}
