package agents

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

func (h *handler) showRuntimeManifest(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if h.deps.AgentManager == nil || h.deps.AgentManager.Runtime == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "runtime supervisor not initialized"})
		return
	}
	manifest, err := h.deps.AgentManager.Runtime.Manifest(name)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, manifest)
}

func (h *handler) showRuntimeStatus(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if h.deps.AgentManager == nil || h.deps.AgentManager.Runtime == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "runtime supervisor not initialized"})
		return
	}
	status, err := h.deps.AgentManager.Runtime.Get(r.Context(), name)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func (h *handler) validateRuntime(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if h.deps.AgentManager == nil || h.deps.AgentManager.Runtime == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "runtime supervisor not initialized"})
		return
	}
	if err := h.deps.AgentManager.Runtime.Validate(r.Context(), name); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"status": "invalid", "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "valid"})
}
