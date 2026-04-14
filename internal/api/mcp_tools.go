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
	Tier        string      `json:"x-agency-tier,omitempty"`
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
	byName       map[string]int // name → index into entries
	defaultTier  string
	toolsPayload map[string][]byte
}

func NewMCPToolRegistry() *MCPToolRegistry {
	return &MCPToolRegistry{
		entries:      make([]mcpToolEntry, 0, 72),
		byName:       make(map[string]int, 72),
		defaultTier:  "core",
		toolsPayload: make(map[string][]byte, 2),
	}
}

func (r *MCPToolRegistry) Register(name, description string, schema interface{}, handler MCPHandler) {
	r.RegisterWithTier(name, description, r.defaultTier, schema, handler)
}

func (r *MCPToolRegistry) RegisterWithTier(name, description, tier string, schema interface{}, handler MCPHandler) {
	if _, exists := r.byName[name]; exists {
		panic(fmt.Sprintf("duplicate MCP tool registration: %s", name))
	}
	if schema == nil {
		schema = map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}
	}
	if tier == "" {
		tier = "core"
	}
	tool := MCPTool{Name: name, Description: description, InputSchema: schema, Tier: tier}
	idx := len(r.entries)
	r.entries = append(r.entries, mcpToolEntry{
		MCPTool: tool,
		handler: handler,
	})
	r.byName[name] = idx
	r.toolsPayload = make(map[string][]byte, 2)
}

func (r *MCPToolRegistry) WithTier(tier string, fn func()) {
	previous := r.defaultTier
	if tier == "" {
		tier = "core"
	}
	r.defaultTier = tier
	defer func() { r.defaultTier = previous }()
	fn()
}

func (r *MCPToolRegistry) Tools() []MCPTool {
	return r.ToolsByView("core")
}

func (r *MCPToolRegistry) ToolsByView(view string) []MCPTool {
	filtered := make([]MCPTool, 0, len(r.entries))
	for _, entry := range r.entries {
		if !includeMCPTool(entry.MCPTool, view) {
			continue
		}
		filtered = append(filtered, entry.MCPTool)
	}
	tools := make([]MCPTool, len(filtered))
	copy(tools, filtered)
	return tools
}

func (r *MCPToolRegistry) Call(name string, d *mcpDeps, args map[string]interface{}) (string, bool) {
	idx, ok := r.byName[name]
	if !ok {
		return "unknown tool: " + name, true
	}
	return r.entries[idx].handler(d, args)
}

func (r *MCPToolRegistry) toolsResponse(view string) []byte {
	if payload, ok := r.toolsPayload[view]; ok {
		return payload
	}
	payload, err := json.Marshal(map[string]interface{}{"tools": r.ToolsByView(view)})
	if err != nil {
		return []byte(`{"tools":[]}`)
	}
	r.toolsPayload[view] = payload
	return payload
}

func mcpToolsHandler(reg *MCPToolRegistry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		view := "core"
		if r.URL.Query().Get("view") == "full" {
			view = "full"
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(reg.toolsResponse(view))
	}
}

func includeMCPTool(tool MCPTool, view string) bool {
	switch view {
	case "full":
		return true
	default:
		return tool.Tier == "" || tool.Tier == "core"
	}
}
