package api

import (
	"encoding/json"
	"net/http"
)

// MCPTool describes a single MCP tool for the tools/list response.
type MCPTool struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	InputSchema interface{} `json:"inputSchema"`
}

// MCPHandler executes a tool call and returns text + isError.
type MCPHandler func(d *mcpDeps, args map[string]interface{}) (string, bool)

// mcpToolEntry stores a tool definition alongside its handler (handler not serialized).
type mcpToolEntry struct {
	MCPTool
	handler MCPHandler
}

// MCPToolRegistry holds all MCP tool definitions and handlers.
type MCPToolRegistry struct {
	entries []mcpToolEntry
	byName  map[string]int // name → index into entries
}

func NewMCPToolRegistry() *MCPToolRegistry {
	return &MCPToolRegistry{
		entries: make([]mcpToolEntry, 0, 72),
		byName:  make(map[string]int, 72),
	}
}

func (r *MCPToolRegistry) Register(name, description string, schema interface{}, handler MCPHandler) {
	if schema == nil {
		schema = map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}
	}
	idx := len(r.entries)
	r.entries = append(r.entries, mcpToolEntry{
		MCPTool: MCPTool{Name: name, Description: description, InputSchema: schema},
		handler: handler,
	})
	r.byName[name] = idx
}

func (r *MCPToolRegistry) Tools() []MCPTool {
	tools := make([]MCPTool, len(r.entries))
	for i, e := range r.entries {
		tools[i] = e.MCPTool
	}
	return tools
}

func (r *MCPToolRegistry) Call(name string, d *mcpDeps, args map[string]interface{}) (string, bool) {
	idx, ok := r.byName[name]
	if !ok {
		return "unknown tool: " + name, true
	}
	return r.entries[idx].handler(d, args)
}

func mcpToolsHandler(reg *MCPToolRegistry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"tools": reg.Tools()})
	}
}
