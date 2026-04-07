package api

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/geoffbelknap/agency/internal/registry"
)

// -- Principal Registry --

// registrySnapshot returns a full JSON snapshot of all registered principals.
func (h *handler) registrySnapshot(w http.ResponseWriter, r *http.Request) {
	if h.infra == nil || h.infra.Registry == nil {
		writeJSON(w, 503, map[string]string{"error": "registry not available"})
		return
	}
	data, err := h.infra.Registry.Snapshot()
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(data)
}

// registryResolve resolves a principal by ?uuid= or by ?type=&name=.
func (h *handler) registryResolve(w http.ResponseWriter, r *http.Request) {
	if h.infra == nil || h.infra.Registry == nil {
		writeJSON(w, 503, map[string]string{"error": "registry not available"})
		return
	}

	uuid := r.URL.Query().Get("uuid")
	pType := r.URL.Query().Get("type")
	name := r.URL.Query().Get("name")

	var p *registry.Principal
	var err error

	if uuid != "" {
		p, err = h.infra.Registry.Resolve(uuid)
	} else if pType != "" && name != "" {
		p, err = h.infra.Registry.ResolveByName(pType, name)
	} else {
		writeJSON(w, 400, map[string]string{"error": "provide ?uuid= or ?type=&name="})
		return
	}

	if err != nil {
		writeJSON(w, 404, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, p)
}

// registryList lists principals, optionally filtered by ?type=.
func (h *handler) registryList(w http.ResponseWriter, r *http.Request) {
	if h.infra == nil || h.infra.Registry == nil {
		writeJSON(w, 503, map[string]string{"error": "registry not available"})
		return
	}
	pType := r.URL.Query().Get("type")
	principals, err := h.infra.Registry.List(pType)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	if principals == nil {
		principals = []registry.Principal{}
	}
	writeJSON(w, 200, principals)
}

// registryRegister creates a new principal. Body: {type, name, parent?, metadata?}
func (h *handler) registryRegister(w http.ResponseWriter, r *http.Request) {
	if h.infra == nil || h.infra.Registry == nil {
		writeJSON(w, 503, map[string]string{"error": "registry not available"})
		return
	}

	var body struct {
		Type     string                 `json:"type"`
		Name     string                 `json:"name"`
		Parent   string                 `json:"parent"`
		Metadata map[string]interface{} `json:"metadata"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid JSON"})
		return
	}
	if body.Type == "" || body.Name == "" {
		writeJSON(w, 400, map[string]string{"error": "type and name are required"})
		return
	}

	var opts []registry.Option
	if body.Parent != "" {
		opts = append(opts, registry.WithParent(body.Parent))
	}
	if body.Metadata != nil {
		opts = append(opts, registry.WithMetadata(body.Metadata))
	}

	uuid, err := h.infra.Registry.Register(body.Type, body.Name, opts...)
	if err != nil {
		writeJSON(w, 409, map[string]string{"error": err.Error()})
		return
	}

	if err := h.infra.WriteRegistrySnapshot(); err != nil {
		h.log.Warn("write registry snapshot", "err", err)
	}

	writeJSON(w, 201, map[string]string{"uuid": uuid, "type": body.Type, "name": body.Name})
}

// registryUpdate updates allowed fields on a principal. Body: {parent?, status?, permissions?}
func (h *handler) registryUpdate(w http.ResponseWriter, r *http.Request) {
	if h.infra == nil || h.infra.Registry == nil {
		writeJSON(w, 503, map[string]string{"error": "registry not available"})
		return
	}

	uuid := chi.URLParam(r, "uuid")
	if uuid == "" {
		writeJSON(w, 400, map[string]string{"error": "uuid is required"})
		return
	}

	var fields map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&fields); err != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid JSON"})
		return
	}

	// Validate suspension/revocation: check for coverage principal.
	if status, ok := fields["status"].(string); ok && (status == "suspended" || status == "revoked") {
		p, err := h.infra.Registry.Resolve(uuid)
		if err != nil {
			writeJSON(w, 404, map[string]string{"error": err.Error()})
			return
		}
		if p.Parent == "" {
			force := r.URL.Query().Get("force")
			if force != "true" {
				writeJSON(w, 400, map[string]string{
					"error": "no coverage principal — governed entities will fail-closed. Use ?force=true",
				})
				return
			}
		}
		// Revocation also revokes all tokens for the principal.
		if status == "revoked" {
			if err := h.infra.Registry.RevokeTokens(uuid); err != nil {
				writeJSON(w, 500, map[string]string{"error": "revoke tokens: " + err.Error()})
				return
			}
		}
	}

	if err := h.infra.Registry.Update(uuid, fields); err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}

	if err := h.infra.WriteRegistrySnapshot(); err != nil {
		h.log.Warn("write registry snapshot", "err", err)
	}

	p, err := h.infra.Registry.Resolve(uuid)
	if err != nil {
		writeJSON(w, 200, map[string]string{"status": "updated"})
		return
	}
	writeJSON(w, 200, p)
}

// registryEffective returns the effective (resolved) permissions for a principal.
func (h *handler) registryEffective(w http.ResponseWriter, r *http.Request) {
	if h.infra == nil || h.infra.Registry == nil {
		writeJSON(w, 503, map[string]string{"error": "registry not available"})
		return
	}

	uuid := chi.URLParam(r, "uuid")
	if uuid == "" {
		http.Error(w, "uuid required", http.StatusBadRequest)
		return
	}
	eff, err := h.infra.Registry.EffectivePermissions(uuid)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"uuid":                  uuid,
		"effective_permissions": eff,
	})
}

// registryDelete removes a principal by UUID.
func (h *handler) registryDelete(w http.ResponseWriter, r *http.Request) {
	if h.infra == nil || h.infra.Registry == nil {
		writeJSON(w, 503, map[string]string{"error": "registry not available"})
		return
	}

	uuid := chi.URLParam(r, "uuid")
	if uuid == "" {
		writeJSON(w, 400, map[string]string{"error": "uuid is required"})
		return
	}

	if err := h.infra.Registry.Delete(uuid); err != nil {
		writeJSON(w, 404, map[string]string{"error": err.Error()})
		return
	}

	if err := h.infra.WriteRegistrySnapshot(); err != nil {
		h.log.Warn("write registry snapshot", "err", err)
	}

	writeJSON(w, 200, map[string]string{"status": "deleted", "uuid": uuid})
}
