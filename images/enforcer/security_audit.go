package main

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"time"
)

const (
	securityScanPassed  = "SECURITY_SCAN_PASSED"
	securityScanFlagged = "SECURITY_SCAN_FLAGGED"
	securityScanSkipped = "SECURITY_SCAN_SKIPPED"
	securityScanNA      = "SECURITY_SCAN_NOT_APPLICABLE"
)

func intPtr(v int) *int {
	return &v
}

func contentDigest(content string) string {
	if content == "" {
		return ""
	}
	return fmt.Sprintf("%x", sha256.Sum256([]byte(content)))
}

func collectToolMessageContent(reqBody map[string]interface{}) (string, int) {
	messages, ok := reqBody["messages"].([]interface{})
	if !ok {
		return "", 0
	}

	var parts []string
	for _, raw := range messages {
		msg, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		role, _ := msg["role"].(string)
		if role != "tool" {
			continue
		}
		content, _ := msg["content"].(string)
		if content == "" {
			continue
		}
		parts = append(parts, content)
	}
	return strings.Join(parts, "\n\n--- tool message boundary ---\n\n"), len(parts)
}

func (lh *LLMHandler) auditXPIAToolMessageScan(reqBody map[string]interface{}, modelAlias, correlationID string) []string {
	start := time.Now()
	content, count := collectToolMessageContent(reqBody)
	if !lh.routing.Settings.XPIAScan {
		lh.audit.Log(AuditEntry{
			Type:          securityScanSkipped,
			Model:         modelAlias,
			CorrelationID: correlationID,
			DurationMs:    time.Since(start).Milliseconds(),
			ScanType:      "xpia",
			ScanSurface:   "llm_tool_messages",
			ScanAction:    "skipped",
			ScanMode:      "pattern",
			FindingCount:  intPtr(0),
			ContentBytes:  len(content),
			ContentCount:  count,
			Error:         "xpia_scan disabled in routing settings",
		})
		return nil
	}

	flags := scanToolMessages(reqBody)
	entryType := securityScanPassed
	action := "allowed"
	if len(flags) > 0 {
		entryType = securityScanFlagged
		action = "flagged"
	}
	lh.audit.Log(AuditEntry{
		Type:          entryType,
		Model:         modelAlias,
		CorrelationID: correlationID,
		DurationMs:    time.Since(start).Milliseconds(),
		ScanType:      "xpia",
		ScanSurface:   "llm_tool_messages",
		ScanAction:    action,
		ScanMode:      "pattern",
		FindingCount:  intPtr(len(flags)),
		Findings:      flags,
		ContentSHA256: contentDigest(content),
		ContentBytes:  len(content),
		ContentCount:  count,
	})
	return flags
}

func (lh *LLMHandler) auditToolDefinitionScan(reqBody map[string]interface{}, correlationID string) string {
	start := time.Now()
	tools, _ := reqBody["tools"].([]interface{})
	if len(tools) == 0 {
		return ""
	}

	flag := trackToolDefinitions(reqBody, lh.toolTracker)
	entryType := securityScanPassed
	action := "allowed"
	findings := []string(nil)
	if flag != "" {
		entryType = securityScanFlagged
		action = "flagged"
		findings = []string{flag}
	}
	lh.audit.Log(AuditEntry{
		Type:          entryType,
		CorrelationID: correlationID,
		DurationMs:    time.Since(start).Milliseconds(),
		ScanType:      "mcp_tool_integrity",
		ScanSurface:   "llm_tool_definitions",
		ScanAction:    action,
		ScanMode:      "hash",
		FindingCount:  intPtr(len(findings)),
		Findings:      findings,
		ContentCount:  len(tools),
	})
	return flag
}

func providerToolExternalContentBoundary(capability string) bool {
	switch capability {
	case capProviderWebSearch, capProviderWebFetch, capProviderURLContext:
		return true
	default:
		return false
	}
}

func externalContentProviderToolUses(uses []ProviderToolUse) []ProviderToolUse {
	var out []ProviderToolUse
	for _, use := range uses {
		if providerToolExternalContentBoundary(use.Capability) {
			out = append(out, use)
		}
	}
	return out
}

func (lh *LLMHandler) auditProviderToolContentBoundary(modelAlias, providerModel, correlationID string, uses []ProviderToolUse) {
	boundaryUses := externalContentProviderToolUses(uses)
	if len(boundaryUses) == 0 {
		return
	}
	extra := summarizeProviderToolUses(boundaryUses)
	if extra == nil {
		extra = map[string]string{}
	}
	extra["security_boundary"] = "provider_hosted_raw_content_not_visible"
	extra["scan_reason"] = "provider-hosted web content is consumed inside the model provider before Agency can scan raw content"

	lh.audit.Log(AuditEntry{
		Type:          securityScanNA,
		Model:         modelAlias,
		ProviderModel: providerModel,
		CorrelationID: correlationID,
		Status:        200,
		ScanType:      "xpia",
		ScanSurface:   "provider_tool_content",
		ScanAction:    "not_applicable",
		ScanMode:      "provider_boundary",
		FindingCount:  intPtr(0),
		ContentCount:  len(boundaryUses),
		Extra:         extra,
	})
}
