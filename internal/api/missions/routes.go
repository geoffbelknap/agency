package missions

import (
	"context"
	"encoding/json"
	"net/http"
	"regexp"

	"log/slog"
	"github.com/go-chi/chi/v5"

	"github.com/geoffbelknap/agency/internal/config"
	"github.com/geoffbelknap/agency/internal/credstore"
	"github.com/geoffbelknap/agency/internal/events"
	"github.com/geoffbelknap/agency/internal/knowledge"
	"github.com/geoffbelknap/agency/internal/logs"
	"github.com/geoffbelknap/agency/internal/orchestrate"
)

// CommsClient is the interface for making requests to the comms service.
// Defined locally per Go convention: interfaces belong where they are consumed.
type CommsClient interface {
	CommsRequest(ctx context.Context, method, path string, body interface{}) ([]byte, error)
}

// SignalSender sends OS signals to named containers.
// Defined locally per Go convention: interfaces belong where they are consumed.
type SignalSender interface {
	SignalContainer(ctx context.Context, containerName, signal string) error
}

// Deps holds the dependencies required by the missions module.
type Deps struct {
	MissionManager *orchestrate.MissionManager
	Claims         *orchestrate.MissionClaimRegistry
	HealthMonitor  *orchestrate.MissionHealthMonitor // may be nil
	Scheduler      *events.Scheduler                 // may be nil
	EventBus       *events.Bus                       // may be nil
	Knowledge      *knowledge.Proxy
	CredStore      *credstore.Store
	Audit          *logs.Writer
	Config         *config.Config
	Logger         *slog.Logger
	Comms          CommsClient
	Signal         SignalSender
}

type handler struct {
	deps Deps
}

// RegisterRoutes mounts all mission and canvas routes onto r.
func RegisterRoutes(r chi.Router, d Deps) {
	h := &handler{deps: d}

	r.Route("/api/v1/missions", func(r chi.Router) {
		r.Post("/", h.createMission)
		r.Get("/", h.listMissions)
		r.Get("/health", h.missionHealth) // all missions health
		r.Get("/{name}", h.showMission)
		r.Put("/{name}", h.updateMission)
		r.Delete("/{name}", h.deleteMission)
		r.Post("/{name}/assign", h.assignMission)
		r.Post("/{name}/pause", h.pauseMission)
		r.Post("/{name}/resume", h.resumeMission)
		r.Post("/{name}/complete", h.completeMission)
		r.Get("/{name}/health", h.missionHealth)
		r.Get("/{name}/history", h.missionHistory)
		r.Post("/{name}/knowledge", h.missionKnowledge)
		r.Post("/{name}/claim", h.claimMissionEvent)
		r.Delete("/{name}/claim", h.releaseMissionClaim)
		r.Get("/{name}/procedures", h.listMissionProcedures)
		r.Get("/{name}/episodes", h.listMissionEpisodes)
		r.Get("/{name}/evaluations", h.listMissionEvaluations)
		r.Get("/{name}/canvas", h.getCanvas)
		r.Put("/{name}/canvas", h.putCanvas)
		r.Delete("/{name}/canvas", h.deleteCanvas)
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

// writeJSON writes a JSON response with the given status code.
func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
