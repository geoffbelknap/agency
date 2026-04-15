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
		"computer_use_preview":    capProviderComputerUse,
		"computer_20250124":       capProviderComputerUse,
		"shell":                   capProviderShell,
		"local_shell":             capProviderShell,
		"bash_20250124":           capProviderShell,
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
