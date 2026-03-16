package generate

import (
	"gopkg.in/yaml.v3"
)

// ComputeSMP computes a strategic merge patch: the minimal YAML document that,
// when deep-merged into oldContent, produces newContent. Only changed and added
// fields are included.
//
// If the YAML cannot be parsed, it returns nil (caller should fall back to
// treating it as a new file).
func ComputeSMP(oldContent, newContent []byte) []byte {
	var oldDoc, newDoc any
	if err := yaml.Unmarshal(oldContent, &oldDoc); err != nil {
		return nil
	}
	if err := yaml.Unmarshal(newContent, &newDoc); err != nil {
		return nil
	}

	diff := diffYAML(oldDoc, newDoc)
	if diff == nil {
		return nil
	}

	out, err := yaml.Marshal(diff)
	if err != nil {
		return nil
	}
	return out
}

// diffYAML returns the minimal structure from newVal that differs from oldVal.
// Returns nil if they are equal.
func diffYAML(oldVal, newVal any) any {
	oldMap, oldIsMap := oldVal.(map[string]any)
	newMap, newIsMap := newVal.(map[string]any)

	if oldIsMap && newIsMap {
		result := make(map[string]any)
		for k, nv := range newMap {
			ov, exists := oldMap[k]
			if !exists {
				// New key added.
				result[k] = nv
				continue
			}
			sub := diffYAML(ov, nv)
			if sub != nil {
				result[k] = sub
			}
		}
		// Note: deleted keys are not included in SMP (they require $patch: delete).
		if len(result) == 0 {
			return nil
		}
		return result
	}

	// For lists and scalars, if they differ, return the new value entirely.
	if !yamlEqual(oldVal, newVal) {
		return newVal
	}
	return nil
}

// yamlEqual does a deep comparison of two YAML values.
func yamlEqual(a, b any) bool {
	// Quick type check.
	aBytes, _ := yaml.Marshal(a)
	bBytes, _ := yaml.Marshal(b)
	return string(aBytes) == string(bBytes)
}
