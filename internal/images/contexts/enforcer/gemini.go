package main

import (
	"encoding/json"
	"fmt"
	"strings"
)

func translateToGemini(openaiBody []byte) ([]byte, error) {
	var req map[string]interface{}
	if err := json.Unmarshal(openaiBody, &req); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}

	out := map[string]interface{}{}
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
