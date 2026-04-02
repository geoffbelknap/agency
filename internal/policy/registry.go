package policy

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// PolicyRegistryError is returned for named policy errors (not found, already exists, invalid).
type PolicyRegistryError struct {
	msg string
}

func (e *PolicyRegistryError) Error() string { return e.msg }

func registryErrorf(format string, args ...interface{}) *PolicyRegistryError {
	return &PolicyRegistryError{msg: fmt.Sprintf(format, args...)}
}

// PolicyRegistry manages named policy templates stored as YAML files under
// ~/.agency/policies/. Corresponds to PolicyRegistry in agency/policy/registry.py.
type PolicyRegistry struct {
	Home       string
	PoliciesDir string
}

// NewPolicyRegistry creates a new PolicyRegistry rooted at agencyHome.
// If agencyHome is empty, it defaults to ~/.agency.
func NewPolicyRegistry(agencyHome string) (*PolicyRegistry, error) {
	if agencyHome == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("cannot determine home directory: %w", err)
		}
		agencyHome = filepath.Join(home, ".agency")
	}
	return &PolicyRegistry{
		Home:       agencyHome,
		PoliciesDir: filepath.Join(agencyHome, "policies"),
	}, nil
}

// PolicyInfo is the summary view returned by ListPolicies.
type PolicyInfo struct {
	Name        string                   `json:"name"`
	Description string                   `json:"description"`
	Parameters  map[string]interface{}   `json:"parameters"`
	Rules       []interface{}            `json:"rules"`
	File        string                   `json:"file"`
}

// ListPolicies returns a summary of all named policies in the policies directory.
// Returns an empty slice if the directory does not exist.
// Corresponds to list_policies() in agency/policy/registry.py.
func (r *PolicyRegistry) ListPolicies() []PolicyInfo {
	entries, err := os.ReadDir(r.PoliciesDir)
	if err != nil {
		return []PolicyInfo{}
	}

	// Collect and sort .yaml files by name (matching sorted() in Python).
	var yamlFiles []os.DirEntry
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".yaml") {
			yamlFiles = append(yamlFiles, e)
		}
	}
	sort.Slice(yamlFiles, func(i, j int) bool {
		return yamlFiles[i].Name() < yamlFiles[j].Name()
	})

	var policies []PolicyInfo
	for _, e := range yamlFiles {
		stem := strings.TrimSuffix(e.Name(), ".yaml")
		fpath := filepath.Join(r.PoliciesDir, e.Name())
		data, err := os.ReadFile(fpath)
		if err != nil {
			policies = append(policies, PolicyInfo{Name: stem, Description: "(error loading)", File: fpath})
			continue
		}
		var raw map[string]interface{}
		if err := yaml.Unmarshal(data, &raw); err != nil {
			policies = append(policies, PolicyInfo{Name: stem, Description: "(error loading)", File: fpath})
			continue
		}

		desc, _ := raw["description"].(string)
		params, _ := raw["parameters"].(map[string]interface{})
		rules := mergeRulesAndAdditions(raw)

		policies = append(policies, PolicyInfo{
			Name:        stem,
			Description: desc,
			Parameters:  params,
			Rules:       rules,
			File:        fpath,
		})
	}
	return policies
}

// GetPolicy loads and returns the raw policy data for the named policy.
// Returns PolicyRegistryError if the policy does not exist.
// Corresponds to get_policy() in agency/policy/registry.py.
func (r *PolicyRegistry) GetPolicy(name string) (map[string]interface{}, error) {
	policyFile := filepath.Join(r.PoliciesDir, name+".yaml")
	data, err := os.ReadFile(policyFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, registryErrorf("Named policy '%s' not found", name)
		}
		return nil, fmt.Errorf("read policy '%s': %w", name, err)
	}
	var raw map[string]interface{}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse policy '%s': %w", name, err)
	}
	return raw, nil
}

// ValidatePolicy checks the named policy against hard floors and platform defaults.
// Returns a list of issue strings (empty means valid).
// Corresponds to validate_policy() in agency/policy/registry.py.
func (r *PolicyRegistry) ValidatePolicy(name string) ([]string, error) {
	raw, err := r.GetPolicy(name)
	if err != nil {
		return nil, err
	}

	var issues []string
	params, _ := raw["parameters"].(map[string]interface{})
	for key, value := range params {
		if IsHardFloor(key) {
			floorVal := HardFloors[key]
			if fmt.Sprintf("%v", value) != fmt.Sprintf("%v", floorVal) {
				issues = append(issues, fmt.Sprintf("Parameter '%s' is a hard floor (must be '%v')", key, floorVal))
			}
			continue
		}
		spec, ok := ParameterDefaults[key]
		if !ok {
			issues = append(issues, fmt.Sprintf("Unknown parameter '%s'", key))
			continue
		}
		if IsLoosening(key, spec.Value, value) {
			issues = append(issues, fmt.Sprintf("Parameter '%s' value '%v' loosens platform default '%v'", key, value, spec.Value))
		}
	}
	return issues, nil
}

// CreatePolicy creates a new named policy file. Returns an error if the policy
// already exists or if validation fails (in which case the file is removed).
// Corresponds to create_policy() in agency/policy/registry.py.
func (r *PolicyRegistry) CreatePolicy(name, description string, parameters map[string]interface{}, rules []interface{}) (string, error) {
	if err := os.MkdirAll(r.PoliciesDir, 0o755); err != nil {
		return "", fmt.Errorf("create policies dir: %w", err)
	}
	policyFile := filepath.Join(r.PoliciesDir, name+".yaml")
	if _, err := os.Stat(policyFile); err == nil {
		return "", registryErrorf("Named policy '%s' already exists", name)
	}

	if description == "" {
		description = fmt.Sprintf("Named policy: %s", name)
	}
	if parameters == nil {
		parameters = map[string]interface{}{}
	}
	if rules == nil {
		rules = []interface{}{}
	}

	data := map[string]interface{}{
		"description": description,
		"parameters":  parameters,
		"rules":       rules,
	}
	out, err := yaml.Marshal(data)
	if err != nil {
		return "", fmt.Errorf("marshal policy: %w", err)
	}
	if err := os.WriteFile(policyFile, out, 0o644); err != nil {
		return "", fmt.Errorf("write policy file: %w", err)
	}

	issues, err := r.ValidatePolicy(name)
	if err != nil {
		_ = os.Remove(policyFile)
		return "", err
	}
	if len(issues) > 0 {
		_ = os.Remove(policyFile)
		return "", registryErrorf("Invalid policy: %s", strings.Join(issues, "; "))
	}
	return policyFile, nil
}

// DeletePolicy removes the named policy file.
// Returns PolicyRegistryError if the policy does not exist.
// Corresponds to delete_policy() in agency/policy/registry.py.
func (r *PolicyRegistry) DeletePolicy(name string) error {
	policyFile := filepath.Join(r.PoliciesDir, name+".yaml")
	if _, err := os.Stat(policyFile); os.IsNotExist(err) {
		return registryErrorf("Named policy '%s' not found", name)
	}
	return os.Remove(policyFile)
}

// ResolveExtends merges a policy that uses the "extends" field with its parent
// template. Parent parameters are the base; child overrides them. Rules are
// concatenated (parent first, then child). If the extended policy does not
// exist, the original data is returned unchanged.
// Corresponds to resolve_extends() in agency/policy/registry.py.
func (r *PolicyRegistry) ResolveExtends(policyData map[string]interface{}) map[string]interface{} {
	extends, _ := policyData["extends"].(string)
	if extends == "" {
		return policyData
	}

	template, err := r.GetPolicy(extends)
	if err != nil {
		// Extended policy not found — return original unchanged.
		return policyData
	}

	// Merge parameters: template is base, child overrides.
	mergedParams := make(map[string]interface{})
	if tp, ok := template["parameters"].(map[string]interface{}); ok {
		for k, v := range tp {
			mergedParams[k] = v
		}
	}
	if cp, ok := policyData["parameters"].(map[string]interface{}); ok {
		for k, v := range cp {
			mergedParams[k] = v
		}
	}

	// Merge rules: template rules first, then child rules (additive).
	mergedRules := mergeRulesAndAdditions(template)
	mergedRules = append(mergedRules, mergeRulesAndAdditions(policyData)...)

	// Build result from child, overriding parameters and rules.
	result := make(map[string]interface{})
	for k, v := range policyData {
		result[k] = v
	}
	result["parameters"] = mergedParams
	result["rules"] = mergedRules
	delete(result, "additions")

	return result
}

// mergeRulesAndAdditions combines the "rules" and "additions" lists from a
// raw policy map into a single []interface{} slice. This mirrors the Python
// expression: data.get("rules", []) + data.get("additions", []).
func mergeRulesAndAdditions(raw map[string]interface{}) []interface{} {
	var out []interface{}
	if rules, ok := raw["rules"].([]interface{}); ok {
		out = append(out, rules...)
	}
	if additions, ok := raw["additions"].([]interface{}); ok {
		out = append(out, additions...)
	}
	return out
}
