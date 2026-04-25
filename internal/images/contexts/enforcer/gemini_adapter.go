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

func (lh *LLMHandler) relayGeminiBuffered(w http.ResponseWriter, resp *http.Response, modelAlias, providerName, providerModel, correlationID string, start time.Time) {
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
		Status:        resp.StatusCode,
		InputTokens:   inputTokens,
		OutputTokens:  outputTokens,
		DurationMs:    durationMs,
	})
	lh.emitErrorSignal(resp.StatusCode, modelAlias, correlationID, 0)
	lh.reportUsage(modelAlias, providerModel, inputTokens, outputTokens, 0, resp.StatusCode, durationMs)
	lh.scanContent(string(translated), "response")
}

func (lh *LLMHandler) relayGeminiStream(w http.ResponseWriter, resp *http.Response, modelAlias, providerName, providerModel, correlationID string, start time.Time) {
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
	var fullText strings.Builder

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "event:") || !strings.HasPrefix(line, "data:") {
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
		fmt.Fprintf(w, "data: %s\n\n", geminiOpenAIStreamChunk(content, "", nil))
		if canFlush {
			flusher.Flush()
		}
	}

	usage := map[string]interface{}{}
	if inputTokens > 0 {
		usage["prompt_tokens"] = inputTokens
	}
	if outputTokens > 0 {
		usage["completion_tokens"] = outputTokens
	}
	if inputTokens > 0 || outputTokens > 0 {
		usage["total_tokens"] = inputTokens + outputTokens
	} else {
		usage = nil
	}
	fmt.Fprintf(w, "data: %s\n\n", geminiOpenAIStreamChunk("", "stop", usage))
	fmt.Fprint(w, "data: [DONE]\n\n")
	if canFlush {
		flusher.Flush()
	}

	durationMs := time.Since(start).Milliseconds()
	lh.audit.Log(AuditEntry{
		Type:          "LLM_DIRECT_STREAM",
		Model:         modelAlias,
		Provider:      providerName,
		ProviderModel: providerModel,
		CorrelationID: correlationID,
		Status:        resp.StatusCode,
		InputTokens:   inputTokens,
		OutputTokens:  outputTokens,
		DurationMs:    durationMs,
	})
	lh.emitErrorSignal(resp.StatusCode, modelAlias, correlationID, 0)
	lh.reportUsage(modelAlias, providerModel, inputTokens, outputTokens, 0, resp.StatusCode, durationMs)
	lh.scanContent(fullText.String(), "response")
}

func translateFromGemini(geminiBody []byte) ([]byte, error) {
	var resp map[string]interface{}
	if err := json.Unmarshal(geminiBody, &resp); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}
	if errVal, ok := resp["error"]; ok {
		return json.Marshal(map[string]interface{}{"error": errVal})
	}

	text := geminiResponseText(resp)
	inputTokens, outputTokens := geminiUsageCounts(resp)
	usage := map[string]interface{}{}
	if inputTokens > 0 {
		usage["prompt_tokens"] = inputTokens
	}
	if outputTokens > 0 {
		usage["completion_tokens"] = outputTokens
	}
	if inputTokens > 0 || outputTokens > 0 {
		usage["total_tokens"] = inputTokens + outputTokens
	}

	return json.Marshal(map[string]interface{}{
		"id":     "gemini-response",
		"object": "chat.completion",
		"choices": []interface{}{
			map[string]interface{}{
				"index": 0,
				"message": map[string]interface{}{
					"role":    "assistant",
					"content": text,
				},
				"finish_reason": "stop",
			},
		},
		"usage": usage,
	})
}

func geminiResponseText(resp map[string]interface{}) string {
	var parts []string
	if candidates, ok := resp["candidates"].([]interface{}); ok && len(candidates) > 0 {
		cand, _ := candidates[0].(map[string]interface{})
		content, _ := cand["content"].(map[string]interface{})
		if rawParts, ok := content["parts"].([]interface{}); ok {
			for _, raw := range rawParts {
				part, _ := raw.(map[string]interface{})
				if text, _ := part["text"].(string); text != "" {
					parts = append(parts, text)
				}
			}
		}
	}
	return strings.Join(parts, "")
}

func geminiUsageCounts(resp map[string]interface{}) (int, int) {
	usage, _ := resp["usageMetadata"].(map[string]interface{})
	return intFromJSON(usage["promptTokenCount"]), intFromJSON(usage["candidatesTokenCount"])
}

func geminiOpenAIStreamChunk(content, finishReason string, usage map[string]interface{}) string {
	delta := map[string]interface{}{}
	if content != "" {
		delta["content"] = content
	}
	chunk := map[string]interface{}{
		"id":     "gemini-stream",
		"object": "chat.completion.chunk",
		"choices": []interface{}{
			map[string]interface{}{
				"index":         0,
				"delta":         delta,
				"finish_reason": nil,
			},
		},
	}
	if finishReason != "" {
		chunk["choices"].([]interface{})[0].(map[string]interface{})["finish_reason"] = finishReason
	}
	if usage != nil {
		chunk["usage"] = usage
	}
	encoded, _ := json.Marshal(chunk)
	return string(encoded)
}

func extractUsageCounts(respBody map[string]interface{}) (int, int) {
	usage, _ := respBody["usage"].(map[string]interface{})
	return intFromJSON(usage["prompt_tokens"]), intFromJSON(usage["completion_tokens"])
}
