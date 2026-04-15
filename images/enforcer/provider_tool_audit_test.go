package main

import "testing"

func TestProviderToolAuditExtraOpenAIResponsesSources(t *testing.T) {
	uses := []ProviderToolUse{{Capability: capProviderWebSearch, ToolType: "web_search"}}
	resp := map[string]interface{}{
		"output": []interface{}{
			map[string]interface{}{
				"type": "web_search_call",
				"action": map[string]interface{}{
					"sources": []interface{}{
						map[string]interface{}{"url": "https://example.com/a"},
						map[string]interface{}{"url": "https://example.com/b"},
					},
					"queries": []interface{}{"example query"},
				},
			},
			map[string]interface{}{
				"type": "message",
				"content": []interface{}{
					map[string]interface{}{
						"type": "output_text",
						"annotations": []interface{}{
							map[string]interface{}{"type": "url_citation", "url": "https://example.com/a"},
						},
					},
				},
			},
		},
	}

	extra := providerToolAuditExtra(uses, resp)
	if extra["provider_tool_capabilities"] != capProviderWebSearch {
		t.Fatalf("capability summary missing: %#v", extra)
	}
	if extra["provider_response_tool_types"] != "web_search_call" {
		t.Fatalf("response tool type missing: %#v", extra)
	}
	if extra["provider_search_query_count"] != "1" {
		t.Fatalf("query count missing: %#v", extra)
	}
	if extra["provider_citation_count"] == "" {
		t.Fatalf("citation count missing: %#v", extra)
	}
	if extra["provider_source_urls"] == "" {
		t.Fatalf("source urls missing: %#v", extra)
	}
}

func TestProviderToolAuditExtraGeminiGrounding(t *testing.T) {
	resp := map[string]interface{}{
		"candidates": []interface{}{
			map[string]interface{}{
				"groundingMetadata": map[string]interface{}{
					"webSearchQueries": []interface{}{"current docs"},
					"groundingChunks": []interface{}{
						map[string]interface{}{"web": map[string]interface{}{"uri": "https://ai.google.dev/gemini-api/docs/google-search"}},
					},
				},
			},
		},
	}

	extra := providerToolAuditExtra(nil, resp)
	if extra["provider_search_query_count"] != "1" {
		t.Fatalf("query count missing: %#v", extra)
	}
	if extra["provider_source_count"] == "" {
		t.Fatalf("source count missing: %#v", extra)
	}
	if extra["provider_source_urls"] == "" {
		t.Fatalf("source urls missing: %#v", extra)
	}
}

func TestProviderToolAuditExtraGeminiURLContext(t *testing.T) {
	resp := map[string]interface{}{
		"candidates": []interface{}{
			map[string]interface{}{
				"urlContextMetadata": map[string]interface{}{
					"urlMetadata": []interface{}{
						map[string]interface{}{"retrieved_url": "https://example.com/report.pdf"},
					},
				},
			},
		},
	}

	extra := providerToolAuditExtra([]ProviderToolUse{{Capability: capProviderURLContext, ToolType: "url_context"}}, resp)
	if extra["provider_source_count"] == "" {
		t.Fatalf("source count missing: %#v", extra)
	}
	if extra["provider_source_urls"] != "https://example.com/report.pdf" {
		t.Fatalf("source url missing: %#v", extra)
	}
}

func TestProviderToolAuditExtraHarnessProposal(t *testing.T) {
	resp := map[string]interface{}{
		"output": []interface{}{
			map[string]interface{}{
				"type": "computer_call",
				"action": map[string]interface{}{
					"type": "click",
				},
			},
			map[string]interface{}{
				"type": "tool_use",
				"name": "bash_20250124",
				"input": map[string]interface{}{
					"command": "whoami",
				},
			},
		},
	}

	extra := providerToolAuditExtra([]ProviderToolUse{{Capability: capProviderComputerUse, ToolType: "computer_use_preview"}}, resp)
	if extra["provider_tool_harness_required"] != "true" {
		t.Fatalf("harness marker missing: %#v", extra)
	}
	if extra["provider_tool_harness_proposal_count"] != "2" {
		t.Fatalf("proposal count missing: %#v", extra)
	}
	if extra["provider_tool_harness_capabilities"] != capProviderComputerUse+","+capProviderShell {
		t.Fatalf("harness capabilities missing: %#v", extra)
	}
}
