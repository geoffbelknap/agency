package main

import (
	"encoding/base64"
	"net/http"
	"strings"
	"sync"
)

// AuthMiddleware validates requests via API keys or agency-scoped tokens.
type AuthMiddleware struct {
	mu     sync.RWMutex
	keys   map[string]string // key -> name
	bypass map[string]bool   // paths that bypass auth
}

// NewAuthMiddleware creates a new auth middleware with the given API keys.
func NewAuthMiddleware(keys []APIKey) *AuthMiddleware {
	am := &AuthMiddleware{
		keys: make(map[string]string),
		bypass: map[string]bool{
			"/health": true,
		},
	}
	am.SetKeys(keys)
	return am
}

// SetKeys replaces the set of valid API keys (used on SIGHUP reload).
func (am *AuthMiddleware) SetKeys(keys []APIKey) {
	am.mu.Lock()
	defer am.mu.Unlock()
	am.keys = make(map[string]string, len(keys))
	for _, k := range keys {
		am.keys[k.Key] = k.Name
	}
}

// Wrap returns an http.Handler that checks auth before calling next.
func (am *AuthMiddleware) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Bypass auth for health and similar paths
		if am.bypass[r.URL.Path] {
			next.ServeHTTP(w, r)
			return
		}

		if !am.validate(r) {
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// validate checks the request for a valid token.
func (am *AuthMiddleware) validate(r *http.Request) bool {
	// Check Authorization header (LLM API calls)
	auth := r.Header.Get("Authorization")
	if auth != "" {
		token := strings.TrimPrefix(auth, "Bearer ")
		if am.isValidToken(token) {
			return true
		}
	}

	// Check X-API-Key header
	apiKey := r.Header.Get("X-API-Key")
	if apiKey != "" && am.isValidToken(apiKey) {
		return true
	}

	// Check Proxy-Authorization header (standard HTTP proxy auth used by
	// urllib/requests when credentials are embedded in the proxy URL).
	// Accepts Basic auth where the username is the API key and the
	// password is empty: base64("key:") or Bearer scheme.
	proxyAuth := r.Header.Get("Proxy-Authorization")
	if proxyAuth != "" {
		if token, ok := extractProxyToken(proxyAuth); ok && am.isValidToken(token) {
			return true
		}
	}

	return false
}

// extractProxyToken extracts a token from a Proxy-Authorization header value.
// Supports Basic (standard proxy auth) and Bearer (convenience) schemes.
func extractProxyToken(header string) (string, bool) {
	if strings.HasPrefix(header, "Bearer ") {
		return strings.TrimPrefix(header, "Bearer "), true
	}
	if strings.HasPrefix(header, "Basic ") {
		decoded, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(header, "Basic "))
		if err != nil {
			return "", false
		}
		// Basic credentials are "username:password"; scoped key is the username
		parts := strings.SplitN(string(decoded), ":", 2)
		return parts[0], parts[0] != ""
	}
	return "", false
}

// isValidToken checks if a token is a registered API key.
// All tokens must be present in the key map — no prefix bypass is permitted.
func (am *AuthMiddleware) isValidToken(token string) bool {
	am.mu.RLock()
	defer am.mu.RUnlock()
	_, ok := am.keys[token]
	return ok
}
