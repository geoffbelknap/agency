package infra

import (
	"bytes"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/geoffbelknap/agency/internal/budget"
	"github.com/geoffbelknap/agency/internal/models"
)

// validInfraCallers lists infrastructure components allowed to call the internal LLM endpoint.
var validInfraCallers = map[string]bool{
	"knowledge-synthesizer": true,
	"knowledge-curator":     true,
	"platform-evaluation":   true,
}

// internalLLM handles POST /api/v1/infra/internal/llm — proxies LLM calls for
// infrastructure components with model resolution, format translation,
// cost tracking, and audit.
func (h *handler) internalLLM(w http.ResponseWriter, r *http.Request) {
	start := time.Now()

	// Validate caller identity
	caller := r.Header.Get("X-Agency-Caller")
	if !validInfraCallers[caller] {
		writeJSON(w, 403, map[string]string{"error": "unknown infrastructure caller"})
		return
	}

	// Validate auth token
	token := r.Header.Get("X-Agency-Token")
	if token == "" || subtle.ConstantTimeCompare([]byte(token), []byte(h.deps.Config.Token)) != 1 {
		writeJSON(w, 401, map[string]string{"error": "invalid or missing token"})
		return
	}

	// Read request body
	bodyBytes, err := io.ReadAll(io.LimitReader(r.Body, 10<<20))
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": "failed to read request body"})
		return
	}
	r.Body.Close()

	var reqBody map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &reqBody); err != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid JSON body"})
		return
	}

	modelAlias, _ := reqBody["model"].(string)
	if modelAlias == "" {
		writeJSON(w, 400, map[string]string{"error": "missing model field"})
		return
	}

	// Load routing config
	rc := loadRoutingConfig(h.deps.Config.Home)
	if rc == nil {
		writeJSON(w, 500, map[string]string{"error": "routing config not available"})
		return
	}

	// Resolve model alias
	providerCfg, modelCfg := rc.ResolveModel(modelAlias)
	if providerCfg == nil || modelCfg == nil {
		writeJSON(w, 400, map[string]string{"error": fmt.Sprintf("unknown model: %s", modelAlias)})
		return
	}

	// Check infrastructure budget
	store := budget.NewStore(filepath.Join(h.deps.Config.Home, "budget"))
	limits := models.DefaultPlatformBudgetConfig()
	remaining, err := store.Remaining("_infrastructure", limits)
	if err == nil && limits.InfraDaily > 0 {
		infraUsed := remaining.DailyUsed
		if infraUsed >= limits.InfraDaily {
			writeJSON(w, 429, map[string]string{"error": "infrastructure LLM budget exhausted"})
			return
		}
	}

	adapter := internalLLMAdapterFor(providerCfg, modelCfg)
	targetURL, modifiedBody, err := adapter.PrepareRequest(providerCfg, modelCfg, reqBody)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}

	// Gateway runs on host — call provider APIs directly (no egress proxy needed).
	// Load API key from env file for credential injection.
	apiKey := h.loadProviderKey(providerCfg)
	client := &http.Client{Timeout: 300 * time.Second}

	// Build upstream request
	outReq, err := http.NewRequestWithContext(r.Context(), "POST", targetURL, bytes.NewReader(modifiedBody))
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "failed to create upstream request"})
		return
	}
	outReq.Header.Set("Content-Type", "application/json")
	adapter.AddAuthHeaders(outReq, providerCfg, apiKey)

	// Send request
	resp, err := client.Do(outReq)
	if err != nil {
		h.deps.Audit.Write("_infrastructure", "LLM_DIRECT_ERROR", map[string]interface{}{
			"source": caller, "model": modelAlias, "error": err.Error(),
		})
		writeJSON(w, 502, map[string]string{"error": "upstream LLM error"})
		return
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	finalBody := adapter.TranslateResponse(respBody, resp.StatusCode)

	// Extract usage for cost tracking
	inputTokens, outputTokens := extractUsageFromResponse(finalBody)

	// Calculate cost
	costUSD := (float64(inputTokens)*modelCfg.CostPerMTokIn +
		float64(outputTokens)*modelCfg.CostPerMTokOut) / 1_000_000

	// Record cost under _infrastructure
	if costUSD > 0 {
		_ = store.RecordCost("_infrastructure", costUSD)
	}

	duration := time.Since(start)

	// Audit event — uses enforcer-compatible field names so routing metrics
	// can aggregate infrastructure LLM usage alongside agent LLM usage.
	h.deps.Audit.Write("_infrastructure", "LLM_DIRECT", map[string]interface{}{
		"source":         caller,
		"model":          modelAlias,
		"provider_model": modelCfg.ProviderModel,
		"provider":       modelCfg.Provider,
		"input_tokens":   inputTokens,
		"output_tokens":  outputTokens,
		"cost_usd":       costUSD,
		"duration_ms":    duration.Milliseconds(),
		"status":         resp.StatusCode,
	})

	// Return response
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	w.Write(finalBody)
}

// loadProviderKey reads the API key for a provider. Checks in order:
// 1. Process environment (for dev/CI overrides)
// 2. Credential store
func (h *handler) loadProviderKey(provider *models.ProviderConfig) string {
	if provider.AuthEnv == "" {
		return ""
	}
	// Check process environment first (for dev/CI overrides)
	if v := os.Getenv(provider.AuthEnv); v != "" {
		return v
	}
	// Check credential store
	if h.deps.CredStore != nil {
		entry, err := h.deps.CredStore.Get(provider.AuthEnv)
		if err == nil && entry.Value != "" {
			return entry.Value
		}
	}
	return ""
}

// extractUsageFromResponse extracts token usage from an OpenAI-format response.
func extractUsageFromResponse(body []byte) (inputTokens, outputTokens int) {
	var resp map[string]interface{}
	if json.Unmarshal(body, &resp) != nil {
		return 0, 0
	}
	usage, ok := resp["usage"].(map[string]interface{})
	if !ok {
		return 0, 0
	}
	if v, ok := usage["prompt_tokens"].(float64); ok {
		inputTokens = int(v)
	}
	if v, ok := usage["completion_tokens"].(float64); ok {
		outputTokens = int(v)
	}
	return
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
