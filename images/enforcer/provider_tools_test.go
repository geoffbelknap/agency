package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDetectProviderToolUses(t *testing.T) {
	body := map[string]interface{}{
		"tools": []interface{}{
			map[string]interface{}{"type": "function", "function": map[string]interface{}{"name": "read_file"}},
			map[string]interface{}{"type": "web_search_preview"},
			map[string]interface{}{"type": "code_interpreter"},
			map[string]interface{}{"google_search": map[string]interface{}{}},
			map[string]interface{}{"urlContext": map[string]interface{}{}},
		},
	}

	uses := DetectProviderToolUses(body)
	got := map[string]bool{}
	for _, use := range uses {
		got[use.Capability] = true
	}

	for _, want := range []string{
		capProviderWebSearch,
		capProviderCodeExecution,
		capProviderURLContext,
	} {
		if !got[want] {
			t.Fatalf("missing capability %q in %#v", want, uses)
		}
	}
	if got["tools"] {
		t.Fatal("function tools should not be classified as provider tools")
	}
}

func TestProviderToolCapabilityInventoryShapes(t *testing.T) {
	cases := map[string]string{
		"web_search":              capProviderWebSearch,
		"web_search_20260209":     capProviderWebSearch,
		"google_search":           capProviderWebSearch,
		"web_fetch":               capProviderWebFetch,
		"url_context":             capProviderURLContext,
		"file_search":             capProviderFileSearch,
		"collections_search":      capProviderFileSearch,
		"code_interpreter":        capProviderCodeExecution,
		"code_execution":          capProviderCodeExecution,
		"code_execution_20250825": capProviderCodeExecution,
		"computer_call":           capProviderComputerUse,
		"computer_use_preview":    capProviderComputerUse,
		"computer_20250124":       capProviderComputerUse,
		"shell":                   capProviderShell,
		"shell_call":              capProviderShell,
		"local_shell":             capProviderShell,
		"local_shell_call":        capProviderShell,
		"bash_code_execution":     capProviderShell,
		"bash_20250124":           capProviderShell,
		"text_editor_call":        capProviderTextEditor,
		"text_editor_20250124":    capProviderTextEditor,
		"memory":                  capProviderMemory,
		"mcp":                     capProviderMCP,
		"remote_mcp":              capProviderMCP,
		"image_generation":        capProviderImageGeneration,
		"google_maps":             capProviderGoogleMaps,
		"tool_search":             capProviderToolSearch,
		"apply_patch":             capProviderApplyPatch,
	}
	for toolType, want := range cases {
		if got := providerToolCapability(toolType); got != want {
			t.Fatalf("providerToolCapability(%q) = %q, want %q", toolType, got, want)
		}
	}
}

func TestProviderToolExecutionModes(t *testing.T) {
	if !providerToolRequiresAgencyHarness(capProviderComputerUse) {
		t.Fatal("computer use must require Agency harness mediation")
	}
	if !providerToolRequiresAgencyHarness(capProviderShell) {
		t.Fatal("shell must require Agency harness mediation")
	}
	if !providerToolRequiresAgencyHarness(capProviderTextEditor) {
		t.Fatal("text editor must require Agency harness mediation")
	}
	if !providerToolRequiresAgencyHarness(capProviderApplyPatch) {
		t.Fatal("apply patch must require Agency harness mediation")
	}
	if providerToolRequiresAgencyHarness(capProviderWebSearch) {
		t.Fatal("web search should remain provider-hosted, not Agency harnessed")
	}

	extra := summarizeProviderToolUses([]ProviderToolUse{{Capability: capProviderComputerUse, ToolType: "computer_use_preview"}})
	if extra["provider_tool_execution_modes"] != providerToolExecutionAgencyHarnessed {
		t.Fatalf("execution mode missing: %#v", extra)
	}
	if extra["provider_tool_harness_required"] != "true" {
		t.Fatalf("harness marker missing: %#v", extra)
	}
}

func TestRewriteHarnessedProviderTools(t *testing.T) {
	body := map[string]interface{}{
		"tools": []interface{}{
			map[string]interface{}{"type": "shell"},
			map[string]interface{}{"type": "text_editor_20250124"},
			map[string]interface{}{"type": "web_search"},
		},
	}

	translated := rewriteHarnessedProviderTools(body)
	if len(translated) != 2 {
		t.Fatalf("translated uses = %#v, want 2", translated)
	}
	rawTools := body["tools"].([]interface{})
	var names []string
	var sawWebSearch bool
	for _, raw := range rawTools {
		tool := raw.(map[string]interface{})
		if typ, _ := tool["type"].(string); typ == "web_search" {
			sawWebSearch = true
			continue
		}
		fn, _ := tool["function"].(map[string]interface{})
		if name, _ := fn["name"].(string); name != "" {
			names = append(names, name)
		}
	}
	got := map[string]bool{}
	for _, name := range names {
		got[name] = true
	}
	for _, want := range []string{"execute_command", "read_file", "write_file"} {
		if !got[want] {
			t.Fatalf("missing translated function %q in %#v", want, rawTools)
		}
	}
	if !sawWebSearch {
		t.Fatalf("non-harnessed provider tool should be preserved: %#v", rawTools)
	}
}

func TestLoadProviderToolPolicy(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "constraints.yaml"), []byte("granted_capabilities:\n  - provider-web-search\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	policy := LoadProviderToolPolicy(dir)
	if !policy.Allows(capProviderWebSearch) {
		t.Fatal("expected provider-web-search grant")
	}
	if policy.Allows(capProviderCodeExecution) {
		t.Fatal("unexpected provider-code-execution grant")
	}
}

func TestLoadProviderToolPolicyIncludesEffectiveGrants(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "constraints.yaml"), []byte("granted_capabilities: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "provider-tools.yaml"), []byte("agent: henry\ngrants:\n  - capability: provider-code-execution\n    source: capabilities.yaml\n    granted_by: operator\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	policy := LoadProviderToolPolicy(dir)
	if !policy.Allows(capProviderCodeExecution) {
		t.Fatal("expected provider-code-execution effective grant")
	}
	if policy.Allows(capProviderWebSearch) {
		t.Fatal("unexpected provider-web-search grant")
	}
}
