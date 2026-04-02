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
func BearerAuth(token string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Allow health checks and web UI config without auth.
			if strings.HasSuffix(r.URL.Path, "/health") || r.URL.Path == "/__agency/config" {
				next.ServeHTTP(w, r)
				return
			}

			// Dev/local mode: no token configured, allow all requests.
			if token == "" {
				next.ServeHTTP(w, r)
				return
			}

			// Extract token from Authorization: Bearer <token> or X-Agency-Token.
			incoming := extractToken(r)
			if !constantTimeEqual(token, incoming) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				json.NewEncoder(w).Encode(map[string]string{"error": "unauthorized"})
				return
			}

			next.ServeHTTP(w, r)
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
