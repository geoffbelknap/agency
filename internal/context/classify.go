package context

import (
	"encoding/json"
	"fmt"
)

func ClassifySeverity(old, new_ map[string]interface{}) Severity {
	oldJSON, _ := json.Marshal(old)
	newJSON, _ := json.Marshal(new_)

	if string(oldJSON) == string(newJSON) {
		return SeverityLow
	}

	if capabilitiesRevoked(old, new_) {
		return SeverityHigh
	}

	if constraintsTightened(old, new_) {
		return SeverityMedium
	}

	return SeverityLow
}

func ApplyEscalation(auto Severity, override string) (Severity, error) {
	if override == "" {
		return auto, nil
	}
	target, err := ParseSeverity(override)
	if err != nil {
		return 0, err
	}
	if target < auto {
		return 0, fmt.Errorf("cannot downgrade severity from %s to %s", auto, target)
	}
	return target, nil
}

func capabilitiesRevoked(old, new_ map[string]interface{}) bool {
	oldCaps := extractStringSlice(old, "granted_capabilities")
	newCaps := extractStringSlice(new_, "granted_capabilities")
	if len(oldCaps) == 0 {
		return false
	}
	newSet := map[string]bool{}
	for _, c := range newCaps {
		newSet[c] = true
	}
	for _, c := range oldCaps {
		if !newSet[c] {
			return true
		}
	}
	return false
}

// constraintsTightened detects numeric tightening (values decreased) in nested maps.
// Known limitations: does not detect new restrictive keys absent from old, and does not
// detect string-valued policy changes (e.g., "open" → "restricted"). In both cases the
// classifier conservatively defaults to LOW severity.
func constraintsTightened(old, new_ map[string]interface{}) bool {
	for key, oldVal := range old {
		newVal, exists := new_[key]
		if !exists {
			continue
		}
		if oldMap, ok := oldVal.(map[string]interface{}); ok {
			if newMap, ok := newVal.(map[string]interface{}); ok {
				if constraintsTightened(oldMap, newMap) {
					return true
				}
				continue
			}
		}
		if oldNum, ok := toFloat64(oldVal); ok {
			if newNum, ok := toFloat64(newVal); ok {
				if newNum < oldNum {
					return true
				}
			}
		}
	}
	return false
}

func extractStringSlice(m map[string]interface{}, key string) []string {
	raw, ok := m[key]
	if !ok {
		return nil
	}
	slice, ok := raw.([]interface{})
	if !ok {
		return nil
	}
	result := make([]string, 0, len(slice))
	for _, v := range slice {
		if s, ok := v.(string); ok {
			result = append(result, s)
		}
	}
	return result
}

func toFloat64(v interface{}) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	default:
		return 0, false
	}
}
