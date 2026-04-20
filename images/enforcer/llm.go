package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
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
	routing       *RoutingConfig
	proxy         string // egress proxy URL
	audit         *AuditLogger
	transport     *http.Transport
	rateLimiter   *RateLimiter
	agentName     string
	budget        *BudgetTracker
	toolTracker   *ToolTracker
	trajectory    *TrajectoryMonitor
	providerTools *ProviderToolPolicy
	stepCounters  map[string]int
	stepMu        sync.Mutex
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
			slog.Warn("error signal relay failed", "error", err)
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

// SetProviderToolPolicy wires provider-side server tool grants into the LLM
// path. These grants are loaded from constraints.yaml and are external to the
// agent boundary.
func (lh *LLMHandler) SetProviderToolPolicy(policy *ProviderToolPolicy) {
	lh.providerTools = policy
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
	providerToolUses := DetectProviderToolUses(reqBody)
	model := lh.routing.Models[modelAlias]

	// Check if resolved model supports required capabilities.
	// Models with no capabilities declared are assumed to support everything
	// (backward compat with routing configs that predate capability declarations).
	if len(requiredCaps) > 0 {
		if len(model.Capabilities) > 0 {
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
	}

	for _, use := range providerToolUses {
		if !lh.providerTools.Allows(use.Capability) {
			errMsg := providerToolDeniedError(use)
			lh.audit.Log(AuditEntry{
				Type:          "PROVIDER_TOOL_DENIED",
				Model:         modelAlias,
				CorrelationID: correlationID,
				Status:        http.StatusForbidden,
				Error:         errMsg,
				Extra: map[string]string{
					"provider_tool_capability": use.Capability,
					"provider_tool_type":       use.ToolType,
					"provider_tool_name":       use.Name,
				},
			})
			http.Error(w, fmt.Sprintf(`{"error":"provider_tool_denied","detail":%q}`, errMsg), http.StatusForbidden)
			return
		}
		if providerToolRequiresAgencyHarness(use.Capability) {
			if providerToolHarnessAvailable(use.Capability) {
				if len(model.Capabilities) > 0 && !model.HasCapability("tools") {
					errMsg := fmt.Sprintf("model %q does not declare ordinary tool support required for provider tool harness capability %q", modelAlias, use.Capability)
					lh.audit.Log(AuditEntry{
						Type:          "PROVIDER_TOOL_UNSUPPORTED",
						Model:         modelAlias,
						ProviderModel: providerModel,
						CorrelationID: correlationID,
						Status:        http.StatusUnprocessableEntity,
						Error:         errMsg,
						Extra: map[string]string{
							"provider_tool_capability":      use.Capability,
							"provider_tool_type":            use.ToolType,
							"provider_tool_name":            use.Name,
							"provider_tool_execution_modes": providerToolExecutionAgencyHarnessed,
						},
					})
					http.Error(w, fmt.Sprintf(`{"error":"provider_tool_unsupported","detail":%q}`, errMsg), http.StatusUnprocessableEntity)
					return
				}
				continue
			}
			errMsg := providerToolHarnessUnavailableError(use)
			lh.audit.Log(AuditEntry{
				Type:          "PROVIDER_TOOL_HARNESS_UNAVAILABLE",
				Model:         modelAlias,
				ProviderModel: providerModel,
				CorrelationID: correlationID,
				Status:        http.StatusNotImplemented,
				Error:         errMsg,
				Extra: map[string]string{
					"provider_tool_capability":      use.Capability,
					"provider_tool_type":            use.ToolType,
					"provider_tool_name":            use.Name,
					"provider_tool_execution_modes": providerToolExecutionAgencyHarnessed,
				},
			})
			http.Error(w, fmt.Sprintf(`{"error":"provider_tool_harness_unavailable","detail":%q}`, errMsg), http.StatusNotImplemented)
			return
		}
		if !model.HasProviderToolCapability(use.Capability) {
			errMsg := providerToolUnsupportedError(modelAlias, use)
			lh.audit.Log(AuditEntry{
				Type:          "PROVIDER_TOOL_UNSUPPORTED",
				Model:         modelAlias,
				ProviderModel: providerModel,
				CorrelationID: correlationID,
				Status:        http.StatusUnprocessableEntity,
				Error:         errMsg,
				Extra: map[string]string{
					"provider_tool_capability": use.Capability,
					"provider_tool_type":       use.ToolType,
					"provider_tool_name":       use.Name,
				},
			})
			http.Error(w, fmt.Sprintf(`{"error":"provider_tool_unsupported","detail":%q}`, errMsg), http.StatusUnprocessableEntity)
			return
		}
	}
	harnessedProviderToolUses := rewriteHarnessedProviderTools(reqBody)
	if len(harnessedProviderToolUses) > 0 {
		lh.audit.Log(AuditEntry{
			Type:          "PROVIDER_TOOL_HARNESS_TRANSLATED",
			Model:         modelAlias,
			ProviderModel: providerModel,
			CorrelationID: correlationID,
			Status:        http.StatusOK,
			Extra:         summarizeHarnessedProviderToolUses(harnessedProviderToolUses),
		})
		providerToolUses = nonHarnessedProviderToolUses(providerToolUses)
	}
	if len(providerToolUses) > 0 {
		lh.audit.Log(AuditEntry{
			Type:          "PROVIDER_TOOL_ALLOWED",
			Model:         modelAlias,
			CorrelationID: correlationID,
			Status:        http.StatusOK,
			Extra:         summarizeProviderToolUses(providerToolUses),
		})
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
	provider := lh.routing.Providers[lh.routing.Models[modelAlias].Provider]
	isGeminiNative := provider.APIFormat == "gemini"
	isResponsesPath := r.URL.Path == "/v1/responses"

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
	if !isAnthropic && !isGeminiNative {
		path := r.URL.Path
		if strings.HasPrefix(path, "/v1/") {
			endpoint := path[3:] // strip /v1, keep /chat/completions etc
			base := strings.TrimRight(provider.APIBase, "/")
			targetURL = base + endpoint
		}
	} else if isResponsesPath && isAnthropic {
		http.Error(w, fmt.Sprintf(`{"error":"responses endpoint is not supported for %s models"}`, providerName), http.StatusBadRequest)
		return
	}

	// Determine if streaming
	isStream, _ := reqBody["stream"].(bool)
	if isStream && isGeminiNative {
		targetURL = strings.Replace(targetURL, ":generateContent", ":streamGenerateContent", 1)
		if !strings.Contains(targetURL, "?") {
			targetURL += "?alt=sse"
		} else {
			targetURL += "&alt=sse"
		}
	}

	// Rewrite model in request body
	reqBody["model"] = providerModel
	if isStream && !isAnthropic && !isResponsesPath {
		ensureStreamUsageRequested(reqBody)
	}
	modifiedBody, err := json.Marshal(reqBody)
	if err != nil {
		http.Error(w, `{"error":"failed to rewrite request body"}`, http.StatusInternalServerError)
		return
	}

	// For Anthropic: translate request body to Anthropic format
	if isAnthropic {
		modifiedBody, err = translateToAnthropic(modifiedBody, provider.CachingEnabled())
		if err != nil {
			http.Error(w, `{"error":"failed to translate request for Anthropic"}`, http.StatusInternalServerError)
			return
		}
	} else if isGeminiNative {
		modifiedBody, err = translateToGemini(modifiedBody)
		if err != nil {
			http.Error(w, `{"error":"failed to translate request for Gemini"}`, http.StatusInternalServerError)
			return
		}
	}

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
				slog.Warn("LLM upstream error", "attempt", attempt+1, "max_retries", llmMaxRetries, "error", err)
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
			lh.relayAnthropicStream(w, resp, modelAlias, providerModel, correlationID, eventID, start, stepIndex, retryOf, providerToolUses)
		} else if isGeminiNative {
			if isResponsesPath {
				lh.relayGeminiStream(w, resp, modelAlias, providerModel, correlationID, eventID, start, stepIndex, retryOf, providerToolUses, geminiStreamResponses)
			} else {
				lh.relayGeminiStream(w, resp, modelAlias, providerModel, correlationID, eventID, start, stepIndex, retryOf, providerToolUses, geminiStreamChat)
			}
		} else {
			lh.relayStream(w, resp, modelAlias, providerModel, correlationID, eventID, start, stepIndex, retryOf, providerToolUses)
		}
	} else {
		if isAnthropic {
			lh.relayAnthropicBuffered(w, resp, modelAlias, providerModel, correlationID, eventID, start, stepIndex, retryOf, providerToolUses)
		} else if isGeminiNative {
			if isResponsesPath {
				lh.relayGeminiResponsesBuffered(w, resp, modelAlias, providerModel, correlationID, eventID, start, stepIndex, retryOf, providerToolUses)
			} else {
				lh.relayGeminiBuffered(w, resp, modelAlias, providerModel, correlationID, eventID, start, stepIndex, retryOf, providerToolUses)
			}
		} else {
			lh.relayBuffered(w, resp, modelAlias, providerModel, correlationID, eventID, start, stepIndex, retryOf, providerToolUses)
		}
	}
}

func ensureStreamUsageRequested(reqBody map[string]interface{}) {
	streamOptions, _ := reqBody["stream_options"].(map[string]interface{})
	if streamOptions == nil {
		streamOptions = make(map[string]interface{})
		reqBody["stream_options"] = streamOptions
	}
	streamOptions["include_usage"] = true
}

// relayBuffered relays a non-streaming LLM response.
func (lh *LLMHandler) relayBuffered(w http.ResponseWriter, resp *http.Response, modelAlias, providerModel, correlationID, eventID string, start time.Time, stepIndex int, retryOf string, providerToolUses []ProviderToolUse) {
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
		inputTokens, outputTokens = extractUsageCounts(respBody)
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
		Extra:         lh.providerToolAuditExtra(modelAlias, providerToolUses, respBody),
	})
	lh.auditProviderToolHarnessProposals(modelAlias, providerModel, correlationID, resp.StatusCode, respBody)
	lh.emitErrorSignal(resp.StatusCode, modelAlias, correlationID, 0)

	// Report usage for budget tracking
	lh.reportUsage(modelAlias, providerModel, inputTokens, outputTokens, 0, lh.providerToolCostEstimate(modelAlias, providerToolUses), resp.StatusCode, durationMs)
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
			slog.Warn("trajectory signal relay failed", "error", err)
			return
		}
		resp.Body.Close()
	}()
}

// relayStream relays an SSE streaming LLM response.
func (lh *LLMHandler) relayStream(w http.ResponseWriter, resp *http.Response, modelAlias, providerModel, correlationID, eventID string, start time.Time, stepIndex int, retryOf string, providerToolUses []ProviderToolUse) {
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
	var providerToolChunks []interface{}
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
			if len(providerToolUses) > 0 {
				providerToolChunks = append(providerToolChunks, extractStreamJSONObjects(chunkStr)...)
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
		Extra: lh.providerToolAuditExtra(modelAlias, providerToolUses, map[string]interface{}{
			"chunks": providerToolChunks,
		}),
	})
	lh.auditProviderToolHarnessProposals(modelAlias, providerModel, correlationID, resp.StatusCode, map[string]interface{}{
		"chunks": providerToolChunks,
	})
	lh.emitErrorSignal(resp.StatusCode, modelAlias, correlationID, 0)

	// Report usage for budget tracking
	lh.reportUsage(modelAlias, providerModel, inputTokens, outputTokens, 0, lh.providerToolCostEstimate(modelAlias, providerToolUses), resp.StatusCode, durationMs)
}

func extractStreamJSONObjects(chunk string) []interface{} {
	var objects []interface{}
	for _, line := range strings.Split(chunk, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data: "))
		if data == "" || data == "[DONE]" {
			continue
		}
		var obj interface{}
		if json.Unmarshal([]byte(data), &obj) == nil {
			objects = append(objects, obj)
		}
	}
	return objects
}

type geminiStreamMode int

const (
	geminiStreamChat geminiStreamMode = iota
	geminiStreamResponses
)

// relayGeminiStream relays native Gemini SSE as either OpenAI-compatible chat
// chunks or Responses events.
func (lh *LLMHandler) relayGeminiStream(w http.ResponseWriter, resp *http.Response, modelAlias, providerModel, correlationID, eventID string, start time.Time, stepIndex int, retryOf string, providerToolUses []ProviderToolUse, mode geminiStreamMode) {
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
	inputTokens, outputTokens := 0, 0
	var ttftTime time.Time
	var rawChunks []interface{}
	var fullText strings.Builder

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "event:") {
			continue
		}
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" || data == "[DONE]" {
			continue
		}

		var chunk map[string]interface{}
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}
		rawChunks = append(rawChunks, chunk)
		if in, out := geminiUsageCounts(chunk); in > 0 || out > 0 {
			if in > 0 {
				inputTokens = in
			}
			if out > 0 {
				outputTokens = out
			}
		}

		content := geminiResponseText(chunk)
		if content == "" {
			continue
		}
		fullText.WriteString(content)
		if ttftTime.IsZero() {
			ttftTime = time.Now()
		}
		if mode == geminiStreamResponses {
			fmt.Fprintf(w, "event: response.output_text.delta\ndata: %s\n\n", geminiOpenAIResponseTextDelta(content))
		} else {
			fmt.Fprintf(w, "data: %s\n\n", geminiOpenAIStreamChunk(content, "", nil))
		}
		if canFlush {
			flusher.Flush()
		}
	}

	finalUsage := map[string]interface{}{}
	if inputTokens > 0 {
		finalUsage["prompt_tokens"] = inputTokens
	}
	if outputTokens > 0 {
		finalUsage["completion_tokens"] = outputTokens
	}
	if inputTokens > 0 || outputTokens > 0 {
		finalUsage["total_tokens"] = inputTokens + outputTokens
	}
	if len(finalUsage) == 0 {
		finalUsage = nil
	}
	if mode == geminiStreamResponses {
		fmt.Fprintf(w, "event: response.completed\ndata: %s\n\n", geminiOpenAIResponseCompleted(fullText.String(), finalUsage))
	} else {
		fmt.Fprintf(w, "data: %s\n\n", geminiOpenAIStreamChunk("", "stop", finalUsage))
		fmt.Fprint(w, "data: [DONE]\n\n")
	}
	if canFlush {
		flusher.Flush()
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

	lh.audit.Log(AuditEntry{
		Type:          "LLM_DIRECT_STREAM",
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
		Extra: lh.providerToolAuditExtra(modelAlias, providerToolUses, map[string]interface{}{
			"chunks": rawChunks,
		}),
	})
	lh.auditProviderToolHarnessProposals(modelAlias, providerModel, correlationID, resp.StatusCode, map[string]interface{}{
		"chunks": rawChunks,
	})
	lh.emitErrorSignal(resp.StatusCode, modelAlias, correlationID, 0)
	lh.reportUsage(modelAlias, providerModel, inputTokens, outputTokens, 0, lh.providerToolCostEstimate(modelAlias, providerToolUses), resp.StatusCode, durationMs)
}

// relayAnthropicBuffered relays a non-streaming Anthropic response, translating
// it back to OpenAI format before sending to the client.
func (lh *LLMHandler) relayAnthropicBuffered(w http.ResponseWriter, resp *http.Response, modelAlias, providerModel, correlationID, eventID string, start time.Time, stepIndex int, retryOf string, providerToolUses []ProviderToolUse) {
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
	var rawRespBody map[string]interface{}
	inputTokens, outputTokens, cacheCreationTokens, cacheReadTokens := 0, 0, 0, 0
	_ = json.Unmarshal(body, &rawRespBody)
	if json.Unmarshal(translated, &respBody) == nil {
		if usage, ok := respBody["usage"].(map[string]interface{}); ok {
			if v, ok := usage["prompt_tokens"].(float64); ok {
				inputTokens = int(v)
			}
			if v, ok := usage["completion_tokens"].(float64); ok {
				outputTokens = int(v)
			}
			cacheCreationTokens = intFromJSON(usage["cache_creation_input_tokens"])
			cacheReadTokens = intFromJSON(usage["cache_read_input_tokens"])
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
		Type:                     "LLM_DIRECT",
		Model:                    modelAlias,
		ProviderModel:            providerModel,
		CorrelationID:            correlationID,
		EventID:                  eventID,
		Status:                   resp.StatusCode,
		InputTokens:              inputTokens,
		OutputTokens:             outputTokens,
		CachedTokens:             cacheReadTokens,
		CacheCreationInputTokens: cacheCreationTokens,
		CacheReadInputTokens:     cacheReadTokens,
		DurationMs:               durationMs,
		TTFTMs:                   durationMs,
		ToolCallValid:            toolCallValid,
		StepIndex:                stepIndex,
		RetryOf:                  retryOf,
		Extra:                    lh.providerToolAuditExtra(modelAlias, providerToolUses, rawRespBody),
	})
	lh.auditProviderToolHarnessProposals(modelAlias, providerModel, correlationID, resp.StatusCode, rawRespBody)
	lh.emitErrorSignal(resp.StatusCode, modelAlias, correlationID, 0)

	// Report usage for budget tracking
	lh.reportUsage(modelAlias, providerModel, inputTokens, outputTokens, cacheReadTokens, lh.providerToolCostEstimate(modelAlias, providerToolUses), resp.StatusCode, durationMs)
}

// relayGeminiBuffered relays a non-streaming Gemini native response,
// translating it back to OpenAI chat/completions format for the body runtime.
func (lh *LLMHandler) relayGeminiBuffered(w http.ResponseWriter, resp *http.Response, modelAlias, providerModel, correlationID, eventID string, start time.Time, stepIndex int, retryOf string, providerToolUses []ProviderToolUse) {
	body, _ := io.ReadAll(resp.Body)

	translated, err := translateFromGemini(body)
	if err != nil {
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

	var respBody map[string]interface{}
	var rawRespBody map[string]interface{}
	_ = json.Unmarshal(body, &rawRespBody)
	inputTokens, outputTokens := 0, 0
	if json.Unmarshal(translated, &respBody) == nil {
		inputTokens, outputTokens = extractUsageCounts(respBody)
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
		StepIndex:     stepIndex,
		RetryOf:       retryOf,
		Extra:         lh.providerToolAuditExtra(modelAlias, providerToolUses, rawRespBody),
	})
	lh.auditProviderToolHarnessProposals(modelAlias, providerModel, correlationID, resp.StatusCode, rawRespBody)
	lh.emitErrorSignal(resp.StatusCode, modelAlias, correlationID, 0)
	lh.reportUsage(modelAlias, providerModel, inputTokens, outputTokens, 0, lh.providerToolCostEstimate(modelAlias, providerToolUses), resp.StatusCode, durationMs)
}

// relayGeminiResponsesBuffered relays a non-streaming Gemini native response
// as an OpenAI Responses-style body.
func (lh *LLMHandler) relayGeminiResponsesBuffered(w http.ResponseWriter, resp *http.Response, modelAlias, providerModel, correlationID, eventID string, start time.Time, stepIndex int, retryOf string, providerToolUses []ProviderToolUse) {
	body, _ := io.ReadAll(resp.Body)

	translated, err := translateFromGeminiResponse(body)
	if err != nil {
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

	var respBody map[string]interface{}
	var rawRespBody map[string]interface{}
	_ = json.Unmarshal(body, &rawRespBody)
	inputTokens, outputTokens := 0, 0
	if json.Unmarshal(translated, &respBody) == nil {
		inputTokens, outputTokens = extractUsageCounts(respBody)
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
		StepIndex:     stepIndex,
		RetryOf:       retryOf,
		Extra:         lh.providerToolAuditExtra(modelAlias, providerToolUses, rawRespBody),
	})
	lh.auditProviderToolHarnessProposals(modelAlias, providerModel, correlationID, resp.StatusCode, rawRespBody)
	lh.emitErrorSignal(resp.StatusCode, modelAlias, correlationID, 0)
	lh.reportUsage(modelAlias, providerModel, inputTokens, outputTokens, 0, lh.providerToolCostEstimate(modelAlias, providerToolUses), resp.StatusCode, durationMs)
}

// relayAnthropicStream relays an Anthropic SSE streaming response, translating
// each event to OpenAI chat.completion.chunk format before sending to the client.
func (lh *LLMHandler) relayAnthropicStream(w http.ResponseWriter, resp *http.Response, modelAlias, providerModel, correlationID, eventID string, start time.Time, stepIndex int, retryOf string, providerToolUses []ProviderToolUse) {
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
	var rawChunks []interface{}
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
		var raw map[string]interface{}
		if json.Unmarshal([]byte(data), &raw) == nil {
			rawChunks = append(rawChunks, raw)
		}

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
		Type:                     "LLM_DIRECT_STREAM",
		Model:                    modelAlias,
		ProviderModel:            providerModel,
		CorrelationID:            correlationID,
		EventID:                  eventID,
		Status:                   resp.StatusCode,
		InputTokens:              translator.inputTokens,
		OutputTokens:             translator.outputTokens,
		CachedTokens:             translator.cacheRead,
		CacheCreationInputTokens: translator.cacheCreation,
		CacheReadInputTokens:     translator.cacheRead,
		DurationMs:               durationMs,
		TTFTMs:                   ttftMs,
		TPOTMs:                   tpotMs,
		StepIndex:                stepIndex,
		RetryOf:                  retryOf,
		Extra: lh.providerToolAuditExtra(modelAlias, providerToolUses, map[string]interface{}{
			"chunks": rawChunks,
		}),
	})
	lh.auditProviderToolHarnessProposals(modelAlias, providerModel, correlationID, resp.StatusCode, map[string]interface{}{
		"chunks": rawChunks,
	})
	lh.emitErrorSignal(resp.StatusCode, modelAlias, correlationID, 0)

	// Report usage for budget tracking
	lh.reportUsage(modelAlias, providerModel, translator.inputTokens, translator.outputTokens, translator.cacheRead, lh.providerToolCostEstimate(modelAlias, providerToolUses), resp.StatusCode, time.Since(start).Milliseconds())
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
func (lh *LLMHandler) reportUsage(modelAlias, providerModel string, inputTokens, outputTokens, cachedTokens int, providerToolCostUSD float64, statusCode int, latencyMs int64) {
	if inputTokens == 0 && outputTokens == 0 && cachedTokens == 0 && providerToolCostUSD <= 0 {
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
		lh.budget.RecordCost(providerToolCostUSD, 0, 0, 0)
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

func (lh *LLMHandler) providerToolAuditExtra(modelAlias string, uses []ProviderToolUse, respBody map[string]interface{}) map[string]string {
	extra := providerToolAuditExtra(uses, respBody)
	if len(uses) == 0 {
		return extra
	}
	if extra == nil {
		extra = map[string]string{}
	}
	extra["provider_tool_call_count"] = fmt.Sprintf("%d", len(uses))

	model, ok := lh.routing.Models[modelAlias]
	if !ok {
		return extra
	}
	var cost float64
	var pricedCount int
	var unknownCount int
	units := map[string]bool{}
	sources := map[string]bool{}
	confidences := map[string]bool{}
	for _, use := range uses {
		price, ok := model.ProviderToolPriceFor(use.Capability)
		if !ok {
			unknownCount++
			confidences["unknown"] = true
			continue
		}
		pricedCount++
		cost += price.USDPerUnit
		if price.Unit != "" {
			units[price.Unit] = true
		}
		if price.Source != "" {
			sources[price.Source] = true
		}
		if price.Confidence != "" {
			confidences[price.Confidence] = true
		}
		if price.Confidence == "unknown" {
			unknownCount++
		}
	}
	if pricedCount > 0 {
		extra["provider_tool_estimated_cost_usd"] = fmt.Sprintf("%.8f", cost)
		if len(units) > 0 {
			extra["provider_tool_cost_unit"] = strings.Join(sortedKeys(units), ",")
		}
		if len(sources) > 0 {
			extra["provider_tool_cost_source"] = strings.Join(sortedKeys(sources), ",")
		}
		if len(confidences) > 0 {
			extra["provider_tool_cost_confidence"] = strings.Join(sortedKeys(confidences), ",")
		}
	}
	if unknownCount > 0 {
		extra["provider_tool_unpriced_count"] = fmt.Sprintf("%d", unknownCount)
		if len(confidences) > 0 {
			extra["provider_tool_cost_confidence"] = strings.Join(sortedKeys(confidences), ",")
		} else {
			extra["provider_tool_cost_confidence"] = "unknown"
		}
	}
	return extra
}

func (lh *LLMHandler) auditProviderToolHarnessProposals(modelAlias, providerModel, correlationID string, statusCode int, respBody map[string]interface{}) {
	if statusCode < 200 || statusCode >= 300 {
		return
	}
	proposals := detectHarnessedProviderToolProposals(respBody)
	if len(proposals) == 0 {
		return
	}
	lh.audit.Log(AuditEntry{
		Type:          "PROVIDER_TOOL_HARNESS_PROPOSED",
		Model:         modelAlias,
		ProviderModel: providerModel,
		CorrelationID: correlationID,
		Status:        statusCode,
		Extra:         summarizeHarnessedProviderToolUses(proposals),
	})
}

func (lh *LLMHandler) providerToolCostEstimate(modelAlias string, uses []ProviderToolUse) float64 {
	if len(uses) == 0 {
		return 0
	}
	model, ok := lh.routing.Models[modelAlias]
	if !ok {
		return 0
	}
	var cost float64
	for _, use := range uses {
		price, ok := model.ProviderToolPriceFor(use.Capability)
		if !ok || price.Confidence == "unknown" {
			continue
		}
		cost += price.USDPerUnit
	}
	return cost
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
		in, out := extractUsageCounts(obj)
		if in > 0 {
			inputTokens = in
		}
		if out > 0 {
			outputTokens = out
		}
	}
	return
}

func extractUsageCounts(obj map[string]interface{}) (inputTokens, outputTokens int) {
	if usage, ok := obj["usage"].(map[string]interface{}); ok {
		inputTokens = firstInt(
			usage["input_tokens"],
			usage["prompt_tokens"],
		)
		outputTokens = firstInt(
			usage["output_tokens"],
			usage["completion_tokens"],
		)
	}

	// Gemini and some proxy layers expose usage at the top level as usageMetadata.
	if usageMeta, ok := obj["usageMetadata"].(map[string]interface{}); ok {
		if inputTokens == 0 {
			inputTokens = firstInt(
				usageMeta["promptTokenCount"],
				usageMeta["prompt_token_count"],
			)
		}
		if outputTokens == 0 {
			outputTokens = firstInt(
				usageMeta["candidatesTokenCount"],
				usageMeta["candidates_token_count"],
				usageMeta["outputTokenCount"],
				usageMeta["output_token_count"],
			)
		}
	}
	if usageMeta, ok := obj["usage_metadata"].(map[string]interface{}); ok {
		if inputTokens == 0 {
			inputTokens = firstInt(
				usageMeta["promptTokenCount"],
				usageMeta["prompt_token_count"],
			)
		}
		if outputTokens == 0 {
			outputTokens = firstInt(
				usageMeta["candidatesTokenCount"],
				usageMeta["candidates_token_count"],
				usageMeta["outputTokenCount"],
				usageMeta["output_token_count"],
			)
		}
	}

	return inputTokens, outputTokens
}

func firstInt(values ...interface{}) int {
	for _, value := range values {
		switch v := value.(type) {
		case float64:
			if v > 0 {
				return int(v)
			}
		case int:
			if v > 0 {
				return v
			}
		case int64:
			if v > 0 {
				return int(v)
			}
		}
	}
	return 0
}
