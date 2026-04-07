package api

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/geoffbelknap/agency/internal/registry"
)

// contextKey is a private type for context keys to avoid collisions.
type contextKey string

// principalContextKey is the context key for the resolved principal.
const principalContextKey contextKey = "principal"

// getPrincipal extracts the principal from the request context.
// Returns nil if no principal is resolved (backward compatibility).
func getPrincipal(r *http.Request) *registry.Principal {
	p, _ := r.Context().Value(principalContextKey).(*registry.Principal)
	return p
}

// BearerAuth returns a middleware that validates the Authorization: Bearer <token>
// or X-Agency-Token header using constant-time comparison.
//
// If token is empty, all requests are allowed (dev/local mode).
// Paths ending in "/health" are always allowed without authentication.
//
// The egressToken parameter is a scoped token that only grants access to
// the credential resolve endpoint (/api/v1/internal/credentials/resolve).
// This limits blast radius if the egress container is compromised (ASK Tenet 4).
//
// The reg parameter is optional — if non-nil, the middleware resolves the
// incoming token to a principal and stores it in the request context.
// Handlers retrieve it via getPrincipal(r).
func BearerAuth(token, egressToken string, reg *registry.Registry) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Allow health checks, web UI config, and WebSocket without auth.
			// WebSocket auth is handled by the handler itself (first message).
			if strings.HasSuffix(r.URL.Path, "/health") || r.URL.Path == "/__agency/config" || r.URL.Path == "/ws" {
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
				if r.URL.Path == "/api/v1/internal/credentials/resolve" && r.Method == http.MethodGet {
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

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(map[string]string{"error": "unauthorized"})
		})
	}
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
	ctx := context.WithValue(r.Context(), principalContextKey, p)
	return r.WithContext(ctx)
}

// extractToken pulls the bearer token from Authorization header or X-Agency-Token header.
func extractToken(r *http.Request) string {
	if auth := r.Header.Get("Authorization"); auth != "" {
		if strings.HasPrefix(auth, "Bearer ") {
			return strings.TrimPrefix(auth, "Bearer ")
		}
	}
	return r.Header.Get("X-Agency-Token")
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
