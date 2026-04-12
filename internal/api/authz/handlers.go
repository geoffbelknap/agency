package authz

import (
	"encoding/json"
	"net/http"

	authzcore "github.com/geoffbelknap/agency/internal/authz"
)

func (h *handler) resolve(w http.ResponseWriter, r *http.Request) {
	var req authzcore.Request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
		return
	}
	decision, err := h.deps.Resolver.Resolve(req)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, decision)
}
