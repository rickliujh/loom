package generate

import (
	"bytes"

	"gopkg.in/yaml.v3"
)

// ComputeSMP computes a strategic merge patch: the minimal YAML document that,
// when applied through the expand+merge2 pipeline in pkg/action/patch.go,
// reproduces newContent from oldContent.
//
// - Maps are recursively diffed; only changed/added keys are included.
// - Scalar lists emit only added items (expandScalarLists prepends old values on apply).
// - Map-lists are matched by an inferred merge key (e.g. "name"); only
//   changed/added items are included (merge2 matches by key on apply).
//
// Returns nil if YAML cannot be parsed or if there are no changes.
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

	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(diff); err != nil {
		return nil
	}
	return buf.Bytes()
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

	oldSlice, oldIsList := oldVal.([]any)
	newSlice, newIsList := newVal.([]any)

	if oldIsList && newIsList {
		return diffList(oldSlice, newSlice)
	}

	// For scalars, if they differ, return the new value entirely.
	if !yamlEqual(oldVal, newVal) {
		return newVal
	}
	return nil
}

// diffList produces the minimal patch for a list change, mirroring the
// expand+merge2 apply pipeline in pkg/action/patch.go.
func diffList(oldSlice, newSlice []any) any {
	if isScalarSlice(oldSlice) && isScalarSlice(newSlice) {
		return diffScalarList(oldSlice, newSlice)
	}

	key := inferMergeKey(oldSlice, newSlice)
	if key != "" {
		return diffMapList(oldSlice, newSlice, key)
	}

	// Fallback: atomic list comparison.
	if !yamlEqual(oldSlice, newSlice) {
		return newSlice
	}
	return nil
}

// diffScalarList returns only items present in newSlice but not in oldSlice.
// On apply, expandScalarLists prepends old values back, so the patch only
// needs the additions.
func diffScalarList(oldSlice, newSlice []any) any {
	oldSet := make(map[any]bool, len(oldSlice))
	for _, v := range oldSlice {
		oldSet[v] = true
	}
	var added []any
	for _, v := range newSlice {
		if !oldSet[v] {
			added = append(added, v)
		}
	}
	if len(added) == 0 {
		return nil
	}
	return added
}

// diffMapList matches items by a common merge key and diffs each pair.
// Only changed or newly added items are included. On apply, merge2 matches
// by key and deep-merges, preserving unmatched old items.
func diffMapList(oldSlice, newSlice []any, key string) any {
	oldIndex := make(map[any]map[string]any)
	for _, item := range oldSlice {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if kv, exists := m[key]; exists {
			oldIndex[kv] = m
		}
	}

	var result []any
	for _, item := range newSlice {
		nm, ok := item.(map[string]any)
		if !ok {
			result = append(result, item)
			continue
		}
		kv, exists := nm[key]
		if !exists {
			result = append(result, item)
			continue
		}
		om, existed := oldIndex[kv]
		if !existed {
			// New item — include entirely.
			result = append(result, item)
			continue
		}
		// Existing item — diff it.
		diff := diffYAML(om, nm)
		if diff == nil {
			continue // unchanged
		}
		diffMap, ok := diff.(map[string]any)
		if !ok {
			result = append(result, item)
			continue
		}
		// Ensure the merge key is present so merge2 can match.
		diffMap[key] = kv
		result = append(result, diffMap)
	}

	if len(result) == 0 {
		return nil
	}
	return result
}

// inferMergeKey finds a common string-valued key across the first map
// elements of both slices (e.g. "name"). Mirrors the logic in
// pkg/action/patch.go inferMapSliceKey.
func inferMergeKey(a, b []any) string {
	if len(a) == 0 || len(b) == 0 {
		return ""
	}
	am, aOk := a[0].(map[string]any)
	bm, bOk := b[0].(map[string]any)
	if !aOk || !bOk {
		return ""
	}
	for k, v := range bm {
		if _, isStr := v.(string); !isStr {
			continue
		}
		if _, exists := am[k]; exists {
			return k
		}
	}
	return ""
}

func isScalarSlice(s []any) bool {
	for _, v := range s {
		switch v.(type) {
		case map[string]any, []any:
			return false
		}
	}
	return true
}

// yamlEqual does a deep comparison of two YAML values.
func yamlEqual(a, b any) bool {
	aBytes, _ := yaml.Marshal(a)
	bBytes, _ := yaml.Marshal(b)
	return string(aBytes) == string(bBytes)
}
