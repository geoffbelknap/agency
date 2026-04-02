package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/geoffbelknap/agency/internal/capabilities"
	"github.com/geoffbelknap/agency/internal/events"
	"github.com/geoffbelknap/agency/internal/pkg/envfile"
)

// -- Capabilities --

func (h *handler) listCapabilities(w http.ResponseWriter, r *http.Request) {
	reg := capabilities.NewRegistry(h.cfg.Home)
	caps := reg.List()
	if caps == nil {
		caps = []capabilities.Entry{}
	}
	writeJSON(w, 200, caps)
}

func (h *handler) showCapability(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	reg := capabilities.NewRegistry(h.cfg.Home)
	entry := reg.Show(name)
	if entry == nil {
		writeJSON(w, 404, map[string]string{"error": "capability not found"})
		return
	}
	writeJSON(w, 200, entry)
}

func (h *handler) enableCapability(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	var body struct {
		Key    string   `json:"key"`
		Agents []string `json:"agents"`
	}
	json.NewDecoder(r.Body).Decode(&body)
	reg := capabilities.NewRegistry(h.cfg.Home)
	if err := reg.Enable(name, body.Key, body.Agents); err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	// Hot-reload: notify running agents about the capability change
	go h.reloadCapabilitiesForRunningAgents(name)
	events.EmitCapabilityEvent(h.eventBus, "capability_granted", "", name)
	writeJSON(w, 200, map[string]string{"status": "enabled", "capability": name})
}

func (h *handler) disableCapability(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	reg := capabilities.NewRegistry(h.cfg.Home)
	if err := reg.Disable(name); err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	// Hot-reload: notify running agents about the capability change
	go h.reloadCapabilitiesForRunningAgents(name)
	events.EmitCapabilityEvent(h.eventBus, "capability_revoked", "", name)
	writeJSON(w, 200, map[string]string{"status": "disabled", "capability": name})
}

func (h *handler) addCapability(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Kind string                 `json:"kind"`
		Name string                 `json:"name"`
		Spec map[string]interface{} `json:"spec"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid JSON"})
		return
	}
	if body.Kind == "" || body.Name == "" {
		writeJSON(w, 400, map[string]string{"error": "kind and name required"})
		return
	}
	reg := capabilities.NewRegistry(h.cfg.Home)
	if err := reg.Add(body.Kind, body.Name, body.Spec); err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 201, map[string]string{"status": "added", "name": body.Name, "kind": body.Kind})
}

func (h *handler) deleteCapability(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	reg := capabilities.NewRegistry(h.cfg.Home)
	if err := reg.Delete(name); err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]string{"status": "deleted", "capability": name})
}

// loadCapabilityKeys reads ~/.agency/.capability-keys.env into a map.
func loadCapabilityKeys(home string) map[string]string {
	result := make(map[string]string)
	data, err := os.ReadFile(filepath.Join(home, ".capability-keys.env"))
	if err != nil {
		return result
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			result[parts[0]] = parts[1]
		}
	}
	return result
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

// reloadCapabilitiesForRunningAgents regenerates service manifests and signals
// running enforcers to reload after a capability is enabled, disabled, or granted.
// Runs in a goroutine so it doesn't block the HTTP response.
func (h *handler) reloadCapabilitiesForRunningAgents(capName string) {
	ctx := context.Background()

	// Get all running agents
	agents, err := h.dc.ListAgents(ctx)
	if err != nil {
		h.log.Warn("capability reload: failed to list agents", "err", err)
		return
	}

	reg := capabilities.NewRegistry(h.cfg.Home)
	allCaps := reg.List()

	// Build a map of enabled service capabilities
	enabledServices := map[string]capabilities.Entry{}
	for _, cap := range allCaps {
		if cap.Kind == "service" && cap.State != "disabled" {
			enabledServices[cap.Name] = cap
		}
	}

	for _, agent := range agents {
		if agent.Status != "running" {
			continue
		}
		name := agent.Name
		agentDir := filepath.Join(h.cfg.Home, "agents", name)

		// Generate services-manifest.json and services.yaml via unified path
		if err := h.generateAgentManifest(name); err != nil {
			h.log.Warn("capability reload: failed to generate manifest", "agent", name, "err", err)
			continue
		}

		// Read back the generated manifest to extract service info for key mapping
		var manifest struct {
			Services []map[string]interface{} `json:"services"`
		}
		if mdata, err := os.ReadFile(filepath.Join(agentDir, "services-manifest.json")); err == nil {
			json.Unmarshal(mdata, &manifest)
		}

		// Update the enforcer's service-keys.env so credential swap works.
		// Maps scoped tokens to real API keys from .capability-keys.env.
		capKeys := loadCapabilityKeys(h.cfg.Home)
		var keyLines []string
		for _, svcMap := range manifest.Services {
			svcName, _ := svcMap["service"].(string)
			scopedToken, _ := svcMap["scoped_token"].(string)
			envVar, _ := nestedStr(svcMap, "credential", "env_var")
			if scopedToken == "" || envVar == "" {
				continue
			}
			// Look up the real key from capability keys
			capKeyName := strings.ToUpper(strings.ReplaceAll(svcName, "-", "_")) + "_KEY"
			realKey := capKeys[capKeyName]
			if realKey == "" {
				continue
			}
			// Format: SCOPED_TOKEN=REAL_KEY (enforcer reads this for credential swap)
			keyLines = append(keyLines, scopedToken+"="+realKey)
			// Also write the env var mapping
			keyLines = append(keyLines, envVar+"="+realKey)
		}
		svcKeysPath := filepath.Join(h.cfg.Home, "infrastructure", ".service-keys.env")
		os.MkdirAll(filepath.Dir(svcKeysPath), 0700)
		// Use envfile.Upsert — merges with existing keys, backs up before write.
		// Preserves provider keys (ANTHROPIC_API_KEY, etc.) and manually-added keys.
		entries := map[string]string{}
		for _, line := range keyLines {
			if idx := strings.Index(line, "="); idx > 0 {
				entries[line[:idx]] = line[idx+1:]
			}
		}
		envfile.Upsert(svcKeysPath, entries)

		// Also write per-agent service keys to the agent's enforcer auth dir
		agentKeysPath := filepath.Join(agentDir, "state", "enforcer-auth", "service-keys.env")
		os.MkdirAll(filepath.Dir(agentKeysPath), 0755)
		envfile.Upsert(agentKeysPath, entries)

		// Copy service definitions to ~/.agency/services/ so the enforcer
		// (which mounts that directory) can read them for credential swap.
		hostServicesDir := filepath.Join(h.cfg.Home, "services")
		os.MkdirAll(hostServicesDir, 0755)
		for svcName := range enabledServices {
			src := filepath.Join(h.cfg.Home, "registry", "services", svcName+".yaml")
			dst := filepath.Join(hostServicesDir, svcName+".yaml")
			if data, err := os.ReadFile(src); err == nil {
				os.WriteFile(dst, data, 0644)
			}
		}

		// SIGHUP the enforcer to reload service keys and config
		enforcerName := fmt.Sprintf("agency-%s-enforcer", name)
		if err := h.dc.RawClient().ContainerKill(ctx, enforcerName, "SIGHUP"); err != nil {
			h.log.Debug("capability reload: enforcer SIGHUP failed (may not be running)", "agent", name, "err", err)
		}

		h.audit.Write(name, "capability_reload", map[string]interface{}{
			"capability": capName,
			"services":   len(manifest.Services),
		})
		h.log.Info("capability reloaded for agent", "agent", name, "capability", capName, "services", len(manifest.Services))
	}
}
