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
func (lh *LLMHandler) relayAnthropicBuffered(w http.ResponseWriter, resp *http.Response, modelAlias, providerName, providerModel, correlationID string, start time.Time) {
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
	inputTokens, outputTokens, cacheCreationTokens, cacheReadTokens := 0, 0, 0, 0
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

	lh.audit.Log(AuditEntry{
		Type:                     "LLM_DIRECT",
		Model:                    modelAlias,
		Provider:                 providerName,
		ProviderModel:            providerModel,
		CorrelationID:            correlationID,
		Status:                   resp.StatusCode,
		InputTokens:              inputTokens,
		OutputTokens:             outputTokens,
		CachedTokens:             cacheReadTokens,
		CacheCreationInputTokens: cacheCreationTokens,
		CacheReadInputTokens:     cacheReadTokens,
		DurationMs:               time.Since(start).Milliseconds(),
	})
	lh.emitErrorSignal(resp.StatusCode, modelAlias, correlationID, 0)
	lh.reportUsage(modelAlias, providerModel, inputTokens, outputTokens, cacheReadTokens, resp.StatusCode, time.Since(start).Milliseconds())
	lh.scanContent(string(translated), "response")
}

// relayAnthropicStream relays an Anthropic SSE streaming response, translating
// each event to OpenAI chat.completion.chunk format before sending to the client.
func (lh *LLMHandler) relayAnthropicStream(w http.ResponseWriter, resp *http.Response, modelAlias, providerName, providerModel, correlationID string, start time.Time) {
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

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "event:") || line == "" {
			continue
		}
		if !strings.HasPrefix(line, "data: ") {
			continue
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

	lh.audit.Log(AuditEntry{
		Type:                     "LLM_DIRECT_STREAM",
		Model:                    modelAlias,
		Provider:                 providerName,
		ProviderModel:            providerModel,
		CorrelationID:            correlationID,
		Status:                   resp.StatusCode,
		InputTokens:              translator.inputTokens,
		OutputTokens:             translator.outputTokens,
		CachedTokens:             translator.cacheRead,
		CacheCreationInputTokens: translator.cacheCreation,
		CacheReadInputTokens:     translator.cacheRead,
		DurationMs:               time.Since(start).Milliseconds(),
	})
	lh.emitErrorSignal(resp.StatusCode, modelAlias, correlationID, 0)
	lh.reportUsage(modelAlias, providerModel, translator.inputTokens, translator.outputTokens, translator.cacheRead, resp.StatusCode, time.Since(start).Milliseconds())
}
