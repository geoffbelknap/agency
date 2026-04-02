package policy

import "fmt"

// HardFloors — absolute minimums that cannot be modified at any level.
// These correspond to HARD_FLOORS in agency/policy/defaults.py.
var HardFloors = map[string]interface{}{
	"logging":                  "required",
	"constraints_readonly":     true,
	"llm_credentials_isolated": true,
	"network_mediation":        "required",
}

// ParamSpec describes the default value and ordering constraints for a policy parameter.
type ParamSpec struct {
	Value   interface{}
	Order   []string
	Numeric bool
}

// ParameterDefaults defines default values and ordering for known policy parameters.
// Order slices are from most restrictive (index 0) to least restrictive.
// These correspond to PARAMETER_DEFAULTS in agency/policy/defaults.py.
var ParameterDefaults = map[string]ParamSpec{
	"risk_tolerance": {
		Value: "medium",
		Order: []string{"low", "medium", "high"},
	},
	"max_concurrent_tasks": {
		Value:   5,
		Numeric: true,
	},
	"max_task_duration": {
		Value: "4 hours",
		Order: []string{"1 hour", "2 hours", "4 hours", "8 hours"},
	},
	"autonomous_interrupt_threshold": {
		Value: "HIGH",
		Order: []string{"LOW", "MEDIUM", "HIGH", "CRITICAL"},
	},
}

// DefaultRules are baseline rules that lower levels can add to but never remove.
// These correspond to DEFAULT_RULES in agency/policy/defaults.py.
var DefaultRules = []map[string]interface{}{
	{"rule": "irreversible actions require confirmation", "applies_to": []string{"file_delete", "db_drop", "git_push_force"}},
	{"rule": "sensitive domains require escalation", "applies_to": []string{"billing", "authentication", "security_config"}},
	{"rule": "production data requires confirmation", "applies_to": []string{"prod_db_access", "prod_api_calls"}},
}

// IsHardFloor returns true if key is a hard floor parameter.
// Corresponds to is_hard_floor() in agency/policy/defaults.py.
func IsHardFloor(key string) bool {
	_, ok := HardFloors[key]
	return ok
}

// IsLoosening checks if a proposed value is less restrictive than the current value
// for the named parameter. Unknown parameters are treated as loosening (return true).
// Corresponds to is_loosening() in agency/policy/defaults.py.
func IsLoosening(param string, current, proposed interface{}) bool {
	spec, ok := ParameterDefaults[param]
	if !ok {
		return true // Unknown parameter — treat as loosening
	}

	if spec.Order != nil {
		currentStr := fmt.Sprintf("%v", current)
		proposedStr := fmt.Sprintf("%v", proposed)
		currentIdx := indexOf(spec.Order, currentStr)
		proposedIdx := indexOf(spec.Order, proposedStr)
		if currentIdx >= 0 && proposedIdx >= 0 {
			return proposedIdx > currentIdx
		}
		return false
	}

	if spec.Numeric {
		c := toFloat(current, 0)
		p := toFloat(proposed, 0)
		return p > c
	}

	return false
}

// GetPlatformDefaults returns the canonical platform default policy map.
// Corresponds to get_platform_defaults() in agency/policy/defaults.py.
func GetPlatformDefaults() map[string]interface{} {
	params := make(map[string]interface{})
	for k, v := range ParameterDefaults {
		params[k] = v.Value
	}
	return map[string]interface{}{
		"agency_default_policy_v1": map[string]interface{}{
			"description": "Agency default policy - standalone operator MVP",
			"hard_floors": HardFloors,
			"parameters":  params,
			"rules":       DefaultRules,
		},
	}
}

// toFloat converts an interface{} to float64. Returns def if conversion fails.
func toFloat(v interface{}, def float64) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case int:
		return float64(n)
	}
	return def
}

// indexOf returns the index of val in slice, or -1 if not found.
func indexOf(slice []string, val string) int {
	for i, s := range slice {
		if s == val {
			return i
		}
	}
	return -1
}
