package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/geoffbelknap/agency/internal/registry"
)

// routePermissions maps "METHOD /path" patterns to required permissions.
// Paths are relative to /api/v1 (the prefix is stripped before matching).
// A trailing "/*" matches any sub-path at that depth.
var routePermissions = map[string]string{
	// Agents
	"POST /agents":     "agent.write",
	"GET /agents":      "agent.read",
	"GET /agents/*":    "agent.read",
	"POST /agents/*":   "agent.write",
	"PUT /agents/*":    "agent.write",
	"DELETE /agents/*": "agent.write",

	// Knowledge
	"GET /knowledge/*":          "knowledge.read",
	"POST /knowledge/query":     "knowledge.read",
	"POST /knowledge/ingest":    "knowledge.write",
	"POST /knowledge/insight":   "knowledge.write",
	"POST /knowledge/*":         "knowledge.write",

	// Registry
	"GET /registry":      "registry.read",
	"GET /registry/*":    "registry.read",
	"POST /registry":     "registry.write",
	"PUT /registry/*":    "registry.write",
	"DELETE /registry/*": "registry.write",

	// Missions
	"GET /missions":      "mission.read",
	"GET /missions/*":    "mission.read",
	"POST /missions":     "mission.write",
	"POST /missions/*":   "mission.write",
	"PUT /missions/*":    "mission.write",
	"DELETE /missions/*": "mission.write",

	// Hub
	"GET /hub/*":    "hub.read",
	"POST /hub/*":   "hub.write",
	"PUT /hub/*":    "hub.write",
	"DELETE /hub/*": "hub.write",

	// Credentials
	"GET /credentials":      "creds.read",
	"GET /credentials/*":    "creds.read",
	"POST /credentials":     "creds.write",
	"POST /credentials/*":   "creds.write",
	"PUT /credentials/*":    "creds.write",
	"DELETE /credentials/*": "creds.write",

	// Infra
	"GET /infra/*":  "infra.read",
	"POST /infra/*": "infra.write",

	// Notifications
	"GET /notifications":      "notification.read",
	"GET /notifications/*":    "notification.read",
	"POST /notifications":     "notification.write",
	"POST /notifications/*":   "notification.write",
	"DELETE /notifications/*": "notification.write",

	// Admin
	"POST /admin/*": "admin.*",
	"GET /admin/*":  "admin.*",
}

// PermissionMiddleware enforces route-level permission checks using the
// principal resolved by BearerAuth. If no principal is present (legacy mode
// or dev mode), the request is allowed through.
//
// ASK Tenet 7: least privilege — each route requires a specific permission.
// ASK Tenet 4: enforcement failure defaults to denial — suspended principals
// are rejected before permission evaluation.
func PermissionMiddleware(reg *registry.Registry) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			principal := getPrincipal(r)
			if principal == nil {
				// No principal resolved — legacy mode, allow through.
				next.ServeHTTP(w, r)
				return
			}

			// Check suspension (ASK Tenet 4: enforcement failure defaults to denial).
			if principal.Status != "active" {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusForbidden)
				json.NewEncoder(w).Encode(map[string]string{"error": "principal suspended or revoked"})
				return
			}

			// Find required permission for this route.
			required := matchRoutePermission(r.Method, r.URL.Path)
			if required == "" {
				required = "admin.*" // unmatched routes require admin
			}

			// Compute effective permissions (hierarchy + ceiling).
			eff, err := reg.EffectivePermissions(principal.UUID)
			if err != nil {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusInternalServerError)
				json.NewEncoder(w).Encode(map[string]string{"error": "permission resolution failed"})
				return
			}

			if !registry.Permits(eff, required) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusForbidden)
				json.NewEncoder(w).Encode(map[string]string{
					"error":    "permission denied",
					"required": required,
				})
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// matchRoutePermission finds the required permission for a given HTTP method
// and path. It strips the /api/v1 prefix, tries an exact match, then
// progressively shorter wildcard paths.
func matchRoutePermission(method, path string) string {
	// Strip /api/v1 prefix.
	path = strings.TrimPrefix(path, "/api/v1")

	// Try exact match first.
	key := method + " " + path
	if p, ok := routePermissions[key]; ok {
		return p
	}

	// Try progressively shorter wildcard paths.
	parts := strings.Split(path, "/")
	for i := len(parts); i > 1; i-- {
		wildcard := method + " " + strings.Join(parts[:i-1], "/") + "/*"
		if p, ok := routePermissions[wildcard]; ok {
			return p
		}
	}

	return "" // unmatched — caller defaults to admin.*
}
