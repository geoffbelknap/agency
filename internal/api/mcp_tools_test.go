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
		"type":       "object",
		"properties": map[string]interface{}{"name": map[string]interface{}{"type": "string"}},
	}, func(d *mcpDeps, args map[string]interface{}) (string, bool) {
		return "ok", false
	})

	tools := reg.Tools()
	if len(tools) != 1 {
		t.Fatalf("tools count = %d, want 1", len(tools))
	}
	if tools[0].Name != "agency_test" {
		t.Errorf("name = %q, want agency_test", tools[0].Name)
	}
	if tools[0].Tier != "core" {
		t.Errorf("tier = %q, want core", tools[0].Tier)
	}
}

func TestMCPToolRegistry_ToolsByView(t *testing.T) {
	reg := NewMCPToolRegistry()
	reg.Register("agency_core", "Core tool", nil, nil)
	reg.RegisterWithTier("agency_experimental", "Experimental tool", "experimental", nil, nil)

	core := reg.ToolsByView("core")
	if len(core) != 1 || core[0].Name != "agency_core" {
		t.Fatalf("core tools = %#v, want only agency_core", core)
	}

	full := reg.ToolsByView("full")
	if len(full) != 2 {
		t.Fatalf("full tools = %d, want 2", len(full))
	}
}

func TestMCPToolRegistry_Call(t *testing.T) {
	reg := NewMCPToolRegistry()
	reg.Register("echo", "Echo tool", nil, func(d *mcpDeps, args map[string]interface{}) (string, bool) {
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

func TestMCPToolsEndpoint_FullViewIncludesExperimental(t *testing.T) {
	reg := NewMCPToolRegistry()
	reg.Register("agency_core", "Core", nil, nil)
	reg.RegisterWithTier("agency_experimental", "Experimental", "experimental", nil, nil)

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/v1/mcp/tools?view=full", nil)
	mcpToolsHandler(reg)(w, r)

	if w.Code != 200 {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var body struct {
		Tools []MCPTool `json:"tools"`
	}
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if len(body.Tools) != 2 {
		t.Fatalf("tools = %d, want 2", len(body.Tools))
	}
}

func TestMCPToolRegistry_RegisterDuplicatePanics(t *testing.T) {
	reg := NewMCPToolRegistry()
	reg.Register("agency_test", "Test", nil, nil)

	defer func() {
		if recover() == nil {
			t.Fatal("expected duplicate registration to panic")
		}
	}()

	reg.Register("agency_test", "Duplicate", nil, nil)
}

func TestMCPToolRegistry_ToolsReturnsCopy(t *testing.T) {
	reg := NewMCPToolRegistry()
	reg.Register("agency_test", "Test", nil, nil)

	tools := reg.Tools()
	tools[0].Name = "mutated"

	fresh := reg.Tools()
	if fresh[0].Name != "agency_test" {
		t.Fatalf("registry tool name = %q, want agency_test", fresh[0].Name)
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
