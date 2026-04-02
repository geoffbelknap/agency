package credstore

import (
	"encoding/json"
	"strings"
)

// entryToBackend serializes an Entry into the flat format expected by
// SecretBackend. Lists become comma-separated strings. ProtocolConfig
// becomes a JSON-encoded string.
func entryToBackend(e Entry) (name string, value string, metadata map[string]string) {
	m := map[string]string{
		"kind":       e.Metadata.Kind,
		"scope":      e.Metadata.Scope,
		"protocol":   e.Metadata.Protocol,
		"source":     e.Metadata.Source,
		"created_at": e.Metadata.CreatedAt,
		"rotated_at": e.Metadata.RotatedAt,
	}

	if e.Metadata.Service != "" {
		m["service"] = e.Metadata.Service
	}
	if e.Metadata.Group != "" {
		m["group"] = e.Metadata.Group
	}
	if e.Metadata.ExpiresAt != "" {
		m["expires_at"] = e.Metadata.ExpiresAt
	}
	if len(e.Metadata.Requires) > 0 {
		m["requires"] = strings.Join(e.Metadata.Requires, ",")
	}
	if len(e.Metadata.ExternalScopes) > 0 {
		m["external_scopes"] = strings.Join(e.Metadata.ExternalScopes, ",")
	}
	if len(e.Metadata.ProtocolConfig) > 0 {
		data, err := json.Marshal(e.Metadata.ProtocolConfig)
		if err == nil {
			m["protocol_config"] = string(data)
		}
	}

	return e.Name, e.Value, m
}

// entryFromBackend deserializes the flat backend format back into an Entry.
func entryFromBackend(name, value string, metadata map[string]string) Entry {
	e := Entry{
		Name:  name,
		Value: value,
		Metadata: Metadata{
			Kind:      metadata["kind"],
			Scope:     metadata["scope"],
			Service:   metadata["service"],
			Group:     metadata["group"],
			Protocol:  metadata["protocol"],
			Source:    metadata["source"],
			ExpiresAt: metadata["expires_at"],
			CreatedAt: metadata["created_at"],
			RotatedAt: metadata["rotated_at"],
		},
	}

	if r := metadata["requires"]; r != "" {
		e.Metadata.Requires = strings.Split(r, ",")
	}
	if s := metadata["external_scopes"]; s != "" {
		e.Metadata.ExternalScopes = strings.Split(s, ",")
	}
	if pc := metadata["protocol_config"]; pc != "" {
		var m map[string]any
		if json.Unmarshal([]byte(pc), &m) == nil {
			e.Metadata.ProtocolConfig = m
		}
	}

	return e
}
