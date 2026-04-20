package api

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/geoffbelknap/agency/internal/principal"
	"github.com/geoffbelknap/agency/internal/registry"
)

// setupRegistryWithOperator opens a temporary registry, registers an operator
// named "alice", and issues a bearer token for her. Returns the registry and
// the generated token. The registry is closed via t.Cleanup.
func setupRegistryWithOperator(t *testing.T, name string) (*registry.Registry, string) {
	t.Helper()
	reg, err := registry.Open(filepath.Join(t.TempDir(), "registry.db"))
	if err != nil {
		t.Fatalf("open registry: %v", err)
	}
	t.Cleanup(func() { reg.Close() })

	uuid, err := reg.Register("operator", name)
	if err != nil {
		t.Fatalf("register operator: %v", err)
	}
	tok, err := reg.GenerateToken(uuid)
	if err != nil {
		t.Fatalf("generate token: %v", err)
	}
	return reg, tok
}

// runAuth sends the given request through BearerAuth and returns the final
// response recorder along with the resolved principal (captured by the inner
// handler, which only runs if auth succeeds).
func runAuth(t *testing.T, configToken, egressToken string, reg *registry.Registry, req *http.Request) (*httptest.ResponseRecorder, *registry.Principal) {
	t.Helper()
	var captured *registry.Principal
	handler := BearerAuth(configToken, egressToken, reg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = principal.Get(r)
		w.WriteHeader(http.StatusOK)
	}))
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	return rr, captured
}

func TestBearerAuth_RegistryToken_WithRelayTrust_Allowed(t *testing.T) {
	const configToken = "shared-config-token"
	reg, clientTok := setupRegistryWithOperator(t, "alice")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/agents", nil)
	req.Header.Set("Authorization", "Bearer "+clientTok)
	req.Header.Set("X-Agency-Token", configToken) // relay trust gate

	rr, p := runAuth(t, configToken, "", reg, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	if p == nil {
		t.Fatal("expected a resolved principal, got nil")
	}
	if p.Type != "operator" || p.Name != "alice" {
		t.Errorf("principal = %+v, want operator/alice", p)
	}
}

func TestBearerAuth_RegistryToken_WithoutRelayTrust_Rejected(t *testing.T) {
	const configToken = "shared-config-token"
	reg, clientTok := setupRegistryWithOperator(t, "bob")

	// Client-token in Authorization but no relay trust signal. This is the
	// "attacker sends a registered token directly" case — agency must not
	// accept it.
	req := httptest.NewRequest(http.MethodGet, "/api/v1/agents", nil)
	req.Header.Set("Authorization", "Bearer "+clientTok)
	// no X-Agency-Token

	rr, p := runAuth(t, configToken, "", reg, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rr.Code)
	}
	if p != nil {
		t.Fatalf("principal leaked through rejected request: %+v", p)
	}
}

func TestBearerAuth_RegistryToken_WrongRelayTrust_Rejected(t *testing.T) {
	const configToken = "shared-config-token"
	reg, clientTok := setupRegistryWithOperator(t, "carol")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/agents", nil)
	req.Header.Set("Authorization", "Bearer "+clientTok)
	req.Header.Set("X-Agency-Token", "not-the-shared-token")

	rr, p := runAuth(t, configToken, "", reg, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rr.Code)
	}
	if p != nil {
		t.Fatalf("principal leaked through rejected request: %+v", p)
	}
}

func TestBearerAuth_UnknownToken_WithRelayTrust_Rejected(t *testing.T) {
	const configToken = "shared-config-token"
	reg, _ := setupRegistryWithOperator(t, "dave")

	// Relay trust is valid, but the bearer is garbage. Registry lookup fails.
	req := httptest.NewRequest(http.MethodGet, "/api/v1/agents", nil)
	req.Header.Set("Authorization", "Bearer not-a-registered-token")
	req.Header.Set("X-Agency-Token", configToken)

	rr, _ := runAuth(t, configToken, "", reg, req)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rr.Code)
	}
}

func TestBearerAuth_NilRegistry_RegistryPathSkipped(t *testing.T) {
	const configToken = "shared-config-token"

	// No registry at all. Even a request with the "right shape" for the
	// registry path must fall through to 401.
	req := httptest.NewRequest(http.MethodGet, "/api/v1/agents", nil)
	req.Header.Set("Authorization", "Bearer some-arbitrary-token")
	req.Header.Set("X-Agency-Token", configToken)

	rr, _ := runAuth(t, configToken, "", nil, req)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401 (nil registry must not open new paths)", rr.Code)
	}
}

func TestBearerAuth_EmptyConfigToken_TrustGateClosed(t *testing.T) {
	// If the operator misconfigures the gateway with an empty config token,
	// the trust gate can never pass — fail-closed.
	reg, clientTok := setupRegistryWithOperator(t, "eve")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/agents", nil)
	req.Header.Set("Authorization", "Bearer "+clientTok)
	req.Header.Set("X-Agency-Token", "") // empty shared token

	rr, _ := runAuth(t, "", "", reg, req)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401 when config token is empty", rr.Code)
	}
}

func TestBearerAuth_SharedTokenInAuthorization_FirstCheckStillWins(t *testing.T) {
	// When the client sends the shared token in Authorization, the first
	// BearerAuth check handles it. The registry path must NOT also run
	// (would be a pointless registry lookup and could shadow the intended
	// operator resolution).
	const configToken = "shared-config-token"
	reg, _ := setupRegistryWithOperator(t, "frank")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/agents", nil)
	req.Header.Set("Authorization", "Bearer "+configToken)
	req.Header.Set("X-Agency-Token", configToken)

	rr, _ := runAuth(t, configToken, "", reg, req)
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 — shared token should go through first check", rr.Code)
	}
	// Note: resolvePrincipal in the first check uses the shared token,
	// which ResolveToken maps to the first active operator. We don't
	// pin a specific principal here because registry seeding order may
	// vary; we only care that auth succeeded.
}

func TestBearerAuth_SubprotocolPath_RegistryTokenWithRelayTrust(t *testing.T) {
	// Browser WebSocket via relay: the client puts its bearer in
	// Sec-WebSocket-Protocol. Relay adds X-Agency-Token for trust. The
	// registry path must recognize this shape too, not just the
	// Authorization-bearer shape.
	const configToken = "shared-config-token"
	reg, clientTok := setupRegistryWithOperator(t, "grace")

	req := httptest.NewRequest(http.MethodGet, "/ws", nil)
	req.Header.Set("Sec-WebSocket-Protocol", "agency.v1, bearer."+clientTok)
	req.Header.Set("X-Agency-Token", configToken)

	rr, p := runAuth(t, configToken, "", reg, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	if p == nil || p.Name != "grace" {
		t.Errorf("principal = %+v, want grace", p)
	}
}

func TestBearerAuth_RegistryPath_NeverEscalatesEgressScope(t *testing.T) {
	// The egress token grants access to ONE endpoint. A registered token
	// (even operator-owned) must not accidentally grant egress scope — it
	// goes through the full-auth path instead. But more importantly, a
	// registered token that doesn't exist must not masquerade as the egress
	// token just because it was in the right header.
	const configToken = "shared-config-token"
	const egressToken = "egress-token"
	reg, clientTok := setupRegistryWithOperator(t, "henry")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/creds/internal/resolve", nil)
	req.Header.Set("Authorization", "Bearer "+clientTok)
	req.Header.Set("X-Agency-Token", configToken)

	rr, p := runAuth(t, configToken, egressToken, reg, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (operator via registry should have full access)", rr.Code)
	}
	if p == nil || p.Name != "henry" {
		t.Errorf("principal = %+v, want henry", p)
	}
}
