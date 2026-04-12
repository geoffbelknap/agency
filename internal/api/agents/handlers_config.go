package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"

	"github.com/go-chi/chi/v5"
	"gopkg.in/yaml.v3"
)

// agentConfigBundle is the response shape for GET /api/v1/agents/{name}/config.
type agentConfigBundle struct {
	AgentYAML   map[string]interface{} `json:"agent_yaml"`
	Identity    string                 `json:"identity"`
	Constraints map[string]interface{} `json:"constraints"`
	Workspace   map[string]interface{} `json:"workspace,omitempty"`
}

// agentConfigDir returns the path to the agent's config directory.
func (h *handler) agentConfigDir(name string) string {
	return filepath.Join(h.deps.Config.Home, "agents", name)
}

// agentConfig handles GET /api/v1/agents/{name}/config.
// Returns the full agent config bundle (agent.yaml, identity.md, constraints.yaml, workspace.yaml).
func (h *handler) agentConfig(w http.ResponseWriter, r *http.Request) {
	name, ok := requireName(w, chi.URLParam(r, "name"))
	if !ok {
		return
	}
	dir := h.agentConfigDir(name)

	// Check agent directory exists.
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		writeJSON(w, 404, map[string]string{"error": "agent not found"})
		return
	}

	bundle := agentConfigBundle{}

	// agent.yaml
	bundle.AgentYAML = readYAMLFile(filepath.Join(dir, "agent.yaml"))

	// identity.md (raw string)
	if data, err := os.ReadFile(filepath.Join(dir, "identity.md")); err == nil {
		bundle.Identity = string(data)
	}

	// constraints.yaml
	bundle.Constraints = readYAMLFile(filepath.Join(dir, "constraints.yaml"))

	// workspace.yaml (optional)
	if ws := readYAMLFile(filepath.Join(dir, "workspace.yaml")); ws != nil {
		bundle.Workspace = ws
	}

	writeJSON(w, 200, bundle)
}

// updateAgentConfig handles PUT /api/v1/agents/{name}/config.
// Accepts partial updates — only fields present in the request body are updated.
// ASK tenet 2: every config change is audit-logged.
// ASK tenet 6: constraint changes trigger enforcer reload so the agent sees old or new — never a mix.
func (h *handler) updateAgentConfig(w http.ResponseWriter, r *http.Request) {
	name, ok := requireName(w, chi.URLParam(r, "name"))
	if !ok {
		return
	}
	dir := h.agentConfigDir(name)

	// Check agent directory exists.
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		writeJSON(w, 404, map[string]string{"error": "agent not found"})
		return
	}

	var body struct {
		AgentYAML   map[string]interface{} `json:"agent_yaml"`
		Identity    *string                `json:"identity"`
		Constraints map[string]interface{} `json:"constraints"`
		Workspace   map[string]interface{} `json:"workspace"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid JSON"})
		return
	}

	var fieldsChanged []string

	// identity.md — write raw string directly.
	if body.Identity != nil {
		if err := os.WriteFile(filepath.Join(dir, "identity.md"), []byte(*body.Identity), 0644); err != nil {
			writeJSON(w, 500, map[string]string{"error": "failed to write identity.md: " + err.Error()})
			return
		}
		fieldsChanged = append(fieldsChanged, "identity")
	}

	// constraints.yaml — read existing, merge provided keys, write back.
	if body.Constraints != nil {
		if err := mergeYAMLFile(filepath.Join(dir, "constraints.yaml"), body.Constraints); err != nil {
			writeJSON(w, 500, map[string]string{"error": "failed to update constraints.yaml: " + err.Error()})
			return
		}
		fieldsChanged = append(fieldsChanged, "constraints")
	}

	// agent.yaml — read existing, merge provided keys, write back.
	if body.AgentYAML != nil {
		if err := mergeYAMLFile(filepath.Join(dir, "agent.yaml"), body.AgentYAML); err != nil {
			writeJSON(w, 500, map[string]string{"error": "failed to update agent.yaml: " + err.Error()})
			return
		}
		fieldsChanged = append(fieldsChanged, "agent_yaml")
	}

	// workspace.yaml — read existing, merge provided keys, write back.
	if body.Workspace != nil {
		if err := mergeYAMLFile(filepath.Join(dir, "workspace.yaml"), body.Workspace); err != nil {
			writeJSON(w, 500, map[string]string{"error": "failed to update workspace.yaml: " + err.Error()})
			return
		}
		fieldsChanged = append(fieldsChanged, "workspace")
	}

	// ASK tenet 2: audit log every config change.
	h.deps.Audit.Write(name, "config_updated", map[string]interface{}{
		"fields_changed": fieldsChanged,
		"operator":       "api",
	})

	// ASK tenet 6: if constraints changed and agent is running, SIGHUP the enforcer
	// so it reloads constraints atomically — the agent sees old or new, never a mix.
	if containsString(fieldsChanged, "constraints") || containsString(fieldsChanged, "identity") {
		h.signalConfigReload(name)
	}
	if containsString(fieldsChanged, "agent_yaml") {
		if err := h.generateAgentManifest(name); err != nil {
			writeJSON(w, 500, map[string]string{"error": "failed to regenerate services manifest: " + err.Error()})
			return
		}
	}

	// Return updated config bundle.
	bundle := agentConfigBundle{}
	bundle.AgentYAML = readYAMLFile(filepath.Join(dir, "agent.yaml"))
	if data, err := os.ReadFile(filepath.Join(dir, "identity.md")); err == nil {
		bundle.Identity = string(data)
	}
	bundle.Constraints = readYAMLFile(filepath.Join(dir, "constraints.yaml"))
	if ws := readYAMLFile(filepath.Join(dir, "workspace.yaml")); ws != nil {
		bundle.Workspace = ws
	}

	writeJSON(w, 200, bundle)
}

// signalConfigReload sends SIGHUP to the agent's enforcer to reload constraints/config.
// Fire-and-forget — the enforcer may not be running (agent not started yet).
func (h *handler) signalConfigReload(agentName string) {
	go func() {
		ctx := context.Background()
		enforcerName := fmt.Sprintf("agency-%s-enforcer", agentName)
		if err := h.deps.Signal.SignalContainer(ctx, enforcerName, "SIGHUP"); err != nil {
			h.deps.Logger.Debug("config reload: enforcer SIGHUP failed (may not be running)", "agent", agentName, "err", err)
		}
	}()
}

// readYAMLFile reads a YAML file and parses it into a map. Returns nil on any error.
func readYAMLFile(path string) map[string]interface{} {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	out := map[string]interface{}{}
	if err := yaml.Unmarshal(data, &out); err != nil {
		return nil
	}
	return out
}

// mergeYAMLFile reads existing YAML at path, overlays the provided updates,
// and writes the result back. Creates the file if it does not exist.
func mergeYAMLFile(path string, updates map[string]interface{}) error {
	existing := map[string]interface{}{}
	if data, err := os.ReadFile(path); err == nil {
		// Ignore unmarshal errors — treat corrupt file as empty.
		_ = yaml.Unmarshal(data, &existing)
	}

	for k, v := range updates {
		existing[k] = v
	}

	out, err := yaml.Marshal(existing)
	if err != nil {
		return err
	}
	return os.WriteFile(path, out, 0644)
}

// containsString reports whether s is in the slice.
func containsString(slice []string, s string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
}
