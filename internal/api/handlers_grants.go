package api

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"

	"github.com/go-chi/chi/v5"
	"gopkg.in/yaml.v3"
)

// ── Agent Grant / Revoke ────────────────────────────────────────────────────

func (h *handler) grantAgent(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	var body struct {
		Capability string `json:"capability"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid JSON"})
		return
	}
	if body.Capability == "" {
		writeJSON(w, 400, map[string]string{"error": "capability required"})
		return
	}

	// Verify agent exists
	if _, err := h.agents.Show(r.Context(), name); err != nil {
		writeJSON(w, 404, map[string]string{"error": err.Error()})
		return
	}

	// Write grant to agent's constraints
	constraintsPath := filepath.Join(h.cfg.Home, "agents", name, "constraints.yaml")
	var constraints map[string]interface{}
	if data, err := os.ReadFile(constraintsPath); err == nil {
		yaml.Unmarshal(data, &constraints)
	}
	if constraints == nil {
		constraints = map[string]interface{}{}
	}

	grants, _ := constraints["granted_capabilities"].([]interface{})
	grants = append(grants, body.Capability)
	constraints["granted_capabilities"] = grants

	data, _ := yaml.Marshal(constraints)
	os.WriteFile(constraintsPath, data, 0644)

	// Rebuild services manifest so the body runtime discovers the new tools
	if err := h.generateAgentManifest(name); err != nil {
		h.log.Warn("grant: failed to rebuild services manifest", "agent", name, "err", err)
	}

	h.log.Info("capability granted", "agent", name, "capability", body.Capability)
	h.audit.Write(name, "capability_granted", map[string]interface{}{"capability": body.Capability})
	writeJSON(w, 200, map[string]string{"status": "granted", "agent": name, "capability": body.Capability})
}

func (h *handler) revokeAgent(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	var body struct {
		Capability string `json:"capability"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid JSON"})
		return
	}
	if body.Capability == "" {
		writeJSON(w, 400, map[string]string{"error": "capability required"})
		return
	}

	// Verify agent exists
	if _, err := h.agents.Show(r.Context(), name); err != nil {
		writeJSON(w, 404, map[string]string{"error": err.Error()})
		return
	}

	// Remove grant from agent's constraints
	constraintsPath := filepath.Join(h.cfg.Home, "agents", name, "constraints.yaml")
	var constraints map[string]interface{}
	if data, err := os.ReadFile(constraintsPath); err == nil {
		yaml.Unmarshal(data, &constraints)
	}
	if constraints != nil {
		if grants, ok := constraints["granted_capabilities"].([]interface{}); ok {
			var filtered []interface{}
			for _, g := range grants {
				if s, ok := g.(string); ok && s != body.Capability {
					filtered = append(filtered, g)
				}
			}
			constraints["granted_capabilities"] = filtered
			data, _ := yaml.Marshal(constraints)
			os.WriteFile(constraintsPath, data, 0644)
		}
	}

	h.log.Info("capability revoked", "agent", name, "capability", body.Capability)
	h.audit.Write(name, "capability_revoked", map[string]interface{}{"capability": body.Capability})
	writeJSON(w, 200, map[string]string{"status": "revoked", "agent": name, "capability": body.Capability})
}
