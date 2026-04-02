package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"gopkg.in/yaml.v3"

	"github.com/geoffbelknap/agency/internal/credstore"
	"github.com/geoffbelknap/agency/internal/hub"
	"github.com/geoffbelknap/agency/internal/pkg/envfile"
)

// ── Credential REST handlers ────────────────────────────────────────────────

// createOrUpdateCredential handles POST /api/v1/credentials
func (h *handler) createOrUpdateCredential(w http.ResponseWriter, r *http.Request) {
	if h.credStore == nil {
		writeJSON(w, 503, map[string]string{"error": "credential store not initialized"})
		return
	}

	var body struct {
		Name           string         `json:"name"`
		Value          string         `json:"value"`
		Kind           string         `json:"kind"`
		Scope          string         `json:"scope"`
		Protocol       string         `json:"protocol"`
		Service        string         `json:"service,omitempty"`
		Group          string         `json:"group,omitempty"`
		ExternalScopes []string       `json:"external_scopes,omitempty"`
		Requires       []string       `json:"requires,omitempty"`
		ExpiresAt      string         `json:"expires_at,omitempty"`
		ProtocolConfig map[string]any `json:"protocol_config,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	if body.Name == "" || body.Value == "" || body.Kind == "" || body.Scope == "" || body.Protocol == "" {
		writeJSON(w, 400, map[string]string{"error": "name, value, kind, scope, and protocol are required"})
		return
	}

	now := time.Now().UTC().Format(time.RFC3339)
	entry := credstore.Entry{
		Name:  body.Name,
		Value: body.Value,
		Metadata: credstore.Metadata{
			Kind:           body.Kind,
			Scope:          body.Scope,
			Protocol:       body.Protocol,
			Service:        body.Service,
			Group:          body.Group,
			ExternalScopes: body.ExternalScopes,
			Requires:       body.Requires,
			ExpiresAt:      body.ExpiresAt,
			ProtocolConfig: body.ProtocolConfig,
			Source:         "api",
			CreatedAt:      now,
			RotatedAt:      now,
		},
	}

	if err := h.credStore.Put(entry); err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}

	h.regenerateSwapConfig()
	writeJSON(w, 200, map[string]string{"status": "ok", "name": body.Name})
}

// listCredentials handles GET /api/v1/credentials
func (h *handler) listCredentials(w http.ResponseWriter, r *http.Request) {
	if h.credStore == nil {
		writeJSON(w, 503, map[string]string{"error": "credential store not initialized"})
		return
	}

	filter := credstore.Filter{
		Kind:    r.URL.Query().Get("kind"),
		Scope:   r.URL.Query().Get("scope"),
		Service: r.URL.Query().Get("service"),
		Group:   r.URL.Query().Get("group"),
	}

	entries, err := h.credStore.List(filter)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}

	// Check for expiring filter
	if exp := r.URL.Query().Get("expiring"); exp != "" {
		dur, err := parseDuration(exp)
		if err != nil {
			writeJSON(w, 400, map[string]string{"error": "invalid expiring duration: " + err.Error()})
			return
		}
		cutoff := time.Now().Add(dur)
		var filtered []credstore.Entry
		for _, e := range entries {
			if e.Metadata.ExpiresAt == "" {
				continue
			}
			t, err := time.Parse(time.RFC3339, e.Metadata.ExpiresAt)
			if err != nil {
				continue
			}
			if t.Before(cutoff) {
				filtered = append(filtered, e)
			}
		}
		entries = filtered
	}

	// Redact values
	type redactedEntry struct {
		Name     string            `json:"name"`
		Value    string            `json:"value"`
		Metadata credstore.Metadata `json:"metadata"`
	}
	result := make([]redactedEntry, len(entries))
	for i, e := range entries {
		result[i] = redactedEntry{
			Name:     e.Name,
			Value:    "[redacted]",
			Metadata: e.Metadata,
		}
	}

	writeJSON(w, 200, result)
}

// showCredential handles GET /api/v1/credentials/{name}
func (h *handler) showCredential(w http.ResponseWriter, r *http.Request) {
	if h.credStore == nil {
		writeJSON(w, 503, map[string]string{"error": "credential store not initialized"})
		return
	}

	name := chi.URLParam(r, "name")
	entry, err := h.credStore.Get(name)
	if err != nil {
		writeJSON(w, 404, map[string]string{"error": err.Error()})
		return
	}

	showValue := r.URL.Query().Get("show_value") == "true"
	value := "[redacted]"
	if showValue {
		value = entry.Value
		if h.audit != nil {
			h.audit.Write("platform", "credential_value_revealed", map[string]interface{}{
				"credential": name,
				"severity":   "high",
			})
		}
	}

	type response struct {
		Name     string            `json:"name"`
		Value    string            `json:"value"`
		Metadata credstore.Metadata `json:"metadata"`
	}
	writeJSON(w, 200, response{
		Name:     entry.Name,
		Value:    value,
		Metadata: entry.Metadata,
	})
}

// deleteCredential handles DELETE /api/v1/credentials/{name}
func (h *handler) deleteCredential(w http.ResponseWriter, r *http.Request) {
	if h.credStore == nil {
		writeJSON(w, 503, map[string]string{"error": "credential store not initialized"})
		return
	}

	name := chi.URLParam(r, "name")
	if err := h.credStore.Delete(name); err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}

	h.regenerateSwapConfig()
	writeJSON(w, 200, map[string]string{"status": "deleted", "name": name})
}

// rotateCredential handles POST /api/v1/credentials/{name}/rotate
func (h *handler) rotateCredential(w http.ResponseWriter, r *http.Request) {
	if h.credStore == nil {
		writeJSON(w, 503, map[string]string{"error": "credential store not initialized"})
		return
	}

	name := chi.URLParam(r, "name")
	var body struct {
		Value string `json:"value"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	if body.Value == "" {
		writeJSON(w, 400, map[string]string{"error": "value is required"})
		return
	}

	if err := h.credStore.Rotate(name, body.Value); err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}

	h.regenerateSwapConfig()
	writeJSON(w, 200, map[string]string{"status": "rotated", "name": name})
}

// testCredential handles POST /api/v1/credentials/{name}/test
func (h *handler) testCredential(w http.ResponseWriter, r *http.Request) {
	if h.credStore == nil {
		writeJSON(w, 503, map[string]string{"error": "credential store not initialized"})
		return
	}

	name := chi.URLParam(r, "name")
	result, err := h.credStore.Test(name)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, 200, result)
}

// createCredentialGroup handles POST /api/v1/credentials/groups
func (h *handler) createCredentialGroup(w http.ResponseWriter, r *http.Request) {
	if h.credStore == nil {
		writeJSON(w, 503, map[string]string{"error": "credential store not initialized"})
		return
	}

	var body struct {
		Name        string            `json:"name"`
		Protocol    string            `json:"protocol"`
		TokenURL    string            `json:"token_url,omitempty"`
		TokenParams map[string]string `json:"token_params,omitempty"`
		Requires    []string          `json:"requires,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	if body.Name == "" || body.Protocol == "" {
		writeJSON(w, 400, map[string]string{"error": "name and protocol are required"})
		return
	}

	now := time.Now().UTC().Format(time.RFC3339)
	pc := make(map[string]any)
	if body.TokenURL != "" {
		pc["token_url"] = body.TokenURL
	}
	if len(body.TokenParams) > 0 {
		tp := make(map[string]any, len(body.TokenParams))
		for k, v := range body.TokenParams {
			tp[k] = v
		}
		pc["token_params"] = tp
	}

	entry := credstore.Entry{
		Name:  body.Name,
		Value: "",
		Metadata: credstore.Metadata{
			Kind:           credstore.KindGroup,
			Scope:          "platform",
			Protocol:       body.Protocol,
			Requires:       body.Requires,
			ProtocolConfig: pc,
			Source:         "api",
			CreatedAt:      now,
			RotatedAt:      now,
		},
	}

	if err := h.credStore.Put(entry); err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}

	h.regenerateSwapConfig()
	writeJSON(w, 200, map[string]string{"status": "ok", "name": body.Name, "kind": "group"})
}


// regenerateSwapConfig rebuilds credential-swaps.yaml from the credential
// store. If the store is nil or empty, it falls back to the legacy hub-based
// generation so existing file-based setups keep working.
func (h *handler) regenerateSwapConfig() {
	if h.credStore == nil {
		hub.WriteSwapConfig(h.cfg.Home)
		return
	}

	data, err := h.credStore.GenerateSwapConfig()
	if err != nil {
		h.log.Warn("failed to generate swap config from store", "err", err)
		hub.WriteSwapConfig(h.cfg.Home)
		return
	}

	// If the store produced an empty swap map, fall back to legacy so that
	// service-definition / routing-based entries are still generated.
	if len(data) == 0 {
		hub.WriteSwapConfig(h.cfg.Home)
		return
	}

	// Merge: generate legacy config, then overlay store entries on top.
	// This ensures service-definition swaps survive while the store is
	// being gradually populated.
	legacyData, legacyErr := hub.GenerateSwapConfig(h.cfg.Home)

	swapPath := filepath.Join(h.cfg.Home, "infrastructure", "credential-swaps.yaml")
	os.MkdirAll(filepath.Dir(swapPath), 0755)

	if legacyErr == nil && len(legacyData) > 0 {
		// Parse both, merge store entries on top of legacy
		var legacy hub.SwapConfigFile
		var store hub.SwapConfigFile
		if yaml.Unmarshal(legacyData, &legacy) == nil && yaml.Unmarshal(data, &store) == nil {
			if legacy.Swaps == nil {
				legacy.Swaps = map[string]hub.SwapEntry{}
			}
			for k, v := range store.Swaps {
				legacy.Swaps[k] = v
			}
			if merged, err := yaml.Marshal(legacy); err == nil {
				os.WriteFile(swapPath, merged, 0644)
				return
			}
		}
	}

	// If merge failed, write store-only config
	os.WriteFile(swapPath, data, 0644)
}

// resolveCredential handles GET /api/v1/internal/credentials/resolve
// Returns the decrypted credential value + protocol metadata for a given name.
// Used by the egress proxy to resolve credentials from the store.
// Auth: socket access IS authorization (restricted socket router, no BearerAuth).
// Also registered on the main router with BearerAuth middleware for direct API calls.
func (h *handler) resolveCredential(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	if name == "" {
		writeJSON(w, 400, map[string]string{"error": "name parameter required"})
		return
	}

	// Try credential store first
	if h.credStore != nil {
		entry, err := h.credStore.Get(name)
		if err == nil {
			// Resolve group config if needed
			resolved, err := credstore.ResolveGroup(*entry, h.credStore.Backend())
			if err != nil {
				writeJSON(w, 500, map[string]string{"error": "group resolution: " + err.Error()})
				return
			}

			result := map[string]interface{}{
				"name":  resolved.Name,
				"value": resolved.Value,
			}
			if resolved.Metadata.Protocol != "" {
				result["protocol"] = resolved.Metadata.Protocol
			}
			if len(resolved.Metadata.ProtocolConfig) > 0 {
				result["protocol_config"] = resolved.Metadata.ProtocolConfig
			}

			if h.audit != nil {
				h.audit.Write("platform", "credential_resolved", map[string]interface{}{
					"credential": name,
					"source":     "store",
					"caller":     "egress",
				})
			}

			writeJSON(w, 200, result)
			return
		}
	}

	// Fallback: read from .service-keys.env
	keys := envfile.Load(filepath.Join(h.cfg.Home, "infrastructure", ".service-keys.env"))
	if val, ok := keys[name]; ok {
		if h.audit != nil {
			h.audit.Write("platform", "credential_resolved", map[string]interface{}{
				"credential": name,
				"source":     "envfile",
				"caller":     "egress",
			})
		}
		writeJSON(w, 200, map[string]string{"name": name, "value": val})
		return
	}

	writeJSON(w, 404, map[string]string{"error": "credential not found"})
}

// parseDuration parses a duration string like "7d", "24h", "30m".
func parseDuration(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if strings.HasSuffix(s, "d") {
		s = strings.TrimSuffix(s, "d")
		var days int
		if _, err := fmt.Sscanf(s, "%d", &days); err != nil {
			return 0, err
		}
		return time.Duration(days) * 24 * time.Hour, nil
	}
	return time.ParseDuration(s)
}
