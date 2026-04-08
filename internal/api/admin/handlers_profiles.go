package admin

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/geoffbelknap/agency/internal/models"
)

// ── Profile REST handlers ───────────────────────────────────────────────────

// listProfiles handles GET /api/v1/admin/profiles?type=operator|agent
func (h *handler) listProfiles(w http.ResponseWriter, r *http.Request) {
	if h.deps.ProfileStore == nil {
		writeJSON(w, 503, map[string]string{"error": "profile store not initialized"})
		return
	}
	filterType := r.URL.Query().Get("type")
	if filterType != "" && filterType != "operator" && filterType != "agent" {
		writeJSON(w, 400, map[string]string{"error": "type must be 'operator' or 'agent'"})
		return
	}
	profiles, err := h.deps.ProfileStore.List(filterType)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	if profiles == nil {
		profiles = []models.Profile{}
	}
	writeJSON(w, 200, profiles)
}

// getProfile handles GET /api/v1/admin/profiles/{id}
func (h *handler) getProfile(w http.ResponseWriter, r *http.Request) {
	if h.deps.ProfileStore == nil {
		writeJSON(w, 503, map[string]string{"error": "profile store not initialized"})
		return
	}
	id := chi.URLParam(r, "id")
	p, err := h.deps.ProfileStore.Get(id)
	if err != nil {
		if isNotFound(err) {
			writeJSON(w, 404, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, p)
}

// createOrUpdateProfile handles PUT /api/v1/admin/profiles/{id}
func (h *handler) createOrUpdateProfile(w http.ResponseWriter, r *http.Request) {
	if h.deps.ProfileStore == nil {
		writeJSON(w, 503, map[string]string{"error": "profile store not initialized"})
		return
	}
	id := chi.URLParam(r, "id")

	var body models.Profile
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	body.ID = id

	if body.Type == "" {
		writeJSON(w, 400, map[string]string{"error": "type is required (operator or agent)"})
		return
	}
	if body.DisplayName == "" {
		writeJSON(w, 400, map[string]string{"error": "display_name is required"})
		return
	}

	if err := h.deps.ProfileStore.Put(body); err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}

	p, _ := h.deps.ProfileStore.Get(id)
	writeJSON(w, 200, p)
}

// deleteProfile handles DELETE /api/v1/admin/profiles/{id}
func (h *handler) deleteProfile(w http.ResponseWriter, r *http.Request) {
	if h.deps.ProfileStore == nil {
		writeJSON(w, 503, map[string]string{"error": "profile store not initialized"})
		return
	}
	id := chi.URLParam(r, "id")
	if err := h.deps.ProfileStore.Delete(id); err != nil {
		if isNotFound(err) {
			writeJSON(w, 404, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]string{"status": "ok", "id": id})
}

// isNotFound checks if an error message indicates a not-found condition.
func isNotFound(err error) bool {
	return err != nil && strings.HasSuffix(err.Error(), "not found")
}
