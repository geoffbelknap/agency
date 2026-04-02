// agency-gateway/internal/policy/routing.go
package policy

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/geoffbelknap/agency/internal/models"
)

// ParameterDomains maps policy parameter names to their default exception domain.
var ParameterDomains = map[string]string{
	"risk_tolerance":                 "security",
	"max_concurrent_tasks":           "operations",
	"max_task_duration":              "operations",
	"autonomous_interrupt_threshold": "security",
	"network_mediation":              "security",
	"logging":                        "compliance",
	"constraints_readonly":           "security",
	"llm_credentials_isolated":       "security",
}

// ExceptionRequest is a pending exception request awaiting routing and approval.
type ExceptionRequest struct {
	RequestID           string                   `yaml:"request_id"`
	AgentName           string                   `yaml:"agent_name"`
	Parameter           string                   `yaml:"parameter"`
	RequestedValue      string                   `yaml:"requested_value"`
	Reason              string                   `yaml:"reason"`
	Domain              string                   `yaml:"domain"`
	Status              string                   `yaml:"status"` // pending, routed, approved, denied
	RoutedTo            []string                 `yaml:"routed_to"`
	Approvals           []map[string]interface{} `yaml:"approvals"`
	Denials             []map[string]interface{} `yaml:"denials"`
	Recommendations     []map[string]interface{} `yaml:"recommendations"`
	RequiresDualApproval bool                    `yaml:"requires_dual_approval"`
	CreatedAt           string                   `yaml:"created_at"`
	ResolvedAt          string                   `yaml:"resolved_at,omitempty"`
}

// NewExceptionRequest creates a new ExceptionRequest with defaults applied.
func NewExceptionRequest(requestID, agentName, parameter, requestedValue, reason, domain string) *ExceptionRequest {
	if domain == "" {
		if d, ok := ParameterDomains[parameter]; ok {
			domain = d
		} else {
			domain = "general"
		}
	}
	return &ExceptionRequest{
		RequestID:            requestID,
		AgentName:            agentName,
		Parameter:            parameter,
		RequestedValue:       requestedValue,
		Reason:               reason,
		Domain:               domain,
		Status:               "pending",
		RoutedTo:             []string{},
		Approvals:            []map[string]interface{}{},
		Denials:              []map[string]interface{}{},
		Recommendations:      []map[string]interface{}{},
		RequiresDualApproval: false,
		CreatedAt:            time.Now().UTC().Format("2006-01-02T15:04:05Z"),
	}
}

// ToMap serialises the request to a plain map for YAML output.
func (r *ExceptionRequest) ToMap() map[string]interface{} {
	m := map[string]interface{}{
		"request_id":             r.RequestID,
		"agent_name":             r.AgentName,
		"parameter":              r.Parameter,
		"requested_value":        r.RequestedValue,
		"reason":                 r.Reason,
		"domain":                 r.Domain,
		"status":                 r.Status,
		"routed_to":              r.RoutedTo,
		"approvals":              r.Approvals,
		"denials":                r.Denials,
		"recommendations":        r.Recommendations,
		"requires_dual_approval": r.RequiresDualApproval,
		"created_at":             r.CreatedAt,
	}
	if r.ResolvedAt != "" {
		m["resolved_at"] = r.ResolvedAt
	}
	return m
}

// FromMap deserialises an ExceptionRequest from a plain map (loaded from YAML).
func FromMap(data map[string]interface{}) (*ExceptionRequest, error) {
	requestID, _ := data["request_id"].(string)
	agentName, _ := data["agent_name"].(string)
	parameter, _ := data["parameter"].(string)
	requestedValue := fmt.Sprintf("%v", data["requested_value"])
	reason, _ := data["reason"].(string)
	domain, _ := data["domain"].(string)

	req := NewExceptionRequest(requestID, agentName, parameter, requestedValue, reason, domain)

	if status, ok := data["status"].(string); ok {
		req.Status = status
	}
	// routed_to may be []string (from in-memory ToMap) or []interface{} (from YAML unmarshal).
	switch v := data["routed_to"].(type) {
	case []string:
		req.RoutedTo = append(req.RoutedTo, v...)
	case []interface{}:
		for _, item := range v {
			if s, ok := item.(string); ok {
				req.RoutedTo = append(req.RoutedTo, s)
			}
		}
	}

	// approvals
	switch v := data["approvals"].(type) {
	case []map[string]interface{}:
		req.Approvals = append(req.Approvals, v...)
	case []interface{}:
		for _, item := range v {
			if m, ok := item.(map[string]interface{}); ok {
				req.Approvals = append(req.Approvals, m)
			}
		}
	}

	// denials
	switch v := data["denials"].(type) {
	case []map[string]interface{}:
		req.Denials = append(req.Denials, v...)
	case []interface{}:
		for _, item := range v {
			if m, ok := item.(map[string]interface{}); ok {
				req.Denials = append(req.Denials, m)
			}
		}
	}

	// recommendations
	switch v := data["recommendations"].(type) {
	case []map[string]interface{}:
		req.Recommendations = append(req.Recommendations, v...)
	case []interface{}:
		for _, item := range v {
			if m, ok := item.(map[string]interface{}); ok {
				req.Recommendations = append(req.Recommendations, m)
			}
		}
	}
	if v, ok := data["requires_dual_approval"].(bool); ok {
		req.RequiresDualApproval = v
	}
	if v, ok := data["created_at"].(string); ok {
		req.CreatedAt = v
	}
	if v, ok := data["resolved_at"].(string); ok {
		req.ResolvedAt = v
	}
	return req, nil
}

// ExceptionRouter routes exception requests to appropriate principals for approval.
// Uses principals.yaml exception_routes to determine who reviews what.
// Falls back to the operator if no route is defined.
type ExceptionRouter struct {
	Home        string
	RequestsDir string
}

// NewExceptionRouter creates a new ExceptionRouter. If home is empty it defaults to ~/.agency.
func NewExceptionRouter(home string) *ExceptionRouter {
	if home == "" {
		h, err := os.UserHomeDir()
		if err != nil {
			h = "."
		}
		home = filepath.Join(h, ".agency")
	}
	return &ExceptionRouter{
		Home:        home,
		RequestsDir: filepath.Join(home, "exception-requests"),
	}
}

// loadPrincipals reads principals.yaml from home. Returns nil if missing or unreadable.
func (er *ExceptionRouter) loadPrincipals() *models.PrincipalsConfig {
	path := filepath.Join(er.Home, "principals.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var cfg models.PrincipalsConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil
	}
	_ = cfg.Validate()
	return &cfg
}

// Route routes an exception request to the appropriate approvers.
// Looks up the domain in principals.yaml exception_routes.
// Falls back to operator if no route is defined.
func (er *ExceptionRouter) Route(request *ExceptionRequest) (*ExceptionRequest, error) {
	principals := er.loadPrincipals()

	if principals != nil {
		// Check explicit exception_routes first.
		for _, route := range principals.ExceptionRoutes {
			if route.Domain == request.Domain {
				request.RoutedTo = make([]string, len(route.Approvers))
				copy(request.RoutedTo, route.Approvers)
				request.RequiresDualApproval = route.RequiresDualApproval
				request.Status = "routed"
				if err := er.save(request); err != nil {
					return nil, err
				}
				return request, nil
			}
		}

		// Check humans with matching exception_domains.
		var domainPrincipals []string
		for _, h := range principals.Humans {
			if h.Status == "active" {
				for _, d := range h.ExceptionDomains {
					if d == request.Domain {
						domainPrincipals = append(domainPrincipals, h.ID)
						break
					}
				}
			}
		}
		if len(domainPrincipals) > 0 {
			request.RoutedTo = domainPrincipals
			request.Status = "routed"
			if err := er.save(request); err != nil {
				return nil, err
			}
			return request, nil
		}
	}

	// Fallback: route to operator.
	request.RoutedTo = []string{"operator"}
	request.Status = "routed"
	if err := er.save(request); err != nil {
		return nil, err
	}
	return request, nil
}

// Approve records an approval from a principal.
// If dual approval is required, the request stays routed until two approvals are received.
func (er *ExceptionRouter) Approve(requestID, principalID string) (*ExceptionRequest, error) {
	request, err := er.Get(requestID)
	if err != nil {
		return nil, err
	}
	if request == nil {
		return nil, nil
	}

	if !contains(request.RoutedTo, principalID) {
		return nil, nil
	}

	request.Approvals = append(request.Approvals, map[string]interface{}{
		"principal_id": principalID,
		"timestamp":    time.Now().UTC().Format("2006-01-02T15:04:05Z"),
	})

	if request.RequiresDualApproval {
		if len(request.Approvals) >= 2 {
			request.Status = "approved"
			request.ResolvedAt = time.Now().UTC().Format("2006-01-02T15:04:05Z")
		}
	} else {
		request.Status = "approved"
		request.ResolvedAt = time.Now().UTC().Format("2006-01-02T15:04:05Z")
	}

	if err := er.save(request); err != nil {
		return nil, err
	}
	return request, nil
}

// Deny denies an exception request.
func (er *ExceptionRouter) Deny(requestID, principalID, reason string) (*ExceptionRequest, error) {
	request, err := er.Get(requestID)
	if err != nil {
		return nil, err
	}
	if request == nil {
		return nil, nil
	}

	request.Denials = append(request.Denials, map[string]interface{}{
		"principal_id": principalID,
		"reason":       reason,
		"timestamp":    time.Now().UTC().Format("2006-01-02T15:04:05Z"),
	})
	request.Status = "denied"
	request.ResolvedAt = time.Now().UTC().Format("2006-01-02T15:04:05Z")

	if err := er.save(request); err != nil {
		return nil, err
	}
	return request, nil
}

// Recommend records a function agent's recommendation on an exception request.
// Recommendations are advisory — they don't change request status.
// action must be "approve" or "deny".
// The recommender is validated by checking that agents/{agentName}/agent.yaml exists.
func (er *ExceptionRouter) Recommend(requestID, agentName, action, reasoning string) (*ExceptionRequest, error) {
	if action != "approve" && action != "deny" {
		return nil, nil
	}

	request, err := er.Get(requestID)
	if err != nil {
		return nil, err
	}
	if request == nil {
		return nil, nil
	}

	// Validate recommender: check that the agent exists.
	if !er.validateRecommender(agentName) {
		return nil, nil
	}

	request.Recommendations = append(request.Recommendations, map[string]interface{}{
		"agent":     agentName,
		"action":    action,
		"reasoning": reasoning,
		"timestamp": time.Now().UTC().Format("2006-01-02T15:04:05Z"),
	})

	if err := er.save(request); err != nil {
		return nil, err
	}
	return request, nil
}

// validateRecommender checks if the named agent exists by looking for agents/{name}/agent.yaml.
func (er *ExceptionRouter) validateRecommender(agentName string) bool {
	agentFile := filepath.Join(er.Home, "agents", agentName, "agent.yaml")
	_, err := os.Stat(agentFile)
	return err == nil
}

// Get loads an exception request by ID.
func (er *ExceptionRouter) Get(requestID string) (*ExceptionRequest, error) {
	path := filepath.Join(er.RequestsDir, requestID+".yaml")
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var raw map[string]interface{}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	return FromMap(raw)
}

// ListPending lists all pending/routed exception requests.
func (er *ExceptionRouter) ListPending() ([]*ExceptionRequest, error) {
	return er.listFiltered(func(req *ExceptionRequest) bool {
		return req.Status == "pending" || req.Status == "routed"
	})
}

// ListForPrincipal lists exception requests routed to a specific principal.
func (er *ExceptionRouter) ListForPrincipal(principalID string) ([]*ExceptionRequest, error) {
	return er.listFiltered(func(req *ExceptionRequest) bool {
		return req.Status == "routed" && contains(req.RoutedTo, principalID)
	})
}

func (er *ExceptionRouter) listFiltered(keep func(*ExceptionRequest) bool) ([]*ExceptionRequest, error) {
	entries, err := os.ReadDir(er.RequestsDir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	// Collect and sort filenames for deterministic ordering.
	var names []string
	for _, e := range entries {
		if !e.IsDir() && filepath.Ext(e.Name()) == ".yaml" {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	var results []*ExceptionRequest
	for _, name := range names {
		data, err := os.ReadFile(filepath.Join(er.RequestsDir, name))
		if err != nil {
			continue
		}
		var raw map[string]interface{}
		if err := yaml.Unmarshal(data, &raw); err != nil {
			continue
		}
		req, err := FromMap(raw)
		if err != nil {
			continue
		}
		if keep(req) {
			results = append(results, req)
		}
	}
	return results, nil
}

func (er *ExceptionRouter) save(request *ExceptionRequest) error {
	if err := os.MkdirAll(er.RequestsDir, 0o755); err != nil {
		return err
	}
	data, err := yaml.Marshal(request.ToMap())
	if err != nil {
		return err
	}
	path := filepath.Join(er.RequestsDir, request.RequestID+".yaml")
	return os.WriteFile(path, data, 0o644)
}

// contains reports whether s is in the slice.
func contains(slice []string, s string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
}
