package api

import (
	"net/http"
	"regexp"
)

// validResourceName matches lowercase alphanumeric names with hyphens, 1-64 chars.
// This is a whitelist — path traversal characters (.., /, \) categorically cannot match.
// Aligns with existing creation-time validators: validateAgentName, reMissionName, validPresetName.
var validResourceName = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,62}[a-z0-9])?$`)

// requireName validates a user-supplied resource name from a URL param, query param, or JSON body.
// Returns the name and true if valid, or writes a 400 response and returns ("", false).
func requireName(w http.ResponseWriter, raw string) (string, bool) {
	if !validResourceName.MatchString(raw) {
		writeJSON(w, 400, map[string]string{"error": "invalid name"})
		return "", false
	}
	return raw, true
}

// requireNameStr validates a resource name without writing an HTTP response.
// For use in MCP tool handlers and internal functions that format their own errors.
func requireNameStr(name string) bool {
	return validResourceName.MatchString(name)
}
