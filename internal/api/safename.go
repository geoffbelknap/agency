package api

import (
	"net/http"
	"path/filepath"
)

// safeName validates that a user-supplied name is a safe single path component.
// It uses filepath.Base to strip directory components, preventing path traversal.
// Returns the sanitized name, or writes a 400 error and returns "" if invalid.
func safeName(w http.ResponseWriter, raw string) string {
	if raw == "" {
		writeJSON(w, 400, map[string]string{"error": "name required"})
		return ""
	}
	clean := filepath.Base(raw)
	if clean == "." || clean == ".." {
		writeJSON(w, 400, map[string]string{"error": "invalid name"})
		return ""
	}
	return clean
}
