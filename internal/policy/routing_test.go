package policy

import (
	"os"
	"path/filepath"
	"testing"
)

// newTestRouter creates an ExceptionRouter backed by a temp directory.
func newTestRouter(t *testing.T) *ExceptionRouter {
	t.Helper()
	dir := t.TempDir()
	return NewExceptionRouter(dir)
}

// writePrincipals writes a principals.yaml file into the router's home directory.
func writePrincipals(t *testing.T, router *ExceptionRouter, content string) {
	t.Helper()
	path := filepath.Join(router.Home, "principals.yaml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write principals.yaml: %v", err)
	}
}

// writeAgentFile creates the agents/{name}/agent.yaml stub so validateRecommender passes.
func writeAgentFile(t *testing.T, router *ExceptionRouter, agentName string) {
	t.Helper()
	dir := filepath.Join(router.Home, "agents", agentName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir agent dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "agent.yaml"), []byte("name: "+agentName+"\n"), 0o644); err != nil {
		t.Fatalf("write agent.yaml: %v", err)
	}
}

// ---- ParameterDomains ----

func TestParameterDomains(t *testing.T) {
	cases := map[string]string{
		"risk_tolerance":                 "security",
		"max_concurrent_tasks":           "operations",
		"max_task_duration":              "operations",
		"autonomous_interrupt_threshold": "security",
		"network_mediation":              "security",
		"logging":                        "compliance",
		"constraints_readonly":           "security",
		"llm_credentials_isolated":       "security",
	}
	for param, want := range cases {
		got := ParameterDomains[param]
		if got != want {
			t.Errorf("ParameterDomains[%q] = %q, want %q", param, got, want)
		}
	}
}

// ---- NewExceptionRequest ----

func TestNewExceptionRequest_DomainFromMap(t *testing.T) {
	req := NewExceptionRequest("req-1", "agent-a", "risk_tolerance", "high", "need it", "")
	if req.Domain != "security" {
		t.Errorf("expected domain=security, got %q", req.Domain)
	}
	if req.Status != "pending" {
		t.Errorf("expected status=pending, got %q", req.Status)
	}
}

func TestNewExceptionRequest_ExplicitDomain(t *testing.T) {
	req := NewExceptionRequest("req-2", "agent-b", "risk_tolerance", "high", "why", "custom")
	if req.Domain != "custom" {
		t.Errorf("expected domain=custom, got %q", req.Domain)
	}
}

func TestNewExceptionRequest_UnknownParameterDefaultsToGeneral(t *testing.T) {
	req := NewExceptionRequest("req-3", "agent-c", "unknown_param", "val", "why", "")
	if req.Domain != "general" {
		t.Errorf("expected domain=general, got %q", req.Domain)
	}
}

// ---- ToMap / FromMap ----

func TestToMapFromMap_RoundTrip(t *testing.T) {
	req := NewExceptionRequest("req-rt", "agent-rt", "logging", "verbose", "audit needs", "")
	req.Status = "routed"
	req.RoutedTo = []string{"alice", "bob"}
	req.RequiresDualApproval = true
	req.ResolvedAt = "2026-01-01T00:00:00Z"

	m := req.ToMap()
	got, err := FromMap(m)
	if err != nil {
		t.Fatalf("FromMap: %v", err)
	}
	if got.RequestID != req.RequestID {
		t.Errorf("RequestID: got %q, want %q", got.RequestID, req.RequestID)
	}
	if got.Domain != req.Domain {
		t.Errorf("Domain: got %q, want %q", got.Domain, req.Domain)
	}
	if got.RequiresDualApproval != req.RequiresDualApproval {
		t.Errorf("RequiresDualApproval: got %v, want %v", got.RequiresDualApproval, req.RequiresDualApproval)
	}
	if len(got.RoutedTo) != 2 {
		t.Errorf("RoutedTo len: got %d, want 2", len(got.RoutedTo))
	}
	if got.ResolvedAt != req.ResolvedAt {
		t.Errorf("ResolvedAt: got %q, want %q", got.ResolvedAt, req.ResolvedAt)
	}
}

// ---- Route ----

func TestRoute_WithExceptionRoutes(t *testing.T) {
	router := newTestRouter(t)
	writePrincipals(t, router, `
version: "0.1"
humans: []
agents: []
teams: []
exception_routes:
  - domain: security
    approvers: [alice, bob]
    requires_dual_approval: true
`)
	req := NewExceptionRequest("req-er", "agent-x", "risk_tolerance", "high", "test", "")
	result, err := router.Route(req)
	if err != nil {
		t.Fatalf("Route: %v", err)
	}
	if result.Status != "routed" {
		t.Errorf("expected status=routed, got %q", result.Status)
	}
	if len(result.RoutedTo) != 2 {
		t.Errorf("expected 2 approvers, got %d", len(result.RoutedTo))
	}
	if !result.RequiresDualApproval {
		t.Error("expected requires_dual_approval=true")
	}
	// File should be saved.
	if _, err := os.Stat(filepath.Join(router.RequestsDir, "req-er.yaml")); err != nil {
		t.Errorf("expected saved file: %v", err)
	}
}

func TestRoute_WithHumanDomainMatch(t *testing.T) {
	router := newTestRouter(t)
	writePrincipals(t, router, `
version: "0.1"
humans:
  - id: carol
    name: Carol
    roles: [security]
    created: "2026-01-01"
    status: active
    exception_domains: [security]
agents: []
teams: []
exception_routes: []
`)
	req := NewExceptionRequest("req-hd", "agent-y", "constraints_readonly", "false", "test", "")
	result, err := router.Route(req)
	if err != nil {
		t.Fatalf("Route: %v", err)
	}
	if result.Status != "routed" {
		t.Errorf("expected status=routed, got %q", result.Status)
	}
	if len(result.RoutedTo) != 1 || result.RoutedTo[0] != "carol" {
		t.Errorf("expected routed_to=[carol], got %v", result.RoutedTo)
	}
}

func TestRoute_InactiveHumanSkipped(t *testing.T) {
	router := newTestRouter(t)
	writePrincipals(t, router, `
version: "0.1"
humans:
  - id: dave
    name: Dave
    roles: [security]
    created: "2026-01-01"
    status: inactive
    exception_domains: [security]
agents: []
teams: []
exception_routes: []
`)
	req := NewExceptionRequest("req-ih", "agent-z", "risk_tolerance", "high", "test", "")
	result, err := router.Route(req)
	if err != nil {
		t.Fatalf("Route: %v", err)
	}
	// Dave is inactive — should fall back to operator.
	if len(result.RoutedTo) != 1 || result.RoutedTo[0] != "operator" {
		t.Errorf("expected fallback to operator, got %v", result.RoutedTo)
	}
}

func TestRoute_FallbackToOperator(t *testing.T) {
	router := newTestRouter(t)
	// No principals.yaml — no file at all.
	req := NewExceptionRequest("req-fb", "agent-a", "max_concurrent_tasks", "10", "busy", "")
	result, err := router.Route(req)
	if err != nil {
		t.Fatalf("Route: %v", err)
	}
	if result.Status != "routed" {
		t.Errorf("expected status=routed, got %q", result.Status)
	}
	if len(result.RoutedTo) != 1 || result.RoutedTo[0] != "operator" {
		t.Errorf("expected routed_to=[operator], got %v", result.RoutedTo)
	}
}

// ---- Approve ----

func TestApprove_SingleApproval(t *testing.T) {
	router := newTestRouter(t)
	req := NewExceptionRequest("req-ap", "agent-a", "logging", "verbose", "test", "")
	req.RoutedTo = []string{"alice"}
	req.Status = "routed"
	if err := router.save(req); err != nil {
		t.Fatalf("save: %v", err)
	}

	result, err := router.Approve("req-ap", "alice")
	if err != nil {
		t.Fatalf("Approve: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.Status != "approved" {
		t.Errorf("expected status=approved, got %q", result.Status)
	}
	if len(result.Approvals) != 1 {
		t.Errorf("expected 1 approval, got %d", len(result.Approvals))
	}
	if result.ResolvedAt == "" {
		t.Error("expected resolved_at to be set")
	}
}

func TestApprove_DualApproval_SingleNotEnough(t *testing.T) {
	router := newTestRouter(t)
	req := NewExceptionRequest("req-da", "agent-a", "risk_tolerance", "high", "test", "")
	req.RoutedTo = []string{"alice", "bob"}
	req.Status = "routed"
	req.RequiresDualApproval = true
	if err := router.save(req); err != nil {
		t.Fatalf("save: %v", err)
	}

	result, err := router.Approve("req-da", "alice")
	if err != nil {
		t.Fatalf("Approve: %v", err)
	}
	if result.Status != "routed" {
		t.Errorf("expected status still routed after 1 of 2, got %q", result.Status)
	}
	if result.ResolvedAt != "" {
		t.Error("expected resolved_at to be empty before second approval")
	}
}

func TestApprove_DualApproval_BothApprove(t *testing.T) {
	router := newTestRouter(t)
	req := NewExceptionRequest("req-da2", "agent-a", "risk_tolerance", "high", "test", "")
	req.RoutedTo = []string{"alice", "bob"}
	req.Status = "routed"
	req.RequiresDualApproval = true
	if err := router.save(req); err != nil {
		t.Fatalf("save: %v", err)
	}

	if _, err := router.Approve("req-da2", "alice"); err != nil {
		t.Fatalf("Approve alice: %v", err)
	}
	result, err := router.Approve("req-da2", "bob")
	if err != nil {
		t.Fatalf("Approve bob: %v", err)
	}
	if result.Status != "approved" {
		t.Errorf("expected status=approved after dual approval, got %q", result.Status)
	}
	if len(result.Approvals) != 2 {
		t.Errorf("expected 2 approvals, got %d", len(result.Approvals))
	}
	if result.ResolvedAt == "" {
		t.Error("expected resolved_at to be set")
	}
}

func TestApprove_UnauthorizedPrincipal(t *testing.T) {
	router := newTestRouter(t)
	req := NewExceptionRequest("req-ua", "agent-a", "logging", "verbose", "test", "")
	req.RoutedTo = []string{"alice"}
	req.Status = "routed"
	if err := router.save(req); err != nil {
		t.Fatalf("save: %v", err)
	}

	result, err := router.Approve("req-ua", "mallory")
	if err != nil {
		t.Fatalf("Approve: %v", err)
	}
	if result != nil {
		t.Error("expected nil result for unauthorized principal")
	}
}

func TestApprove_NotFound(t *testing.T) {
	router := newTestRouter(t)
	result, err := router.Approve("nonexistent", "alice")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != nil {
		t.Error("expected nil for missing request")
	}
}

// ---- Deny ----

func TestDeny(t *testing.T) {
	router := newTestRouter(t)
	req := NewExceptionRequest("req-deny", "agent-a", "logging", "verbose", "test", "")
	req.RoutedTo = []string{"alice"}
	req.Status = "routed"
	if err := router.save(req); err != nil {
		t.Fatalf("save: %v", err)
	}

	result, err := router.Deny("req-deny", "alice", "policy violation")
	if err != nil {
		t.Fatalf("Deny: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.Status != "denied" {
		t.Errorf("expected status=denied, got %q", result.Status)
	}
	if len(result.Denials) != 1 {
		t.Errorf("expected 1 denial, got %d", len(result.Denials))
	}
	if result.ResolvedAt == "" {
		t.Error("expected resolved_at to be set")
	}
}

func TestDeny_NotFound(t *testing.T) {
	router := newTestRouter(t)
	result, err := router.Deny("nonexistent", "alice", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != nil {
		t.Error("expected nil for missing request")
	}
}

// ---- Recommend ----

func TestRecommend_ValidAgent(t *testing.T) {
	router := newTestRouter(t)
	writeAgentFile(t, router, "security-agent")

	req := NewExceptionRequest("req-rec", "agent-a", "risk_tolerance", "high", "test", "")
	req.Status = "routed"
	if err := router.save(req); err != nil {
		t.Fatalf("save: %v", err)
	}

	result, err := router.Recommend("req-rec", "security-agent", "approve", "looks safe")
	if err != nil {
		t.Fatalf("Recommend: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if len(result.Recommendations) != 1 {
		t.Errorf("expected 1 recommendation, got %d", len(result.Recommendations))
	}
	// Status must not change.
	if result.Status != "routed" {
		t.Errorf("status should not change, got %q", result.Status)
	}
}

func TestRecommend_InvalidAction(t *testing.T) {
	router := newTestRouter(t)
	writeAgentFile(t, router, "security-agent")

	req := NewExceptionRequest("req-ia", "agent-a", "risk_tolerance", "high", "test", "")
	req.Status = "routed"
	if err := router.save(req); err != nil {
		t.Fatalf("save: %v", err)
	}

	result, err := router.Recommend("req-ia", "security-agent", "maybe", "not sure")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != nil {
		t.Error("expected nil for invalid action")
	}
}

func TestRecommend_UnknownAgent(t *testing.T) {
	router := newTestRouter(t)
	// No agent.yaml written — agent does not exist.

	req := NewExceptionRequest("req-ua2", "agent-a", "risk_tolerance", "high", "test", "")
	req.Status = "routed"
	if err := router.save(req); err != nil {
		t.Fatalf("save: %v", err)
	}

	result, err := router.Recommend("req-ua2", "ghost-agent", "approve", "trust me")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != nil {
		t.Error("expected nil for unknown agent")
	}
}

// ---- Get ----

func TestGet_Exists(t *testing.T) {
	router := newTestRouter(t)
	req := NewExceptionRequest("req-get", "agent-a", "logging", "verbose", "test", "")
	if err := router.save(req); err != nil {
		t.Fatalf("save: %v", err)
	}

	got, err := router.Get("req-get")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got == nil {
		t.Fatal("expected non-nil request")
	}
	if got.RequestID != "req-get" {
		t.Errorf("RequestID: got %q, want %q", got.RequestID, "req-get")
	}
}

func TestGet_Missing(t *testing.T) {
	router := newTestRouter(t)
	got, err := router.Get("no-such-req")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Error("expected nil for missing request")
	}
}

// ---- ListPending ----

func TestListPending(t *testing.T) {
	router := newTestRouter(t)

	// pending
	r1 := NewExceptionRequest("req-lp1", "agent-a", "logging", "verbose", "test", "")
	r1.Status = "pending"
	// routed
	r2 := NewExceptionRequest("req-lp2", "agent-b", "risk_tolerance", "high", "test", "")
	r2.Status = "routed"
	// approved — should not appear
	r3 := NewExceptionRequest("req-lp3", "agent-c", "logging", "verbose", "test", "")
	r3.Status = "approved"

	for _, r := range []*ExceptionRequest{r1, r2, r3} {
		if err := router.save(r); err != nil {
			t.Fatalf("save: %v", err)
		}
	}

	results, err := router.ListPending()
	if err != nil {
		t.Fatalf("ListPending: %v", err)
	}
	if len(results) != 2 {
		t.Errorf("expected 2 pending/routed, got %d", len(results))
	}
}

func TestListPending_EmptyDir(t *testing.T) {
	router := newTestRouter(t)
	results, err := router.ListPending()
	if err != nil {
		t.Fatalf("ListPending: %v", err)
	}
	if results != nil && len(results) != 0 {
		t.Errorf("expected empty, got %d", len(results))
	}
}

// ---- ListForPrincipal ----

func TestListForPrincipal(t *testing.T) {
	router := newTestRouter(t)

	r1 := NewExceptionRequest("req-lfp1", "agent-a", "logging", "verbose", "test", "")
	r1.Status = "routed"
	r1.RoutedTo = []string{"alice", "bob"}

	r2 := NewExceptionRequest("req-lfp2", "agent-b", "risk_tolerance", "high", "test", "")
	r2.Status = "routed"
	r2.RoutedTo = []string{"carol"}

	// approved — should not appear even if alice is listed
	r3 := NewExceptionRequest("req-lfp3", "agent-c", "logging", "verbose", "test", "")
	r3.Status = "approved"
	r3.RoutedTo = []string{"alice"}

	for _, r := range []*ExceptionRequest{r1, r2, r3} {
		if err := router.save(r); err != nil {
			t.Fatalf("save: %v", err)
		}
	}

	results, err := router.ListForPrincipal("alice")
	if err != nil {
		t.Fatalf("ListForPrincipal: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("expected 1 result for alice, got %d", len(results))
	}
	if results[0].RequestID != "req-lfp1" {
		t.Errorf("expected req-lfp1, got %q", results[0].RequestID)
	}
}

func TestListForPrincipal_EmptyDir(t *testing.T) {
	router := newTestRouter(t)
	results, err := router.ListForPrincipal("alice")
	if err != nil {
		t.Fatalf("ListForPrincipal: %v", err)
	}
	if results != nil && len(results) != 0 {
		t.Errorf("expected empty, got %d", len(results))
	}
}
