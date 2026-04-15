package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	capProviderWebSearch       = "provider-web-search"
	capProviderWebFetch        = "provider-web-fetch"
	capProviderURLContext      = "provider-url-context"
	capProviderFileSearch      = "provider-file-search"
	capProviderCodeExecution   = "provider-code-execution"
	capProviderComputerUse     = "provider-computer-use"
	capProviderShell           = "provider-shell"
	capProviderTextEditor      = "provider-text-editor"
	capProviderMemory          = "provider-memory"
	capProviderMCP             = "provider-mcp"
	capProviderImageGeneration = "provider-image-generation"
	capProviderGoogleMaps      = "provider-google-maps"
	capProviderToolSearch      = "provider-tool-search"
	capProviderApplyPatch      = "provider-apply-patch"
)

// ProviderToolUse describes a provider-executed server-side tool requested in
// an LLM payload. These tools execute outside Agency's client-side tool runner,
// so they need explicit mediation before the request is forwarded upstream.
type ProviderToolUse struct {
	Capability string
	ToolType   string
	Name       string
}

// ProviderToolPolicy is the external grant set used by the LLM handler.
type ProviderToolPolicy struct {
	Granted map[string]bool
}

func LoadProviderToolPolicy(agentDir string) *ProviderToolPolicy {
	p := &ProviderToolPolicy{Granted: map[string]bool{}}
	data, err := os.ReadFile(filepath.Join(agentDir, "constraints.yaml"))
	if err != nil {
		return p
	}
	var cfg struct {
		GrantedCapabilities []string `yaml:"granted_capabilities"`
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return p
	}
	for _, cap := range cfg.GrantedCapabilities {
		p.Granted[strings.TrimSpace(cap)] = true
	}
	return p
}

func (p *ProviderToolPolicy) Allows(capability string) bool {
	if p == nil {
		return false
	}
	return p.Granted[capability]
}

// DetectProviderToolUses recognizes provider-executed tools across the request
// shapes used by OpenAI, Anthropic, xAI-compatible APIs, and Gemini native
// tools. It intentionally ignores OpenAI-style function tools, which are
// client-side tool calls already covered by existing MCP mediation.
func DetectProviderToolUses(body map[string]interface{}) []ProviderToolUse {
	rawTools, ok := body["tools"].([]interface{})
	if !ok || len(rawTools) == 0 {
		return nil
	}

	var uses []ProviderToolUse
	for _, raw := range rawTools {
		tool, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		uses = append(uses, detectProviderToolUse(tool)...)
	}
	return dedupeProviderToolUses(uses)
}

func detectProviderToolUse(tool map[string]interface{}) []ProviderToolUse {
	// OpenAI-compatible and Anthropic server tools usually carry a type.
	if typ, _ := tool["type"].(string); typ != "" {
		if typ == "function" {
			return nil
		}
		if cap := providerToolCapability(typ); cap != "" {
			name, _ := tool["name"].(string)
			return []ProviderToolUse{{Capability: cap, ToolType: typ, Name: name}}
		}
	}

	// Gemini native tools are represented as object keys rather than a type.
	var uses []ProviderToolUse
	for key := range tool {
		if cap := providerToolCapability(key); cap != "" {
			uses = append(uses, ProviderToolUse{Capability: cap, ToolType: key})
		}
	}
	return uses
}

func providerToolCapability(toolType string) string {
	t := strings.ToLower(strings.TrimSpace(toolType))
	t = strings.ReplaceAll(t, "-", "_")

	switch {
	case t == "web_search", t == "web_search_preview", strings.HasPrefix(t, "web_search_"), t == "googlesearch", t == "google_search":
		return capProviderWebSearch
	case t == "web_fetch", strings.HasPrefix(t, "web_fetch_"):
		return capProviderWebFetch
	case t == "url_context", t == "urlcontext":
		return capProviderURLContext
	case t == "file_search", t == "filesearch", t == "collections_search", t == "collectionssearch":
		return capProviderFileSearch
	case t == "code_interpreter", t == "code_execution", t == "codeexecution", strings.HasPrefix(t, "code_execution_"):
		return capProviderCodeExecution
	case t == "computer", t == "computer_use", t == "computeruse", t == "computer_use_preview", strings.HasPrefix(t, "computer_"):
		return capProviderComputerUse
	case t == "bash", t == "shell", t == "local_shell", strings.HasPrefix(t, "bash_"):
		return capProviderShell
	case t == "text_editor", t == "texteditor", strings.HasPrefix(t, "text_editor_"):
		return capProviderTextEditor
	case t == "memory", strings.HasPrefix(t, "memory_"):
		return capProviderMemory
	case t == "mcp", t == "mcp_toolset", t == "remote_mcp":
		return capProviderMCP
	case t == "image_generation", t == "imagegeneration":
		return capProviderImageGeneration
	case t == "google_maps", t == "googlemaps":
		return capProviderGoogleMaps
	case t == "tool_search", t == "toolsearch":
		return capProviderToolSearch
	case t == "apply_patch", t == "applypatch":
		return capProviderApplyPatch
	default:
		return ""
	}
}

func dedupeProviderToolUses(uses []ProviderToolUse) []ProviderToolUse {
	if len(uses) < 2 {
		return uses
	}
	seen := map[string]bool{}
	var out []ProviderToolUse
	for _, use := range uses {
		key := use.Capability + "\x00" + use.ToolType + "\x00" + use.Name
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, use)
	}
	return out
}

func summarizeProviderToolUses(uses []ProviderToolUse) map[string]string {
	if len(uses) == 0 {
		return nil
	}
	caps := make([]string, 0, len(uses))
	types := make([]string, 0, len(uses))
	names := make([]string, 0, len(uses))
	for _, use := range uses {
		caps = append(caps, use.Capability)
		types = append(types, use.ToolType)
		if use.Name != "" {
			names = append(names, use.Name)
		}
	}
	sort.Strings(caps)
	sort.Strings(types)
	sort.Strings(names)

	extra := map[string]string{
		"provider_tool_capabilities": strings.Join(uniqueStrings(caps), ","),
		"provider_tool_types":        strings.Join(uniqueStrings(types), ","),
	}
	if len(names) > 0 {
		extra["provider_tool_names"] = strings.Join(uniqueStrings(names), ",")
	}
	return extra
}

func uniqueStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := values[:0]
	var last string
	for i, value := range values {
		if i == 0 || value != last {
			out = append(out, value)
			last = value
		}
	}
	return out
}

func providerToolDeniedError(use ProviderToolUse) string {
	return fmt.Sprintf("provider tool %q requires capability %q", use.ToolType, use.Capability)
}

func providerToolUnsupportedError(modelAlias string, use ProviderToolUse) string {
	return fmt.Sprintf("model %q does not declare support for provider tool capability %q", modelAlias, use.Capability)
}
