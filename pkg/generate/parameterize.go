package generate

import (
	"sort"
	"strings"
)

// Parameterize replaces literal occurrences of param values in content with
// Go template expressions ({{ .key }}). Replacements are applied longest-value-first
// to avoid partial-match issues.
func Parameterize(content string, params map[string]string) string {
	pairs := sortedParamPairs(params)
	for _, p := range pairs {
		content = strings.ReplaceAll(content, p.value, "{{ ."+p.key+" }}")
	}
	return content
}

// ParameterizePath replaces literal occurrences of param values in a file path
// with the __key__ placeholder syntax. Replacements are applied longest-value-first.
func ParameterizePath(path string, params map[string]string) string {
	pairs := sortedParamPairs(params)
	for _, p := range pairs {
		path = strings.ReplaceAll(path, p.value, "__"+p.key+"__")
	}
	return path
}

type paramPair struct {
	key   string
	value string
}

// sortedParamPairs returns params sorted by value length descending,
// so longer values are replaced first to avoid partial matches.
func sortedParamPairs(params map[string]string) []paramPair {
	pairs := make([]paramPair, 0, len(params))
	for k, v := range params {
		if v == "" {
			continue
		}
		pairs = append(pairs, paramPair{key: k, value: v})
	}
	sort.Slice(pairs, func(i, j int) bool {
		return len(pairs[i].value) > len(pairs[j].value)
	})
	return pairs
}
