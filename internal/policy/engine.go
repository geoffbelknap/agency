package policy

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// PolicyStep describes one level in the resolution chain.
type PolicyStep struct {
	Level  string `json:"level"`
	File   string `json:"file,omitempty"`
	Status string `json:"status"` // ok, missing, violation
	Detail string `json:"detail,omitempty"`
}

// ExceptionInfo describes a validated policy exception.
type ExceptionInfo struct {
	ExceptionID  string `json:"exception_id"`
	GrantRef     string `json:"grant_ref"`
	Parameter    string `json:"parameter"`
	GrantedValue string `json:"granted_value"`
	Status       string `json:"status"` // active, expired, invalid
	Detail       string `json:"detail,omitempty"`
}

// EffectivePolicy is the computed policy for an agent — sealed and immutable.
type EffectivePolicy struct {
	Agent      string                   `json:"agent"`
	Parameters map[string]interface{}   `json:"parameters"`
	Rules      []map[string]interface{} `json:"rules"`
	HardFloors map[string]interface{}   `json:"hard_floors"`
	Exceptions []ExceptionInfo          `json:"exceptions,omitempty"`
	Chain      []PolicyStep             `json:"chain"`
	Valid       bool                    `json:"valid"`
	Violations []string                 `json:"violations,omitempty"`
}

// Engine computes and validates effective policy for agents.
type Engine struct {
	Home string
}

func NewEngine(home string) *Engine {
	return &Engine{Home: home}
}

// Compute walks the policy chain and returns the effective policy.
func (e *Engine) Compute(agentName string) *EffectivePolicy {
	agentName = filepath.Base(agentName)
	ep := &EffectivePolicy{
		Agent:      agentName,
		Parameters: e.defaultParameters(),
		Rules:      DefaultRules,
		HardFloors: HardFloors,
		Valid:       true,
	}

	// Step 1: Platform defaults
	ep.Chain = append(ep.Chain, PolicyStep{Level: "platform", Status: "ok", Detail: "platform defaults"})

	// Step 2: Org policy
	orgFile := filepath.Join(e.Home, "policy.yaml")
	orgParams := e.loadAndMerge(orgFile, "org", nil, ep)

	// Step 3: Department
	effectiveParent := orgParams
	deptName := e.getDepartment(agentName)
	if deptName != "" {
		deptFile := filepath.Join(e.Home, "departments", deptName, "policy.yaml")
		deptParams := e.loadAndMerge(deptFile, "department", orgParams, ep)
		if deptParams != nil {
			effectiveParent = deptParams
		}
	} else {
		ep.Chain = append(ep.Chain, PolicyStep{Level: "department", Status: "missing", Detail: "no department policy"})
	}

	// Step 4: Team
	teamName := e.getTeam(agentName)
	if teamName != "" {
		teamFile := filepath.Join(e.Home, "teams", teamName, "policy.yaml")
		teamParams := e.loadAndMerge(teamFile, "team", effectiveParent, ep)
		if teamParams != nil {
			effectiveParent = teamParams
		}
	} else {
		ep.Chain = append(ep.Chain, PolicyStep{Level: "team", Status: "missing", Detail: "no team policy"})
	}

	// Step 5: Agent policy
	agentFile := filepath.Join(e.Home, "agents", agentName, "policy.yaml")
	e.loadAndMerge(agentFile, "agent", effectiveParent, ep)

	// Validate exceptions
	e.validateExceptions(agentName, ep)

	return ep
}

// Validate checks a policy chain without computing effective policy.
func (e *Engine) Validate(agentName string) *EffectivePolicy {
	return e.Compute(agentName)
}

// Show returns the effective policy for display.
func (e *Engine) Show(agentName string) *EffectivePolicy {
	return e.Compute(agentName)
}

func (e *Engine) defaultParameters() map[string]interface{} {
	params := make(map[string]interface{})
	for k, v := range ParameterDefaults {
		params[k] = v.Value
	}
	return params
}

// getDepartment resolves the department name for an agent from its agent.yaml.
// Looks for "departments/<name>" in the policy.inherits_from field.
func (e *Engine) getDepartment(agentName string) string {
	return e.extractHierarchyName(agentName, "departments")
}

// getTeam resolves the team name for an agent from its agent.yaml.
// Looks for "teams/<name>" in the policy.inherits_from field.
func (e *Engine) getTeam(agentName string) string {
	return e.extractHierarchyName(agentName, "teams")
}

var hierarchyNameRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`)

// extractHierarchyName parses the inherits_from field in agent.yaml for a given
// segment keyword ("departments" or "teams") and returns the following name component.
func (e *Engine) extractHierarchyName(agentName, segment string) string {
	agentName = filepath.Base(agentName)
	agentYAML := filepath.Join(e.Home, "agents", filepath.Base(agentName), "agent.yaml")
	data, err := os.ReadFile(agentYAML)
	if err != nil {
		return ""
	}

	var config map[string]interface{}
	if err := yaml.Unmarshal(data, &config); err != nil {
		return ""
	}

	policySection, _ := config["policy"].(map[string]interface{})
	if policySection == nil {
		return ""
	}
	inherits, _ := policySection["inherits_from"].(string)
	if !strings.Contains(inherits, segment+"/") {
		return ""
	}

	parts := strings.Split(inherits, "/")
	for i, part := range parts {
		if part == segment && i+1 < len(parts) {
			name := parts[i+1]
			if len(name) >= 2 && hierarchyNameRe.MatchString(name) {
				return name
			}
		}
	}
	return ""
}

// loadAndMerge loads a policy YAML file, validates it against hard floors and
// loosening rules, and merges its parameters and rules into ep.
// parentParams is the raw policy map of the parent level (used for loosening checks).
// Returns the raw policy map on success, nil on missing/error/violation.
func (e *Engine) loadAndMerge(file, level string, parentParams map[string]interface{}, ep *EffectivePolicy) map[string]interface{} {
	if _, err := os.Stat(file); err != nil {
		ep.Chain = append(ep.Chain, PolicyStep{Level: level, File: file, Status: "missing", Detail: "no " + level + " policy"})
		return nil
	}

	data, err := os.ReadFile(file)
	if err != nil {
		ep.Chain = append(ep.Chain, PolicyStep{Level: level, File: file, Status: "violation", Detail: "read error: " + err.Error()})
		ep.Valid = false
		ep.Violations = append(ep.Violations, fmt.Sprintf("%s: %v", file, err))
		return nil
	}

	var policy map[string]interface{}
	if err := yaml.Unmarshal(data, &policy); err != nil {
		ep.Chain = append(ep.Chain, PolicyStep{Level: level, File: file, Status: "violation", Detail: "invalid YAML: " + err.Error()})
		ep.Valid = false
		ep.Violations = append(ep.Violations, fmt.Sprintf("%s: invalid YAML: %v", file, err))
		return nil
	}

	params, _ := policy["parameters"].(map[string]interface{})

	// Check hard floors at this level — no level may modify a hard floor value.
	for key, floorVal := range HardFloors {
		if paramVal, exists := params[key]; exists {
			if fmt.Sprintf("%v", paramVal) != fmt.Sprintf("%v", floorVal) {
				ep.Valid = false
				violation := fmt.Sprintf("hard floor '%s' modified at %s level (expected %v, got %v)", key, level, floorVal, paramVal)
				ep.Violations = append(ep.Violations, violation)
				ep.Chain = append(ep.Chain, PolicyStep{Level: level, File: file, Status: "violation", Detail: "hard floor modified: " + key})
				return policy
			}
		}
	}

	// Check for loosening relative to parent
	if parentParams != nil && params != nil {
		parentP, _ := parentParams["parameters"].(map[string]interface{})
		if parentP == nil {
			parentP = make(map[string]interface{})
		}
		for key, val := range params {
			if parentVal, exists := parentP[key]; exists {
				if IsLoosening(key, parentVal, val) {
					ep.Valid = false
					violation := fmt.Sprintf("%s: parameter '%s' loosened (parent=%v, %s=%v)", file, key, parentVal, level, val)
					ep.Violations = append(ep.Violations, violation)
					ep.Chain = append(ep.Chain, PolicyStep{Level: level, File: file, Status: "violation", Detail: "parameter loosened: " + key})
					return policy
				}
			}
		}
	}

	// Merge parameters (lower level overrides, but only to restrict)
	if params != nil {
		for k, v := range params {
			ep.Parameters[k] = v
		}
	}

	// Merge rules (additive only)
	if rules, ok := policy["rules"].([]interface{}); ok {
		for _, r := range rules {
			if rm, ok := r.(map[string]interface{}); ok {
				ep.Rules = append(ep.Rules, rm)
			}
		}
	}

	ep.Chain = append(ep.Chain, PolicyStep{Level: level, File: file, Status: "ok"})
	return policy
}

// validateExceptions validates all exceptions declared in the agent's policy.yaml.
func (e *Engine) validateExceptions(agentName string, ep *EffectivePolicy) {
	agentName = filepath.Base(agentName)
	agentFile := filepath.Join(e.Home, "agents", agentName, "policy.yaml")
	data, err := os.ReadFile(agentFile)
	if err != nil {
		return // No agent policy — no exceptions to validate.
	}

	var policy map[string]interface{}
	if err := yaml.Unmarshal(data, &policy); err != nil {
		return
	}

	rawExceptions, _ := policy["exceptions"].([]interface{})
	for _, rawExc := range rawExceptions {
		exc, _ := rawExc.(map[string]interface{})
		if exc == nil {
			continue
		}
		info := e.validateSingleException(exc)
		ep.Exceptions = append(ep.Exceptions, info)
		if info.Status == "invalid" {
			ep.Valid = false
			ep.Violations = append(ep.Violations, fmt.Sprintf("invalid exception %s: %s", info.ExceptionID, info.Detail))
		}
	}
}

// validateSingleException validates one exception entry against the two-key model.
func (e *Engine) validateSingleException(exc map[string]interface{}) ExceptionInfo {
	exceptionID, _ := exc["exception_id"].(string)
	if exceptionID == "" {
		exceptionID = "unknown"
	}
	grantRef, _ := exc["grant_ref"].(string)
	parameter, _ := exc["parameter"].(string)
	grantedValue := fmt.Sprintf("%v", exc["granted_value"])
	expires, _ := exc["expires"].(string)

	// Hard floors cannot be overridden by exceptions.
	if parameter != "" && IsHardFloor(parameter) {
		return ExceptionInfo{
			ExceptionID:  exceptionID,
			GrantRef:     grantRef,
			Parameter:    parameter,
			GrantedValue: grantedValue,
			Status:       "invalid",
			Detail:       fmt.Sprintf("parameter '%s' is a hard floor and cannot be overridden by exception", parameter),
		}
	}

	// Key 1: grant_ref must be present.
	if grantRef == "" {
		return ExceptionInfo{
			ExceptionID:  exceptionID,
			GrantRef:     "",
			Parameter:    parameter,
			GrantedValue: grantedValue,
			Status:       "invalid",
			Detail:       "missing grant_ref (Key 1) — exception requires delegation grant",
		}
	}

	// Grant must exist in org policy.
	grant := e.findDelegationGrant(grantRef)
	if grant == nil {
		return ExceptionInfo{
			ExceptionID:  exceptionID,
			GrantRef:     grantRef,
			Parameter:    parameter,
			GrantedValue: grantedValue,
			Status:       "invalid",
			Detail:       fmt.Sprintf("delegation grant '%s' not found in org policy", grantRef),
		}
	}

	// Key 2: approved_by must be present.
	approvedBy, _ := exc["approved_by"].(string)
	if approvedBy == "" {
		return ExceptionInfo{
			ExceptionID:  exceptionID,
			GrantRef:     grantRef,
			Parameter:    parameter,
			GrantedValue: grantedValue,
			Status:       "invalid",
			Detail:       "missing approved_by (Key 2) — exception requires approval",
		}
	}

	// Check exception expiry.
	if expires != "" {
		expiry, err := time.Parse(time.RFC3339, expires)
		if err != nil {
			return ExceptionInfo{
				ExceptionID:  exceptionID,
				GrantRef:     grantRef,
				Parameter:    parameter,
				GrantedValue: grantedValue,
				Status:       "invalid",
				Detail:       fmt.Sprintf("unparseable expiry date: %s", expires),
			}
		}
		if time.Now().UTC().After(expiry) {
			return ExceptionInfo{
				ExceptionID:  exceptionID,
				GrantRef:     grantRef,
				Parameter:    parameter,
				GrantedValue: grantedValue,
				Status:       "expired",
				Detail:       fmt.Sprintf("expired on %s", expires),
			}
		}
	}

	// Check grant expiry (only enforced when it looks like an ISO date).
	constraints, _ := grant["constraints"].(map[string]interface{})
	if constraints != nil {
		maxExpiry := fmt.Sprintf("%v", constraints["max_expiry"])
		if maxExpiry != "" && maxExpiry != "<nil>" && len(maxExpiry) > 0 && maxExpiry[0] >= '0' && maxExpiry[0] <= '9' {
			grantExpiry, err := time.Parse(time.RFC3339, maxExpiry)
			if err == nil && time.Now().UTC().After(grantExpiry) {
				return ExceptionInfo{
					ExceptionID:  exceptionID,
					GrantRef:     grantRef,
					Parameter:    parameter,
					GrantedValue: grantedValue,
					Status:       "expired",
					Detail:       fmt.Sprintf("delegation grant expired on %s", maxExpiry),
				}
			}
		}
	}

	return ExceptionInfo{
		ExceptionID:  exceptionID,
		GrantRef:     grantRef,
		Parameter:    parameter,
		GrantedValue: grantedValue,
		Status:       "active",
	}
}

// findDelegationGrant looks up a grant by ID in the org's policy.yaml.
func (e *Engine) findDelegationGrant(grantRef string) map[string]interface{} {
	orgFile := filepath.Join(e.Home, "policy.yaml")
	data, err := os.ReadFile(orgFile)
	if err != nil {
		return nil
	}

	var policy map[string]interface{}
	if err := yaml.Unmarshal(data, &policy); err != nil {
		return nil
	}

	grants, _ := policy["delegation_grants"].([]interface{})
	for _, rawGrant := range grants {
		grant, _ := rawGrant.(map[string]interface{})
		if grant == nil {
			continue
		}
		grantID, _ := grant["grant_id"].(string)
		if grantID == grantRef {
			return grant
		}
	}
	return nil
}

// ValidatePolicy checks a policy map against hard floor constraints before
// any write/save operation. Returns nil if the policy is valid, or an error
// describing all violations. This prevents persisting policies that violate
// immutable safety guarantees (ASK tenet 5: governance is operator-owned and
// read-only).
//
// Hard floors enforced:
//   - network.egress_mode cannot be "open"
//   - hard_limits list cannot be empty
func ValidatePolicy(policy map[string]interface{}) error {
	var violations []string

	// Check network.egress_mode != "open"
	if network, ok := policy["network"].(map[string]interface{}); ok {
		if mode, ok := network["egress_mode"].(string); ok {
			if strings.EqualFold(mode, "open") {
				violations = append(violations, "hard floor violation: network.egress_mode cannot be \"open\"")
			}
		}
	}

	// Check hard_limits is not empty
	if hl, exists := policy["hard_limits"]; exists {
		switch v := hl.(type) {
		case []interface{}:
			if len(v) == 0 {
				violations = append(violations, "hard floor violation: hard_limits list cannot be empty")
			}
		case nil:
			violations = append(violations, "hard floor violation: hard_limits list cannot be empty")
		}
	} else {
		violations = append(violations, "hard floor violation: hard_limits list cannot be empty")
	}

	if len(violations) > 0 {
		return fmt.Errorf("policy hard floor violations:\n  - %s", strings.Join(violations, "\n  - "))
	}
	return nil
}

// parseDuration parses human-readable durations like "4 hours", "24h", "30 minutes".
func parseDuration(s string) (time.Duration, error) {
	// Try standard Go duration first
	if d, err := time.ParseDuration(s); err == nil {
		return d, nil
	}

	// Parse human-readable formats: "N hours", "N hour", "N minutes", etc.
	s = strings.TrimSpace(strings.ToLower(s))
	var n float64
	var unit string
	if _, err := fmt.Sscanf(s, "%f %s", &n, &unit); err == nil {
		switch {
		case strings.HasPrefix(unit, "hour"):
			return time.Duration(n * float64(time.Hour)), nil
		case strings.HasPrefix(unit, "minute"):
			return time.Duration(n * float64(time.Minute)), nil
		case strings.HasPrefix(unit, "day"):
			return time.Duration(n * 24 * float64(time.Hour)), nil
		}
	}
	return 0, fmt.Errorf("cannot parse duration %q", s)
}
