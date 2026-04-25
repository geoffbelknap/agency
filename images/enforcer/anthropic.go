package main

import (
	"encoding/json"
	"fmt"
	"strings"
)

const defaultMaxTokens = 8192

// translateToAnthropic converts an OpenAI chat/completions request body to
// Anthropic Messages API format. System messages are extracted to the top-level
// "system" array, cache_control is added for prompt caching (when enabled), and
// tools are converted from OpenAI function format to Anthropic input_schema format.
func translateToAnthropic(openaiBody []byte, cachingEnabled bool) ([]byte, error) {
	var req map[string]interface{}
	if err := json.Unmarshal(openaiBody, &req); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}

	result := make(map[string]interface{})

	// Preserve model, stream, temperature, top_p
	for _, key := range []string{"model", "stream", "temperature", "top_p"} {
		if v, ok := req[key]; ok {
			result[key] = v
		}
	}

	// Set max_tokens (Anthropic requires it)
	if v, ok := req["max_tokens"]; ok {
		result["max_tokens"] = v
	} else {
		result["max_tokens"] = float64(defaultMaxTokens)
	}

	// Process messages: extract system messages, translate the rest
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
			block := map[string]interface{}{
				"type": "text",
				"text": content,
			}
			systemBlocks = append(systemBlocks, block)
		} else {
			anthropicMessages = append(anthropicMessages, translateMessage(msg))
		}
	}

	if len(systemBlocks) > 0 {
		if cachingEnabled {
			// Add cache_control to the last system block for prompt caching
			last := systemBlocks[len(systemBlocks)-1].(map[string]interface{})
			last["cache_control"] = map[string]interface{}{"type": "ephemeral"}
		}
		result["system"] = systemBlocks
	}

	if cachingEnabled {
		// Cache conversation history: mark the second-to-last user message so the
		// prefix is cached and each turn only pays for the delta.
		addHistoryCacheControl(anthropicMessages)
	}

	result["messages"] = anthropicMessages

	// Translate tools if present
	if tools, ok := req["tools"].([]interface{}); ok && len(tools) > 0 {
		result["tools"] = translateTools(tools, cachingEnabled)
	}

	return json.Marshal(result)
}

// translateMessage converts a single OpenAI message to Anthropic format.
func translateMessage(msg map[string]interface{}) map[string]interface{} {
	role, _ := msg["role"].(string)

	switch role {
	case "assistant":
		return translateAssistantMessage(msg)
	case "tool":
		return translateToolMessage(msg)
	default:
		// user and other roles pass through
		out := make(map[string]interface{})
		for k, v := range msg {
			out[k] = v
		}
		return out
	}
}

// translateAssistantMessage converts an assistant message. If it has tool_calls,
// they are converted to Anthropic tool_use content blocks.
func translateAssistantMessage(msg map[string]interface{}) map[string]interface{} {
	toolCalls, hasToolCalls := msg["tool_calls"].([]interface{})
	if !hasToolCalls {
		// Plain assistant message, pass through
		out := make(map[string]interface{})
		for k, v := range msg {
			out[k] = v
		}
		return out
	}

	// Convert tool_calls to content array with tool_use blocks
	var content []interface{}
	for _, rawCall := range toolCalls {
		call, ok := rawCall.(map[string]interface{})
		if !ok {
			continue
		}
		fn, _ := call["function"].(map[string]interface{})
		name, _ := fn["name"].(string)
		argsStr, _ := fn["arguments"].(string)
		id, _ := call["id"].(string)

		var input interface{}
		if err := json.Unmarshal([]byte(argsStr), &input); err != nil {
			input = map[string]interface{}{}
		}

		block := map[string]interface{}{
			"type":  "tool_use",
			"id":    id,
			"name":  name,
			"input": input,
		}
		content = append(content, block)
	}

	return map[string]interface{}{
		"role":    "assistant",
		"content": content,
	}
}

// translateToolMessage converts an OpenAI tool result message to Anthropic
// format: role becomes "user" with a tool_result content block.
func translateToolMessage(msg map[string]interface{}) map[string]interface{} {
	toolCallID, _ := msg["tool_call_id"].(string)
	content, _ := msg["content"].(string)

	return map[string]interface{}{
		"role": "user",
		"content": []interface{}{
			map[string]interface{}{
				"type":        "tool_result",
				"tool_use_id": toolCallID,
				"content":     content,
			},
		},
	}
}

// joinStrings joins non-empty string parts with spaces.
func joinStrings(parts []string) string {
	return strings.Join(parts, " ")
}

// translateFromAnthropic converts an Anthropic Messages API response into the
// normalized runtime contract: chat/completions-like control-flow fields plus
// additive stop_reason metadata for audit and artifacts.
func translateFromAnthropic(anthropicBody []byte) ([]byte, error) {
	var resp map[string]interface{}
	if err := json.Unmarshal(anthropicBody, &resp); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}

	// Pass through error responses
	if errVal, ok := resp["error"]; ok {
		return json.Marshal(map[string]interface{}{"error": errVal})
	}

	// Extract content blocks
	contentBlocks, _ := resp["content"].([]interface{})
	var textParts []string
	var toolCalls []interface{}

	for _, rawBlock := range contentBlocks {
		block, ok := rawBlock.(map[string]interface{})
		if !ok {
			continue
		}
		blockType, _ := block["type"].(string)
		switch blockType {
		case "text":
			text, _ := block["text"].(string)
			textParts = append(textParts, text)
		case "tool_use":
			input, _ := block["input"]
			argsBytes, err := json.Marshal(input)
			if err != nil {
				argsBytes = []byte("{}")
			}
			tc := map[string]interface{}{
				"id":   block["id"],
				"type": "function",
				"function": map[string]interface{}{
					"name":      block["name"],
					"arguments": string(argsBytes),
				},
			}
			toolCalls = append(toolCalls, tc)
		}
	}

	// Build message
	message := map[string]interface{}{
		"role":    "assistant",
		"content": joinStrings(textParts),
	}
	if len(toolCalls) > 0 {
		message["tool_calls"] = toolCalls
	}

	// Normalize Anthropic termination metadata at the adapter boundary. The
	// runtime consumes finish_reason generically and retains stop_reason as
	// additive provider detail for audit.
	stopReason, _ := resp["stop_reason"].(string)
	if stopReason != "" {
		message["stop_reason"] = stopReason
	}
	var finishReason string
	switch stopReason {
	case "tool_use":
		finishReason = "tool_calls"
	case "end_turn", "stop_sequence":
		finishReason = "stop"
	case "max_tokens":
		finishReason = "length"
	default:
		finishReason = "stop"
	}

	// Build usage
	usage := make(map[string]interface{})
	if u, ok := resp["usage"].(map[string]interface{}); ok {
		if v, ok := u["input_tokens"]; ok {
			usage["prompt_tokens"] = v
		}
		if v, ok := u["output_tokens"]; ok {
			usage["completion_tokens"] = v
		}
		if v, ok := u["cache_creation_input_tokens"]; ok {
			usage["cache_creation_input_tokens"] = v
		}
		if v, ok := u["cache_read_input_tokens"]; ok {
			usage["cache_read_input_tokens"] = v
		}
	}

	result := map[string]interface{}{
		"id":     resp["id"],
		"object": "chat.completion",
		"model":  resp["model"],
		"choices": []interface{}{
			map[string]interface{}{
				"index":         0,
				"message":       message,
				"finish_reason": finishReason,
				"stop_reason":   stopReason,
			},
		},
		"usage": usage,
	}

	return json.Marshal(result)
}

// streamTranslator holds state for converting Anthropic SSE events to OpenAI
// chat.completion.chunk format across a single streaming response.
type streamTranslator struct {
	msgID         string
	model         string
	inputTokens   int
	outputTokens  int
	cacheCreation int
	cacheRead     int
	blocks        map[int]*contentBlock
}

type contentBlock struct {
	blockType string // "text" or "tool_use"
	toolID    string
	toolName  string
	jsonAcc   string // accumulated JSON for tool_use
}

func newStreamTranslator() *streamTranslator {
	return &streamTranslator{
		blocks: make(map[int]*contentBlock),
	}
}

// translateEvent takes one Anthropic SSE data payload (JSON) and returns zero
// or more OpenAI-format chunk JSON strings.
func (st *streamTranslator) translateEvent(data string) []string {
	var event map[string]interface{}
	if err := json.Unmarshal([]byte(data), &event); err != nil {
		return nil
	}

	eventType, _ := event["type"].(string)

	switch eventType {
	case "message_start":
		msg, _ := event["message"].(map[string]interface{})
		if msg == nil {
			return nil
		}
		st.msgID, _ = msg["id"].(string)
		st.model, _ = msg["model"].(string)
		if usage, ok := msg["usage"].(map[string]interface{}); ok {
			st.inputTokens = intFromJSON(usage["input_tokens"])
			st.cacheCreation = intFromJSON(usage["cache_creation_input_tokens"])
			st.cacheRead = intFromJSON(usage["cache_read_input_tokens"])
		}
		return nil

	case "content_block_start":
		index := intFromJSON(event["index"])
		cb, _ := event["content_block"].(map[string]interface{})
		if cb == nil {
			return nil
		}
		blockType, _ := cb["type"].(string)
		block := &contentBlock{blockType: blockType}
		if blockType == "tool_use" {
			block.toolID, _ = cb["id"].(string)
			block.toolName, _ = cb["name"].(string)
		}
		st.blocks[index] = block

		if blockType == "tool_use" {
			// Emit initial tool call chunk with id and name
			delta := map[string]interface{}{
				"tool_calls": []interface{}{
					map[string]interface{}{
						"index": index,
						"id":    block.toolID,
						"type":  "function",
						"function": map[string]interface{}{
							"name":      block.toolName,
							"arguments": "",
						},
					},
				},
			}
			return []string{st.makeChunk(delta, "")}
		}
		return nil

	case "content_block_delta":
		index := intFromJSON(event["index"])
		delta, _ := event["delta"].(map[string]interface{})
		if delta == nil {
			return nil
		}
		deltaType, _ := delta["type"].(string)

		switch deltaType {
		case "text_delta":
			text, _ := delta["text"].(string)
			return []string{st.makeChunk(map[string]interface{}{
				"content": text,
			}, "")}
		case "input_json_delta":
			partial, _ := delta["partial_json"].(string)
			if block, ok := st.blocks[index]; ok {
				block.jsonAcc += partial
			}
			return []string{st.makeChunk(map[string]interface{}{
				"tool_calls": []interface{}{
					map[string]interface{}{
						"index": index,
						"function": map[string]interface{}{
							"arguments": partial,
						},
					},
				},
			}, "")}
		}
		return nil

	case "content_block_stop":
		return nil

	case "message_delta":
		delta, _ := event["delta"].(map[string]interface{})
		if usage, ok := event["usage"].(map[string]interface{}); ok {
			st.outputTokens = intFromJSON(usage["output_tokens"])
		}
		stopReason := ""
		if delta != nil {
			stopReason, _ = delta["stop_reason"].(string)
		}
		var finishReason string
		switch stopReason {
		case "tool_use":
			finishReason = "tool_calls"
		case "max_tokens":
			finishReason = "length"
		default:
			finishReason = "stop"
		}

		chunk := map[string]interface{}{
			"id":     st.msgID,
			"object": "chat.completion.chunk",
			"model":  st.model,
			"choices": []interface{}{
				map[string]interface{}{
					"index":         0,
					"delta":         map[string]interface{}{},
					"finish_reason": finishReason,
					"stop_reason":   stopReason,
				},
			},
			"usage": map[string]interface{}{
				"prompt_tokens":               st.inputTokens,
				"completion_tokens":           st.outputTokens,
				"cache_creation_input_tokens": st.cacheCreation,
				"cache_read_input_tokens":     st.cacheRead,
			},
		}
		out, _ := json.Marshal(chunk)
		return []string{string(out)}

	case "message_stop":
		return nil
	}

	return nil
}

// makeChunk builds a single OpenAI chat.completion.chunk JSON string.
func (st *streamTranslator) makeChunk(delta map[string]interface{}, finishReason string) string {
	var fr interface{}
	if finishReason != "" {
		fr = finishReason
	}
	chunk := map[string]interface{}{
		"id":     st.msgID,
		"object": "chat.completion.chunk",
		"model":  st.model,
		"choices": []interface{}{
			map[string]interface{}{
				"index":         0,
				"delta":         delta,
				"finish_reason": fr,
			},
		},
	}
	out, _ := json.Marshal(chunk)
	return string(out)
}

// intFromJSON converts a JSON number (float64) to int.
func intFromJSON(v interface{}) int {
	if f, ok := v.(float64); ok {
		return int(f)
	}
	return 0
}

const defaultAnthropicWebSearchToolType = "web_search_20250305"

// translateTools converts OpenAI function tool definitions to Anthropic format,
// normalizes generic provider-side tool aliases to Anthropic server tool
// definitions, and preserves Anthropic-native server tools such as
// web_search_*. When caching is enabled, the last translated or preserved tool
// gets cache_control.
func translateTools(tools []interface{}, cachingEnabled bool) []interface{} {
	var result []interface{}

	for _, rawTool := range tools {
		tool, ok := rawTool.(map[string]interface{})
		if !ok {
			continue
		}
		fn, _ := tool["function"].(map[string]interface{})
		if fn != nil {
			anthropicTool := map[string]interface{}{
				"name":         fn["name"],
				"description":  fn["description"],
				"input_schema": fn["parameters"],
			}
			result = append(result, anthropicTool)
			continue
		}

		if normalized := normalizeAnthropicServerTool(tool); normalized != nil {
			result = append(result, normalized)
			continue
		}

		toolType, _ := tool["type"].(string)
		if providerToolCapability(toolType) == "" {
			continue
		}
		preserved := make(map[string]interface{}, len(tool))
		for k, v := range tool {
			preserved[k] = v
		}
		result = append(result, preserved)
	}

	// Add cache_control to the last tool when caching is enabled
	if cachingEnabled && len(result) > 0 {
		last := result[len(result)-1].(map[string]interface{})
		last["cache_control"] = map[string]interface{}{"type": "ephemeral"}
	}

	return result
}

func normalizeAnthropicServerTool(tool map[string]interface{}) map[string]interface{} {
	toolType, _ := tool["type"].(string)
	switch strings.ToLower(strings.TrimSpace(toolType)) {
	case "web_search", "web_search_preview":
		normalized := copyStringInterfaceMap(tool)
		normalized["type"] = defaultAnthropicWebSearchToolType
		if _, ok := normalized["name"].(string); !ok {
			normalized["name"] = "web_search"
		}
		return normalized
	default:
		return nil
	}
}

func copyStringInterfaceMap(in map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

// addHistoryCacheControl marks the second-to-last user message with
// cache_control: ephemeral. This caches the conversation prefix so each new
// turn only pays for the delta rather than re-processing the full history.
// Only user-role messages accept cache_control per the Anthropic API.
func addHistoryCacheControl(messages []interface{}) {
	var userIndices []int
	for i, rawMsg := range messages {
		msg, ok := rawMsg.(map[string]interface{})
		if !ok {
			continue
		}
		if msg["role"] == "user" {
			userIndices = append(userIndices, i)
		}
	}
	if len(userIndices) < 2 {
		return
	}
	target := messages[userIndices[len(userIndices)-2]].(map[string]interface{})
	addCacheControlToLastBlock(target)
}

// addCacheControlToLastBlock injects cache_control: ephemeral into the last
// content block of a message. Handles both string and array content forms.
func addCacheControlToLastBlock(msg map[string]interface{}) {
	cc := map[string]interface{}{"type": "ephemeral"}
	switch c := msg["content"].(type) {
	case string:
		msg["content"] = []interface{}{
			map[string]interface{}{"type": "text", "text": c, "cache_control": cc},
		}
	case []interface{}:
		if len(c) == 0 {
			return
		}
		if last, ok := c[len(c)-1].(map[string]interface{}); ok {
			last["cache_control"] = cc
		}
	}
}
