package infra

import (
	"net/http"
	"os"
	"strings"

	"github.com/go-chi/chi/v5"
	"gopkg.in/yaml.v3"

	"github.com/geoffbelknap/agency/internal/hub"
	"github.com/geoffbelknap/agency/internal/models"
	"github.com/geoffbelknap/agency/internal/routing"
)

// routingMetrics returns aggregated LLM usage metrics from enforcer audit logs.
//
//	GET /api/v1/infra/routing/metrics?agent=&since=&until=
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
	costs := loadModelCosts(h.deps.Config.Home)

	summary, err := routing.CollectWithCosts(h.deps.Config.Home, q, costs)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "failed to collect metrics: " + err.Error()})
		return
	}
	writeJSON(w, 200, summary)
}

// routingConfig returns the current model routing configuration (sanitised —
// no credential values, only env var names).
//
//	GET /api/v1/infra/routing/config
func (h *handler) routingConfig(w http.ResponseWriter, r *http.Request) {
	rc := loadRoutingConfig(h.deps.Config.Home)
	if rc == nil {
		writeJSON(w, 200, map[string]interface{}{
			"configured": false,
			"error":      "routing.yaml not found",
		})
		return
	}

	// Build a sanitised view: show providers (without leaking actual keys),
	// models with cost info, tier rankings, and settings.
	type providerView struct {
		APIBase string `json:"api_base"`
		AuthEnv string `json:"auth_env"`
		Caching bool   `json:"caching"`
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

	configured := false
	for _, p := range rc.Providers {
		if h.credentialConfigured(p.AuthEnv) {
			configured = true
			break
		}
	}
	writeJSON(w, 200, map[string]interface{}{
		"configured": configured,
		"version":    rc.Version,
		"providers":  providers,
		"models":     modelsMap,
		"tiers":      rc.Tiers,
		"settings":   rc.Settings,
	})
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
//	GET /api/v1/infra/providers
func (h *handler) listProviders(w http.ResponseWriter, r *http.Request) {
	hubMgr := hub.NewManager(h.deps.Config.Home)

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
			credentialNames := []string{pr.CredentialName}
			if envVar := strField(cred, "env_var"); envVar != "" {
				credentialNames = append(credentialNames, envVar)
			}

			for _, name := range credentialNames {
				if h.credentialConfigured(name) {
					pr.CredentialConfigured = true
					break
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

func (h *handler) credentialConfigured(name string) bool {
	if name == "" {
		return true
	}
	if h.deps.CredStore == nil {
		return false
	}
	for _, candidate := range credentialNameCandidates(name) {
		if _, err := h.deps.CredStore.Get(candidate); err == nil {
			return true
		}
	}
	return false
}

func credentialNameCandidates(name string) []string {
	normalized := strings.ToLower(strings.ReplaceAll(name, "_", "-"))
	if normalized == name {
		return []string{name}
	}
	return []string{name, normalized}
}

// setupConfig returns the wizard configuration (capability tiers) from the hub cache.
//
//	GET /api/v1/infra/setup/config
func (h *handler) setupConfig(w http.ResponseWriter, r *http.Request) {
	hubMgr := hub.NewManager(h.deps.Config.Home)

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

// LoadModelCosts is an exported wrapper around loadModelCosts for use by
// the parent api package's budget handlers.
func LoadModelCosts(home string) map[string]routing.ModelCost {
	return loadModelCosts(home)
}

// LoadRoutingConfig is an exported wrapper around loadRoutingConfig for use
// by the parent api package's internal LLM handler and budget handlers.
func LoadRoutingConfig(home string) *models.RoutingConfig {
	return loadRoutingConfig(home)
}

// routingSuggestions returns routing optimization suggestions.
//
//	GET /api/v1/infra/routing/suggestions?status=pending
//
// Query params:
//
//	status — filter by suggestion status: pending, approved, rejected (optional)
func (h *handler) routingSuggestions(w http.ResponseWriter, r *http.Request) {
	if h.deps.Infra == nil || h.deps.Infra.Optimizer == nil {
		writeJSON(w, 503, map[string]string{"error": "routing optimizer not available"})
		return
	}

	suggestions := h.deps.Infra.Optimizer.Suggestions()

	if status := r.URL.Query().Get("status"); status != "" {
		var filtered []interface{}
		for _, s := range suggestions {
			if s.Status == status {
				filtered = append(filtered, s)
			}
		}
		if filtered == nil {
			filtered = []interface{}{}
		}
		writeJSON(w, 200, filtered)
		return
	}

	writeJSON(w, 200, suggestions)
}

// routingSuggestionApprove approves a routing suggestion.
//
//	POST /api/v1/infra/routing/suggestions/{id}/approve
func (h *handler) routingSuggestionApprove(w http.ResponseWriter, r *http.Request) {
	if h.deps.Infra == nil || h.deps.Infra.Optimizer == nil {
		writeJSON(w, 503, map[string]string{"error": "routing optimizer not available"})
		return
	}

	id := chi.URLParam(r, "id")
	if id == "" {
		writeJSON(w, 400, map[string]string{"error": "missing suggestion id"})
		return
	}

	suggestion, err := h.deps.Infra.Optimizer.Approve(id)
	if err != nil {
		writeJSON(w, 404, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, 200, suggestion)
}

// routingSuggestionReject rejects a routing suggestion.
//
//	POST /api/v1/infra/routing/suggestions/{id}/reject
func (h *handler) routingSuggestionReject(w http.ResponseWriter, r *http.Request) {
	if h.deps.Infra == nil || h.deps.Infra.Optimizer == nil {
		writeJSON(w, 503, map[string]string{"error": "routing optimizer not available"})
		return
	}

	id := chi.URLParam(r, "id")
	if id == "" {
		writeJSON(w, 400, map[string]string{"error": "missing suggestion id"})
		return
	}

	if err := h.deps.Infra.Optimizer.Reject(id); err != nil {
		writeJSON(w, 404, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, 200, map[string]string{"status": "rejected", "id": id})
}

// routingStats returns per-model per-task-type statistics from the optimizer.
//
//	GET /api/v1/infra/routing/stats?task_type=tool_use
//
// Query params:
//
//	task_type — filter to a single task type (optional)
func (h *handler) routingStats(w http.ResponseWriter, r *http.Request) {
	if h.deps.Infra == nil || h.deps.Infra.Optimizer == nil {
		writeJSON(w, 503, map[string]string{"error": "routing optimizer not available"})
		return
	}

	stats := h.deps.Infra.Optimizer.Stats()

	if taskType := r.URL.Query().Get("task_type"); taskType != "" {
		var filtered []interface{}
		for _, s := range stats {
			if s.TaskType == taskType {
				filtered = append(filtered, s)
			}
		}
		if filtered == nil {
			filtered = []interface{}{}
		}
		writeJSON(w, 200, filtered)
		return
	}

	writeJSON(w, 200, stats)
}
