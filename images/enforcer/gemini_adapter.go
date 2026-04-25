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

type geminiStreamMode int

const (
	geminiStreamChat geminiStreamMode = iota
	geminiStreamResponses
)

// relayGeminiStream relays native Gemini SSE as either OpenAI-compatible chat
// chunks or Responses events.
func (lh *LLMHandler) relayGeminiStream(w http.ResponseWriter, resp *http.Response, modelAlias, providerName, providerModel, correlationID, eventID string, start time.Time, stepIndex int, retryOf string, providerToolUses []ProviderToolUse, mode geminiStreamMode) {
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
		Provider:      providerName,
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

// relayGeminiBuffered relays a non-streaming Gemini native response,
// translating it into the normalized runtime contract for the body runtime.
func (lh *LLMHandler) relayGeminiBuffered(w http.ResponseWriter, resp *http.Response, modelAlias, providerName, providerModel, correlationID, eventID string, start time.Time, stepIndex int, retryOf string, providerToolUses []ProviderToolUse) {
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
		Provider:      providerName,
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
func (lh *LLMHandler) relayGeminiResponsesBuffered(w http.ResponseWriter, resp *http.Response, modelAlias, providerName, providerModel, correlationID, eventID string, start time.Time, stepIndex int, retryOf string, providerToolUses []ProviderToolUse) {
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
		Provider:      providerName,
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
