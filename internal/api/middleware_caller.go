package api

import (
	"net/http"
	"strings"
)

// CallerValidation returns middleware that validates X-Agency-Caller headers
// against a per-route allowlist. Routes not in the allowlist are unrestricted.
// This is defense-in-depth, not authentication — a compromised container can
// spoof the header.
func CallerValidation(allowlist map[string][]string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			allowed := findAllowed(allowlist, r.Method, r.URL.Path)
			if allowed == nil {
				next.ServeHTTP(w, r)
				return
			}

			caller := r.Header.Get("X-Agency-Caller")
			if caller == "" {
				writeJSON(w, 403, map[string]string{"error": "X-Agency-Caller header required"})
				return
			}

			for _, a := range allowed {
				if a == caller || a == "any" {
					next.ServeHTTP(w, r)
					return
				}
			}

			writeJSON(w, 403, map[string]string{"error": "caller not authorized for this endpoint"})
		})
	}
}

func findAllowed(allowlist map[string][]string, method, path string) []string {
	for pattern, callers := range allowlist {
		parts := strings.SplitN(pattern, " ", 2)
		if len(parts) != 2 {
			continue
		}
		if parts[0] != method {
			continue
		}
		if matchPath(parts[1], path) {
			return callers
		}
	}
	return nil
}

func matchPath(pattern, path string) bool {
	patParts := strings.Split(strings.Trim(pattern, "/"), "/")
	pathParts := strings.Split(strings.Trim(path, "/"), "/")

	if strings.HasSuffix(pattern, "/*") {
		prefix := patParts[:len(patParts)-1]
		if len(pathParts) < len(prefix) {
			return false
		}
		for i, p := range prefix {
			if strings.HasPrefix(p, "{") {
				continue
			}
			if p != pathParts[i] {
				return false
			}
		}
		return true
	}

	if len(patParts) != len(pathParts) {
		return false
	}
	for i, p := range patParts {
		if strings.HasPrefix(p, "{") {
			continue
		}
		if p != pathParts[i] {
			return false
		}
	}
	return true
}
