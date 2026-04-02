package policy

import "testing"

func TestIsHardFloor(t *testing.T) {
	if !IsHardFloor("logging") {
		t.Error("logging should be a hard floor")
	}
	if !IsHardFloor("network_mediation") {
		t.Error("network_mediation should be a hard floor")
	}
	if IsHardFloor("risk_tolerance") {
		t.Error("risk_tolerance should NOT be a hard floor")
	}
	if IsHardFloor("unknown") {
		t.Error("unknown should NOT be a hard floor")
	}
}

func TestIsLoosening_Ordered(t *testing.T) {
	// risk_tolerance: low < medium < high
	if !IsLoosening("risk_tolerance", "low", "medium") {
		t.Error("medium > low should be loosening")
	}
	if IsLoosening("risk_tolerance", "medium", "low") {
		t.Error("low < medium should NOT be loosening")
	}
	if IsLoosening("risk_tolerance", "low", "low") {
		t.Error("same value should NOT be loosening")
	}
}

func TestIsLoosening_Numeric(t *testing.T) {
	if !IsLoosening("max_concurrent_tasks", 5, 10) {
		t.Error("10 > 5 should be loosening")
	}
	if IsLoosening("max_concurrent_tasks", 10, 5) {
		t.Error("5 < 10 should NOT be loosening")
	}
}

func TestIsLoosening_Unknown(t *testing.T) {
	if !IsLoosening("unknown_param", "a", "b") {
		t.Error("unknown params should be treated as loosening")
	}
}

func TestGetPlatformDefaults(t *testing.T) {
	defaults := GetPlatformDefaults()
	policy, ok := defaults["agency_default_policy_v1"].(map[string]interface{})
	if !ok {
		t.Fatal("expected agency_default_policy_v1")
	}
	params, ok := policy["parameters"].(map[string]interface{})
	if !ok {
		t.Fatal("expected parameters")
	}
	if params["risk_tolerance"] != "medium" {
		t.Error("expected risk_tolerance=medium")
	}
}
