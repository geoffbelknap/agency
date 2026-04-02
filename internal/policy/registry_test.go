package policy

import (
	"os"
	"path/filepath"
	"testing"
)

// newTestRegistry creates a PolicyRegistry backed by a temp directory.
func newTestRegistry(t *testing.T) *PolicyRegistry {
	t.Helper()
	dir := t.TempDir()
	r, err := NewPolicyRegistry(dir)
	if err != nil {
		t.Fatalf("NewPolicyRegistry: %v", err)
	}
	return r
}

// writePolicy writes a YAML policy file into the registry's policies dir.
func writePolicy(t *testing.T, r *PolicyRegistry, name, content string) {
	t.Helper()
	if err := os.MkdirAll(r.PoliciesDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(r.PoliciesDir, name+".yaml"), []byte(content), 0o644); err != nil {
		t.Fatalf("write policy file: %v", err)
	}
}

// ---- ListPolicies ----

func TestListPolicies_EmptyDir(t *testing.T) {
	r := newTestRegistry(t)
	// PoliciesDir does not exist yet — should return empty slice.
	policies := r.ListPolicies()
	if len(policies) != 0 {
		t.Errorf("expected 0 policies, got %d", len(policies))
	}
}

func TestListPolicies_WithPolicies(t *testing.T) {
	r := newTestRegistry(t)
	writePolicy(t, r, "alpha", `
description: Alpha policy
parameters:
  risk_tolerance: low
rules: []
`)
	writePolicy(t, r, "beta", `
description: Beta policy
parameters: {}
rules: []
`)

	policies := r.ListPolicies()
	if len(policies) != 2 {
		t.Fatalf("expected 2 policies, got %d", len(policies))
	}
	// Should be sorted alphabetically.
	if policies[0].Name != "alpha" {
		t.Errorf("expected first policy name=alpha, got %s", policies[0].Name)
	}
	if policies[1].Name != "beta" {
		t.Errorf("expected second policy name=beta, got %s", policies[1].Name)
	}
	if policies[0].Description != "Alpha policy" {
		t.Errorf("expected description 'Alpha policy', got %q", policies[0].Description)
	}
}

func TestListPolicies_MergesAdditions(t *testing.T) {
	r := newTestRegistry(t)
	writePolicy(t, r, "mixed", `
description: Policy with additions
rules:
  - rule: base rule
additions:
  - rule: extra rule
`)
	policies := r.ListPolicies()
	if len(policies) != 1 {
		t.Fatalf("expected 1 policy, got %d", len(policies))
	}
	if len(policies[0].Rules) != 2 {
		t.Errorf("expected 2 rules (rules+additions), got %d", len(policies[0].Rules))
	}
}

// ---- GetPolicy ----

func TestGetPolicy_Exists(t *testing.T) {
	r := newTestRegistry(t)
	writePolicy(t, r, "mypolicy", `
description: My policy
parameters:
  risk_tolerance: low
rules: []
`)
	raw, err := r.GetPolicy("mypolicy")
	if err != nil {
		t.Fatalf("GetPolicy: %v", err)
	}
	if raw["description"] != "My policy" {
		t.Errorf("expected description 'My policy', got %v", raw["description"])
	}
}

func TestGetPolicy_NotFound(t *testing.T) {
	r := newTestRegistry(t)
	_, err := r.GetPolicy("nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent policy")
	}
	regErr, ok := err.(*PolicyRegistryError)
	if !ok {
		t.Fatalf("expected PolicyRegistryError, got %T: %v", err, err)
	}
	if regErr.Error() == "" {
		t.Error("expected non-empty error message")
	}
}

// ---- ValidatePolicy ----

func TestValidatePolicy_Valid(t *testing.T) {
	r := newTestRegistry(t)
	writePolicy(t, r, "valid", `
description: Valid policy
parameters:
  risk_tolerance: low
rules: []
`)
	issues, err := r.ValidatePolicy("valid")
	if err != nil {
		t.Fatalf("ValidatePolicy: %v", err)
	}
	if len(issues) != 0 {
		t.Errorf("expected no issues, got: %v", issues)
	}
}

func TestValidatePolicy_HardFloorViolation(t *testing.T) {
	r := newTestRegistry(t)
	// logging is a hard floor — its value must be "required".
	writePolicy(t, r, "badfloor", `
description: Bad floor policy
parameters:
  logging: disabled
rules: []
`)
	issues, err := r.ValidatePolicy("badfloor")
	if err != nil {
		t.Fatalf("ValidatePolicy: %v", err)
	}
	if len(issues) == 0 {
		t.Error("expected hard floor violation issue")
	}
	found := false
	for _, issue := range issues {
		if len(issue) > 0 {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected at least one non-empty issue")
	}
}

func TestValidatePolicy_LooseningSviolation(t *testing.T) {
	r := newTestRegistry(t)
	// risk_tolerance default is "medium"; setting "high" is loosening.
	writePolicy(t, r, "loosening", `
description: Loosening policy
parameters:
  risk_tolerance: high
rules: []
`)
	issues, err := r.ValidatePolicy("loosening")
	if err != nil {
		t.Fatalf("ValidatePolicy: %v", err)
	}
	if len(issues) == 0 {
		t.Error("expected loosening violation issue")
	}
}

func TestValidatePolicy_UnknownParameter(t *testing.T) {
	r := newTestRegistry(t)
	writePolicy(t, r, "unknown_param", `
description: Policy with unknown param
parameters:
  totally_unknown: something
rules: []
`)
	issues, err := r.ValidatePolicy("unknown_param")
	if err != nil {
		t.Fatalf("ValidatePolicy: %v", err)
	}
	if len(issues) == 0 {
		t.Error("expected unknown parameter issue")
	}
}

// ---- CreatePolicy ----

func TestCreatePolicy_Success(t *testing.T) {
	r := newTestRegistry(t)
	fpath, err := r.CreatePolicy("newpolicy", "A new policy", map[string]interface{}{
		"risk_tolerance": "low",
	}, nil)
	if err != nil {
		t.Fatalf("CreatePolicy: %v", err)
	}
	if fpath == "" {
		t.Error("expected non-empty file path")
	}
	if _, statErr := os.Stat(fpath); statErr != nil {
		t.Errorf("policy file not found at %s: %v", fpath, statErr)
	}
}

func TestCreatePolicy_Duplicate(t *testing.T) {
	r := newTestRegistry(t)
	_, err := r.CreatePolicy("dup", "First", nil, nil)
	if err != nil {
		t.Fatalf("first CreatePolicy: %v", err)
	}
	_, err = r.CreatePolicy("dup", "Second", nil, nil)
	if err == nil {
		t.Fatal("expected error for duplicate policy")
	}
	if _, ok := err.(*PolicyRegistryError); !ok {
		t.Errorf("expected PolicyRegistryError, got %T: %v", err, err)
	}
}

func TestCreatePolicy_InvalidRejected(t *testing.T) {
	r := newTestRegistry(t)
	// risk_tolerance "high" loosens the platform default "medium" — should be rejected.
	_, err := r.CreatePolicy("invalid", "Invalid", map[string]interface{}{
		"risk_tolerance": "high",
	}, nil)
	if err == nil {
		t.Fatal("expected error for invalid policy")
	}
	// File should not exist after rejection.
	policyFile := filepath.Join(r.PoliciesDir, "invalid.yaml")
	if _, statErr := os.Stat(policyFile); !os.IsNotExist(statErr) {
		t.Error("expected policy file to be cleaned up after rejection")
	}
}

// ---- DeletePolicy ----

func TestDeletePolicy_Exists(t *testing.T) {
	r := newTestRegistry(t)
	writePolicy(t, r, "todelete", `description: Delete me`)
	if err := r.DeletePolicy("todelete"); err != nil {
		t.Fatalf("DeletePolicy: %v", err)
	}
	// File should be gone.
	policyFile := filepath.Join(r.PoliciesDir, "todelete.yaml")
	if _, err := os.Stat(policyFile); !os.IsNotExist(err) {
		t.Error("expected policy file to be deleted")
	}
}

func TestDeletePolicy_NotFound(t *testing.T) {
	r := newTestRegistry(t)
	err := r.DeletePolicy("ghost")
	if err == nil {
		t.Fatal("expected error for nonexistent policy")
	}
	if _, ok := err.(*PolicyRegistryError); !ok {
		t.Errorf("expected PolicyRegistryError, got %T: %v", err, err)
	}
}

// ---- ResolveExtends ----

func TestResolveExtends_WithoutExtends(t *testing.T) {
	r := newTestRegistry(t)
	input := map[string]interface{}{
		"description": "Standalone",
		"parameters":  map[string]interface{}{"risk_tolerance": "low"},
		"rules":       []interface{}{"rule A"},
	}
	result := r.ResolveExtends(input)
	// Should be unchanged (no extends field).
	if result["description"] != "Standalone" {
		t.Errorf("expected description 'Standalone', got %v", result["description"])
	}
	params, _ := result["parameters"].(map[string]interface{})
	if params["risk_tolerance"] != "low" {
		t.Errorf("expected risk_tolerance=low, got %v", params["risk_tolerance"])
	}
}

func TestResolveExtends_WithExtends(t *testing.T) {
	r := newTestRegistry(t)
	// Create template policy.
	writePolicy(t, r, "base", `
description: Base policy
parameters:
  risk_tolerance: low
  max_concurrent_tasks: 3
rules:
  - rule: base rule
`)
	// Child policy extends base, overrides one param, adds a rule.
	child := map[string]interface{}{
		"description": "Child policy",
		"extends":     "base",
		"parameters": map[string]interface{}{
			"max_concurrent_tasks": 2,
		},
		"rules": []interface{}{
			map[string]interface{}{"rule": "child rule"},
		},
	}
	result := r.ResolveExtends(child)

	// Parameters: base values merged, child overrides win.
	params, _ := result["parameters"].(map[string]interface{})
	if params["risk_tolerance"] != "low" {
		t.Errorf("expected risk_tolerance=low from base, got %v", params["risk_tolerance"])
	}
	maxTasks := params["max_concurrent_tasks"]
	// Child overrides base: should be 2 (child wins).
	switch v := maxTasks.(type) {
	case int:
		if v != 2 {
			t.Errorf("expected max_concurrent_tasks=2 (child override), got %d", v)
		}
	default:
		t.Errorf("unexpected type for max_concurrent_tasks: %T = %v", maxTasks, maxTasks)
	}

	// Rules: base rules first, then child rules.
	rules, _ := result["rules"].([]interface{})
	if len(rules) != 2 {
		t.Errorf("expected 2 rules (1 base + 1 child), got %d", len(rules))
	}

	// "additions" key should be absent.
	if _, hasAdditions := result["additions"]; hasAdditions {
		t.Error("expected 'additions' key to be removed from result")
	}
}

func TestResolveExtends_ExtendsNotFound(t *testing.T) {
	r := newTestRegistry(t)
	// extends a policy that doesn't exist — should return original unchanged.
	input := map[string]interface{}{
		"description": "Orphan",
		"extends":     "nonexistent",
		"parameters":  map[string]interface{}{},
	}
	result := r.ResolveExtends(input)
	if result["description"] != "Orphan" {
		t.Errorf("expected original data returned unchanged, got description=%v", result["description"])
	}
}

func TestResolveExtends_MergesAdditions(t *testing.T) {
	r := newTestRegistry(t)
	writePolicy(t, r, "base_with_additions", `
description: Base with additions
parameters: {}
rules:
  - rule: base rule
additions:
  - rule: base addition
`)
	child := map[string]interface{}{
		"description": "Child",
		"extends":     "base_with_additions",
		"parameters":  map[string]interface{}{},
		"additions": []interface{}{
			map[string]interface{}{"rule": "child addition"},
		},
	}
	result := r.ResolveExtends(child)

	// Should have: base rule + base addition + child addition = 3
	rules, _ := result["rules"].([]interface{})
	if len(rules) != 3 {
		t.Errorf("expected 3 rules (base rule + base addition + child addition), got %d: %v", len(rules), rules)
	}
	if _, hasAdditions := result["additions"]; hasAdditions {
		t.Error("expected 'additions' key to be removed from result")
	}
}
