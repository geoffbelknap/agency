package api

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/geoffbelknap/agency/internal/knowledge"
)

// knowledgePrincipalsList proxies GET /principals to the knowledge service.
// Returns all registered principals, optionally filtered by type.
// ASK tenet 6: all trust is explicit and auditable — principal registry is the source of truth.
func (h *handler) knowledgePrincipalsList(w http.ResponseWriter, r *http.Request) {
	principalType := r.URL.Query().Get("type")
	proxy := knowledge.NewProxy()
	data, err := proxy.Principals(r.Context(), principalType)
	if err != nil {
		writeJSON(w, 502, map[string]string{"error": "knowledge service unavailable: " + err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)
	w.Write(data)
}

// knowledgePrincipalsRegister proxies POST /principals to the knowledge service.
// Registers a new principal with a given type and name.
// ASK tenet 6: all trust is explicit and auditable — principals must be registered before use.
func (h *handler) knowledgePrincipalsRegister(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Type string `json:"type"`
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid JSON"})
		return
	}
	if body.Type == "" {
		writeJSON(w, 400, map[string]string{"error": "type required"})
		return
	}
	if body.Name == "" {
		writeJSON(w, 400, map[string]string{"error": "name required"})
		return
	}

	proxy := knowledge.NewProxy()
	data, err := proxy.RegisterPrincipal(r.Context(), body.Type, body.Name)
	if err != nil {
		writeJSON(w, 502, map[string]string{"error": "knowledge service unavailable: " + err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(201)
	w.Write(data)
}

// knowledgePrincipalsResolve proxies GET /principals/{uuid} to the knowledge service.
// Resolves a principal by UUID.
func (h *handler) knowledgePrincipalsResolve(w http.ResponseWriter, r *http.Request) {
	uuid := chi.URLParam(r, "uuid")
	if uuid == "" {
		writeJSON(w, 400, map[string]string{"error": "uuid required"})
		return
	}
	proxy := knowledge.NewProxy()
	data, err := proxy.ResolvePrincipal(r.Context(), uuid)
	if err != nil {
		writeJSON(w, 502, map[string]string{"error": "knowledge service unavailable: " + err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)
	w.Write(data)
}
