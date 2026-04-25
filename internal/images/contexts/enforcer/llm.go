package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	llmMaxRetries     = 3
	llmRetryDelay     = 500 * time.Millisecond
	llmReadTimeout    = 300 * time.Second
	llmMaxRequestBody = 10 << 20 // 10 MB
)

// safeResponseHeaders are the only headers relayed from LLM provider responses.
var safeResponseHeaders = map[string]bool{
	"content-type":     true,
	"content-length":   true,
	"cache-control":    true,
	"retry-after":      true,
	"x-request-id":     true,
	"x-correlation-id": true,
}

// safeStreamHeaders are relayed for SSE streaming responses.
var safeStreamHeaders = map[string]bool{
	"content-type":     true,
	"cache-control":    true,
	"x-request-id":     true,
	"x-correlation-id": true,
}

// LLMHandler handles /v1/* requests: model resolution, body rewriting,
// forwarding to providers, and response relay.
type LLMHandler struct {
	routing   *RoutingConfig
	proxy     string // egress proxy URL
	audit     *AuditLogger
	transport *http.Transport
	analysis  *AnalysisClient
	agentName string
	budget    *BudgetTracker
}

// NewLLMHandler creates an LLM proxy handler.
func NewLLMHandler(routing *RoutingConfig, egressProxy string, audit *AuditLogger) *LLMHandler {
	transport := &http.Transport{
		Proxy:                 http.ProxyURL(mustParseURL(egressProxy)),
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: llmReadTimeout,
	}

	return &LLMHandler{
		routing:   routing,
		proxy:     egressProxy,
		audit:     audit,
		transport: transport,
	}
}

// SetRouting replaces the routing config (used on SIGHUP reload).
func (lh *LLMHandler) SetRouting(rc *RoutingConfig) {
	lh.routing = rc
}

// emitErrorSignal sends an agent_signal_error via the signal relay when an
// LLM call fails with a non-2xx status code.
func (lh *LLMHandler) emitErrorSignal(status int, model, correlationID string, retries int) {
	if status >= 200 && status < 300 {
		return
	}
	var stage, message string
	switch {
	case status == 401 || status == 403:
		stage = "provider_auth"
		message = fmt.Sprintf("LLM call failed: authentication rejected by provider (%d)", status)
	case status == 429:
		stage = "provider_rate_limit"
		message = fmt.Sprintf("LLM call failed: rate limited by provider (%d)", status)
	case status >= 500:
		stage = "provider_error"
		message = fmt.Sprintf("LLM call failed: upstream provider error (%d)", status)
	default:
		stage = "request_rejected"
		message = fmt.Sprintf("LLM call failed: provider returned %d", status)
	}
	payload, _ := json.Marshal(map[string]interface{}{
		"signal_type": "error",
		"data": map[string]interface{}{
			"category":          "llm.call_failed",
			"stage":             stage,
			"status":            status,
			"message":           message,
			"model":             model,
			"correlation_id":    correlationID,
			"retries_attempted": retries,
		},
	})
	// Best-effort relay via local signal endpoint
	go func() {
		resp, err := http.Post("http://localhost:3128/signal", "application/json", bytes.NewReader(payload))
		if err != nil {
			log.Printf("error signal relay failed: %v", err)
			return
		}
		resp.Body.Close()
	}()
}

// SetAnalysis wires the analysis client for rate limiting and budget checks.
func (lh *LLMHandler) SetAnalysis(client *AnalysisClient, agentName string) {
	lh.analysis = client
	lh.agentName = agentName
}

// SetBudget wires the budget tracker for per-task cost tracking.
func (lh *LLMHandler) SetBudget(bt *BudgetTracker) {
	lh.budget = bt
}

// ServeHTTP handles /v1/* LLM API requests.
func (lh *LLMHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	correlationID := r.Header.Get("X-Correlation-Id")

	// Extract task ID for per-task budget tracking
	if taskID := r.Header.Get("X-Agency-Task-Id"); taskID != "" && lh.budget != nil {
		lh.budget.SetTask(taskID)
	}

	// Budget check — block if any budget level is exhausted
	if lh.budget != nil {
		if budgetErr := lh.budget.CheckBudget(); budgetErr != nil {
			lh.audit.Log(AuditEntry{
				Type:          "LLM_BUDGET_EXHAUSTED",
				CorrelationID: correlationID,
				Status:        429,
				Error:         budgetErr.Error.Message,
			})
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusTooManyRequests)
			json.NewEncoder(w).Encode(budgetErr)
			return
		}
	}

	// Read and parse request body to extract model
	bodyBytes, err := io.ReadAll(io.LimitReader(r.Body, llmMaxRequestBody+1))
	if err != nil {
		http.Error(w, `{"error":"failed to read request body"}`, http.StatusBadRequest)
		return
	}
	if len(bodyBytes) > llmMaxRequestBody {
		http.Error(w, `{"error":"request body too large"}`, http.StatusRequestEntityTooLarge)
		return
	}
	r.Body.Close()

	var reqBody map[string]interface{}
	if len(bodyBytes) > 0 {
		if err := json.Unmarshal(bodyBytes, &reqBody); err != nil {
			http.Error(w, `{"error":"invalid JSON body"}`, http.StatusBadRequest)
			return
		}
	}

	// Extract and resolve model alias
	modelAlias, _ := reqBody["model"].(string)
	if modelAlias == "" {
		http.Error(w, `{"error":"missing model field"}`, http.StatusBadRequest)
		return
	}

	targetURL, providerModel, providerName, err := lh.routing.ResolveModel(modelAlias)
	if err != nil {
		lh.audit.Log(AuditEntry{
			Type:          "LLM_UNKNOWN_MODEL",
			Model:         modelAlias,
			CorrelationID: correlationID,
			Status:        400,
			Error:         err.Error(),
		})
		http.Error(w, fmt.Sprintf(`{"error":"unknown model: %s"}`, modelAlias), http.StatusBadRequest)
		return
	}

	provider := lh.routing.Providers[lh.routing.Models[modelAlias].Provider]
	adapter := providerAdapterFor(provider)

	// Acquire rate limit slot if analysis client is configured
	if lh.analysis != nil {
		if err := lh.acquireRateSlot(w, providerName); err != nil {
			// acquireRateSlot already wrote the error response
			return
		}
	}

	// Budget check — block if agent has exceeded hard budget limit
	if lh.analysis != nil && lh.agentName != "" {
		allowed, reason, err := lh.analysis.CheckBudget(lh.agentName)
		if err == nil && !allowed {
			lh.audit.Log(AuditEntry{
				Type:          "LLM_BUDGET_EXCEEDED",
				Model:         modelAlias,
				CorrelationID: correlationID,
				Status:        429,
				Error:         reason,
			})
			http.Error(w, fmt.Sprintf(`{"error":"budget exceeded: %s"}`, reason), http.StatusTooManyRequests)
			return
		}
	}

	prepared, err := adapter.PrepareRequest(providerRequestContext{
		RequestPath:   r.URL.Path,
		TargetURL:     targetURL,
		ProviderName:  providerName,
		ProviderModel: providerModel,
		Provider:      provider,
		Body:          reqBody,
	})
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusBadRequest)
		return
	}

	// Determine if streaming
	isStream, _ := reqBody["stream"].(bool)

	// Build and send the upstream request with retries
	var resp *http.Response
	for attempt := 0; attempt < llmMaxRetries; attempt++ {
		outReq, err := http.NewRequestWithContext(r.Context(), r.Method, prepared.TargetURL, bytes.NewReader(prepared.Body))
		if err != nil {
			http.Error(w, `{"error":"failed to create upstream request"}`, http.StatusInternalServerError)
			return
		}

		// Set content type
		outReq.Header.Set("Content-Type", "application/json")

		adapter.AddHeaders(outReq)

		// Propagate correlation ID
		if correlationID != "" {
			outReq.Header.Set("X-Correlation-Id", correlationID)
		}

		// Use transport with egress proxy (configured at init)
		resp, err = lh.transport.RoundTrip(outReq)
		if err != nil {
			if attempt < llmMaxRetries-1 {
				log.Printf("LLM upstream error (attempt %d/%d): %v", attempt+1, llmMaxRetries, err)
				time.Sleep(llmRetryDelay * time.Duration(attempt+1))
				continue
			}
			lh.audit.Log(AuditEntry{
				Type:          "LLM_DIRECT_ERROR",
				Model:         modelAlias,
				Provider:      providerName,
				ProviderModel: providerModel,
				CorrelationID: correlationID,
				Error:         err.Error(),
				DurationMs:    time.Since(start).Milliseconds(),
			})
			lh.emitErrorSignal(502, modelAlias, correlationID, llmMaxRetries)
			http.Error(w, `{"error":"upstream LLM error"}`, http.StatusBadGateway)
			return
		}
		break
	}
	defer resp.Body.Close()

	// Report rate limit headers from upstream response
	if lh.analysis != nil {
		lh.reportRateLimitHeaders(resp, providerName)
	}

	adapter.RelayResponse(lh, w, providerRelayContext{
		Response:      resp,
		ModelAlias:    modelAlias,
		ProviderName:  providerName,
		ProviderModel: providerModel,
		CorrelationID: correlationID,
		Start:         start,
		Stream:        isStream,
	})
}

// relayBuffered relays a non-streaming LLM response.
func (lh *LLMHandler) relayBuffered(w http.ResponseWriter, resp *http.Response, modelAlias, providerName, providerModel, correlationID string, start time.Time) {
	// Copy safe headers
	for k, vv := range resp.Header {
		if safeResponseHeaders[strings.ToLower(k)] {
			for _, v := range vv {
				w.Header().Add(k, v)
			}
		}
	}

	w.WriteHeader(resp.StatusCode)
	body, _ := io.ReadAll(resp.Body)
	w.Write(body)

	// Extract usage from response
	var respBody map[string]interface{}
	inputTokens, outputTokens, cachedTokens := 0, 0, 0
	if json.Unmarshal(body, &respBody) == nil {
		if usage, ok := respBody["usage"].(map[string]interface{}); ok {
			if v, ok := usage["input_tokens"].(float64); ok {
				inputTokens = int(v)
			}
			if v, ok := usage["prompt_tokens"].(float64); ok {
				inputTokens = int(v)
			}
			if v, ok := usage["output_tokens"].(float64); ok {
				outputTokens = int(v)
			}
			if v, ok := usage["completion_tokens"].(float64); ok {
				outputTokens = int(v)
			}
			cachedTokens = intFromJSON(usage["cache_read_input_tokens"])
		}
	}

	auditType := "LLM_DIRECT"
	lh.audit.Log(AuditEntry{
		Type:                 auditType,
		Model:                modelAlias,
		Provider:             providerName,
		ProviderModel:        providerModel,
		CorrelationID:        correlationID,
		Status:               resp.StatusCode,
		InputTokens:          inputTokens,
		OutputTokens:         outputTokens,
		CachedTokens:         cachedTokens,
		CacheReadInputTokens: cachedTokens,
		DurationMs:           time.Since(start).Milliseconds(),
	})
	lh.emitErrorSignal(resp.StatusCode, modelAlias, correlationID, 0)

	// Report usage for budget tracking
	lh.reportUsage(modelAlias, providerModel, inputTokens, outputTokens, cachedTokens, resp.StatusCode, time.Since(start).Milliseconds())

	// XPIA scan on response content
	lh.scanContent(string(body), "response")
}

// relayStream relays an SSE streaming LLM response.
func (lh *LLMHandler) relayStream(w http.ResponseWriter, resp *http.Response, modelAlias, providerName, providerModel, correlationID string, start time.Time) {
	// Copy safe stream headers
	for k, vv := range resp.Header {
		if safeStreamHeaders[strings.ToLower(k)] {
			for _, v := range vv {
				w.Header().Add(k, v)
			}
		}
	}

	w.WriteHeader(resp.StatusCode)

	flusher, canFlush := w.(http.Flusher)

	inputTokens, outputTokens := 0, 0
	buf := make([]byte, 4096)
	for {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			chunk := buf[:n]
			w.Write(chunk)
			if canFlush {
				flusher.Flush()
			}

			// Try to extract usage from final SSE chunk
			chunkStr := string(chunk)
			if strings.Contains(chunkStr, `"usage"`) {
				in, out := extractStreamUsage(chunkStr)
				if in > 0 {
					inputTokens = in
				}
				if out > 0 {
					outputTokens = out
				}
			}
		}
		if err != nil {
			break
		}
	}

	auditType := "LLM_DIRECT_STREAM"
	lh.audit.Log(AuditEntry{
		Type:          auditType,
		Model:         modelAlias,
		Provider:      providerName,
		ProviderModel: providerModel,
		CorrelationID: correlationID,
		Status:        resp.StatusCode,
		InputTokens:   inputTokens,
		OutputTokens:  outputTokens,
		DurationMs:    time.Since(start).Milliseconds(),
	})
	lh.emitErrorSignal(resp.StatusCode, modelAlias, correlationID, 0)

	// Report usage for budget tracking
	lh.reportUsage(modelAlias, providerModel, inputTokens, outputTokens, 0, resp.StatusCode, time.Since(start).Milliseconds())
}

// acquireRateSlot blocks until a rate limit slot is available from the analysis
// service. Sends SSE keepalive comments (": queued\n\n") while waiting. Returns
// an error if the maximum wait time (120s) is exceeded.
func (lh *LLMHandler) acquireRateSlot(w http.ResponseWriter, providerName string) error {
	deadline := time.Now().Add(120 * time.Second)
	flusher, canFlush := w.(http.Flusher)

	for {
		granted, waitSecs, err := lh.analysis.AcquireRateLimit(providerName, lh.agentName)
		if err != nil {
			// Fail open on error
			return nil
		}
		if granted {
			return nil
		}

		if time.Now().After(deadline) {
			http.Error(w, `{"error":"rate limit timeout"}`, http.StatusTooManyRequests)
			return fmt.Errorf("rate limit timeout")
		}

		// Send SSE keepalive comment while queued
		w.Write([]byte(": queued\n\n"))
		if canFlush {
			flusher.Flush()
		}

		// Wait the suggested time, capped at remaining deadline
		wait := time.Duration(waitSecs * float64(time.Second))
		remaining := time.Until(deadline)
		if wait > remaining {
			wait = remaining
		}
		if wait > 0 {
			time.Sleep(wait)
		}
	}
}

// reportRateLimitHeaders extracts rate limit headers from the upstream response
// and reports them to the analysis service for tracking.
func (lh *LLMHandler) reportRateLimitHeaders(resp *http.Response, providerName string) {
	limitReqs := atoiOr(resp.Header.Get("X-Ratelimit-Limit-Requests"), 0)
	remainingReqs := atoiOr(resp.Header.Get("X-Ratelimit-Remaining-Requests"), 0)
	resetSecs := atofOr(resp.Header.Get("X-Ratelimit-Reset-Requests"), 0)
	retryAfter := atofOr(resp.Header.Get("Retry-After"), 0)

	if limitReqs > 0 || remainingReqs > 0 || resetSecs > 0 || retryAfter > 0 {
		lh.analysis.UpdateRateLimit(providerName, limitReqs, remainingReqs, resetSecs, resp.StatusCode, retryAfter)
	}
}

// atoiOr converts a string to int, returning fallback on error or empty string.
func atoiOr(s string, fallback int) int {
	if s == "" {
		return fallback
	}
	v, err := strconv.Atoi(s)
	if err != nil {
		return fallback
	}
	return v
}

// atofOr converts a string to float64, returning fallback on error or empty string.
func atofOr(s string, fallback float64) float64 {
	if s == "" {
		return fallback
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return fallback
	}
	return v
}

// reportUsage sends token usage to the analysis service for budget tracking
// and per-model metrics collection (used by the auto-router).
func (lh *LLMHandler) reportUsage(modelAlias, providerModel string, inputTokens, outputTokens, cachedTokens int, statusCode int, latencyMs int64) {
	if inputTokens == 0 && outputTokens == 0 && cachedTokens == 0 {
		return
	}
	model, ok := lh.routing.Models[modelAlias]
	if !ok {
		return
	}

	// Record to budget tracker for per-task cost tracking
	if lh.budget != nil {
		lh.budget.RecordUsage(
			int64(inputTokens), int64(outputTokens), int64(cachedTokens),
			model.CostIn, model.CostOut, model.CostCached,
		)
	}

	if lh.analysis == nil {
		return
	}
	lh.analysis.PostUsage(UsageData{
		Agent:        lh.agentName,
		InputTokens:  inputTokens,
		OutputTokens: outputTokens,
		CostIn:       model.CostIn,
		CostOut:      model.CostOut,
		Model:        providerModel,
		StatusCode:   statusCode,
		LatencyMs:    latencyMs,
	})
}

// scanContent sends content to the analysis service for XPIA scanning.
func (lh *LLMHandler) scanContent(content string, contentType string) {
	if lh.analysis == nil || !lh.routing.Settings.XPIAScan || content == "" {
		return
	}
	lh.analysis.PostScan(lh.agentName, content, contentType)
}

// extractStreamUsage attempts to extract usage info from an SSE chunk.
func extractStreamUsage(chunk string) (inputTokens, outputTokens int) {
	// SSE data lines start with "data: "
	for _, line := range strings.Split(chunk, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			continue
		}
		var obj map[string]interface{}
		if json.Unmarshal([]byte(data), &obj) != nil {
			continue
		}
		usage, ok := obj["usage"].(map[string]interface{})
		if !ok {
			continue
		}
		if v, ok := usage["input_tokens"].(float64); ok {
			inputTokens = int(v)
		}
		if v, ok := usage["prompt_tokens"].(float64); ok {
			inputTokens = int(v)
		}
		if v, ok := usage["output_tokens"].(float64); ok {
			outputTokens = int(v)
		}
		if v, ok := usage["completion_tokens"].(float64); ok {
			outputTokens = int(v)
		}
	}
	return
}
