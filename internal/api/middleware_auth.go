package api

import (
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"strings"
)

// BearerAuth returns a middleware that validates the Authorization: Bearer <token>
// or X-Agency-Token header using constant-time comparison.
//
// If token is empty, all requests are allowed (dev/local mode).
// Paths ending in "/health" are always allowed without authentication.
//
// The egressToken parameter is a scoped token that only grants access to
// the credential resolve endpoint (/api/v1/internal/credentials/resolve).
// This limits blast radius if the egress container is compromised (ASK Tenet 4).
func BearerAuth(token, egressToken string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Allow health checks, web UI config, and WebSocket without auth.
			// WebSocket auth is handled by the handler itself (first message).
			if strings.HasSuffix(r.URL.Path, "/health") || r.URL.Path == "/__agency/config" || r.URL.Path == "/ws" {
				next.ServeHTTP(w, r)
				return
			}

			// Dev/local mode: no token configured, allow all requests.
			if token == "" {
				next.ServeHTTP(w, r)
				return
			}

			incoming := extractToken(r)

			// Full token: access to all endpoints.
			if constantTimeEqual(token, incoming) {
				next.ServeHTTP(w, r)
				return
			}

			// Scoped egress token: only credential resolve endpoint.
			if egressToken != "" && constantTimeEqual(egressToken, incoming) {
				if r.URL.Path == "/api/v1/internal/credentials/resolve" && r.Method == http.MethodGet {
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
