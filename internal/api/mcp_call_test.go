package api

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"testing"
)

func TestMCPCallEndpoint(t *testing.T) {
	reg := NewMCPToolRegistry()
	reg.Register("echo", "Echo tool", nil, func(h *handler, args map[string]interface{}) (string, bool) {
		msg, _ := args["message"].(string)
		return "echo: " + msg, false
	})

	body, _ := json.Marshal(map[string]interface{}{
		"name":      "echo",
		"arguments": map[string]string{"message": "hello"},
	})
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/v1/mcp/call", bytes.NewReader(body))
	mcpCallHandler(reg, nil)(w, r)

	if w.Code != 200 {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var resp struct {
		Content []struct{ Type, Text string } `json:"content"`
		IsError bool                          `json:"isError"`
	}
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Content[0].Text != "echo: hello" {
		t.Errorf("text = %q, want %q", resp.Content[0].Text, "echo: hello")
	}
	if resp.IsError {
		t.Error("isError should be false")
	}
}

func TestMCPCallUnknownTool(t *testing.T) {
	reg := NewMCPToolRegistry()
	body, _ := json.Marshal(map[string]interface{}{"name": "nope", "arguments": map[string]interface{}{}})
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/v1/mcp/call", bytes.NewReader(body))
	mcpCallHandler(reg, nil)(w, r)

	var resp struct {
		IsError bool `json:"isError"`
	}
	json.NewDecoder(w.Body).Decode(&resp)
	if !resp.IsError {
		t.Error("expected isError=true")
	}
}

func TestMCPCallEnvForwarding(t *testing.T) {
	reg := NewMCPToolRegistry()
	reg.Register("env_test", "Test env", nil, func(h *handler, args map[string]interface{}) (string, bool) {
		env, _ := args["_env"].(string)
		return "env: " + env, false
	})

	body, _ := json.Marshal(map[string]interface{}{"name": "env_test", "arguments": map[string]interface{}{}})
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/v1/mcp/call", bytes.NewReader(body))
	r.Header.Set("X-Agency-Env", "ANTHROPIC_API_KEY=sk-test")
	mcpCallHandler(reg, nil)(w, r)

	var resp struct {
		Content []struct{ Text string } `json:"content"`
	}
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Content[0].Text != "env: ANTHROPIC_API_KEY=sk-test" {
		t.Errorf("text = %q", resp.Content[0].Text)
	}
}
