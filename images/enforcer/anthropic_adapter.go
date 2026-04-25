package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// relayAnthropicBuffered relays a non-streaming Anthropic response, translating
// it back to OpenAI format before sending to the client.
func (lh *LLMHandler) relayAnthropicBuffered(w http.ResponseWriter, resp *http.Response, modelAlias, providerName, providerModel, correlationID, eventID string, start time.Time, stepIndex int, retryOf string, providerToolUses []ProviderToolUse) {
	body, _ := io.ReadAll(resp.Body)

	translated, err := translateFromAnthropic(body)
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
		Provider:                 providerName,
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
	lh.reportUsage(modelAlias, providerModel, inputTokens, outputTokens, cacheReadTokens, lh.providerToolCostEstimate(modelAlias, providerToolUses), resp.StatusCode, durationMs)
}

// relayAnthropicStream relays an Anthropic SSE streaming response, translating
// each event to OpenAI chat.completion.chunk format before sending to the client.
func (lh *LLMHandler) relayAnthropicStream(w http.ResponseWriter, resp *http.Response, modelAlias, providerName, providerModel, correlationID, eventID string, start time.Time, stepIndex int, retryOf string, providerToolUses []ProviderToolUse) {
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

	if evidenceChunk := providerToolEvidenceStreamChunk(providerToolUses, rawChunks); evidenceChunk != "" {
		fmt.Fprintf(w, "data: %s\n\n", evidenceChunk)
		if canFlush {
			flusher.Flush()
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
		Provider:                 providerName,
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
	lh.reportUsage(modelAlias, providerModel, translator.inputTokens, translator.outputTokens, translator.cacheRead, lh.providerToolCostEstimate(modelAlias, providerToolUses), resp.StatusCode, time.Since(start).Milliseconds())
}
