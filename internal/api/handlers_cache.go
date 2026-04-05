package api

import (
	"net/http"

	"github.com/geoffbelknap/agency/internal/knowledge"
	"github.com/go-chi/chi/v5"
)

// clearAgentCache handles DELETE /api/v1/agents/{name}/cache
// Soft-deletes all cached_result nodes for the specified agent via the knowledge service.
func (h *handler) clearAgentCache(w http.ResponseWriter, r *http.Request) {
	agentName := chi.URLParam(r, "name")
	if agentName == "" {
		writeJSON(w, 400, map[string]string{"error": "missing agent name"})
		return
	}

	kp := knowledge.NewProxy()
	body := map[string]interface{}{
		"kind":   "cached_result",
		"filter": map[string]string{"agent": agentName},
	}
	data, err := kp.Post(r.Context(), "/delete-by-kind", body)
	if err != nil {
		writeJSON(w, 502, map[string]string{"error": "failed to clear cache: " + err.Error()})
		return
	}

	// Forward knowledge service response (contains deleted count)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)
	w.Write(data)
}
