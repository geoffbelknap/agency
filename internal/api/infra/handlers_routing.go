package infra

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/geoffbelknap/agency/internal/hub"
	"github.com/geoffbelknap/agency/internal/models"
	"github.com/geoffbelknap/agency/internal/pkg/urlsafety"
	"github.com/geoffbelknap/agency/internal/providercatalog"
	"github.com/geoffbelknap/agency/internal/routing"
	"gopkg.in/yaml.v3"
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
		Provider                 string                              `json:"provider"`
		ProviderModel            string                              `json:"provider_model"`
		ProviderToolCapabilities []string                            `json:"provider_tool_capabilities,omitempty"`
		ProviderToolPricing      map[string]models.ProviderToolPrice `json:"provider_tool_pricing,omitempty"`
		CostPerMTokIn            float64                             `json:"cost_per_mtok_in"`
		CostPerMTokOut           float64                             `json:"cost_per_mtok_out"`
		CostPerMTokCached        float64                             `json:"cost_per_mtok_cached"`
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
			Provider:                 m.Provider,
			ProviderModel:            m.ProviderModel,
			ProviderToolCapabilities: m.ProviderToolCapabilities,
			ProviderToolPricing:      m.ProviderToolPricing,
			CostPerMTokIn:            m.CostPerMTokIn,
			CostPerMTokOut:           m.CostPerMTokOut,
			CostPerMTokCached:        m.CostPerMTokCached,
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
			CostPerMTokIn:       m.CostPerMTokIn,
			CostPerMTokOut:      m.CostPerMTokOut,
			CostPerMTokCached:   m.CostPerMTokCached,
			ProviderToolCosts:   m.ProviderToolCosts,
			ProviderToolPricing: routingProviderToolPricing(m.ProviderToolPricing),
		}
	}
	if len(costs) == 0 {
		return nil
	}
	return costs
}

func routingProviderToolPricing(in map[string]models.ProviderToolPrice) map[string]routing.ProviderToolPrice {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]routing.ProviderToolPrice, len(in))
	for cap, p := range in {
		out[cap] = routing.ProviderToolPrice{
			Unit:        p.Unit,
			USDPerUnit:  p.USDPerUnit,
			Source:      p.Source,
			Confidence:  p.Confidence,
			Description: p.Description,
		}
	}
	return out
}

// listProviders returns available bundled LLM providers with credential status.
//
//	GET /api/v1/infra/providers
func (h *handler) listProviders(w http.ResponseWriter, r *http.Request) {
	type providerResponse struct {
		Name                  string `json:"name"`
		DisplayName           string `json:"display_name"`
		Description           string `json:"description"`
		Category              string `json:"category"`
		QuickstartSelectable  bool   `json:"quickstart_selectable,omitempty"`
		QuickstartOrder       int    `json:"quickstart_order,omitempty"`
		QuickstartRecommended bool   `json:"quickstart_recommended,omitempty"`
		QuickstartPromptBlurb string `json:"quickstart_prompt_blurb,omitempty"`
		Installed             bool   `json:"installed"`
		CredentialName        string `json:"credential_name,omitempty"`
		CredentialLabel       string `json:"credential_label,omitempty"`
		APIKeyURL             string `json:"api_key_url,omitempty"`
		APIBaseConfigurable   bool   `json:"api_base_configurable,omitempty"`
		CredentialConfigured  bool   `json:"credential_configured"`
	}

	installedProviders := map[string]bool{}
	if rc := loadRoutingConfig(h.deps.Config.Home); rc != nil {
		for name := range rc.Providers {
			installedProviders[name] = true
		}
	}

	available, err := providercatalog.List()
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "failed to load bundled providers"})
		return
	}
	var results []providerResponse
	for _, doc := range available {

		pr := providerResponse{
			Name:        doc.Name,
			DisplayName: doc.DisplayName,
			Description: doc.Description,
			Category:    doc.Category,
			Installed:   installedProviders[doc.Name],
		}
		if qs := doc.Quickstart; qs != nil {
			pr.QuickstartSelectable = qs.Selectable
			pr.QuickstartOrder = qs.Order
			pr.QuickstartRecommended = qs.Recommended
			pr.QuickstartPromptBlurb = qs.PromptBlurb
		}

		if cred := doc.Credential; cred != nil {
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

		if routing := doc.Routing; routing != nil {
			if abc, ok := routing["api_base_configurable"].(bool); ok {
				pr.APIBaseConfigurable = abc
			}
		}

		results = append(results, pr)
	}

	writeJSON(w, 200, results)
}

// providerTools returns the canonical bundled provider-tool inventory.
//
//	GET /api/v1/infra/provider-tools
func (h *handler) providerTools(w http.ResponseWriter, r *http.Request) {
	inv, err := providercatalog.ProviderTools()
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "failed to load provider tool inventory"})
		return
	}
	writeJSON(w, 200, inv)
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

type providerVerifyRequest struct {
	APIKey  string `json:"api_key"`
	APIBase string `json:"api_base"`
}

type providerInstallRequest struct {
	APIBase string `json:"api_base"`
}

// verifyProvider validates a provider using its declared verification probe.
//
//	POST /api/v1/infra/providers/{name}/verify
func (h *handler) verifyProvider(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSpace(chi.URLParam(r, "name"))
	if name == "" {
		writeJSON(w, 400, map[string]string{"error": "provider name required"})
		return
	}

	var req providerVerifyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err != io.EOF {
		writeJSON(w, 400, map[string]string{"error": "invalid request body"})
		return
	}

	doc, _, err := providercatalog.Get(name)
	if err != nil {
		writeJSON(w, 404, map[string]string{"error": err.Error()})
		return
	}

	if doc.Quickstart == nil || doc.Quickstart.Probe == nil {
		writeJSON(w, 200, map[string]interface{}{
			"ok":      true,
			"message": "No verification probe configured for this provider.",
		})
		return
	}

	apiKey := strings.TrimSpace(req.APIKey)
	if apiKey == "" {
		apiKey = h.providerCredentialValue(doc)
	}
	if doc.Credential != nil && apiKey == "" {
		writeJSON(w, 400, map[string]interface{}{
			"ok":      false,
			"message": "Provider credential is required before verification.",
		})
		return
	}

	started := time.Now()
	statusCode, message, err := performProviderProbe(doc, doc.Quickstart.Probe, apiKey, strings.TrimSpace(req.APIBase))
	latency := time.Since(started).Milliseconds()
	if err != nil {
		writeJSON(w, 200, map[string]interface{}{
			"ok":         false,
			"status":     statusCode,
			"message":    err.Error(),
			"latency_ms": latency,
		})
		return
	}

	writeJSON(w, 200, map[string]interface{}{
		"ok":         true,
		"status":     statusCode,
		"message":    message,
		"latency_ms": latency,
	})
}

// installProvider merges a bundled provider definition into routing.yaml.
//
//	POST /api/v1/infra/providers/{name}/install
func (h *handler) installProvider(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSpace(chi.URLParam(r, "name"))
	if name == "" {
		writeJSON(w, 400, map[string]string{"error": "provider name required"})
		return
	}
	var req providerInstallRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err != io.EOF {
		writeJSON(w, 400, map[string]string{"error": "invalid request body"})
		return
	}
	if err := installBundledProviderRouting(h.deps.Config.Home, name, strings.TrimSpace(req.APIBase)); err != nil {
		writeJSON(w, 404, map[string]string{"error": err.Error()})
		return
	}
	h.regenerateSwapConfig()
	writeJSON(w, 200, map[string]string{"status": "installed", "provider": name, "api_base": strings.TrimSpace(req.APIBase)})
}

func installBundledProviderRouting(home, name, apiBase string) error {
	_, data, err := providercatalog.Get(name)
	if err != nil {
		return err
	}
	if strings.TrimSpace(apiBase) != "" {
		var doc map[string]interface{}
		if err := yaml.Unmarshal(data, &doc); err != nil {
			return fmt.Errorf("parse provider %q: %w", name, err)
		}
		routing, _ := doc["routing"].(map[string]interface{})
		if routing == nil {
			return fmt.Errorf("provider %q has no routing block", name)
		}
		routing["api_base"] = strings.TrimSpace(apiBase)
		data, err = yaml.Marshal(doc)
		if err != nil {
			return fmt.Errorf("marshal provider %q: %w", name, err)
		}
	}
	return hub.MergeProviderRouting(home, name, data)
}

func (h *handler) providerCredentialValue(doc providercatalog.ProviderDoc) string {
	if doc.Credential == nil || h.deps.CredStore == nil {
		return ""
	}
	names := []string{strField(doc.Credential, "name"), strField(doc.Credential, "env_var")}
	for _, name := range names {
		for _, candidate := range credentialNameCandidates(name) {
			if entry, err := h.deps.CredStore.Get(candidate); err == nil {
				return entry.Value
			}
		}
	}
	return ""
}

func performProviderProbe(doc providercatalog.ProviderDoc, probe *providercatalog.QuickstartProbeConfig, apiKey, apiBase string) (int, string, error) {
	probeURL, err := providerProbeURL(doc, probe, apiBase)
	if err != nil {
		return 0, "", err
	}

	method := strings.ToUpper(strings.TrimSpace(probe.Method))
	if method == "" {
		method = http.MethodGet
	}

	var body io.Reader
	if probe.Body != "" {
		body = bytes.NewBufferString(probe.Body)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	if err := urlsafety.Validate(probeURL); err != nil {
		return 0, "", err
	}
	req, err := http.NewRequestWithContext(ctx, method, probeURL, body)
	if err != nil {
		return 0, "", err
	}
	for key, value := range probe.Headers {
		req.Header.Set(key, value)
	}
	if apiKey != "" {
		authHeader := strField(doc.Routing, "auth_header")
		if authHeader == "" {
			authHeader = "Authorization"
		}
		req.Header.Set(authHeader, strField(doc.Routing, "auth_prefix")+apiKey)
	}

	client := urlsafety.SafeClient()
	client.Timeout = 8 * time.Second
	resp, err := client.Do(req)
	if err != nil {
		return 0, "", err
	}
	defer resp.Body.Close()

	if len(probe.SuccessStatuses) == 0 {
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return resp.StatusCode, resp.Status, nil
		}
		return resp.StatusCode, "", providerProbeStatusError(resp.StatusCode)
	}
	for _, status := range probe.SuccessStatuses {
		if resp.StatusCode == status {
			return resp.StatusCode, resp.Status, nil
		}
	}
	return resp.StatusCode, "", providerProbeStatusError(resp.StatusCode)
}

func providerProbeURL(doc providercatalog.ProviderDoc, probe *providercatalog.QuickstartProbeConfig, apiBase string) (string, error) {
	probeURL := strings.TrimSpace(probe.URL)
	if apiBase == "" || !boolField(doc.Routing, "api_base_configurable") {
		return probeURL, nil
	}

	routingBase := strings.TrimSpace(strField(doc.Routing, "api_base"))
	if routingBase == "" {
		return apiBase, nil
	}

	baseParsed, err := url.Parse(apiBase)
	if err != nil {
		return "", err
	}
	probeParsed, err := url.Parse(probeURL)
	if err != nil {
		return "", err
	}
	routingParsed, err := url.Parse(routingBase)
	if err != nil {
		return "", err
	}

	suffix := strings.TrimPrefix(probeParsed.Path, routingParsed.Path)
	baseParsed.Path = strings.TrimRight(baseParsed.Path, "/") + "/" + strings.TrimLeft(suffix, "/")
	baseParsed.RawQuery = probeParsed.RawQuery
	return baseParsed.String(), nil
}

func boolField(m map[string]interface{}, key string) bool {
	v, _ := m[key].(bool)
	return v
}

func providerProbeStatusError(status int) error {
	return &providerVerifyError{Status: status}
}

type providerVerifyError struct {
	Status int
}

func (e *providerVerifyError) Error() string {
	if e.Status <= 0 {
		return "provider verification failed"
	}
	return fmt.Sprintf("provider verification returned unexpected status %d", e.Status)
}

func credentialNameCandidates(name string) []string {
	normalized := strings.ToLower(strings.ReplaceAll(name, "_", "-"))
	if normalized == name {
		return []string{name}
	}
	return []string{name, normalized}
}

// setupConfig returns the bundled wizard configuration (capability tiers).
//
//	GET /api/v1/infra/setup/config
func (h *handler) setupConfig(w http.ResponseWriter, r *http.Request) {
	doc, err := providercatalog.SetupConfig()
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "failed to load setup config"})
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
