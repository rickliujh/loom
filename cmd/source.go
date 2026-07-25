package cmd

import (
	"sort"

	"github.com/rickliujh/loom/pkg/alias"
)

// resolveSourceArg expands an alias reference (":name") into the module source
// it names and the alias's default parameters. Any other argument is returned
// unchanged, so local paths and git URLs keep their existing behavior (AL4).
//
// This runs at the CLI boundary only. module.ResolveSource is deliberately not
// alias-aware: it also resolves spec.modules[].source, and a module whose child
// sources depended on the operator's personal alias file would not be portable
// (AL8).
func resolveSourceArg(source string) (string, map[string]string, error) {
	if !alias.IsRef(source) {
		return source, nil, nil
	}

	name, err := alias.ParseRef(source)
	if err != nil {
		return "", nil, err
	}
	f, err := alias.Load()
	if err != nil {
		return "", nil, err
	}
	def, err := f.Resolve(name)
	if err != nil {
		return "", nil, err
	}
	return def.Source, def.Params, nil
}

// parseParamsWithDefaults merges parameters in ascending precedence: defaults
// (an alias's params), then the params file, then -p flags (AL9).
func parseParamsWithDefaults(defaults map[string]string, cliParams []string, paramsFile string) (map[string]string, error) {
	result, err := parseParams(cliParams, paramsFile)
	if err != nil {
		return nil, err
	}
	for k, v := range defaults {
		if _, set := result[k]; !set {
			result[k] = v
		}
	}
	return result, nil
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
