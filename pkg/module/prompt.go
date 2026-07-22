package module

import "github.com/rickliujh/loom/pkg/config"

// Prompter obtains a value for a required static parameter that was not
// provided and has no default. It is supplied only for the root module run
// with --interactive; a nil Prompter (the default) makes missing required
// params fail fast per P1/P7. See P8.
type Prompter interface {
	Prompt(param config.ParamDef) (string, error)
}

// loadOptions holds optional Load configuration.
type loadOptions struct {
	prompter Prompter
}

// LoadOption configures Load.
type LoadOption func(*loadOptions)

// WithPrompter sets the Prompter used to fill missing required params (P8).
// It is passed only for the root module; child modules never receive it, so a
// child's missing required params remain the parent's responsibility (M-rules).
func WithPrompter(p Prompter) LoadOption {
	return func(o *loadOptions) { o.prompter = p }
}

// promptMissingRequired asks the prompter for each required static param that
// was neither provided nor has a default (P8), returning a params map with the
// prompted values merged in. The caller's map is left untouched. Params that
// are already provided or carry a default are never prompted, and dynamic
// params are out of scope since their values come from commands.
func promptMissingRequired(declared []config.ParamDef, provided map[string]string, p Prompter) (map[string]string, error) {
	out := provided
	copied := false
	for _, d := range declared {
		if !d.Required {
			continue
		}
		if _, ok := out[d.Name]; ok {
			continue
		}
		if d.Default != "" {
			continue
		}
		val, err := p.Prompt(d)
		if err != nil {
			return nil, err
		}
		if !copied {
			out = make(map[string]string, len(provided)+1)
			for k, v := range provided {
				out[k] = v
			}
			copied = true
		}
		out[d.Name] = val
	}
	return out, nil
}
