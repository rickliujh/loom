package config

import "fmt"

const (
	ExpectedAPIVersion = "loom.rickliujh.github.io/v1beta1"
	ExpectedKind       = "Loom"
)

// Validate checks that a LoomFile has required fields and valid structure.
func Validate(lf *LoomFile) error {
	if lf.APIVersion != ExpectedAPIVersion {
		return fmt.Errorf("unsupported apiVersion %q, expected %q", lf.APIVersion, ExpectedAPIVersion)
	}
	if lf.Kind != ExpectedKind {
		return fmt.Errorf("unsupported kind %q, expected %q", lf.Kind, ExpectedKind)
	}
	if lf.Metadata.Name == "" {
		return fmt.Errorf("metadata.name is required")
	}

	paramNames := make(map[string]bool)
	for _, p := range lf.Spec.Params {
		if p.Name == "" {
			return fmt.Errorf("param name cannot be empty")
		}
		if paramNames[p.Name] {
			return fmt.Errorf("duplicate param name %q", p.Name)
		}
		if p.Dynamic != "" && p.Required {
			return fmt.Errorf("param %q: dynamic and required are mutually exclusive", p.Name)
		}
		paramNames[p.Name] = true
	}

	opNames := make(map[string]bool)
	for _, op := range lf.Spec.Operations {
		if op.Name == "" {
			return fmt.Errorf("operation name cannot be empty")
		}
		if opNames[op.Name] {
			return fmt.Errorf("duplicate operation name %q", op.Name)
		}
		opNames[op.Name] = true

		count := 0
		if op.NewFiles != nil {
			count++
		}
		if op.Patch != nil {
			count++
		}
		if op.Shell != nil {
			count++
		}
		if op.CommitPush != nil {
			count++
		}
		if op.PR != nil {
			count++
		}
		if count != 1 {
			return fmt.Errorf("operation %q must have exactly one action type, got %d", op.Name, count)
		}

		if op.Patch != nil && op.Patch.Engine != "" {
			switch op.Patch.Engine {
			case "smp", "json6902":
				// valid
			default:
				return fmt.Errorf("operation %q: unknown patch engine %q (supported: smp, json6902)", op.Name, op.Patch.Engine)
			}
		}
	}

	return nil
}
