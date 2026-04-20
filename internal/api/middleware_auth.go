package api

import (
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/geoffbelknap/agency/internal/principal"
	"github.com/geoffbelknap/agency/internal/registry"
)

// getPrincipal extracts the principal from the request context.
// Returns nil if no principal is resolved (backward compatibility).
func getPrincipal(r *http.Request) *registry.Principal {
	return principal.Get(r)
}

// GetPrincipal returns the resolved principal from request context.
// Exported for resource-scoped authorization checks in subpackages.
func GetPrincipal(r *http.Request) *registry.Principal {
	return principal.Get(r)
}

// BearerAuth returns a middleware that validates the Authorization: Bearer <token>
// or X-Agency-Token header using constant-time comparison.
//
// Empty tokens are rejected. config.Load() should generate and persist a
// non-empty token before the gateway starts, including on clean first run.
// Paths ending in "/health" are always allowed without authentication.
//
// WebSocket clients that cannot set an Authorization header (browsers) may
// present the token as a Sec-WebSocket-Protocol entry of the form
// "bearer.<token>". The upgrade handler echoes back the agreed app
// subprotocol ("agency.v1") and never echoes the bearer entry.
//
// The egressToken parameter is a scoped token that only grants access to
// the credential resolve endpoint (/api/v1/creds/internal/resolve).
// This limits blast radius if the egress container is compromised (ASK Tenet 4).
//
// The reg parameter is optional — if non-nil, the middleware resolves the
// incoming token to a principal and stores it in the request context.
// Handlers retrieve it via getPrincipal(r).
//
// Three accepted credential shapes, in order:
//
//  1. Full config token (identity slot matches cfg.Token) — operator access.
//  2. Scoped egress token on its one allowed endpoint.
//  3. Registry-resolvable token carried in the identity slot, with the
//     shared config token in X-Agency-Token as a relay trust gate.
//     This enables per-operator identity when a trusted relay forwards
//     a client's own bearer alongside its own shared secret. Without the
//     trust gate, unknown bearers never bypass the first two checks.
//     See TASK-ios-relay-fwd-003 and agency-relay PRs #5 / #6.
func BearerAuth(token, egressToken string, reg *registry.Registry) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Allow health checks and web UI config without auth. The WebSocket
			// endpoint (/ws) is NOT exempt: the upgrade must carry a valid
			// token via Authorization, X-Agency-Token, or Sec-WebSocket-Protocol
			// bearer entry. See extractToken().
			if strings.HasSuffix(r.URL.Path, "/health") || r.URL.Path == "/__agency/config" {
				next.ServeHTTP(w, r)
				return
			}

			incoming := extractToken(r)

			// Full token: access to all endpoints.
			if constantTimeEqual(token, incoming) {
				r = resolvePrincipal(r, reg, incoming)
				next.ServeHTTP(w, r)
				return
			}

			// Scoped egress token: only credential resolve endpoint.
			if egressToken != "" && constantTimeEqual(egressToken, incoming) {
				if r.URL.Path == "/api/v1/creds/internal/resolve" && r.Method == http.MethodGet {
					r = resolvePrincipal(r, reg, incoming)
					next.ServeHTTP(w, r)
					return
				}
				// Valid egress token but wrong endpoint — forbidden, not unauthorized.
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusForbidden)
				json.NewEncoder(w).Encode(map[string]string{"error": "token scope insufficient"})
				return
			}

			// Registry-resolvable per-operator token, gated by relay trust.
			// The incoming token must resolve to a Principal in the registry,
			// AND the X-Agency-Token header must carry the shared config
			// token, proving this request came through the trusted relay
			// path — not a direct caller who happens to know a bearer.
			if resolved := resolveTrustedRegistryToken(r, token, reg, incoming); resolved != nil {
				r = principal.With(r, resolved)
				next.ServeHTTP(w, r)
				return
			}

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(map[string]string{"error": "unauthorized"})
		})
	}
}

// resolveTrustedRegistryToken attempts to authenticate `incoming` as a
// registry-known bearer forwarded by a trusted relay. Returns a Principal
// only when:
//   - registry is available
//   - X-Agency-Token carries the shared config token (relay trust gate)
//   - the shared token is non-empty (fail-closed if misconfigured)
//   - the incoming token is not the shared token itself (would be handled
//     by the first constant-time check; avoids registry work and closes a
//     theoretical self-gating loop)
//   - the registry resolves the incoming token to a Principal
//
// Any failure returns nil and the caller moves on to 401. The trust gate
// check runs in constant time to avoid leaking whether a bearer is known
// to the registry.
func resolveTrustedRegistryToken(r *http.Request, configToken string, reg *registry.Registry, incoming string) *registry.Principal {
	if reg == nil || configToken == "" || incoming == "" {
		return nil
	}
	// Relay trust gate. Read X-Agency-Token directly rather than going
	// through extractToken, since we need to know it came from THIS header
	// specifically (relay trust is a specific channel, not a token
	// preference order).
	relayTrust := r.Header.Get("X-Agency-Token")
	if !constantTimeEqual(configToken, relayTrust) {
		return nil
	}
	// If incoming happens to be the shared token, the first BearerAuth
	// check already handled it; short-circuit so we don't do a pointless
	// registry lookup (and don't give the shared token two interpretations).
	if constantTimeEqual(configToken, incoming) {
		return nil
	}
	p, err := reg.ResolveToken(incoming)
	if err != nil || p == nil {
		return nil
	}
	return p
}

// resolvePrincipal attempts to resolve the token to a principal via the registry
// and stores it in the request context. If the registry is nil or resolution fails,
// the request is returned unchanged (backward compatible).
func resolvePrincipal(r *http.Request, reg *registry.Registry, token string) *http.Request {
	if reg == nil {
		return r
	}
	p, err := reg.ResolveToken(token)
	if err != nil {
		return r
	}
	return principal.With(r, p)
}

// extractToken pulls the bearer token from, in order of preference:
//  1. Authorization: Bearer <token>
//  2. Sec-WebSocket-Protocol: agency.v1, bearer.<token>
//  3. X-Agency-Token: <token>
//
// Sec-WebSocket-Protocol comes before X-Agency-Token so that a relay-
// forwarded browser WebSocket upgrade — which carries the client's token
// in the subprotocol AND the shared relay trust token in X-Agency-Token —
// resolves to the client's identity, not the shared token. X-Agency-Token
// remains the identity slot for direct callers that set only that header
// (dev scripts, the knowledge synthesizer, etc.).
//
// The Sec-WebSocket-Protocol path exists because browser WebSocket clients
// cannot set arbitrary headers on the upgrade request. Clients include
// "bearer.<token>" alongside the app protocol ("agency.v1"); the upgrader
// only echoes "agency.v1" back, so the token is never reflected.
func extractToken(r *http.Request) string {
	if auth := r.Header.Get("Authorization"); auth != "" {
		if strings.HasPrefix(auth, "Bearer ") {
			return strings.TrimPrefix(auth, "Bearer ")
		}
	}
	if t := extractBearerSubprotocol(r.Header.Get("Sec-WebSocket-Protocol")); t != "" {
		return t
	}
	return r.Header.Get("X-Agency-Token")
}

// extractBearerSubprotocol scans a Sec-WebSocket-Protocol header for a
// "bearer.<token>" entry and returns the token, or "" if none is present.
// The header is a comma-separated list; each value is a protocol name.
func extractBearerSubprotocol(hdr string) string {
	if hdr == "" {
		return ""
	}
	for _, p := range strings.Split(hdr, ",") {
		p = strings.TrimSpace(p)
		if strings.HasPrefix(p, "bearer.") {
			return strings.TrimPrefix(p, "bearer.")
		}
	}
	return ""
}

// constantTimeEqual compares two strings in constant time to prevent timing attacks.
// Returns false if incoming is empty (missing token).
func constantTimeEqual(expected, incoming string) bool {
	if incoming == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(expected), []byte(incoming)) == 1
}

// canAccessAgent checks whether a principal can access a specific agent.
// Used for resource-scoped authorization on agent-specific handlers.
// ASK Tenet 7: least privilege — scope access to the minimum required.
func (d *mcpDeps) canAccessAgent(principal *registry.Principal, agentName string) bool {
	if principal == nil {
		return true // legacy mode — no principal resolved
	}
	if principal.Type == "operator" {
		return true // operators access all agents
	}
	if principal.Type == "agent" && principal.Name == agentName {
		return true // agent can access itself
	}
	// Team members can access agents in their team
	if principal.Parent != "" && d.infra != nil && d.infra.Registry != nil {
		agent, err := d.infra.Registry.ResolveByName("agent", agentName)
		if err == nil && agent.Parent == principal.Parent {
			return true // same team
		}
	}
	return false
}

// writeAgentForbidden writes a 403 response for agent access denial.
func writeAgentForbidden(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusForbidden)
	json.NewEncoder(w).Encode(map[string]string{"error": "access denied to this agent"})
}
