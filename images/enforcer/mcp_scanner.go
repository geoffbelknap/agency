package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"sync"
)

// injectionPattern pairs a compiled regex with a human-readable description.
type injectionPattern struct {
	re   *regexp.Regexp
	desc string
}

// Pre-compiled injection patterns (ASK Tenet 17: XPIA defense).
var injectionPatterns = func() []injectionPattern {
	raw := []struct {
		pattern string
		desc    string
	}{
		{`(?:^|\n)\s*(?:system|assistant)\s*:`, "role impersonation"},
		{`ignore\s+(?:previous|above|all)\s+instructions`, "instruction override"},
		{`disregard\s+(?:previous|above|your)\s+(?:instructions|constraints|rules)`, "instruction override"},
		{`you\s+are\s+now\s+(?:a|an|the)\b`, "identity override"},
		{`new\s+instructions?\s*:`, "instruction injection"},
		{`<\s*(?:system|prompt|instruction)\s*>`, "tag injection"},
		{`forget\s+(?:everything|all|your)\s+(?:previous|prior)`, "memory wipe"},
		{`act\s+as\s+if\s+you\s+(?:have\s+no|don't\s+have)\s+constraints`, "constraint bypass"},
		{`(?:do\s+not|don't)\s+follow\s+(?:your|the|any)\s+(?:rules|constraints|instructions)`, "constraint bypass"},
	}
	out := make([]injectionPattern, 0, len(raw))
	for _, r := range raw {
		out = append(out, injectionPattern{
			re:   regexp.MustCompile("(?i)" + r.pattern),
			desc: r.desc,
		})
	}
	return out
}()

// scanText checks text for injection patterns. Returns matched descriptions.
func scanText(text string) []string {
	if len(text) < 10 {
		return nil
	}
	lower := strings.ToLower(text)
	var flags []string
	seen := make(map[string]bool)
	for _, p := range injectionPatterns {
		if p.re.MatchString(lower) && !seen[p.desc] {
			flags = append(flags, p.desc)
			seen[p.desc] = true
		}
	}
	return flags
}

// trackToolDefinitions checks if the tools array in the LLM request has
// changed since the first request. Returns a flag description if mutation
// detected, or empty string if unchanged.
func trackToolDefinitions(reqBody map[string]interface{}, tracker *ToolTracker) string {
	tools, ok := reqBody["tools"].([]interface{})
	if !ok || len(tools) == 0 {
		return ""
	}

	// Serialize deterministically and hash
	data, err := json.Marshal(tools)
	if err != nil {
		return ""
	}
	hash := fmt.Sprintf("%x", sha256.Sum256(data))

	return tracker.Check(hash)
}

// ToolTracker tracks tool definition hashes across requests.
type ToolTracker struct {
	mu       sync.Mutex
	initial  string // hash of first tools array seen
	lastSeen string
}

func NewToolTracker() *ToolTracker {
	return &ToolTracker{}
}

func (tt *ToolTracker) Check(hash string) string {
	tt.mu.Lock()
	defer tt.mu.Unlock()

	if tt.initial == "" {
		tt.initial = hash
		tt.lastSeen = hash
		return ""
	}

	if hash != tt.lastSeen {
		tt.lastSeen = hash
		if hash != tt.initial {
			return fmt.Sprintf("tool definitions mutated (initial=%s current=%s)", tt.initial[:12], hash[:12])
		}
	}
	return ""
}

// scanToolMessages extracts tool-role messages from a chat completions request
// and scans their content for injection patterns AND cross-tool references.
// This runs automatically in the LLM proxy path — the body runtime doesn't
// opt in or out.
//
// ASK Tenet 1: enforcement is external and inviolable.
// ASK Tenet 17: external entities produce data, not instructions.
func scanToolMessages(reqBody map[string]interface{}) []string {
	messages, ok := reqBody["messages"].([]interface{})
	if !ok {
		return nil
	}

	// Build set of known tool names from the tools array
	toolNames := make(map[string]bool)
	if tools, ok := reqBody["tools"].([]interface{}); ok {
		for _, t := range tools {
			tool, ok := t.(map[string]interface{})
			if !ok {
				continue
			}
			fn, ok := tool["function"].(map[string]interface{})
			if !ok {
				continue
			}
			if name, ok := fn["name"].(string); ok {
				toolNames[name] = true
			}
		}
	}

	var allFlags []string
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

		// Injection pattern scan
		flags := scanText(content)
		allFlags = append(allFlags, flags...)

		// Cross-tool reference detection: flag when a tool's output
		// mentions another tool by name with call/invoke language.
		// This catches prompt injection via tool output poisoning.
		if len(toolNames) >= 2 {
			lower := strings.ToLower(content)
			for name := range toolNames {
				if !strings.Contains(lower, strings.ToLower(name)) {
					continue
				}
				// Check for invocation language around the tool name
				patterns := []string{
					`(?i)(?:call|use|invoke|run|execute)\s+` + regexp.QuoteMeta(name),
					`(?i)` + regexp.QuoteMeta(name) + `\s*\(`,
				}
				for _, pat := range patterns {
					if re, err := regexp.Compile(pat); err == nil && re.MatchString(content) {
						allFlags = append(allFlags, "cross-tool: output references tool '"+name+"'")
						break
					}
				}
			}
		}
	}
	return allFlags
}
