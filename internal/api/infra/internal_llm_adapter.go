package infra

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/geoffbelknap/agency/internal/models"
)

type internalLLMAdapter interface {
	PrepareRequest(provider *models.ProviderConfig, model *models.ModelConfig, body map[string]interface{}) (string, []byte, error)
	AddAuthHeaders(req *http.Request, provider *models.ProviderConfig, apiKey string)
	TranslateResponse(body []byte, statusCode int) []byte
}

func internalLLMAdapterFor(provider *models.ProviderConfig, model *models.ModelConfig) internalLLMAdapter {
	if provider == nil {
		return internalOpenAICompatibleAdapter{}
	}
	switch provider.APIFormat {
	case "gemini":
		return internalGeminiAdapter{}
	case "anthropic":
		return internalAnthropicAdapter{}
	}
	return internalOpenAICompatibleAdapter{}
}

type internalOpenAICompatibleAdapter struct{}

func (internalOpenAICompatibleAdapter) PrepareRequest(provider *models.ProviderConfig, model *models.ModelConfig, body map[string]interface{}) (string, []byte, error) {
	body["model"] = model.ProviderModel
	modifiedBody, err := json.Marshal(body)
	if err != nil {
		return "", nil, fmt.Errorf("rewrite request body: %w", err)
	}
	base := strings.TrimRight(provider.APIBase, "/")
	return base + "/chat/completions", modifiedBody, nil
}

func (internalOpenAICompatibleAdapter) AddAuthHeaders(req *http.Request, provider *models.ProviderConfig, apiKey string) {
	if apiKey == "" {
		return
	}
	prefix := provider.AuthPrefix
	if prefix == "" {
		prefix = "Bearer "
	}
	req.Header.Set(provider.AuthHeader, prefix+apiKey)
}

func (internalOpenAICompatibleAdapter) TranslateResponse(body []byte, _ int) []byte {
	return body
}

type internalAnthropicAdapter struct{}

func (internalAnthropicAdapter) PrepareRequest(provider *models.ProviderConfig, model *models.ModelConfig, body map[string]interface{}) (string, []byte, error) {
	body["model"] = model.ProviderModel
	modifiedBody, err := json.Marshal(body)
	if err != nil {
		return "", nil, fmt.Errorf("rewrite request body: %w", err)
	}
	modifiedBody, err = infraTranslateToAnthropic(modifiedBody)
	if err != nil {
		return "", nil, fmt.Errorf("translate request: %w", err)
	}
	base := strings.TrimRight(provider.APIBase, "/")
	return base + "/messages", modifiedBody, nil
}

func (internalAnthropicAdapter) AddAuthHeaders(req *http.Request, provider *models.ProviderConfig, apiKey string) {
	req.Header.Set("anthropic-version", "2023-06-01")
	if apiKey != "" {
		req.Header.Set(provider.AuthHeader, apiKey)
	}
}

func (internalAnthropicAdapter) TranslateResponse(body []byte, statusCode int) []byte {
	if statusCode < 200 || statusCode >= 300 {
		return body
	}
	translated, err := infraTranslateFromAnthropic(body)
	if err != nil {
		return body
	}
	return translated
}

type internalGeminiAdapter struct{}

func (internalGeminiAdapter) PrepareRequest(provider *models.ProviderConfig, model *models.ModelConfig, body map[string]interface{}) (string, []byte, error) {
	body["model"] = model.ProviderModel
	modifiedBody, err := json.Marshal(body)
	if err != nil {
		return "", nil, fmt.Errorf("rewrite request body: %w", err)
	}
	modifiedBody, err = infraTranslateToGemini(modifiedBody)
	if err != nil {
		return "", nil, fmt.Errorf("translate request: %w", err)
	}
	base := strings.TrimRight(provider.APIBase, "/")
	return fmt.Sprintf("%s/models/%s:generateContent", base, model.ProviderModel), modifiedBody, nil
}

func (internalGeminiAdapter) AddAuthHeaders(req *http.Request, provider *models.ProviderConfig, apiKey string) {
	if apiKey != "" {
		req.Header.Set(provider.AuthHeader, apiKey)
	}
}

func (internalGeminiAdapter) TranslateResponse(body []byte, statusCode int) []byte {
	if statusCode < 200 || statusCode >= 300 {
		return body
	}
	translated, err := infraTranslateFromGemini(body)
	if err != nil {
		return body
	}
	return translated
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
	for _, key := range []string{"model", "temperature", "top_p"} {
		if v, ok := req[key]; ok {
			result[key] = v
		}
	}
	if v, ok := req["max_tokens"]; ok {
		result["max_tokens"] = v
	} else {
		result["max_tokens"] = float64(4096)
	}

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
			systemBlocks = append(systemBlocks, map[string]interface{}{
				"type": "text",
				"text": content,
			})
			continue
		}
		out := make(map[string]interface{})
		for k, v := range msg {
			out[k] = v
		}
		anthropicMessages = append(anthropicMessages, out)
	}

	if len(systemBlocks) > 0 {
		result["system"] = systemBlocks
	}
	result["messages"] = anthropicMessages

	return json.Marshal(result)
}

func infraTranslateFromAnthropic(anthropicBody []byte) ([]byte, error) {
	var resp map[string]interface{}
	if err := json.Unmarshal(anthropicBody, &resp); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}
	if errVal, ok := resp["error"]; ok {
		return json.Marshal(map[string]interface{}{"error": errVal})
	}

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
					"role":        "assistant",
					"content":     strings.Join(textParts, " "),
					"stop_reason": stopReason,
				},
				"finish_reason": finishReason,
				"stop_reason":   stopReason,
			},
		},
		"usage": usage,
	}

	return json.Marshal(result)
}

func infraTranslateToGemini(openaiBody []byte) ([]byte, error) {
	var req map[string]interface{}
	if err := json.Unmarshal(openaiBody, &req); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}
	messages, ok := req["messages"].([]interface{})
	if !ok {
		return nil, fmt.Errorf("messages field missing or not an array")
	}

	var contents []interface{}
	var systemParts []interface{}
	for _, raw := range messages {
		msg, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		role, _ := msg["role"].(string)
		content, _ := msg["content"].(string)
		if content == "" {
			continue
		}
		if role == "system" {
			systemParts = append(systemParts, map[string]interface{}{"text": content})
			continue
		}
		if role == "assistant" {
			role = "model"
		} else {
			role = "user"
		}
		contents = append(contents, map[string]interface{}{
			"role":  role,
			"parts": []interface{}{map[string]interface{}{"text": content}},
		})
	}
	out := map[string]interface{}{"contents": contents}
	if len(systemParts) > 0 {
		out["system_instruction"] = map[string]interface{}{"parts": systemParts}
	}
	genCfg := map[string]interface{}{}
	if v, ok := req["temperature"]; ok {
		genCfg["temperature"] = v
	}
	if v, ok := req["top_p"]; ok {
		genCfg["topP"] = v
	}
	if v, ok := req["max_tokens"]; ok {
		genCfg["maxOutputTokens"] = v
	}
	if len(genCfg) > 0 {
		out["generationConfig"] = genCfg
	}
	return json.Marshal(out)
}

func infraTranslateFromGemini(geminiBody []byte) ([]byte, error) {
	var resp map[string]interface{}
	if err := json.Unmarshal(geminiBody, &resp); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}
	if errVal, ok := resp["error"]; ok {
		return json.Marshal(map[string]interface{}{"error": errVal})
	}

	var textParts []string
	if candidates, ok := resp["candidates"].([]interface{}); ok && len(candidates) > 0 {
		cand, _ := candidates[0].(map[string]interface{})
		content, _ := cand["content"].(map[string]interface{})
		if parts, ok := content["parts"].([]interface{}); ok {
			for _, raw := range parts {
				part, _ := raw.(map[string]interface{})
				if text, _ := part["text"].(string); text != "" {
					textParts = append(textParts, text)
				}
			}
		}
	}

	usage := make(map[string]interface{})
	if u, ok := resp["usageMetadata"].(map[string]interface{}); ok {
		if v, ok := u["promptTokenCount"]; ok {
			usage["prompt_tokens"] = v
		}
		if v, ok := u["candidatesTokenCount"]; ok {
			usage["completion_tokens"] = v
		}
		if v, ok := u["totalTokenCount"]; ok {
			usage["total_tokens"] = v
		}
	}
	stopReason := "end_turn"

	result := map[string]interface{}{
		"id":     "gemini-response",
		"object": "chat.completion",
		"choices": []interface{}{
			map[string]interface{}{
				"index": 0,
				"message": map[string]interface{}{
					"role":        "assistant",
					"content":     strings.Join(textParts, ""),
					"stop_reason": stopReason,
				},
				"finish_reason": "stop",
				"stop_reason":   stopReason,
			},
		},
		"usage": usage,
	}
	return json.Marshal(result)
}
