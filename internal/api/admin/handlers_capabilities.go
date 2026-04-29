package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/geoffbelknap/agency/internal/capabilities"
	"github.com/geoffbelknap/agency/internal/credstore"
	"github.com/geoffbelknap/agency/internal/events"
	runtimecontract "github.com/geoffbelknap/agency/internal/runtime/contract"
)

// -- Capabilities --

func (h *handler) listCapabilities(w http.ResponseWriter, r *http.Request) {
	reg := capabilities.NewRegistry(h.deps.Config.Home)
	caps := reg.List()
	if caps == nil {
		caps = []capabilities.Entry{}
	}
	writeJSON(w, 200, caps)
}

func (h *handler) showCapability(w http.ResponseWriter, r *http.Request) {
	name, ok := requireName(w, chi.URLParam(r, "name"))
	if !ok {
		return
	}
	reg := capabilities.NewRegistry(h.deps.Config.Home)
	entry := reg.Show(name)
	if entry == nil {
		writeJSON(w, 404, map[string]string{"error": "capability not found"})
		return
	}
	writeJSON(w, 200, entry)
}

func (h *handler) enableCapability(w http.ResponseWriter, r *http.Request) {
	name, ok := requireName(w, chi.URLParam(r, "name"))
	if !ok {
		return
	}
	var body struct {
		Key    string   `json:"key"`
		Agents []string `json:"agents"`
	}
	json.NewDecoder(r.Body).Decode(&body)
	reg := capabilities.NewRegistry(h.deps.Config.Home)
	// Pass empty key — credential storage is handled via the credential store below.
	if err := reg.Enable(name, "", body.Agents); err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	// Store capability key in credential store
	if body.Key != "" && h.deps.CredStore != nil {
		capKeyName := strings.ToUpper(strings.ReplaceAll(name, "-", "_")) + "_KEY"
		now := time.Now().UTC().Format(time.RFC3339)
		if err := h.deps.CredStore.Put(credstore.Entry{
			Name:  capKeyName,
			Value: body.Key,
			Metadata: credstore.Metadata{
				Kind:      "service",
				Scope:     "platform",
				Service:   name,
				Protocol:  "api-key",
				Source:    "capability",
				CreatedAt: now,
				RotatedAt: now,
			},
		}); err != nil {
			h.deps.Logger.Warn("capability: failed to store key in credential store", "name", name, "err", err)
		}
		h.regenerateSwapConfig()
	}
	// Hot-reload: notify running agents about the capability change
	go h.reloadCapabilitiesForRunningAgents(name)
	events.EmitCapabilityEvent(h.deps.EventBus, "capability_granted", "", name)
	writeJSON(w, 200, map[string]string{"status": "enabled", "capability": name})
}

func (h *handler) disableCapability(w http.ResponseWriter, r *http.Request) {
	name, ok := requireName(w, chi.URLParam(r, "name"))
	if !ok {
		return
	}
	reg := capabilities.NewRegistry(h.deps.Config.Home)
	if err := reg.Disable(name); err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	// Hot-reload: notify running agents about the capability change
	go h.reloadCapabilitiesForRunningAgents(name)
	events.EmitCapabilityEvent(h.deps.EventBus, "capability_revoked", "", name)
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
	reg := capabilities.NewRegistry(h.deps.Config.Home)
	if err := reg.Add(body.Kind, body.Name, body.Spec); err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 201, map[string]string{"status": "added", "name": body.Name, "kind": body.Kind})
}

func (h *handler) deleteCapability(w http.ResponseWriter, r *http.Request) {
	name, ok := requireName(w, chi.URLParam(r, "name"))
	if !ok {
		return
	}
	reg := capabilities.NewRegistry(h.deps.Config.Home)
	if err := reg.Delete(name); err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]string{"status": "deleted", "capability": name})
}

// reloadCapabilitiesForRunningAgents regenerates service manifests, stores
// credential mappings in the credential store, and signals running enforcers
// to reload after a capability is enabled, disabled, or granted.
// Runs in a goroutine so it doesn't block the HTTP response.
func (h *handler) reloadCapabilitiesForRunningAgents(capName string) {
	ctx := context.Background()

	// Get all running agents
	agents, err := h.deps.Runtime.ListAgents(ctx)
	if err != nil {
		h.deps.Logger.Warn("capability reload: failed to list agents", "err", err)
		return
	}

	reg := capabilities.NewRegistry(h.deps.Config.Home)
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
		agentDir := filepath.Join(h.deps.Config.Home, "agents", name)

		// Generate services-manifest.json and services.yaml via unified path
		if err := h.generateAgentManifest(name); err != nil {
			h.deps.Logger.Warn("capability reload: failed to generate manifest", "agent", name, "err", err)
			continue
		}

		// Read back the generated manifest to extract service info for key mapping
		var manifest struct {
			Services []map[string]interface{} `json:"services"`
		}
		if mdata, err := os.ReadFile(filepath.Join(agentDir, "services-manifest.json")); err == nil {
			json.Unmarshal(mdata, &manifest)
		}

		// Store credential mappings in the credential store. The egress proxy
		// resolves credentials via the gateway socket — no flat files needed.
		entries := map[string]string{}
		for _, svcMap := range manifest.Services {
			svcName, _ := svcMap["service"].(string)
			scopedToken, _ := svcMap["scoped_token"].(string)
			envVar, _ := nestedStr(svcMap, "credential", "env_var")
			if scopedToken == "" || envVar == "" {
				continue
			}
			// Look up the real key from credential store
			capKeyName := strings.ToUpper(strings.ReplaceAll(svcName, "-", "_")) + "_KEY"
			var realKey string
			if h.deps.CredStore != nil {
				if entry, err := h.deps.CredStore.Get(capKeyName); err == nil {
					realKey = entry.Value
				}
			}
			if realKey == "" {
				continue
			}
			entries[scopedToken] = realKey
			entries[envVar] = realKey
		}

		// Write service credential mappings to credential store
		if h.deps.CredStore != nil && len(entries) > 0 {
			now := time.Now().UTC().Format(time.RFC3339)
			for k, v := range entries {
				_ = h.deps.CredStore.Put(credstore.Entry{
					Name:  k,
					Value: v,
					Metadata: credstore.Metadata{
						Kind:      "service",
						Scope:     "platform",
						Protocol:  "api-key",
						Source:    "capability-reload",
						CreatedAt: now,
						RotatedAt: now,
					},
				})
			}
			h.regenerateSwapConfig()
		}

		// Copy service definitions to ~/.agency/services/ so the enforcer
		// (which mounts that directory) can read them for credential swap.
		hostServicesDir := filepath.Join(h.deps.Config.Home, "services")
		os.MkdirAll(hostServicesDir, 0755)
		for svcName := range enabledServices {
			src := filepath.Join(h.deps.Config.Home, "registry", "services", svcName+".yaml")
			dst := filepath.Join(hostServicesDir, svcName+".yaml")
			if data, err := os.ReadFile(src); err == nil {
				os.WriteFile(dst, data, 0644)
			}
		}

		// SIGHUP the enforcer to reload service keys and config
		if h.deps.AgentManager != nil && h.deps.AgentManager.Runtime != nil {
			if err := h.deps.AgentManager.Runtime.ReloadEnforcer(ctx, name); err != nil {
				h.deps.Logger.Debug("capability reload: runtime reload failed (may not be running)", "agent", name, "err", err)
			}
		} else if h.deps.Signal != nil {
			ref := runtimecontract.InstanceRef{RuntimeID: name, Role: runtimecontract.RoleEnforcer}
			if err := h.deps.Signal.Signal(ctx, ref, "SIGHUP"); err != nil {
				h.deps.Logger.Debug("capability reload: enforcer SIGHUP failed (may not be running)", "agent", name, "err", err)
			}
		}

		h.deps.Audit.Write(name, "capability_reload", map[string]interface{}{
			"capability": capName,
			"services":   len(manifest.Services),
		})
		h.deps.Logger.Info("capability reloaded for agent", "agent", name, "capability", capName, "services", len(manifest.Services))
	}
}
