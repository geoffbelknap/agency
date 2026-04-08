package api

import (
	"encoding/json"
	"fmt"
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
	entries      []mcpToolEntry
	tools        []MCPTool
	byName       map[string]int // name → index into entries
	toolsPayload []byte
}

func NewMCPToolRegistry() *MCPToolRegistry {
	return &MCPToolRegistry{
		entries: make([]mcpToolEntry, 0, 72),
		tools:   make([]MCPTool, 0, 72),
		byName:  make(map[string]int, 72),
	}
}

func (r *MCPToolRegistry) Register(name, description string, schema interface{}, handler MCPHandler) {
	if _, exists := r.byName[name]; exists {
		panic(fmt.Sprintf("duplicate MCP tool registration: %s", name))
	}
	if schema == nil {
		schema = map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}
	}
	tool := MCPTool{Name: name, Description: description, InputSchema: schema}
	idx := len(r.entries)
	r.entries = append(r.entries, mcpToolEntry{
		MCPTool: tool,
		handler: handler,
	})
	r.tools = append(r.tools, tool)
	r.byName[name] = idx
	r.toolsPayload = nil
}

func (r *MCPToolRegistry) Tools() []MCPTool {
	tools := make([]MCPTool, len(r.tools))
	copy(tools, r.tools)
	return tools
}

func (r *MCPToolRegistry) Call(name string, d *mcpDeps, args map[string]interface{}) (string, bool) {
	idx, ok := r.byName[name]
	if !ok {
		return "unknown tool: " + name, true
	}
	return r.entries[idx].handler(d, args)
}

func (r *MCPToolRegistry) toolsResponse() []byte {
	if r.toolsPayload != nil {
		return r.toolsPayload
	}
	payload, err := json.Marshal(map[string]interface{}{"tools": r.tools})
	if err != nil {
		return []byte(`{"tools":[]}`)
	}
	r.toolsPayload = payload
	return payload
}

func mcpToolsHandler(reg *MCPToolRegistry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(reg.toolsResponse())
	}
}
