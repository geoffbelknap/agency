package api

import (
	"net/http"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"

	"github.com/geoffbelknap/agency/internal/hub"
	"github.com/geoffbelknap/agency/internal/models"
	"github.com/geoffbelknap/agency/internal/routing"
)

// routingMetrics returns aggregated LLM usage metrics from enforcer audit logs.
//
//	GET /api/v1/routing/metrics?agent=&since=&until=
//
// Query params:
//
//	agent — filter to a single agent (optional)
//	since — start of time window, RFC3339 or YYYY-MM-DD (default: last 24h)
//	until — end of time window, RFC3339 or YYYY-MM-DD (default: now)
func (h *handler) routingMetrics(w http.ResponseWriter, r *http.Request) {
	q := routing.MetricsQuery{
		Agent: r.URL.Query().Get("agent"),
		Since: r.URL.Query().Get("since"),
		Until: r.URL.Query().Get("until"),
	}

	// Load routing config for cost estimation.
	costs := loadModelCosts(h.cfg.Home)

	summary, err := routing.CollectWithCosts(h.cfg.Home, q, costs)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "failed to collect metrics: " + err.Error()})
		return
	}
	writeJSON(w, 200, summary)
}

// routingConfig returns the current model routing configuration (sanitised —
// no credential values, only env var names).
//
//	GET /api/v1/routing/config
func (h *handler) routingConfig(w http.ResponseWriter, r *http.Request) {
	routingPath := filepath.Join(h.cfg.Home, "infrastructure", "routing.yaml")
	data, err := os.ReadFile(routingPath)
	if err != nil {
		writeJSON(w, 200, map[string]interface{}{
			"configured": false,
			"error":      "routing.yaml not found",
		})
		return
	}

	var rc models.RoutingConfig
	if err := yaml.Unmarshal(data, &rc); err != nil {
		writeJSON(w, 500, map[string]string{"error": "failed to parse routing.yaml: " + err.Error()})
		return
	}

	// Build a sanitised view: show providers (without leaking actual keys),
	// models with cost info, tier rankings, and settings.
	type providerView struct {
		APIBase    string `json:"api_base"`
		AuthEnv    string `json:"auth_env"`
		Caching    bool   `json:"caching"`
	}
	type modelView struct {
		Provider          string  `json:"provider"`
		ProviderModel     string  `json:"provider_model"`
		CostPerMTokIn     float64 `json:"cost_per_mtok_in"`
		CostPerMTokOut    float64 `json:"cost_per_mtok_out"`
		CostPerMTokCached float64 `json:"cost_per_mtok_cached"`
	}

	providers := make(map[string]providerView, len(rc.Providers))
	for k, p := range rc.Providers {
		providers[k] = providerView{
			APIBase: p.APIBase,
			AuthEnv: p.AuthEnv,
			Caching: p.Caching,
		}
	}

	modelsMap := make(map[string]modelView, len(rc.Models))
	for k, m := range rc.Models {
		modelsMap[k] = modelView{
			Provider:          m.Provider,
			ProviderModel:     m.ProviderModel,
			CostPerMTokIn:     m.CostPerMTokIn,
			CostPerMTokOut:    m.CostPerMTokOut,
			CostPerMTokCached: m.CostPerMTokCached,
		}
	}

	writeJSON(w, 200, map[string]interface{}{
		"configured": true,
		"version":    rc.Version,
		"providers":  providers,
		"models":     modelsMap,
		"tiers":      rc.Tiers,
		"settings":   rc.Settings,
	})
}

// loadRoutingConfig reads routing.yaml (hub-managed) and routing.local.yaml
// (operator overrides), merging them. Local overlay wins on conflicts.
func loadRoutingConfig(home string) *models.RoutingConfig {
	infraDir := filepath.Join(home, "infrastructure")

	// Base: routing.yaml (managed by hub update)
	var rc models.RoutingConfig
	if data, err := os.ReadFile(filepath.Join(infraDir, "routing.yaml")); err == nil {
		yaml.Unmarshal(data, &rc)
	}

	// Overlay: routing.local.yaml (operator customizations, never hub-managed)
	if data, err := os.ReadFile(filepath.Join(infraDir, "routing.local.yaml")); err == nil {
		var local models.RoutingConfig
		if yaml.Unmarshal(data, &local) == nil {
			// Merge providers
			if rc.Providers == nil {
				rc.Providers = local.Providers
			} else {
				for k, v := range local.Providers {
					rc.Providers[k] = v
				}
			}
			// Merge models (local overrides base)
			if rc.Models == nil {
				rc.Models = local.Models
			} else {
				for k, v := range local.Models {
					rc.Models[k] = v
				}
			}
		}
	}

	return &rc
}

// loadModelCosts extracts pricing from the merged routing config.
func loadModelCosts(home string) map[string]routing.ModelCost {
	rc := loadRoutingConfig(home)
	if rc == nil || len(rc.Models) == 0 {
		return nil
	}
	costs := make(map[string]routing.ModelCost, len(rc.Models))
	for alias, m := range rc.Models {
		costs[alias] = routing.ModelCost{
			CostPerMTokIn:     m.CostPerMTokIn,
			CostPerMTokOut:    m.CostPerMTokOut,
			CostPerMTokCached: m.CostPerMTokCached,
		}
	}
	if len(costs) == 0 {
		return nil
	}
	return costs
}

// listProviders returns available LLM providers from the hub cache with credential status.
//
//	GET /api/v1/providers
func (h *handler) listProviders(w http.ResponseWriter, r *http.Request) {
	hubMgr := hub.NewManager(h.cfg.Home)

	// Get all provider components from hub cache
	available := hubMgr.Search("", "provider")

	type providerResponse struct {
		Name                 string `json:"name"`
		DisplayName          string `json:"display_name"`
		Description          string `json:"description"`
		Category             string `json:"category"`
		Installed            bool   `json:"installed"`
		CredentialName       string `json:"credential_name,omitempty"`
		CredentialLabel      string `json:"credential_label,omitempty"`
		APIKeyURL            string `json:"api_key_url,omitempty"`
		APIBaseConfigurable  bool   `json:"api_base_configurable,omitempty"`
		CredentialConfigured bool   `json:"credential_configured"`
	}

	// Check which providers are installed
	installed := hubMgr.List()
	installedNames := make(map[string]bool)
	for _, inst := range installed {
		if inst.Kind == "provider" {
			installedNames[inst.DisplayName()] = true
		}
	}

	var results []providerResponse
	for _, comp := range available {
		data, err := os.ReadFile(comp.Path)
		if err != nil {
			continue
		}
		var doc map[string]interface{}
		if yaml.Unmarshal(data, &doc) != nil {
			continue
		}

		pr := providerResponse{
			Name:        comp.Name,
			DisplayName: strField(doc, "display_name"),
			Description: comp.Description,
			Category:    strField(doc, "category"),
			Installed:   installedNames[comp.Name],
		}

		if cred, ok := doc["credential"].(map[string]interface{}); ok {
			pr.CredentialName = strField(cred, "name")
			pr.CredentialLabel = strField(cred, "label")
			pr.APIKeyURL = strField(cred, "api_key_url")

			if pr.CredentialName != "" && h.credStore != nil {
				if _, err := h.credStore.Get(pr.CredentialName); err == nil {
					pr.CredentialConfigured = true
				}
			}
		}

		if routing, ok := doc["routing"].(map[string]interface{}); ok {
			if abc, ok := routing["api_base_configurable"].(bool); ok {
				pr.APIBaseConfigurable = abc
			}
		}

		results = append(results, pr)
	}

	writeJSON(w, 200, results)
}

// strField extracts a string value from a map by key.
func strField(m map[string]interface{}, key string) string {
	v, _ := m[key].(string)
	return v
}

// setupConfig returns the wizard configuration (capability tiers) from the hub cache.
//
//	GET /api/v1/setup/config
func (h *handler) setupConfig(w http.ResponseWriter, r *http.Request) {
	hubMgr := hub.NewManager(h.cfg.Home)

	setupComps := hubMgr.Search("", "setup")

	if len(setupComps) == 0 {
		writeJSON(w, 200, map[string]interface{}{
			"capability_tiers": map[string]interface{}{},
		})
		return
	}

	data, err := os.ReadFile(setupComps[0].Path)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "failed to read setup config"})
		return
	}

	var doc map[string]interface{}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		writeJSON(w, 500, map[string]string{"error": "failed to parse setup config"})
		return
	}

	writeJSON(w, 200, doc)
}
