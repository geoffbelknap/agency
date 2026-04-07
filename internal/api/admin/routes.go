package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"regexp"

	"log/slog"
	"github.com/go-chi/chi/v5"

	"github.com/geoffbelknap/agency/internal/config"
	"github.com/geoffbelknap/agency/internal/credstore"
	"github.com/geoffbelknap/agency/internal/docker"
	"github.com/geoffbelknap/agency/internal/events"
	"github.com/geoffbelknap/agency/internal/knowledge"
	"github.com/geoffbelknap/agency/internal/logs"
	"github.com/geoffbelknap/agency/internal/orchestrate"
	"github.com/geoffbelknap/agency/internal/profiles"
)

// SignalSender sends OS signals to named containers.
// Defined locally per Go convention: interfaces belong where they are consumed.
type SignalSender interface {
	SignalContainer(ctx context.Context, containerName, signal string) error
}

// Deps holds the dependencies required by the admin module.
type Deps struct {
	AgentManager *orchestrate.AgentManager
	Infra        *orchestrate.Infra
	Knowledge    *knowledge.Proxy
	Audit        *logs.Writer
	ProfileStore *profiles.Store
	CredStore    *credstore.Store
	Config       *config.Config
	Logger       *slog.Logger
	DC           *docker.Client
	Signal       SignalSender
	EventBus     *events.Bus // may be nil
}

type handler struct {
	deps Deps
}

// RegisterRoutes mounts all admin, team, capabilities, profiles, and policy routes onto r.
func RegisterRoutes(r chi.Router, d Deps) {
	h := &handler{deps: d}

	// Admin
	r.Get("/api/v1/admin/doctor", h.adminDoctor)
	r.Post("/api/v1/admin/destroy", h.adminDestroy)
	r.Post("/api/v1/admin/trust", h.adminTrust)
	r.Get("/api/v1/admin/audit", h.adminAudit)
	r.Get("/api/v1/admin/egress", h.adminEgress)
	r.Post("/api/v1/admin/knowledge", h.adminKnowledge)
	r.Post("/api/v1/admin/department", h.adminDepartment)

	// Teams
	r.Get("/api/v1/teams", h.listTeams)
	r.Post("/api/v1/teams", h.createTeam)
	r.Get("/api/v1/teams/{name}", h.showTeam)
	r.Get("/api/v1/teams/{name}/activity", h.teamActivity)

	// Capabilities
	r.Get("/api/v1/capabilities", h.listCapabilities)
	r.Get("/api/v1/capabilities/{name}", h.showCapability)
	r.Post("/api/v1/capabilities/{name}/enable", h.enableCapability)
	r.Post("/api/v1/capabilities/{name}/disable", h.disableCapability)
	r.Post("/api/v1/capabilities", h.addCapability)
	r.Delete("/api/v1/capabilities/{name}", h.deleteCapability)

	// Profiles
	r.Get("/api/v1/profiles", h.listProfiles)
	r.Get("/api/v1/profiles/{id}", h.getProfile)
	r.Put("/api/v1/profiles/{id}", h.createOrUpdateProfile)
	r.Delete("/api/v1/profiles/{id}", h.deleteProfile)

	// Policy
	r.Get("/api/v1/policy/{agent}", h.showPolicy)
	r.Post("/api/v1/policy/{agent}/validate", h.validatePolicy)

	// Rebuild
	r.Post("/api/v1/agents/{name}/rebuild", h.rebuildAgent)

	// Principal registry
	r.Get("/api/v1/registry", h.registrySnapshot)
	r.Get("/api/v1/registry/resolve", h.registryResolve)
	r.Get("/api/v1/registry/list", h.registryList)
	r.Post("/api/v1/registry", h.registryRegister)
	r.Get("/api/v1/registry/{uuid}/effective", h.registryEffective)
	r.Put("/api/v1/registry/{uuid}", h.registryUpdate)
	r.Delete("/api/v1/registry/{uuid}", h.registryDelete)
}

// validResourceName matches lowercase alphanumeric names with hyphens, 1-64 chars.
var validResourceName = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,62}[a-z0-9])?$`)

// requireName validates a user-supplied resource name from a URL param, query param, or JSON body.
func requireName(w http.ResponseWriter, raw string) (string, bool) {
	if !validResourceName.MatchString(raw) {
		writeJSON(w, 400, map[string]string{"error": "invalid name"})
		return "", false
	}
	return raw, true
}

// requireNameStr validates a resource name without writing an HTTP response.
func requireNameStr(name string) bool {
	return validResourceName.MatchString(name)
}

// writeJSON writes a JSON response with the given status code.
func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// nestedStr extracts a string from a nested map: m[key1][key2].
func nestedStr(m map[string]interface{}, keys ...string) (string, bool) {
	cur := m
	for i, k := range keys {
		v, ok := cur[k]
		if !ok {
			return "", false
		}
		if i == len(keys)-1 {
			s, ok := v.(string)
			return s, ok
		}
		next, ok := v.(map[string]interface{})
		if !ok {
			return "", false
		}
		cur = next
	}
	return "", false
}
