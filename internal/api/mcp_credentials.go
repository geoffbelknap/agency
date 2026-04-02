package api

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/geoffbelknap/agency/internal/credstore"
)

// ── Credentials (7 tools) ──────────────────────────────────────────────────

func registerCredentialTools(reg *MCPToolRegistry) {

	// 1. agency_credential_set
	reg.Register(
		"agency_credential_set",
		"Create or update a credential in the store. Requires name, value, kind, scope, and protocol.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"name":            map[string]interface{}{"type": "string", "description": "Credential name (unique identifier)"},
				"value":           map[string]interface{}{"type": "string", "description": "Secret value"},
				"kind":            map[string]interface{}{"type": "string", "enum": []string{"provider", "service", "gateway", "internal"}, "description": "Credential kind"},
				"scope":           map[string]interface{}{"type": "string", "description": "Scope: platform, team:<name>, or agent:<name>"},
				"protocol":        map[string]interface{}{"type": "string", "enum": []string{"api-key", "jwt-exchange", "bearer", "github-app", "oauth2"}, "description": "Authentication protocol"},
				"service":         map[string]interface{}{"type": "string", "description": "Service name for routing"},
				"group":           map[string]interface{}{"type": "string", "description": "Group name to inherit protocol config from"},
				"external_scopes": map[string]interface{}{"type": "string", "description": "Comma-separated external scopes"},
				"requires":        map[string]interface{}{"type": "string", "description": "Comma-separated dependency credential names"},
				"expires_at":      map[string]interface{}{"type": "string", "description": "Expiration time in RFC3339 format"},
				"header":          map[string]interface{}{"type": "string", "description": "HTTP header for injection (protocol_config)"},
				"format":          map[string]interface{}{"type": "string", "description": "Header format e.g. 'Bearer {key}' (protocol_config)"},
				"token_url":       map[string]interface{}{"type": "string", "description": "Token exchange URL (protocol_config)"},
			},
			"required": []string{"name", "value", "kind", "scope", "protocol"},
		},
		func(h *handler, args map[string]interface{}) (string, bool) {
			if h.credStore == nil {
				return "Error: credential store not initialized", true
			}

			name, _ := args["name"].(string)
			value, _ := args["value"].(string)
			kind, _ := args["kind"].(string)
			scope, _ := args["scope"].(string)
			protocol, _ := args["protocol"].(string)
			service, _ := args["service"].(string)
			group, _ := args["group"].(string)
			expiresAt, _ := args["expires_at"].(string)

			var extScopes []string
			if es, ok := args["external_scopes"].(string); ok && es != "" {
				extScopes = strings.Split(es, ",")
			}
			var requires []string
			if req, ok := args["requires"].(string); ok && req != "" {
				requires = strings.Split(req, ",")
			}

			pc := make(map[string]any)
			if h, ok := args["header"].(string); ok && h != "" {
				pc["header"] = h
			}
			if f, ok := args["format"].(string); ok && f != "" {
				pc["format"] = f
			}
			if tu, ok := args["token_url"].(string); ok && tu != "" {
				pc["token_url"] = tu
			}

			now := time.Now().UTC().Format(time.RFC3339)
			entry := credstore.Entry{
				Name:  name,
				Value: value,
				Metadata: credstore.Metadata{
					Kind:           kind,
					Scope:          scope,
					Protocol:       protocol,
					Service:        service,
					Group:          group,
					ExternalScopes: extScopes,
					Requires:       requires,
					ExpiresAt:      expiresAt,
					ProtocolConfig: pc,
					Source:         "mcp",
					CreatedAt:      now,
					RotatedAt:      now,
				},
			}

			if err := h.credStore.Put(entry); err != nil {
				return "Error: " + err.Error(), true
			}
			return fmt.Sprintf("Credential %q stored (kind=%s scope=%s protocol=%s)", name, kind, scope, protocol), false
		},
	)

	// 2. agency_credential_list
	reg.Register(
		"agency_credential_list",
		"List credentials in the store, optionally filtered by kind, scope, service, or group. Values are always redacted.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"kind":    map[string]interface{}{"type": "string", "description": "Filter by kind"},
				"scope":   map[string]interface{}{"type": "string", "description": "Filter by scope"},
				"service": map[string]interface{}{"type": "string", "description": "Filter by service"},
				"group":   map[string]interface{}{"type": "string", "description": "Filter by group"},
			},
		},
		func(h *handler, args map[string]interface{}) (string, bool) {
			if h.credStore == nil {
				return "Error: credential store not initialized", true
			}

			filter := credstore.Filter{}
			if k, ok := args["kind"].(string); ok {
				filter.Kind = k
			}
			if s, ok := args["scope"].(string); ok {
				filter.Scope = s
			}
			if s, ok := args["service"].(string); ok {
				filter.Service = s
			}
			if g, ok := args["group"].(string); ok {
				filter.Group = g
			}

			entries, err := h.credStore.List(filter)
			if err != nil {
				return "Error: " + err.Error(), true
			}

			if len(entries) == 0 {
				return "No credentials found", false
			}

			var lines []string
			lines = append(lines, fmt.Sprintf("Credentials (%d):", len(entries)))
			for _, e := range entries {
				line := fmt.Sprintf("  %-30s kind=%-10s scope=%-20s protocol=%-12s", e.Name, e.Metadata.Kind, e.Metadata.Scope, e.Metadata.Protocol)
				if e.Metadata.Service != "" {
					line += " service=" + e.Metadata.Service
				}
				if e.Metadata.Group != "" {
					line += " group=" + e.Metadata.Group
				}
				if e.Metadata.ExpiresAt != "" {
					line += " expires=" + e.Metadata.ExpiresAt
				}
				lines = append(lines, line)
			}
			return strings.Join(lines, "\n"), false
		},
	)

	// 3. agency_credential_show
	reg.Register(
		"agency_credential_show",
		"Show a credential's metadata. Value is redacted.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"name": map[string]interface{}{"type": "string", "description": "Credential name"},
			},
			"required": []string{"name"},
		},
		func(h *handler, args map[string]interface{}) (string, bool) {
			if h.credStore == nil {
				return "Error: credential store not initialized", true
			}

			name, _ := args["name"].(string)
			entry, err := h.credStore.Get(name)
			if err != nil {
				return "Error: " + err.Error(), true
			}

			var lines []string
			lines = append(lines, fmt.Sprintf("Credential: %s", entry.Name))
			lines = append(lines, fmt.Sprintf("  Value:     [redacted]"))
			lines = append(lines, fmt.Sprintf("  Kind:      %s", entry.Metadata.Kind))
			lines = append(lines, fmt.Sprintf("  Scope:     %s", entry.Metadata.Scope))
			lines = append(lines, fmt.Sprintf("  Protocol:  %s", entry.Metadata.Protocol))
			if entry.Metadata.Service != "" {
				lines = append(lines, fmt.Sprintf("  Service:   %s", entry.Metadata.Service))
			}
			if entry.Metadata.Group != "" {
				lines = append(lines, fmt.Sprintf("  Group:     %s", entry.Metadata.Group))
			}
			if len(entry.Metadata.ExternalScopes) > 0 {
				lines = append(lines, fmt.Sprintf("  ExtScopes: %s", strings.Join(entry.Metadata.ExternalScopes, ", ")))
			}
			if len(entry.Metadata.Requires) > 0 {
				lines = append(lines, fmt.Sprintf("  Requires:  %s", strings.Join(entry.Metadata.Requires, ", ")))
			}
			if entry.Metadata.ExpiresAt != "" {
				lines = append(lines, fmt.Sprintf("  Expires:   %s", entry.Metadata.ExpiresAt))
			}
			lines = append(lines, fmt.Sprintf("  Created:   %s", entry.Metadata.CreatedAt))
			lines = append(lines, fmt.Sprintf("  Rotated:   %s", entry.Metadata.RotatedAt))
			if entry.Metadata.Source != "" {
				lines = append(lines, fmt.Sprintf("  Source:    %s", entry.Metadata.Source))
			}
			return strings.Join(lines, "\n"), false
		},
	)

	// 4. agency_credential_delete
	reg.Register(
		"agency_credential_delete",
		"Delete a credential from the store.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"name": map[string]interface{}{"type": "string", "description": "Credential name to delete"},
			},
			"required": []string{"name"},
		},
		func(h *handler, args map[string]interface{}) (string, bool) {
			if h.credStore == nil {
				return "Error: credential store not initialized", true
			}

			name, _ := args["name"].(string)
			if err := h.credStore.Delete(name); err != nil {
				return "Error: " + err.Error(), true
			}
			return fmt.Sprintf("Credential %q deleted", name), false
		},
	)

	// 5. agency_credential_rotate
	reg.Register(
		"agency_credential_rotate",
		"Rotate a credential's value while preserving all metadata.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"name":  map[string]interface{}{"type": "string", "description": "Credential name"},
				"value": map[string]interface{}{"type": "string", "description": "New secret value"},
			},
			"required": []string{"name", "value"},
		},
		func(h *handler, args map[string]interface{}) (string, bool) {
			if h.credStore == nil {
				return "Error: credential store not initialized", true
			}

			name, _ := args["name"].(string)
			value, _ := args["value"].(string)
			if err := h.credStore.Rotate(name, value); err != nil {
				return "Error: " + err.Error(), true
			}
			return fmt.Sprintf("Credential %q rotated", name), false
		},
	)

	// 6. agency_credential_test
	reg.Register(
		"agency_credential_test",
		"Test a credential's connectivity by performing an end-to-end health check.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"name": map[string]interface{}{"type": "string", "description": "Credential name to test"},
			},
			"required": []string{"name"},
		},
		func(h *handler, args map[string]interface{}) (string, bool) {
			if h.credStore == nil {
				return "Error: credential store not initialized", true
			}

			name, _ := args["name"].(string)
			result, err := h.credStore.Test(name)
			if err != nil {
				return "Error: " + err.Error(), true
			}

			status := "PASS"
			if !result.OK {
				status = "FAIL"
			}
			msg := fmt.Sprintf("Test %s: %s (%dms)", status, result.Message, result.Latency)
			if result.Status > 0 {
				msg += fmt.Sprintf(" [HTTP %d]", result.Status)
			}
			return msg, !result.OK
		},
	)

	// 7. agency_credential_group_create
	reg.Register(
		"agency_credential_group_create",
		"Create a credential group that shares protocol config across multiple credentials.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"name":         map[string]interface{}{"type": "string", "description": "Group name"},
				"protocol":     map[string]interface{}{"type": "string", "description": "Authentication protocol"},
				"token_url":    map[string]interface{}{"type": "string", "description": "Token exchange URL (for jwt-exchange)"},
				"token_params": map[string]interface{}{"type": "string", "description": "Token params as JSON string"},
				"requires":     map[string]interface{}{"type": "string", "description": "Comma-separated dependency credential names"},
			},
			"required": []string{"name", "protocol"},
		},
		func(h *handler, args map[string]interface{}) (string, bool) {
			if h.credStore == nil {
				return "Error: credential store not initialized", true
			}

			name, _ := args["name"].(string)
			protocol, _ := args["protocol"].(string)

			pc := make(map[string]any)
			if tu, ok := args["token_url"].(string); ok && tu != "" {
				pc["token_url"] = tu
			}
			if tp, ok := args["token_params"].(string); ok && tp != "" {
				var params map[string]any
				if json.Unmarshal([]byte(tp), &params) == nil {
					pc["token_params"] = params
				}
			}

			var requires []string
			if req, ok := args["requires"].(string); ok && req != "" {
				requires = strings.Split(req, ",")
			}

			now := time.Now().UTC().Format(time.RFC3339)
			entry := credstore.Entry{
				Name:  name,
				Value: "",
				Metadata: credstore.Metadata{
					Kind:           credstore.KindGroup,
					Scope:          "platform",
					Protocol:       protocol,
					Requires:       requires,
					ProtocolConfig: pc,
					Source:         "mcp",
					CreatedAt:      now,
					RotatedAt:      now,
				},
			}

			if err := h.credStore.Put(entry); err != nil {
				return "Error: " + err.Error(), true
			}
			return fmt.Sprintf("Group %q created (protocol=%s)", name, protocol), false
		},
	)
}
