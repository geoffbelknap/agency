package api

import (
	"encoding/json"
	"fmt"

	"github.com/geoffbelknap/agency/internal/models"
)

func registerProfileTools(reg *MCPToolRegistry) {

	// agency_profile_list
	reg.Register(
		"agency_profile_list",
		"List all profiles. Optionally filter by type (operator or agent).",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"type": map[string]interface{}{"type": "string", "description": "Filter by profile type: operator or agent"},
			},
		},
		func(d *mcpDeps, args map[string]interface{}) (string, bool) {
			if d.profileStore == nil {
				return "Profile store not initialized.", true
			}
			filterType := mapStr(args, "type")
			profiles, err := d.profileStore.List(filterType)
			if err != nil {
				return "Error: " + err.Error(), true
			}
			if len(profiles) == 0 {
				return "No profiles found.", false
			}
			data, _ := json.MarshalIndent(profiles, "", "  ")
			return string(data), false
		},
	)

	// agency_profile_show
	reg.Register(
		"agency_profile_show",
		"Show a profile by ID.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"id": map[string]interface{}{"type": "string", "description": "Profile ID"},
			},
			"required": []string{"id"},
		},
		func(d *mcpDeps, args map[string]interface{}) (string, bool) {
			if d.profileStore == nil {
				return "Profile store not initialized.", true
			}
			id := mapStr(args, "id")
			if id == "" {
				return "Error: id is required", true
			}
			p, err := d.profileStore.Get(id)
			if err != nil {
				return "Error: " + err.Error(), true
			}
			data, _ := json.MarshalIndent(p, "", "  ")
			return string(data), false
		},
	)

	// agency_profile_set
	reg.Register(
		"agency_profile_set",
		"Create or update a profile for an operator or agent. Sets display name, avatar, email, department, title, bio, and metadata.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"id":           map[string]interface{}{"type": "string", "description": "Profile ID (matches principal ID or agent name)"},
				"type":         map[string]interface{}{"type": "string", "description": "Profile type: operator or agent"},
				"display_name": map[string]interface{}{"type": "string", "description": "Human-friendly display name"},
				"avatar_url":   map[string]interface{}{"type": "string", "description": "URL or path to avatar image"},
				"email":        map[string]interface{}{"type": "string", "description": "Contact email address"},
				"department":   map[string]interface{}{"type": "string", "description": "Department name"},
				"title":        map[string]interface{}{"type": "string", "description": "Job title or role"},
				"bio":          map[string]interface{}{"type": "string", "description": "Short biography or description"},
				"metadata":     map[string]interface{}{"type": "object", "description": "Arbitrary key-value metadata", "additionalProperties": map[string]interface{}{"type": "string"}},
			},
			"required": []string{"id", "type", "display_name"},
		},
		func(d *mcpDeps, args map[string]interface{}) (string, bool) {
			if d.profileStore == nil {
				return "Profile store not initialized.", true
			}

			id := mapStr(args, "id")
			pType := mapStr(args, "type")
			displayName := mapStr(args, "display_name")

			if id == "" || pType == "" || displayName == "" {
				return "Error: id, type, and display_name are required", true
			}

			p := models.Profile{
				ID:          id,
				Type:        pType,
				DisplayName: displayName,
				AvatarURL:   mapStr(args, "avatar_url"),
				Email:       mapStr(args, "email"),
				Department:  mapStr(args, "department"),
				Title:       mapStr(args, "title"),
				Bio:         mapStr(args, "bio"),
			}

			if md, ok := args["metadata"].(map[string]interface{}); ok {
				p.Metadata = make(map[string]string, len(md))
				for k, v := range md {
					if s, ok := v.(string); ok {
						p.Metadata[k] = s
					}
				}
			}

			if err := d.profileStore.Put(p); err != nil {
				return "Error: " + err.Error(), true
			}

			return fmt.Sprintf("Profile %q (%s) saved: %s", id, pType, displayName), false
		},
	)

	// agency_profile_delete
	reg.Register(
		"agency_profile_delete",
		"Delete a profile by ID.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"id": map[string]interface{}{"type": "string", "description": "Profile ID to delete"},
			},
			"required": []string{"id"},
		},
		func(d *mcpDeps, args map[string]interface{}) (string, bool) {
			if d.profileStore == nil {
				return "Profile store not initialized.", true
			}
			id := mapStr(args, "id")
			if id == "" {
				return "Error: id is required", true
			}
			if err := d.profileStore.Delete(id); err != nil {
				return "Error: " + err.Error(), true
			}
			return fmt.Sprintf("Profile %q deleted.", id), false
		},
	)
}
