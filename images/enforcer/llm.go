package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	llmMaxRetries      = 3
	llmRetryDelay      = 500 * time.Millisecond
	llmReadTimeout     = 300 * time.Second
	llmMaxRequestBody  = 10 << 20 // 10 MB
)

// safeResponseHeaders are the only headers relayed from LLM provider responses.
var safeResponseHeaders = map[string]bool{
	"content-type":    true,
	"content-length":  true,
	"cache-control":   true,
	"retry-after":     true,
	"x-request-id":    true,
	"x-correlation-id": true,
}

// safeStreamHeaders are relayed for SSE streaming responses.
var safeStreamHeaders = map[string]bool{
	"content-type":    true,
	"cache-control":   true,
	"x-request-id":    true,
	"x-correlation-id": true,
}

// LLMHandler handles /v1/* requests: model resolution, body rewriting,
// forwarding to providers, and response relay.
type LLMHandler struct {
	routing     *RoutingConfig
	proxy       string // egress proxy URL
	audit       *AuditLogger
	transport   *http.Transport
	rateLimiter *RateLimiter
	agentName   string
	budget      *BudgetTracker
	toolTracker  *ToolTracker
	trajectory   *TrajectoryMonitor
	stepCounters map[string]int
	stepMu       sync.Mutex
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
		routing:      routing,
		proxy:        egressProxy,
		audit:        audit,
		transport:    transport,
		toolTracker:  NewToolTracker(),
		stepCounters: make(map[string]int),
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

// SetRateLimiter wires the in-process rate limiter.
func (lh *LLMHandler) SetRateLimiter(rl *RateLimiter, agentName string) {
	lh.rateLimiter = rl
	lh.agentName = agentName
}

// SetBudget wires the budget tracker for per-task cost tracking.
func (lh *LLMHandler) SetBudget(bt *BudgetTracker) {
	lh.budget = bt
}

// SetTrajectory wires the trajectory monitor for tool call anomaly detection.
func (lh *LLMHandler) SetTrajectory(tm *TrajectoryMonitor) {
	lh.trajectory = tm
}

func (lh *LLMHandler) nextStepIndex(taskID string) int {
	lh.stepMu.Lock()
	defer lh.stepMu.Unlock()
	if lh.stepCounters == nil {
		lh.stepCounters = make(map[string]int)
	}
	lh.stepCounters[taskID]++
	return lh.stepCounters[taskID]
}

// ServeHTTP handles /v1/* LLM API requests.
func (lh *LLMHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	correlationID := r.Header.Get("X-Correlation-Id")
	eventID := r.Header.Get("X-Agency-Event-Id")

	// Extract task ID for per-task budget tracking and step index
	taskID := r.Header.Get("X-Agency-Task-Id")
	if taskID != "" && lh.budget != nil {
		lh.budget.SetTask(taskID)
	}

	var stepIndex int
	if taskID != "" {
		stepIndex = lh.nextStepIndex(taskID)
	}

	retryOf := r.Header.Get("X-Agency-Retry-Of")

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

	// Detect required capabilities from request body
	requiredCaps := detectRequiredCaps(reqBody)

	// Check if resolved model supports required capabilities
	if len(requiredCaps) > 0 {
		model := lh.routing.Models[modelAlias]
		for _, req := range requiredCaps {
			if !model.HasCapability(req) {
				lh.audit.Log(AuditEntry{
					Type:          "LLM_CAPABILITY_MISMATCH",
					Model:         modelAlias,
					CorrelationID: correlationID,
					Error:         fmt.Sprintf("model %s does not support %s", modelAlias, req),
				})
				http.Error(w, fmt.Sprintf(`{"error":"model %s does not support capability: %s"}`, modelAlias, req), 422)
				return
			}
		}
	}

	// Scan tool-role messages for injection patterns (ASK Tenet 1 + 17).
	// This runs automatically — the body cannot skip it.
	if flags := scanToolMessages(reqBody); len(flags) > 0 {
		lh.audit.Log(AuditEntry{
			Type:          "XPIA_TOOL_OUTPUT",
			Model:         modelAlias,
			CorrelationID: correlationID,
			Error:         strings.Join(flags, "; "),
		})
	}

	// Track tool definitions for mutation detection (ASK Tenet 17).
	if flag := trackToolDefinitions(reqBody, lh.toolTracker); flag != "" {
		lh.audit.Log(AuditEntry{
			Type:          "MCP_TOOL_MUTATION",
			CorrelationID: correlationID,
			Error:         flag,
		})
	}

	isAnthropic := providerName == "anthropic"

	// Acquire rate limit slot if rate limiter is configured
	if lh.rateLimiter != nil {
		if err := lh.acquireRateSlot(w, providerName); err != nil {
			// acquireRateSlot already wrote the error response
			return
		}
	}

	// Determine target URL from request path
	// The path after /v1/ determines the endpoint
	// For Anthropic, always use the resolved URL (/messages)
	if !isAnthropic {
		path := r.URL.Path
		if strings.HasPrefix(path, "/v1/") {
			endpoint := path[3:] // strip /v1, keep /chat/completions etc
			base := strings.TrimRight(lh.routing.Providers[lh.routing.Models[modelAlias].Provider].APIBase, "/")
			targetURL = base + endpoint
		}
	}

	// Rewrite model in request body
	reqBody["model"] = providerModel
	modifiedBody, err := json.Marshal(reqBody)
	if err != nil {
		http.Error(w, `{"error":"failed to rewrite request body"}`, http.StatusInternalServerError)
		return
	}

	// For Anthropic: translate request body to Anthropic format
	if isAnthropic {
		provider := lh.routing.Providers[lh.routing.Models[modelAlias].Provider]
		modifiedBody, err = translateToAnthropic(modifiedBody, provider.CachingEnabled())
		if err != nil {
			http.Error(w, `{"error":"failed to translate request for Anthropic"}`, http.StatusInternalServerError)
			return
		}
	}

	// Determine if streaming
	isStream, _ := reqBody["stream"].(bool)

	// Build and send the upstream request with retries
	var resp *http.Response
	for attempt := 0; attempt < llmMaxRetries; attempt++ {
		outReq, err := http.NewRequestWithContext(r.Context(), r.Method, targetURL, bytes.NewReader(modifiedBody))
		if err != nil {
			http.Error(w, `{"error":"failed to create upstream request"}`, http.StatusInternalServerError)
			return
		}

		// Set content type
		outReq.Header.Set("Content-Type", "application/json")

		// Add Anthropic-specific headers
		if isAnthropic {
			outReq.Header.Set("anthropic-version", "2023-06-01")
		}

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
	if lh.rateLimiter != nil {
		lh.reportRateLimitHeaders(resp, providerName)
	}

	// Relay response
	if isStream {
		if isAnthropic {
			lh.relayAnthropicStream(w, resp, modelAlias, providerModel, correlationID, eventID, start, stepIndex, retryOf)
		} else {
			lh.relayStream(w, resp, modelAlias, providerModel, correlationID, eventID, start, stepIndex, retryOf)
		}
	} else {
		if isAnthropic {
			lh.relayAnthropicBuffered(w, resp, modelAlias, providerModel, correlationID, eventID, start, stepIndex, retryOf)
		} else {
			lh.relayBuffered(w, resp, modelAlias, providerModel, correlationID, eventID, start, stepIndex, retryOf)
		}
	}
}

// relayBuffered relays a non-streaming LLM response.
func (lh *LLMHandler) relayBuffered(w http.ResponseWriter, resp *http.Response, modelAlias, providerModel, correlationID, eventID string, start time.Time, stepIndex int, retryOf string) {
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
	inputTokens, outputTokens := 0, 0
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
		}
	}

	// Trajectory monitoring: record tool calls and response content
	if lh.trajectory != nil && resp.StatusCode >= 200 && resp.StatusCode < 300 {
		if choices, ok := respBody["choices"].([]interface{}); ok && len(choices) > 0 {
			if choice, ok := choices[0].(map[string]interface{}); ok {
				if message, ok := choice["message"].(map[string]interface{}); ok {
					// Record tool calls
					if toolCalls, ok := message["tool_calls"].([]interface{}); ok {
						for _, tc := range toolCalls {
							if tcMap, ok := tc.(map[string]interface{}); ok {
								if fn, ok := tcMap["function"].(map[string]interface{}); ok {
									toolName, _ := fn["name"].(string)
									if toolName != "" {
										lh.trajectory.RecordToolCall(ToolEntry{
											ToolName:   toolName,
											Timestamp:  time.Now(),
											Success:    true,
											TokensUsed: outputTokens,
										})
									}
								}
							}
						}
					}
					// Record response text for repetition detection
					if content, ok := message["content"].(string); ok && content != "" {
						lh.trajectory.RecordResponse(content)
					}
					// Run all detectors (tool + response)
					for _, anomaly := range lh.trajectory.RunDetectors() {
						lh.emitTrajectoryAnomaly(anomaly)
					}
				}
			}
		}
	}

	// Validate tool call arguments for hallucination detection
	var toolCallValid *bool
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		if choices, ok := respBody["choices"].([]interface{}); ok && len(choices) > 0 {
			if choice, ok := choices[0].(map[string]interface{}); ok {
				if message, ok := choice["message"].(map[string]interface{}); ok {
					if toolCalls, ok := message["tool_calls"].([]interface{}); ok && len(toolCalls) > 0 {
						allValid := true
						for _, tc := range toolCalls {
							tcMap, ok := tc.(map[string]interface{})
							if !ok {
								allValid = false
								break
							}
							fn, ok := tcMap["function"].(map[string]interface{})
							if !ok {
								allValid = false
								break
							}
							argsStr, _ := fn["arguments"].(string)
							if argsStr != "" {
								var args json.RawMessage
								if json.Unmarshal([]byte(argsStr), &args) != nil {
									allValid = false
								}
							}
						}
						toolCallValid = &allValid
					}
				}
			}
		}
	}

	durationMs := time.Since(start).Milliseconds()

	auditType := "LLM_DIRECT"
	lh.audit.Log(AuditEntry{
		Type:          auditType,
		Model:         modelAlias,
		ProviderModel: providerModel,
		CorrelationID: correlationID,
		EventID:       eventID,
		Status:        resp.StatusCode,
		InputTokens:   inputTokens,
		OutputTokens:  outputTokens,
		DurationMs:    durationMs,
		TTFTMs:        durationMs,
		ToolCallValid: toolCallValid,
		StepIndex:     stepIndex,
		RetryOf:       retryOf,
	})
	lh.emitErrorSignal(resp.StatusCode, modelAlias, correlationID, 0)

	// Report usage for budget tracking
	lh.reportUsage(modelAlias, providerModel, inputTokens, outputTokens, resp.StatusCode, durationMs)
}

// emitTrajectoryAnomaly logs a trajectory anomaly to the audit log and relays
// it to the gateway signal endpoint for operator notification.
func (lh *LLMHandler) emitTrajectoryAnomaly(anomaly Anomaly) {
	lh.audit.Log(AuditEntry{
		Type:  "TRAJECTORY_ANOMALY",
		Error: fmt.Sprintf("[%s] %s (severity: %s)", anomaly.Detector, anomaly.Detail, anomaly.Severity),
	})

	payload, _ := json.Marshal(map[string]interface{}{
		"signal_type": "trajectory_anomaly",
		"data": map[string]interface{}{
			"agent":    lh.agentName,
			"detector": anomaly.Detector,
			"detail":   anomaly.Detail,
			"severity": anomaly.Severity,
		},
	})
	go func() {
		resp, err := http.Post("http://localhost:3128/signal", "application/json", bytes.NewReader(payload))
		if err != nil {
			log.Printf("trajectory signal relay failed: %v", err)
			return
		}
		resp.Body.Close()
	}()
}

// relayStream relays an SSE streaming LLM response.
func (lh *LLMHandler) relayStream(w http.ResponseWriter, resp *http.Response, modelAlias, providerModel, correlationID, eventID string, start time.Time, stepIndex int, retryOf string) {
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
	var ttftTime time.Time
	buf := make([]byte, 4096)
	for {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			if ttftTime.IsZero() {
				ttftTime = time.Now()
			}
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

	ttftMs := int64(0)
	if !ttftTime.IsZero() {
		ttftMs = ttftTime.Sub(start).Milliseconds()
	}
	tpotMs := float64(0)
	durationMs := time.Since(start).Milliseconds()
	if outputTokens > 0 && ttftMs > 0 {
		tpotMs = float64(durationMs-ttftMs) / float64(outputTokens)
	}

	auditType := "LLM_DIRECT_STREAM"
	lh.audit.Log(AuditEntry{
		Type:          auditType,
		Model:         modelAlias,
		ProviderModel: providerModel,
		CorrelationID: correlationID,
		EventID:       eventID,
		Status:        resp.StatusCode,
		InputTokens:   inputTokens,
		OutputTokens:  outputTokens,
		DurationMs:    durationMs,
		TTFTMs:        ttftMs,
		TPOTMs:        tpotMs,
		StepIndex:     stepIndex,
		RetryOf:       retryOf,
	})
	lh.emitErrorSignal(resp.StatusCode, modelAlias, correlationID, 0)

	// Report usage for budget tracking
	lh.reportUsage(modelAlias, providerModel, inputTokens, outputTokens, resp.StatusCode, durationMs)
}

// relayAnthropicBuffered relays a non-streaming Anthropic response, translating
// it back to OpenAI format before sending to the client.
func (lh *LLMHandler) relayAnthropicBuffered(w http.ResponseWriter, resp *http.Response, modelAlias, providerModel, correlationID, eventID string, start time.Time, stepIndex int, retryOf string) {
	body, _ := io.ReadAll(resp.Body)

	// Translate Anthropic response to OpenAI format
	translated, err := translateFromAnthropic(body)
	if err != nil {
		// Fallback: relay raw response
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resp.StatusCode)
		w.Write(body)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	for k, vv := range resp.Header {
		if safeResponseHeaders[strings.ToLower(k)] && strings.ToLower(k) != "content-type" && strings.ToLower(k) != "content-length" {
			for _, v := range vv {
				w.Header().Add(k, v)
			}
		}
	}
	w.WriteHeader(resp.StatusCode)
	w.Write(translated)

	// Extract usage from translated response for audit
	var respBody map[string]interface{}
	inputTokens, outputTokens := 0, 0
	if json.Unmarshal(translated, &respBody) == nil {
		if usage, ok := respBody["usage"].(map[string]interface{}); ok {
			if v, ok := usage["prompt_tokens"].(float64); ok {
				inputTokens = int(v)
			}
			if v, ok := usage["completion_tokens"].(float64); ok {
				outputTokens = int(v)
			}
		}
	}

	// Validate tool call arguments for hallucination detection (translated response is in OpenAI format)
	var toolCallValid *bool
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		if choices, ok := respBody["choices"].([]interface{}); ok && len(choices) > 0 {
			if choice, ok := choices[0].(map[string]interface{}); ok {
				if message, ok := choice["message"].(map[string]interface{}); ok {
					if toolCalls, ok := message["tool_calls"].([]interface{}); ok && len(toolCalls) > 0 {
						allValid := true
						for _, tc := range toolCalls {
							tcMap, ok := tc.(map[string]interface{})
							if !ok {
								allValid = false
								break
							}
							fn, ok := tcMap["function"].(map[string]interface{})
							if !ok {
								allValid = false
								break
							}
							argsStr, _ := fn["arguments"].(string)
							if argsStr != "" {
								var args json.RawMessage
								if json.Unmarshal([]byte(argsStr), &args) != nil {
									allValid = false
								}
							}
						}
						toolCallValid = &allValid
					}
				}
			}
		}
	}

	durationMs := time.Since(start).Milliseconds()

	lh.audit.Log(AuditEntry{
		Type:          "LLM_DIRECT",
		Model:         modelAlias,
		ProviderModel: providerModel,
		CorrelationID: correlationID,
		EventID:       eventID,
		Status:        resp.StatusCode,
		InputTokens:   inputTokens,
		OutputTokens:  outputTokens,
		DurationMs:    durationMs,
		TTFTMs:        durationMs,
		ToolCallValid: toolCallValid,
		StepIndex:     stepIndex,
		RetryOf:       retryOf,
	})
	lh.emitErrorSignal(resp.StatusCode, modelAlias, correlationID, 0)

	// Report usage for budget tracking
	lh.reportUsage(modelAlias, providerModel, inputTokens, outputTokens, resp.StatusCode, durationMs)
}

// relayAnthropicStream relays an Anthropic SSE streaming response, translating
// each event to OpenAI chat.completion.chunk format before sending to the client.
func (lh *LLMHandler) relayAnthropicStream(w http.ResponseWriter, resp *http.Response, modelAlias, providerModel, correlationID, eventID string, start time.Time, stepIndex int, retryOf string) {
	for k, vv := range resp.Header {
		if safeStreamHeaders[strings.ToLower(k)] {
			for _, v := range vv {
				w.Header().Add(k, v)
			}
		}
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(resp.StatusCode)

	flusher, canFlush := w.(http.Flusher)
	translator := newStreamTranslator()

	var ttftTime time.Time
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "event:") || line == "" {
			continue
		}
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		if ttftTime.IsZero() {
			ttftTime = time.Now()
		}
		data := line[6:]

		chunks := translator.translateEvent(data)
		for _, chunk := range chunks {
			fmt.Fprintf(w, "data: %s\n\n", chunk)
			if canFlush {
				flusher.Flush()
			}
		}
	}

	fmt.Fprint(w, "data: [DONE]\n\n")
	if canFlush {
		flusher.Flush()
	}

	ttftMs := int64(0)
	if !ttftTime.IsZero() {
		ttftMs = ttftTime.Sub(start).Milliseconds()
	}
	tpotMs := float64(0)
	durationMs := time.Since(start).Milliseconds()
	if translator.outputTokens > 0 && ttftMs > 0 {
		tpotMs = float64(durationMs-ttftMs) / float64(translator.outputTokens)
	}

	lh.audit.Log(AuditEntry{
		Type:          "LLM_DIRECT_STREAM",
		Model:         modelAlias,
		ProviderModel: providerModel,
		CorrelationID: correlationID,
		EventID:       eventID,
		Status:        resp.StatusCode,
		InputTokens:   translator.inputTokens,
		OutputTokens:  translator.outputTokens,
		DurationMs:    durationMs,
		TTFTMs:        ttftMs,
		TPOTMs:        tpotMs,
		StepIndex:     stepIndex,
		RetryOf:       retryOf,
	})
	lh.emitErrorSignal(resp.StatusCode, modelAlias, correlationID, 0)

	// Report usage for budget tracking
	lh.reportUsage(modelAlias, providerModel, translator.inputTokens, translator.outputTokens, resp.StatusCode, time.Since(start).Milliseconds())
}

// acquireRateSlot blocks until a rate limit slot is available from the
// in-process rate limiter. Sends SSE keepalive comments while waiting.
// Returns an error if the maximum wait time (120s) is exceeded.
func (lh *LLMHandler) acquireRateSlot(w http.ResponseWriter, providerName string) error {
	if lh.rateLimiter == nil {
		return nil
	}
	deadline := time.Now().Add(120 * time.Second)
	flusher, canFlush := w.(http.Flusher)
	for {
		granted, waitSecs := lh.rateLimiter.Acquire(providerName)
		if granted {
			return nil
		}
		if time.Now().After(deadline) {
			http.Error(w, `{"error":"rate limit timeout"}`, http.StatusTooManyRequests)
			return fmt.Errorf("rate limit timeout for %s", providerName)
		}
		if canFlush {
			fmt.Fprint(w, ": queued\n\n")
			flusher.Flush()
		}
		wait := time.Duration(waitSecs * float64(time.Second))
		if wait < 100*time.Millisecond {
			wait = 100 * time.Millisecond
		}
		if wait > 5*time.Second {
			wait = 5 * time.Second
		}
		time.Sleep(wait)
	}
}

// reportRateLimitHeaders extracts rate limit headers from the upstream response
// and updates the in-process rate limiter.
func (lh *LLMHandler) reportRateLimitHeaders(resp *http.Response, providerName string) {
	if lh.rateLimiter == nil {
		return
	}
	limitReqs := atoiOr(resp.Header.Get("X-Ratelimit-Limit-Requests"), 0)
	remainingReqs := atoiOr(resp.Header.Get("X-Ratelimit-Remaining-Requests"), 0)
	resetSecs := atofOr(resp.Header.Get("X-Ratelimit-Reset-Requests"), 0)
	if limitReqs > 0 || remainingReqs > 0 || resetSecs > 0 {
		lh.rateLimiter.Update(providerName, limitReqs, remainingReqs, resetSecs)
	}
	if resp.StatusCode == 429 {
		retryAfter := atofOr(resp.Header.Get("Retry-After"), 0)
		if retryAfter > 0 {
			lh.rateLimiter.Update(providerName, 0, 0, retryAfter)
		}
		lh.rateLimiter.Report429(providerName)
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

// reportUsage records token usage for budget tracking and rate limiting.
func (lh *LLMHandler) reportUsage(modelAlias, providerModel string, inputTokens, outputTokens int, statusCode int, latencyMs int64) {
	if inputTokens == 0 && outputTokens == 0 {
		return
	}
	model, ok := lh.routing.Models[modelAlias]
	if !ok {
		return
	}

	// Record to budget tracker for per-task cost tracking
	if lh.budget != nil {
		lh.budget.RecordUsage(
			int64(inputTokens), int64(outputTokens), 0,
			model.CostIn, model.CostOut, model.CostCached,
		)
	}

	// Record request for rate limiter window tracking
	if lh.rateLimiter != nil {
		providerName := ""
		if m, ok := lh.routing.Models[modelAlias]; ok {
			providerName = m.Provider
		}
		if providerName != "" {
			lh.rateLimiter.RecordRequest(providerName)
		}
	}
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
