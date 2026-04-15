package main

import (
	"encoding/json"
	"testing"
)

func TestTranslateRequestBasic(t *testing.T) {
	openai := map[string]interface{}{
		"model": "claude-sonnet-4-20250514",
		"messages": []interface{}{
			map[string]interface{}{"role": "system", "content": "You are helpful."},
			map[string]interface{}{"role": "user", "content": "Hello"},
		},
		"stream": true,
	}
	body, _ := json.Marshal(openai)
	result, err := translateToAnthropic(body, true)
	if err != nil {
		t.Fatalf("translation failed: %v", err)
	}
	var req map[string]interface{}
	json.Unmarshal(result, &req)
	if req["model"] != "claude-sonnet-4-20250514" {
		t.Errorf("model not preserved: %v", req["model"])
	}
	system, ok := req["system"].([]interface{})
	if !ok {
		t.Fatalf("system should be array, got: %T", req["system"])
	}
	block := system[0].(map[string]interface{})
	if block["text"] != "You are helpful." {
		t.Errorf("system text wrong: %v", block["text"])
	}
	cc, ok := block["cache_control"].(map[string]interface{})
	if !ok || cc["type"] != "ephemeral" {
		t.Error("system block should have cache_control ephemeral")
	}
	msgs := req["messages"].([]interface{})
	if len(msgs) != 1 {
		t.Errorf("expected 1 message, got %d", len(msgs))
	}
	if req["stream"] != true {
		t.Error("stream should be preserved")
	}
	if req["max_tokens"] == nil {
		t.Error("max_tokens should be set")
	}
}

func TestTranslateRequestWithTools(t *testing.T) {
	openai := map[string]interface{}{
		"model": "claude-sonnet-4-20250514",
		"messages": []interface{}{
			map[string]interface{}{"role": "user", "content": "read file"},
		},
		"tools": []interface{}{
			map[string]interface{}{
				"type": "function",
				"function": map[string]interface{}{
					"name":        "read_file",
					"description": "Read a file",
					"parameters": map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"path": map[string]interface{}{"type": "string"},
						},
					},
				},
			},
		},
	}
	body, _ := json.Marshal(openai)
	result, err := translateToAnthropic(body, true)
	if err != nil {
		t.Fatalf("translation failed: %v", err)
	}
	var req map[string]interface{}
	json.Unmarshal(result, &req)
	tools := req["tools"].([]interface{})
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(tools))
	}
	tool := tools[0].(map[string]interface{})
	if tool["name"] != "read_file" {
		t.Errorf("tool name wrong: %v", tool["name"])
	}
	if tool["input_schema"] == nil {
		t.Error("input_schema should be set")
	}
	cc, ok := tool["cache_control"].(map[string]interface{})
	if !ok || cc["type"] != "ephemeral" {
		t.Error("last tool should have cache_control ephemeral")
	}
}

func TestTranslateRequestPreservesAnthropicServerTools(t *testing.T) {
	openai := map[string]interface{}{
		"model": "claude-sonnet-4-20250514",
		"messages": []interface{}{
			map[string]interface{}{"role": "user", "content": "search"},
		},
		"tools": []interface{}{
			map[string]interface{}{
				"type":     "web_search_20260209",
				"name":     "web_search",
				"max_uses": float64(3),
			},
		},
	}
	body, _ := json.Marshal(openai)
	result, err := translateToAnthropic(body, true)
	if err != nil {
		t.Fatalf("translation failed: %v", err)
	}
	var req map[string]interface{}
	json.Unmarshal(result, &req)
	tools := req["tools"].([]interface{})
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(tools))
	}
	tool := tools[0].(map[string]interface{})
	if tool["type"] != "web_search_20260209" {
		t.Fatalf("server tool type not preserved: %v", tool["type"])
	}
	if tool["name"] != "web_search" {
		t.Fatalf("server tool name not preserved: %v", tool["name"])
	}
	if tool["input_schema"] != nil {
		t.Fatalf("server tool should not be converted to input_schema: %#v", tool)
	}
	if cc, ok := tool["cache_control"].(map[string]interface{}); !ok || cc["type"] != "ephemeral" {
		t.Fatal("preserved server tool should receive cache_control when caching enabled")
	}
}

func TestTranslateRequestNormalizesGenericWebSearchTool(t *testing.T) {
	openai := map[string]interface{}{
		"model": "claude-sonnet-4-20250514",
		"messages": []interface{}{
			map[string]interface{}{"role": "user", "content": "search"},
		},
		"tools": []interface{}{
			map[string]interface{}{
				"type":     "web_search",
				"max_uses": float64(2),
			},
		},
	}
	body, _ := json.Marshal(openai)
	result, err := translateToAnthropic(body, false)
	if err != nil {
		t.Fatalf("translation failed: %v", err)
	}
	var req map[string]interface{}
	json.Unmarshal(result, &req)
	tools := req["tools"].([]interface{})
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(tools))
	}
	tool := tools[0].(map[string]interface{})
	if tool["type"] != defaultAnthropicWebSearchToolType {
		t.Fatalf("generic web_search should map to %s, got %v", defaultAnthropicWebSearchToolType, tool["type"])
	}
	if tool["name"] != "web_search" {
		t.Fatalf("generic web_search should set name, got %v", tool["name"])
	}
	if tool["max_uses"] != float64(2) {
		t.Fatalf("generic web_search should preserve options: %#v", tool)
	}
}

func TestTranslateRequestToolCalls(t *testing.T) {
	openai := map[string]interface{}{
		"model": "claude-sonnet-4-20250514",
		"messages": []interface{}{
			map[string]interface{}{"role": "user", "content": "Hello"},
			map[string]interface{}{
				"role":    "assistant",
				"content": "",
				"tool_calls": []interface{}{
					map[string]interface{}{
						"id":   "call_1",
						"type": "function",
						"function": map[string]interface{}{
							"name":      "read_file",
							"arguments": `{"path":"/tmp/test"}`,
						},
					},
				},
			},
			map[string]interface{}{
				"role":         "tool",
				"tool_call_id": "call_1",
				"content":      "file contents here",
			},
		},
	}
	body, _ := json.Marshal(openai)
	result, err := translateToAnthropic(body, true)
	if err != nil {
		t.Fatalf("translation failed: %v", err)
	}
	var req map[string]interface{}
	json.Unmarshal(result, &req)
	msgs := req["messages"].([]interface{})
	if len(msgs) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(msgs))
	}
	assistant := msgs[1].(map[string]interface{})
	content := assistant["content"].([]interface{})
	toolUse := content[0].(map[string]interface{})
	if toolUse["type"] != "tool_use" {
		t.Errorf("expected tool_use, got: %v", toolUse["type"])
	}
	if toolUse["name"] != "read_file" {
		t.Errorf("tool name wrong: %v", toolUse["name"])
	}
	toolResult := msgs[2].(map[string]interface{})
	if toolResult["role"] != "user" {
		t.Errorf("tool result should have role user, got: %v", toolResult["role"])
	}
	resultContent := toolResult["content"].([]interface{})
	tr := resultContent[0].(map[string]interface{})
	if tr["type"] != "tool_result" {
		t.Errorf("expected tool_result, got: %v", tr["type"])
	}
}

func TestTranslateResponseBasic(t *testing.T) {
	anthropic := `{
		"id": "msg_123",
		"type": "message",
		"role": "assistant",
		"model": "claude-sonnet-4-20250514",
		"content": [{"type": "text", "text": "Hello there!"}],
		"stop_reason": "end_turn",
		"usage": {"input_tokens": 50, "output_tokens": 20, "cache_creation_input_tokens": 1000, "cache_read_input_tokens": 50}
	}`
	result, err := translateFromAnthropic([]byte(anthropic))
	if err != nil {
		t.Fatalf("translation failed: %v", err)
	}
	var resp map[string]interface{}
	json.Unmarshal(result, &resp)
	choices := resp["choices"].([]interface{})
	if len(choices) != 1 {
		t.Fatalf("expected 1 choice, got %d", len(choices))
	}
	choice := choices[0].(map[string]interface{})
	msg := choice["message"].(map[string]interface{})
	if msg["role"] != "assistant" {
		t.Errorf("wrong role: %v", msg["role"])
	}
	if msg["content"] != "Hello there!" {
		t.Errorf("wrong content: %v", msg["content"])
	}
	if choice["finish_reason"] != "stop" {
		t.Errorf("wrong finish_reason: %v", choice["finish_reason"])
	}
	usage := resp["usage"].(map[string]interface{})
	if usage["prompt_tokens"].(float64) != 50 {
		t.Errorf("wrong prompt_tokens: %v", usage["prompt_tokens"])
	}
	if usage["completion_tokens"].(float64) != 20 {
		t.Errorf("wrong completion_tokens: %v", usage["completion_tokens"])
	}
}

func TestTranslateResponseToolUse(t *testing.T) {
	anthropic := `{
		"id": "msg_123",
		"type": "message",
		"role": "assistant",
		"content": [
			{"type": "text", "text": "Let me read that."},
			{"type": "tool_use", "id": "tu_1", "name": "read_file", "input": {"path": "/tmp/test"}}
		],
		"stop_reason": "tool_use",
		"usage": {"input_tokens": 50, "output_tokens": 30}
	}`
	result, err := translateFromAnthropic([]byte(anthropic))
	if err != nil {
		t.Fatalf("translation failed: %v", err)
	}
	var resp map[string]interface{}
	json.Unmarshal(result, &resp)
	choice := resp["choices"].([]interface{})[0].(map[string]interface{})
	msg := choice["message"].(map[string]interface{})
	if msg["content"] != "Let me read that." {
		t.Errorf("wrong content: %v", msg["content"])
	}
	toolCalls := msg["tool_calls"].([]interface{})
	if len(toolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(toolCalls))
	}
	tc := toolCalls[0].(map[string]interface{})
	if tc["id"] != "tu_1" {
		t.Errorf("wrong tool call id: %v", tc["id"])
	}
	fn := tc["function"].(map[string]interface{})
	if fn["name"] != "read_file" {
		t.Errorf("wrong function name: %v", fn["name"])
	}
	if choice["finish_reason"] != "tool_calls" {
		t.Errorf("wrong finish_reason: %v", choice["finish_reason"])
	}
}

func TestTranslateStreamEvents(t *testing.T) {
	events := []string{
		`{"type":"message_start","message":{"id":"msg_1","model":"claude-sonnet-4-20250514","usage":{"input_tokens":100,"cache_creation_input_tokens":500,"cache_read_input_tokens":0}}}`,
		`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":" world"}}`,
		`{"type":"content_block_stop","index":0}`,
		`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":20}}`,
		`{"type":"message_stop"}`,
	}
	translator := newStreamTranslator()
	var openaiEvents []string
	for _, event := range events {
		translated := translator.translateEvent(event)
		openaiEvents = append(openaiEvents, translated...)
	}
	found := false
	for _, e := range openaiEvents {
		var chunk map[string]interface{}
		json.Unmarshal([]byte(e), &chunk)
		choices, ok := chunk["choices"].([]interface{})
		if !ok || len(choices) == 0 {
			continue
		}
		choice := choices[0].(map[string]interface{})
		delta, ok := choice["delta"].(map[string]interface{})
		if !ok {
			continue
		}
		if content, ok := delta["content"].(string); ok && content == "Hello" {
			found = true
		}
	}
	if !found {
		t.Error("expected to find Hello content delta in translated events")
	}
	lastEvent := openaiEvents[len(openaiEvents)-1]
	var last map[string]interface{}
	json.Unmarshal([]byte(lastEvent), &last)
	usage, ok := last["usage"].(map[string]interface{})
	if !ok {
		t.Fatal("last event should have usage")
	}
	if usage["prompt_tokens"].(float64) != 100 {
		t.Errorf("wrong prompt_tokens: %v", usage["prompt_tokens"])
	}
}

func TestHistoryCacheControl(t *testing.T) {
	// Two-turn conversation: second-to-last user message should get cache_control,
	// last user message should not.
	openai := map[string]interface{}{
		"model": "claude-sonnet-4-20250514",
		"messages": []interface{}{
			map[string]interface{}{"role": "system", "content": "You are helpful."},
			map[string]interface{}{"role": "user", "content": "First question"},
			map[string]interface{}{"role": "assistant", "content": "First answer"},
			map[string]interface{}{"role": "user", "content": "Second question"},
		},
	}
	body, _ := json.Marshal(openai)
	result, err := translateToAnthropic(body, true)
	if err != nil {
		t.Fatalf("translation failed: %v", err)
	}
	var req map[string]interface{}
	json.Unmarshal(result, &req)
	msgs := req["messages"].([]interface{})

	// msg[0] = user "First question" — second-to-last user, should have cache_control
	first := msgs[0].(map[string]interface{})
	firstContent := first["content"].([]interface{})
	firstBlock := firstContent[0].(map[string]interface{})
	cc, ok := firstBlock["cache_control"].(map[string]interface{})
	if !ok || cc["type"] != "ephemeral" {
		t.Error("second-to-last user message should have cache_control ephemeral")
	}

	// msg[2] = user "Second question" — last user, should NOT have cache_control
	last := msgs[2].(map[string]interface{})
	if s, ok := last["content"].(string); ok {
		// still a plain string — no cache_control injected
		if s != "Second question" {
			t.Errorf("last user content wrong: %v", s)
		}
	} else if arr, ok := last["content"].([]interface{}); ok {
		for _, blk := range arr {
			b := blk.(map[string]interface{})
			if _, hasCc := b["cache_control"]; hasCc {
				t.Error("last user message should NOT have cache_control")
			}
		}
	}
}

func TestHistoryCacheControlSingleUser(t *testing.T) {
	// Only one user message — no history cache should be added.
	openai := map[string]interface{}{
		"model": "claude-sonnet-4-20250514",
		"messages": []interface{}{
			map[string]interface{}{"role": "user", "content": "Hello"},
		},
	}
	body, _ := json.Marshal(openai)
	result, err := translateToAnthropic(body, true)
	if err != nil {
		t.Fatalf("translation failed: %v", err)
	}
	var req map[string]interface{}
	json.Unmarshal(result, &req)
	msgs := req["messages"].([]interface{})
	msg := msgs[0].(map[string]interface{})
	// Content should still be a plain string (not converted to array)
	if _, ok := msg["content"].(string); !ok {
		t.Error("single user message content should remain a string when no caching applied")
	}
}

func TestHistoryCacheControlToolResult(t *testing.T) {
	// Tool-call turn followed by a new user message: the tool_result (user role)
	// is second-to-last; cache_control should land on it.
	openai := map[string]interface{}{
		"model": "claude-sonnet-4-20250514",
		"messages": []interface{}{
			map[string]interface{}{"role": "user", "content": "Do something"},
			map[string]interface{}{
				"role":    "assistant",
				"content": "",
				"tool_calls": []interface{}{
					map[string]interface{}{
						"id": "call_1", "type": "function",
						"function": map[string]interface{}{
							"name": "read_file", "arguments": `{"path":"/tmp/x"}`,
						},
					},
				},
			},
			map[string]interface{}{"role": "tool", "tool_call_id": "call_1", "content": "file data"},
			map[string]interface{}{"role": "user", "content": "Now summarise"},
		},
	}
	body, _ := json.Marshal(openai)
	result, err := translateToAnthropic(body, true)
	if err != nil {
		t.Fatalf("translation failed: %v", err)
	}
	var req map[string]interface{}
	json.Unmarshal(result, &req)
	msgs := req["messages"].([]interface{})
	// After translation: [user, assistant, user(tool_result), user]
	// user indices: 0, 2, 3 → second-to-last user = index 2 (tool_result)
	toolResultMsg := msgs[2].(map[string]interface{})
	trContent := toolResultMsg["content"].([]interface{})
	trBlock := trContent[0].(map[string]interface{})
	cc, ok := trBlock["cache_control"].(map[string]interface{})
	if !ok || cc["type"] != "ephemeral" {
		t.Error("tool_result (second-to-last user) should have cache_control ephemeral")
	}
	// Last user message should NOT have cache_control
	lastMsg := msgs[3].(map[string]interface{})
	if _, ok := lastMsg["content"].(string); !ok {
		t.Error("last user message content should remain a plain string")
	}
}

func TestTranslateStreamToolUse(t *testing.T) {
	events := []string{
		`{"type":"message_start","message":{"id":"msg_1","model":"claude-sonnet-4-20250514","usage":{"input_tokens":50}}}`,
		`{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"tu_1","name":"read_file"}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"path\":"}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"\"/tmp\"}"}}`,
		`{"type":"content_block_stop","index":0}`,
		`{"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":30}}`,
		`{"type":"message_stop"}`,
	}
	translator := newStreamTranslator()
	var openaiEvents []string
	for _, event := range events {
		translated := translator.translateEvent(event)
		openaiEvents = append(openaiEvents, translated...)
	}
	foundTool := false
	for _, e := range openaiEvents {
		var chunk map[string]interface{}
		json.Unmarshal([]byte(e), &chunk)
		choices, ok := chunk["choices"].([]interface{})
		if !ok || len(choices) == 0 {
			continue
		}
		choice := choices[0].(map[string]interface{})
		delta, ok := choice["delta"].(map[string]interface{})
		if !ok {
			continue
		}
		if tcs, ok := delta["tool_calls"].([]interface{}); ok && len(tcs) > 0 {
			foundTool = true
		}
	}
	if !foundTool {
		t.Error("expected tool_calls in translated stream")
	}
}

func TestTranslateNoCacheWhenDisabled(t *testing.T) {
	openai := map[string]interface{}{
		"model": "claude-sonnet-4-6",
		"messages": []interface{}{
			map[string]interface{}{"role": "system", "content": "You are helpful."},
			map[string]interface{}{"role": "user", "content": "Hello"},
		},
		"tools": []interface{}{
			map[string]interface{}{
				"type": "function",
				"function": map[string]interface{}{
					"name":        "read_file",
					"description": "Read a file",
					"parameters":  map[string]interface{}{"type": "object"},
				},
			},
		},
	}
	body, _ := json.Marshal(openai)
	result, err := translateToAnthropic(body, false)
	if err != nil {
		t.Fatalf("translation failed: %v", err)
	}
	var req map[string]interface{}
	json.Unmarshal(result, &req)

	// System block should NOT have cache_control
	system := req["system"].([]interface{})
	block := system[0].(map[string]interface{})
	if _, ok := block["cache_control"]; ok {
		t.Error("system block should NOT have cache_control when caching disabled")
	}

	// Tools should NOT have cache_control
	tools := req["tools"].([]interface{})
	lastTool := tools[len(tools)-1].(map[string]interface{})
	if _, ok := lastTool["cache_control"]; ok {
		t.Error("last tool should NOT have cache_control when caching disabled")
	}
}
