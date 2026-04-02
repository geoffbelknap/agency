package api

import (
	"encoding/json"
	"testing"
)

func TestValidInfraCallers(t *testing.T) {
	allowed := []string{"knowledge-synthesizer", "knowledge-curator"}
	for _, c := range allowed {
		if !validInfraCallers[c] {
			t.Errorf("expected %q to be a valid infra caller", c)
		}
	}

	rejected := []string{"agent-body", "unknown", "", "admin"}
	for _, c := range rejected {
		if validInfraCallers[c] {
			t.Errorf("expected %q to NOT be a valid infra caller", c)
		}
	}
}

func TestInfraTranslateToAnthropic(t *testing.T) {
	input := `{
		"model": "claude-3-haiku-20240307",
		"messages": [
			{"role": "system", "content": "You are a knowledge extractor."},
			{"role": "user", "content": "Extract entities from this text."}
		],
		"max_tokens": 4096
	}`

	result, err := infraTranslateToAnthropic([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(result, &parsed); err != nil {
		t.Fatalf("invalid JSON output: %v", err)
	}

	// System should be extracted to top-level
	system, ok := parsed["system"].([]interface{})
	if !ok || len(system) != 1 {
		t.Error("expected system to be extracted as array with 1 element")
	}

	// Messages should only contain user message
	messages, ok := parsed["messages"].([]interface{})
	if !ok || len(messages) != 1 {
		t.Error("expected messages to contain only user message")
	}

	// max_tokens should be preserved
	if mt, ok := parsed["max_tokens"].(float64); !ok || mt != 4096 {
		t.Errorf("expected max_tokens=4096, got %v", parsed["max_tokens"])
	}
}

func TestInfraTranslateFromAnthropic(t *testing.T) {
	input := `{
		"id": "msg_123",
		"type": "message",
		"role": "assistant",
		"model": "claude-3-haiku-20240307",
		"content": [
			{"type": "text", "text": "Here are the entities."}
		],
		"stop_reason": "end_turn",
		"usage": {
			"input_tokens": 100,
			"output_tokens": 50
		}
	}`

	result, err := infraTranslateFromAnthropic([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(result, &parsed); err != nil {
		t.Fatalf("invalid JSON output: %v", err)
	}

	if parsed["object"] != "chat.completion" {
		t.Errorf("expected object=chat.completion, got %v", parsed["object"])
	}

	choices, ok := parsed["choices"].([]interface{})
	if !ok || len(choices) != 1 {
		t.Fatal("expected 1 choice")
	}

	choice := choices[0].(map[string]interface{})
	msg := choice["message"].(map[string]interface{})
	if msg["content"] != "Here are the entities." {
		t.Errorf("unexpected content: %v", msg["content"])
	}
	if choice["finish_reason"] != "stop" {
		t.Errorf("expected finish_reason=stop, got %v", choice["finish_reason"])
	}

	usage := parsed["usage"].(map[string]interface{})
	if usage["prompt_tokens"].(float64) != 100 {
		t.Errorf("expected prompt_tokens=100, got %v", usage["prompt_tokens"])
	}
	if usage["completion_tokens"].(float64) != 50 {
		t.Errorf("expected completion_tokens=50, got %v", usage["completion_tokens"])
	}
}

func TestExtractUsageFromResponse(t *testing.T) {
	body := `{"usage": {"prompt_tokens": 150, "completion_tokens": 75}}`
	in, out := extractUsageFromResponse([]byte(body))
	if in != 150 {
		t.Errorf("expected input_tokens=150, got %d", in)
	}
	if out != 75 {
		t.Errorf("expected output_tokens=75, got %d", out)
	}
}

func TestExtractUsageFromResponse_Empty(t *testing.T) {
	in, out := extractUsageFromResponse([]byte(`{}`))
	if in != 0 || out != 0 {
		t.Errorf("expected 0,0 for empty response, got %d,%d", in, out)
	}
}
