package api

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMCPToolRegistry_RegisterAndList(t *testing.T) {
	reg := NewMCPToolRegistry()
	reg.Register("agency_test", "A test tool", map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{"name": map[string]interface{}{"type": "string"}},
	}, func(h *handler, args map[string]interface{}) (string, bool) {
		return "ok", false
	})

	tools := reg.Tools()
	if len(tools) != 1 {
		t.Fatalf("tools count = %d, want 1", len(tools))
	}
	if tools[0].Name != "agency_test" {
		t.Errorf("name = %q, want agency_test", tools[0].Name)
	}
}

func TestMCPToolRegistry_Call(t *testing.T) {
	reg := NewMCPToolRegistry()
	reg.Register("echo", "Echo tool", nil, func(h *handler, args map[string]interface{}) (string, bool) {
		return "hello", false
	})

	text, isErr := reg.Call("echo", nil, nil)
	if isErr {
		t.Error("expected isError=false")
	}
	if text != "hello" {
		t.Errorf("text = %q, want hello", text)
	}
}

func TestMCPToolRegistry_CallUnknown(t *testing.T) {
	reg := NewMCPToolRegistry()
	text, isErr := reg.Call("nope", nil, nil)
	if !isErr {
		t.Error("expected isError=true")
	}
	if text != "unknown tool: nope" {
		t.Errorf("text = %q", text)
	}
}

func TestMCPToolsEndpoint(t *testing.T) {
	reg := NewMCPToolRegistry()
	reg.Register("agency_test", "Test", nil, nil)

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/v1/mcp/tools", nil)
	mcpToolsHandler(reg)(w, r)

	if w.Code != 200 {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var body struct {
		Tools []MCPTool `json:"tools"`
	}
	json.NewDecoder(w.Body).Decode(&body)
	if len(body.Tools) != 1 {
		t.Fatalf("tools = %d, want 1", len(body.Tools))
	}
}

func TestMCPValidateResourceName(t *testing.T) {
	tests := []struct {
		name    string
		wantErr bool
		errMsg  string
	}{
		{"my-team", false, ""},
		{"valid_name", false, ""},
		{"team123", false, ""},
		{"", true, "team name is required"},
		{"..", true, "must not be a relative path component"},
		{".", true, "must not be a relative path component"},
		{"../etc", true, "must not contain path separators"},
		{"foo/bar", true, "must not contain path separators"},
		// backslash is not a path separator on Linux; filepath.Base handles it per-OS
		{"team..evil", true, "must not contain '..'"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := mcpValidateResourceName(tt.name, "team")
			if tt.wantErr {
				if err == nil {
					t.Errorf("mcpValidateResourceName(%q) = nil, want error containing %q", tt.name, tt.errMsg)
				} else if !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("mcpValidateResourceName(%q) error = %q, want it to contain %q", tt.name, err.Error(), tt.errMsg)
				}
			} else {
				if err != nil {
					t.Errorf("mcpValidateResourceName(%q) = %v, want nil", tt.name, err)
				}
			}
		})
	}
}
