package api

import (
	"bytes"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/geoffbelknap/agency/internal/models"
	"github.com/geoffbelknap/agency/internal/pkg/envfile"
)

// validInfraCallers lists infrastructure components allowed to call the internal LLM endpoint.
var validInfraCallers = map[string]bool{
	"knowledge-synthesizer": true,
	"knowledge-curator":     true,
	"platform-evaluation":   true,
}

// internalLLM handles POST /api/v1/internal/llm — proxies LLM calls for
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
	if token == "" || subtle.ConstantTimeCompare([]byte(token), []byte(h.cfg.Token)) != 1 {
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
	rc := h.loadRoutingConfig()
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
	store := h.budgetStore()
	limits := h.budgetConfig()
	remaining, err := store.Remaining("_infrastructure", limits)
	if err == nil && limits.InfraDaily > 0 {
		infraUsed := remaining.DailyUsed
		if infraUsed >= limits.InfraDaily {
			writeJSON(w, 429, map[string]string{"error": "infrastructure LLM budget exhausted"})
			return
		}
	}

	// Determine provider and target URL
	isAnthropic := modelCfg.Provider == "anthropic"
	base := strings.TrimRight(providerCfg.APIBase, "/")
	var targetURL string
	if isAnthropic {
		targetURL = base + "/messages"
	} else {
		targetURL = base + "/chat/completions"
	}

	// Rewrite model in request body to provider model
	reqBody["model"] = modelCfg.ProviderModel
	modifiedBody, err := json.Marshal(reqBody)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "failed to rewrite request body"})
		return
	}

	// Translate to Anthropic format if needed
	if isAnthropic {
		modifiedBody, err = infraTranslateToAnthropic(modifiedBody)
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": "failed to translate request for Anthropic"})
			return
		}
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
	if isAnthropic {
		outReq.Header.Set("anthropic-version", "2023-06-01")
		if apiKey != "" {
			outReq.Header.Set(providerCfg.AuthHeader, apiKey)
		}
	} else {
		if apiKey != "" {
			prefix := providerCfg.AuthPrefix
			if prefix == "" {
				prefix = "Bearer "
			}
			outReq.Header.Set(providerCfg.AuthHeader, prefix+apiKey)
		}
	}

	// Send request
	resp, err := client.Do(outReq)
	if err != nil {
		h.audit.Write("_infrastructure", "LLM_DIRECT_ERROR", map[string]interface{}{
			"source": caller, "model": modelAlias, "error": err.Error(),
		})
		writeJSON(w, 502, map[string]string{"error": "upstream LLM error"})
		return
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	// Translate Anthropic response back to OpenAI format
	var finalBody []byte
	if isAnthropic && resp.StatusCode >= 200 && resp.StatusCode < 300 {
		translated, err := infraTranslateFromAnthropic(respBody)
		if err != nil {
			finalBody = respBody // fallback: raw response
		} else {
			finalBody = translated
		}
	} else {
		finalBody = respBody
	}

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
	h.audit.Write("_infrastructure", "LLM_DIRECT", map[string]interface{}{
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

// loadRoutingConfig loads the routing.yaml from the infrastructure directory.
func (h *handler) loadRoutingConfig() *models.RoutingConfig {
	routingPath := filepath.Join(h.cfg.Home, "infrastructure", "routing.yaml")
	data, err := os.ReadFile(routingPath)
	if err != nil {
		return nil
	}
	var rc models.RoutingConfig
	if err := yaml.Unmarshal(data, &rc); err != nil {
		return nil
	}
	return &rc
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

// loadProviderKey reads the API key for a provider. Checks in order:
// 1. Process environment
// 2. ~/.agency/.env (operator config)
// Service keys (.service-keys.env) are for the enforcer/egress, not gateway internal LLM calls.
func (h *handler) loadProviderKey(provider *models.ProviderConfig) string {
	if provider.AuthEnv == "" {
		return ""
	}
	// Check process environment first
	if v := os.Getenv(provider.AuthEnv); v != "" {
		return v
	}
	// Check operator env file only — provider keys live here
	env := envfile.Load(filepath.Join(h.cfg.Home, ".env"))
	if v, ok := env[provider.AuthEnv]; ok && v != "" {
		return v
	}
	return ""
}

// infraTranslateToAnthropic converts an OpenAI chat/completions request body to
// Anthropic Messages API format. Simplified version for infrastructure calls
// (no tool use, no streaming, no caching).
func infraTranslateToAnthropic(openaiBody []byte) ([]byte, error) {
	var req map[string]interface{}
	if err := json.Unmarshal(openaiBody, &req); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}

	result := make(map[string]interface{})

	// Preserve model, temperature, top_p
	for _, key := range []string{"model", "temperature", "top_p"} {
		if v, ok := req[key]; ok {
			result[key] = v
		}
	}

	// Set max_tokens (Anthropic requires it)
	if v, ok := req["max_tokens"]; ok {
		result["max_tokens"] = v
	} else {
		result["max_tokens"] = float64(4096)
	}

	// Process messages: extract system messages
	messages, ok := req["messages"].([]interface{})
	if !ok {
		return nil, fmt.Errorf("messages field missing or not an array")
	}

	var systemBlocks []interface{}
	var anthropicMessages []interface{}

	for _, rawMsg := range messages {
		msg, ok := rawMsg.(map[string]interface{})
		if !ok {
			continue
		}
		role, _ := msg["role"].(string)
		if role == "system" {
			content, _ := msg["content"].(string)
			block := map[string]interface{}{
				"type": "text",
				"text": content,
			}
			systemBlocks = append(systemBlocks, block)
		} else {
			out := make(map[string]interface{})
			for k, v := range msg {
				out[k] = v
			}
			anthropicMessages = append(anthropicMessages, out)
		}
	}

	if len(systemBlocks) > 0 {
		result["system"] = systemBlocks
	}
	result["messages"] = anthropicMessages

	return json.Marshal(result)
}

// infraTranslateFromAnthropic converts an Anthropic Messages API response to
// OpenAI chat/completions format.
func infraTranslateFromAnthropic(anthropicBody []byte) ([]byte, error) {
	var resp map[string]interface{}
	if err := json.Unmarshal(anthropicBody, &resp); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}

	// Pass through error responses
	if errVal, ok := resp["error"]; ok {
		return json.Marshal(map[string]interface{}{"error": errVal})
	}

	// Extract text from content blocks
	contentBlocks, _ := resp["content"].([]interface{})
	var textParts []string
	for _, rawBlock := range contentBlocks {
		block, ok := rawBlock.(map[string]interface{})
		if !ok {
			continue
		}
		if blockType, _ := block["type"].(string); blockType == "text" {
			text, _ := block["text"].(string)
			textParts = append(textParts, text)
		}
	}

	// Map stop_reason to finish_reason
	stopReason, _ := resp["stop_reason"].(string)
	var finishReason string
	switch stopReason {
	case "end_turn", "stop_sequence":
		finishReason = "stop"
	case "max_tokens":
		finishReason = "length"
	default:
		finishReason = "stop"
	}

	// Build usage
	usage := make(map[string]interface{})
	if u, ok := resp["usage"].(map[string]interface{}); ok {
		if v, ok := u["input_tokens"]; ok {
			usage["prompt_tokens"] = v
		}
		if v, ok := u["output_tokens"]; ok {
			usage["completion_tokens"] = v
		}
	}

	result := map[string]interface{}{
		"id":     resp["id"],
		"object": "chat.completion",
		"model":  resp["model"],
		"choices": []interface{}{
			map[string]interface{}{
				"index": 0,
				"message": map[string]interface{}{
					"role":    "assistant",
					"content": strings.Join(textParts, " "),
				},
				"finish_reason": finishReason,
			},
		},
		"usage": usage,
	}

	return json.Marshal(result)
}
