package main

import (
	"fmt"
	"sort"
	"strings"
)

const maxAuditedProviderToolURLs = 12

// providerToolAuditExtra extracts compact, non-secret evidence metadata from a
// provider response. It intentionally records counts and source URLs rather
// than provider-returned content.
func providerToolAuditExtra(uses []ProviderToolUse, respBody map[string]interface{}) map[string]string {
	extra := summarizeProviderToolUses(uses)
	if extra == nil {
		extra = map[string]string{}
	}
	if len(uses) == 0 && respBody == nil {
		return nil
	}

	stats := &providerToolAuditStats{
		toolTypes: map[string]bool{},
		urls:      map[string]bool{},
	}
	collectProviderToolAuditStats(respBody, stats)

	if len(stats.toolTypes) > 0 {
		extra["provider_response_tool_types"] = strings.Join(sortedKeys(stats.toolTypes), ",")
	}
	if stats.sourceCount > 0 {
		extra["provider_source_count"] = fmt.Sprintf("%d", stats.sourceCount)
	}
	if stats.citationCount > 0 {
		extra["provider_citation_count"] = fmt.Sprintf("%d", stats.citationCount)
	}
	if stats.searchQueryCount > 0 {
		extra["provider_search_query_count"] = fmt.Sprintf("%d", stats.searchQueryCount)
	}
	if len(stats.urls) > 0 {
		urls := sortedKeys(stats.urls)
		if len(urls) > maxAuditedProviderToolURLs {
			urls = urls[:maxAuditedProviderToolURLs]
		}
		extra["provider_source_urls"] = strings.Join(urls, ",")
	}
	if len(extra) == 0 {
		return nil
	}
	return extra
}

type providerToolAuditStats struct {
	toolTypes        map[string]bool
	urls             map[string]bool
	sourceCount      int
	citationCount    int
	searchQueryCount int
}

func collectProviderToolAuditStats(v interface{}, stats *providerToolAuditStats) {
	switch x := v.(type) {
	case map[string]interface{}:
		if typ, _ := x["type"].(string); typ != "" {
			if isProviderResponseToolType(typ) {
				stats.toolTypes[typ] = true
			}
			if strings.Contains(strings.ToLower(typ), "citation") {
				stats.citationCount++
			}
		}
		for key, value := range x {
			lower := strings.ToLower(key)
			switch lower {
			case "url", "uri", "retrieved_url", "source_url":
				if url, ok := value.(string); ok && strings.HasPrefix(url, "http") {
					stats.urls[url] = true
					stats.sourceCount++
				}
			case "sources", "groundingchunks", "grounding_chunks", "urlmetadata", "url_metadata", "citations", "annotations", "groundingsupports", "grounding_supports":
				if items, ok := value.([]interface{}); ok {
					if lower == "sources" || lower == "groundingchunks" || lower == "grounding_chunks" || lower == "urlmetadata" || lower == "url_metadata" {
						stats.sourceCount += len(items)
					}
					if lower == "citations" || lower == "annotations" || lower == "groundingsupports" || lower == "grounding_supports" {
						stats.citationCount += len(items)
					}
				}
			case "websearchqueries", "web_search_queries", "queries":
				if items, ok := value.([]interface{}); ok {
					stats.searchQueryCount += len(items)
				}
			}
			collectProviderToolAuditStats(value, stats)
		}
	case []interface{}:
		for _, item := range x {
			collectProviderToolAuditStats(item, stats)
		}
	}
}

func isProviderResponseToolType(typ string) bool {
	t := strings.ToLower(typ)
	return strings.Contains(t, "web_search") ||
		strings.Contains(t, "web_fetch") ||
		strings.Contains(t, "file_search") ||
		strings.Contains(t, "code") ||
		strings.Contains(t, "computer") ||
		strings.Contains(t, "tool_result") ||
		strings.Contains(t, "server_tool")
}

func sortedKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
