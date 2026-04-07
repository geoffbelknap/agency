package hub

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-chi/chi/v5"
	"gopkg.in/yaml.v3"
)

// presetInfo is the summary view returned by listPresets.
type presetInfo struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Type        string `json:"type"`
	Source      string `json:"source"` // "built-in" or "user"
}

// presetFull is the full preset document stored in YAML / returned by getPreset.
type presetFull struct {
	Name         string            `yaml:"name"         json:"name"`
	Description  string            `yaml:"description"  json:"description"`
	Type         string            `yaml:"type"         json:"type"`
	ModelTier    string            `yaml:"model_tier"   json:"model_tier,omitempty"`
	Tools        []string          `yaml:"tools"        json:"tools,omitempty"`
	Capabilities []string          `yaml:"capabilities" json:"capabilities,omitempty"`
	HardLimits   []hardLimit       `yaml:"hard_limits"  json:"hard_limits,omitempty"`
	Escalation   *escalationConfig `yaml:"escalation"   json:"escalation,omitempty"`
	Identity     *identityConfig   `yaml:"identity"     json:"identity,omitempty"`
}

type hardLimit struct {
	Rule   string `yaml:"rule"   json:"rule"`
	Reason string `yaml:"reason" json:"reason"`
}

type escalationConfig struct {
	AlwaysEscalate       []string `yaml:"always_escalate"        json:"always_escalate,omitempty"`
	FlagBeforeProceeding []string `yaml:"flag_before_proceeding" json:"flag_before_proceeding,omitempty"`
}

type identityConfig struct {
	Purpose string `yaml:"purpose" json:"purpose,omitempty"`
	Body    string `yaml:"body"    json:"body,omitempty"`
}

// builtinPresetsDir returns the built-in presets directory, or "" if not set.
func (h *handler) builtinPresetsDir() string {
	if h.deps.Config.SourceDir == "" {
		return ""
	}
	return filepath.Join(h.deps.Config.SourceDir, "presets")
}

// userPresetsDir returns ~/.agency/presets.
func (h *handler) userPresetsDir() string {
	return h.deps.Config.PresetsDir()
}

// readPresetsFromDir reads all .yaml files from dir and returns presetInfo entries
// labelled with the given source string. Non-fatal errors (unreadable / invalid
// files) are silently skipped, consistent with the original listPresets behaviour.
func readPresetsFromDir(dir, source string) []presetInfo {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []presetInfo
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".yaml" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var raw struct {
			Name        string `yaml:"name"`
			Description string `yaml:"description"`
			Type        string `yaml:"type"`
		}
		if err := yaml.Unmarshal(data, &raw); err != nil || raw.Name == "" {
			continue
		}
		out = append(out, presetInfo{
			Name:        raw.Name,
			Description: raw.Description,
			Type:        raw.Type,
			Source:      source,
		})
	}
	return out
}

// listPresets returns the merged list of built-in and user presets.
// User presets with the same name shadow built-in presets.
func (h *handler) listPresets(w http.ResponseWriter, r *http.Request) {
	builtinDir := h.builtinPresetsDir()
	userDir := h.userPresetsDir()

	builtins := readPresetsFromDir(builtinDir, "built-in")
	users := readPresetsFromDir(userDir, "user")

	// Build a name → index map for quick shadow detection.
	merged := make([]presetInfo, 0, len(builtins)+len(users))
	seen := make(map[string]bool)

	// User presets take priority; add them first.
	for _, p := range users {
		seen[p.Name] = true
		merged = append(merged, p)
	}
	// Add built-ins that are not shadowed.
	for _, p := range builtins {
		if !seen[p.Name] {
			merged = append(merged, p)
		}
	}

	// If we ended up with nothing (e.g. fresh install with no source tree),
	// fall back to the static list so the CLI always has something to show.
	if len(merged) == 0 {
		merged = []presetInfo{
			{Name: "generalist", Description: "General-purpose assistant", Type: "standard", Source: "built-in"},
			{Name: "engineer", Description: "Software engineering specialist", Type: "standard", Source: "built-in"},
			{Name: "researcher", Description: "Research and analysis specialist", Type: "standard", Source: "built-in"},
			{Name: "writer", Description: "Content and documentation writer", Type: "standard", Source: "built-in"},
			{Name: "analyst", Description: "Data and business analyst", Type: "standard", Source: "built-in"},
			{Name: "ops", Description: "Operations and infrastructure specialist", Type: "standard", Source: "built-in"},
			{Name: "reviewer", Description: "Code and document reviewer", Type: "standard", Source: "built-in"},
			{Name: "coordinator", Description: "Multi-agent team coordinator", Type: "coordinator", Source: "built-in"},
			{Name: "minimal", Description: "Minimal agent with core tools only", Type: "standard", Source: "built-in"},
			{Name: "function", Description: "Single-purpose function agent", Type: "function", Source: "built-in"},
			{Name: "security-reviewer", Description: "Security review specialist", Type: "function", Source: "built-in"},
			{Name: "compliance-auditor", Description: "Compliance and policy auditor", Type: "function", Source: "built-in"},
			{Name: "privacy-monitor", Description: "Privacy monitoring agent", Type: "function", Source: "built-in"},
			{Name: "code-reviewer", Description: "Code quality reviewer", Type: "function", Source: "built-in"},
			{Name: "ops-monitor", Description: "Operations monitoring agent", Type: "function", Source: "built-in"},
		}
	}

	writeJSON(w, 200, merged)
}

// getPreset returns the full content of a single preset (user has priority over built-in).
func (h *handler) getPreset(w http.ResponseWriter, r *http.Request) {
	name, ok := requireName(w, chi.URLParam(r, "name"))
	if !ok {
		return
	}

	// Try user preset first, then built-in.
	data, source, err := h.readPresetFile(name)
	if err != nil {
		writeJSON(w, 404, map[string]string{"error": "preset not found"})
		return
	}

	var preset presetFull
	if err := yaml.Unmarshal(data, &preset); err != nil {
		writeJSON(w, 500, map[string]string{"error": "failed to parse preset"})
		return
	}

	type presetResponse struct {
		presetFull
		Source string `json:"source"`
	}
	writeJSON(w, 200, presetResponse{presetFull: preset, Source: source})
}

// createPreset writes a new user preset to ~/.agency/presets/{name}.yaml.
func (h *handler) createPreset(w http.ResponseWriter, r *http.Request) {
	var body presetFull
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid JSON"})
		return
	}
	if _, ok := requireName(w, body.Name); !ok {
		return
	}

	destDir := h.userPresetsDir()
	if err := os.MkdirAll(destDir, 0755); err != nil {
		writeJSON(w, 500, map[string]string{"error": "failed to create presets directory"})
		return
	}

	destPath := filepath.Join(destDir, body.Name+".yaml")
	if _, err := os.Stat(destPath); err == nil {
		writeJSON(w, 409, map[string]string{"error": "preset already exists"})
		return
	}

	out, err := yaml.Marshal(&body)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "failed to serialise preset"})
		return
	}
	if err := os.WriteFile(destPath, out, 0644); err != nil {
		writeJSON(w, 500, map[string]string{"error": "failed to write preset"})
		return
	}

	h.deps.Audit.WriteSystem("preset_created", map[string]interface{}{"preset": body.Name})
	writeJSON(w, 201, map[string]string{"status": "created", "name": body.Name, "source": "user"})
}

// updatePreset replaces an existing user preset. Built-in presets are read-only.
func (h *handler) updatePreset(w http.ResponseWriter, r *http.Request) {
	name, ok := requireName(w, chi.URLParam(r, "name"))
	if !ok {
		return
	}

	// Refuse to overwrite a built-in if there is no user copy.
	userPath := filepath.Join(h.userPresetsDir(), name+".yaml")
	if _, err := os.Stat(userPath); os.IsNotExist(err) {
		// Check if it's a built-in — if so, reject.
		if h.isBuiltinPreset(name) {
			writeJSON(w, 403, map[string]string{"error": "built-in presets are read-only"})
			return
		}
		writeJSON(w, 404, map[string]string{"error": "preset not found"})
		return
	}

	var body presetFull
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid JSON"})
		return
	}
	// Keep the name from the URL; ignore any name in the body.
	body.Name = name

	out, err := yaml.Marshal(&body)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "failed to serialise preset"})
		return
	}
	if err := os.WriteFile(userPath, out, 0644); err != nil {
		writeJSON(w, 500, map[string]string{"error": "failed to write preset"})
		return
	}

	h.deps.Audit.WriteSystem("preset_updated", map[string]interface{}{"preset": name})
	writeJSON(w, 200, map[string]string{"status": "updated", "name": name, "source": "user"})
}

// deletePreset removes a user preset. Built-in presets cannot be deleted.
func (h *handler) deletePreset(w http.ResponseWriter, r *http.Request) {
	name, ok := requireName(w, chi.URLParam(r, "name"))
	if !ok {
		return
	}

	userPath := filepath.Join(h.userPresetsDir(), name+".yaml")
	if _, err := os.Stat(userPath); os.IsNotExist(err) {
		if h.isBuiltinPreset(name) {
			writeJSON(w, 403, map[string]string{"error": "built-in presets are read-only"})
			return
		}
		writeJSON(w, 404, map[string]string{"error": "preset not found"})
		return
	}

	if err := os.Remove(userPath); err != nil {
		writeJSON(w, 500, map[string]string{"error": "failed to delete preset"})
		return
	}

	h.deps.Audit.WriteSystem("preset_deleted", map[string]interface{}{"preset": name})
	writeJSON(w, 200, map[string]string{"status": "deleted", "name": name})
}

// ── helpers ──────────────────────────────────────────────────────────────────

// readPresetFile reads the raw YAML bytes for a preset, trying the user
// directory first, then built-in. Returns (data, source, error).
func (h *handler) readPresetFile(name string) ([]byte, string, error) {
	userPath := filepath.Join(h.userPresetsDir(), name+".yaml")
	if data, err := os.ReadFile(userPath); err == nil {
		return data, "user", nil
	}
	builtinDir := h.builtinPresetsDir()
	if builtinDir != "" {
		builtinPath := filepath.Join(builtinDir, name+".yaml")
		if data, err := os.ReadFile(builtinPath); err == nil {
			return data, "built-in", nil
		}
	}
	return nil, "", os.ErrNotExist
}

// isBuiltinPreset returns true if name exists in the built-in presets directory
// or in the static fallback list.
func (h *handler) isBuiltinPreset(name string) bool {
	builtinDir := h.builtinPresetsDir()
	if builtinDir != "" {
		if _, err := os.Stat(filepath.Join(builtinDir, name+".yaml")); err == nil {
			return true
		}
	}
	// Check static fallback list.
	static := []string{
		"generalist", "engineer", "researcher", "writer", "analyst",
		"ops", "reviewer", "coordinator", "minimal", "function",
		"security-reviewer", "compliance-auditor", "privacy-monitor",
		"code-reviewer", "ops-monitor",
	}
	for _, s := range static {
		if strings.EqualFold(s, name) {
			return true
		}
	}
	return false
}
