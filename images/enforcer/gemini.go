package main

import (
	"encoding/json"
	"fmt"
	"strings"
)

// translateToGemini converts an OpenAI chat/completions-style request into the
// native Gemini generateContent REST shape. It preserves Gemini-native provider
// tools and maps common provider tool aliases to Gemini tool keys.
func translateToGemini(openaiBody []byte) ([]byte, error) {
	var req map[string]interface{}
	if err := json.Unmarshal(openaiBody, &req); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}

	out := make(map[string]interface{})
	if contents := geminiContentsFromMessages(req["messages"]); len(contents) > 0 {
		out["contents"] = contents
	} else if input, ok := req["input"]; ok {
		out["contents"] = geminiContentsFromInput(input)
	} else {
		return nil, fmt.Errorf("messages or input field missing")
	}

	if system := geminiSystemInstruction(req["messages"]); system != nil {
		out["system_instruction"] = system
	}
	if tools := geminiTools(req["tools"]); len(tools) > 0 {
		out["tools"] = tools
	}

	genCfg := map[string]interface{}{}
	for openAIKey, geminiKey := range map[string]string{
		"temperature": "temperature",
		"top_p":       "topP",
		"max_tokens":  "maxOutputTokens",
	} {
		if v, ok := req[openAIKey]; ok {
			genCfg[geminiKey] = v
		}
	}
	if len(genCfg) > 0 {
		out["generationConfig"] = genCfg
	}

	return json.Marshal(out)
}

func geminiSystemInstruction(rawMessages interface{}) map[string]interface{} {
	messages, ok := rawMessages.([]interface{})
	if !ok {
		return nil
	}
	var parts []interface{}
	for _, raw := range messages {
		msg, ok := raw.(map[string]interface{})
		if !ok || msg["role"] != "system" {
			continue
		}
		text := geminiTextFromContent(msg["content"])
		if text != "" {
			parts = append(parts, map[string]interface{}{"text": text})
		}
	}
	if len(parts) == 0 {
		return nil
	}
	return map[string]interface{}{"parts": parts}
}

func geminiContentsFromMessages(rawMessages interface{}) []interface{} {
	messages, ok := rawMessages.([]interface{})
	if !ok {
		return nil
	}
	var contents []interface{}
	for _, raw := range messages {
		msg, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		role, _ := msg["role"].(string)
		switch role {
		case "system":
			continue
		case "assistant":
			role = "model"
		case "tool":
			role = "user"
		default:
			role = "user"
		}
		text := geminiTextFromContent(msg["content"])
		if text == "" {
			continue
		}
		contents = append(contents, map[string]interface{}{
			"role":  role,
			"parts": []interface{}{map[string]interface{}{"text": text}},
		})
	}
	return contents
}

func geminiContentsFromInput(input interface{}) []interface{} {
	switch v := input.(type) {
	case string:
		if v == "" {
			return nil
		}
		return []interface{}{
			map[string]interface{}{
				"role":  "user",
				"parts": []interface{}{map[string]interface{}{"text": v}},
			},
		}
	case []interface{}:
		var contents []interface{}
		for _, raw := range v {
			msg, ok := raw.(map[string]interface{})
			if !ok {
				text := geminiTextFromContent(raw)
				if text != "" {
					contents = append(contents, map[string]interface{}{
						"role":  "user",
						"parts": []interface{}{map[string]interface{}{"text": text}},
					})
				}
				continue
			}
			role, _ := msg["role"].(string)
			if role == "assistant" {
				role = "model"
			} else {
				role = "user"
			}
			text := geminiTextFromContent(msg["content"])
			if text == "" {
				continue
			}
			contents = append(contents, map[string]interface{}{
				"role":  role,
				"parts": []interface{}{map[string]interface{}{"text": text}},
			})
		}
		return contents
	default:
		text := geminiTextFromContent(input)
		if text == "" {
			return nil
		}
		return []interface{}{
			map[string]interface{}{
				"role":  "user",
				"parts": []interface{}{map[string]interface{}{"text": text}},
			},
		}
	}
}

func geminiTextFromContent(content interface{}) string {
	switch c := content.(type) {
	case string:
		return c
	case []interface{}:
		var parts []string
		for _, raw := range c {
			block, ok := raw.(map[string]interface{})
			if !ok {
				continue
			}
			if text, _ := block["text"].(string); text != "" {
				parts = append(parts, text)
				continue
			}
			if text, _ := block["input_text"].(string); text != "" {
				parts = append(parts, text)
				continue
			}
			if text, _ := block["output_text"].(string); text != "" {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, "\n")
	default:
		if content == nil {
			return ""
		}
		return fmt.Sprintf("%v", content)
	}
}

func geminiTools(rawTools interface{}) []interface{} {
	tools, ok := rawTools.([]interface{})
	if !ok || len(tools) == 0 {
		return nil
	}

	var out []interface{}
	var functionDecls []interface{}
	for _, raw := range tools {
		tool, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		if fn, _ := tool["function"].(map[string]interface{}); fn != nil {
			functionDecls = append(functionDecls, map[string]interface{}{
				"name":        fn["name"],
				"description": fn["description"],
				"parameters":  fn["parameters"],
			})
			continue
		}
		if native := geminiProviderTool(tool); native != nil {
			out = append(out, native)
		}
	}
	if len(functionDecls) > 0 {
		out = append(out, map[string]interface{}{"function_declarations": functionDecls})
	}
	return out
}

func geminiProviderTool(tool map[string]interface{}) map[string]interface{} {
	for key, value := range tool {
		if geminiToolKey(key) != "" {
			return map[string]interface{}{geminiToolKey(key): value}
		}
	}
	typ, _ := tool["type"].(string)
	switch providerToolCapability(typ) {
	case capProviderWebSearch:
		return map[string]interface{}{"google_search": map[string]interface{}{}}
	case capProviderURLContext:
		return map[string]interface{}{"url_context": map[string]interface{}{}}
	case capProviderCodeExecution:
		return map[string]interface{}{"code_execution": map[string]interface{}{}}
	case capProviderFileSearch:
		return map[string]interface{}{"file_search": map[string]interface{}{}}
	case capProviderGoogleMaps:
		return map[string]interface{}{"google_maps": map[string]interface{}{}}
	case capProviderComputerUse:
		return map[string]interface{}{"computer_use": map[string]interface{}{}}
	default:
		return nil
	}
}

func geminiToolKey(key string) string {
	normalized := strings.ToLower(strings.ReplaceAll(key, "-", "_"))
	switch normalized {
	case "google_search", "url_context", "code_execution", "file_search", "google_maps", "computer_use":
		return normalized
	default:
		return ""
	}
}

func translateFromGemini(geminiBody []byte) ([]byte, error) {
	var resp map[string]interface{}
	if err := json.Unmarshal(geminiBody, &resp); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}
	if errVal, ok := resp["error"]; ok {
		return json.Marshal(map[string]interface{}{"error": errVal})
	}

	content := geminiResponseText(resp)
	usage := map[string]interface{}{}
	if usageMeta, ok := resp["usageMetadata"].(map[string]interface{}); ok {
		usage = geminiOpenAIUsage(usageMeta)
	}
	if usage == nil {
		usage = map[string]interface{}{}
	}
	// The body runtime consumes a normalized response contract. Preserve Gemini's
	// native completion as additive stop_reason metadata at the adapter boundary.
	stopReason := "end_turn"

	result := map[string]interface{}{
		"id":     "gemini-response",
		"object": "chat.completion",
		"choices": []interface{}{
			map[string]interface{}{
				"index": 0,
				"message": map[string]interface{}{
					"role":        "assistant",
					"content":     content,
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

func translateFromGeminiResponse(geminiBody []byte) ([]byte, error) {
	var resp map[string]interface{}
	if err := json.Unmarshal(geminiBody, &resp); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}
	if errVal, ok := resp["error"]; ok {
		return json.Marshal(map[string]interface{}{"error": errVal})
	}

	content := geminiResponseText(resp)
	usage := map[string]interface{}{}
	if usageMeta, ok := resp["usageMetadata"].(map[string]interface{}); ok {
		usage = geminiOpenAIUsage(usageMeta)
	}
	if usage == nil {
		usage = map[string]interface{}{}
	}

	result := map[string]interface{}{
		"id":     "gemini-response",
		"object": "response",
		"output": []interface{}{
			map[string]interface{}{
				"type": "message",
				"role": "assistant",
				"content": []interface{}{
					map[string]interface{}{
						"type": "output_text",
						"text": content,
					},
				},
			},
		},
		"usage": usage,
	}
	return json.Marshal(result)
}

func geminiResponseText(resp map[string]interface{}) string {
	candidates, _ := resp["candidates"].([]interface{})
	if len(candidates) == 0 {
		return ""
	}
	cand, _ := candidates[0].(map[string]interface{})
	content, _ := cand["content"].(map[string]interface{})
	parts, _ := content["parts"].([]interface{})
	var text []string
	for _, raw := range parts {
		part, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		if t, _ := part["text"].(string); t != "" {
			text = append(text, t)
		}
	}
	return strings.Join(text, "")
}

func geminiUsageCounts(resp map[string]interface{}) (inputTokens, outputTokens int) {
	usageMeta, ok := resp["usageMetadata"].(map[string]interface{})
	if !ok {
		return 0, 0
	}
	if v, ok := usageMeta["promptTokenCount"].(float64); ok {
		inputTokens = int(v)
	}
	if v, ok := usageMeta["candidatesTokenCount"].(float64); ok {
		outputTokens = int(v)
	}
	return inputTokens, outputTokens
}

func geminiOpenAIStreamChunk(content string, finishReason string, usage map[string]interface{}) []byte {
	chunk := map[string]interface{}{
		"id":     "gemini-response",
		"object": "chat.completion.chunk",
		"choices": []interface{}{
			map[string]interface{}{
				"index": 0,
				"delta": map[string]interface{}{},
			},
		},
	}
	choice := chunk["choices"].([]interface{})[0].(map[string]interface{})
	if content != "" {
		choice["delta"] = map[string]interface{}{"content": content}
	}
	if finishReason != "" {
		choice["finish_reason"] = finishReason
	}
	if usage != nil {
		chunk["usage"] = usage
	}
	data, _ := json.Marshal(chunk)
	return data
}

func geminiOpenAIResponseTextDelta(content string) []byte {
	event := map[string]interface{}{
		"type":  "response.output_text.delta",
		"delta": content,
	}
	data, _ := json.Marshal(event)
	return data
}

func geminiOpenAIResponseCompleted(text string, usage map[string]interface{}) []byte {
	event := map[string]interface{}{
		"type": "response.completed",
		"response": map[string]interface{}{
			"id":     "gemini-response",
			"object": "response",
			"output": []interface{}{
				map[string]interface{}{
					"type": "message",
					"role": "assistant",
					"content": []interface{}{
						map[string]interface{}{
							"type": "output_text",
							"text": text,
						},
					},
				},
			},
			"usage": usage,
		},
	}
	data, _ := json.Marshal(event)
	return data
}

func geminiOpenAIUsage(usageMeta map[string]interface{}) map[string]interface{} {
	if usageMeta == nil {
		return nil
	}
	usage := map[string]interface{}{}
	if v, ok := usageMeta["promptTokenCount"]; ok {
		usage["prompt_tokens"] = v
	}
	if v, ok := usageMeta["candidatesTokenCount"]; ok {
		usage["completion_tokens"] = v
	}
	if v, ok := usageMeta["totalTokenCount"]; ok {
		usage["total_tokens"] = v
	}
	if len(usage) == 0 {
		return nil
	}
	return usage
}
