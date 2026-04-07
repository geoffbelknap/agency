package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"regexp"

	"github.com/charmbracelet/log"
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
	Logger       *log.Logger
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

	r.Route("/api/v1", func(r chi.Router) {
		// Admin
		r.Get("/admin/doctor", h.adminDoctor)
		r.Post("/admin/destroy", h.adminDestroy)
		r.Post("/admin/trust", h.adminTrust)
		r.Get("/admin/audit", h.adminAudit)
		r.Get("/admin/egress", h.adminEgress)
		r.Post("/admin/knowledge", h.adminKnowledge)
		r.Post("/admin/department", h.adminDepartment)

		// Teams
		r.Get("/teams", h.listTeams)
		r.Post("/teams", h.createTeam)
		r.Get("/teams/{name}", h.showTeam)
		r.Get("/teams/{name}/activity", h.teamActivity)

		// Capabilities
		r.Get("/capabilities", h.listCapabilities)
		r.Get("/capabilities/{name}", h.showCapability)
		r.Post("/capabilities/{name}/enable", h.enableCapability)
		r.Post("/capabilities/{name}/disable", h.disableCapability)
		r.Post("/capabilities", h.addCapability)
		r.Delete("/capabilities/{name}", h.deleteCapability)

		// Profiles
		r.Get("/profiles", h.listProfiles)
		r.Get("/profiles/{id}", h.getProfile)
		r.Put("/profiles/{id}", h.createOrUpdateProfile)
		r.Delete("/profiles/{id}", h.deleteProfile)

		// Policy
		r.Get("/policy/{agent}", h.showPolicy)
		r.Post("/policy/{agent}/validate", h.validatePolicy)

		// Rebuild
		r.Post("/agents/{name}/rebuild", h.rebuildAgent)
	})
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
