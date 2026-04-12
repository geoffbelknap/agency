package admin

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"

	"github.com/geoffbelknap/agency/internal/manifestgen"
)

// generateAgentManifest builds services-manifest.json and services.yaml for an
// agent from its granted_capabilities in constraints.yaml, enabled platform
// capabilities, and preset scope declarations. Both the capability-reload path
// (cap enable/disable) and the grant/revoke path call this single function so
// the resulting files are always identical in structure.
func (h *handler) generateAgentManifest(agentName string) error {
	if !requireNameStr(agentName) {
		return fmt.Errorf("invalid agent name")
	}
	return manifestgen.Generator{
		Home:             h.deps.Config.Home,
		Logger:           h.deps.Logger,
		LoadPresetScopes: h.loadPresetScopes,
	}.GenerateAgentManifest(agentName)
}

// loadPresetScopes loads scope declarations from an agent's preset.
// Returns a map of service grant_name -> set of allowed scope strings.
// If no scopes are declared for a service, the map entry is nil (allow all).
func (h *handler) loadPresetScopes(agentName string) map[string]map[string]bool {
	if !requireNameStr(agentName) {
		return nil
	}
	agentDir := filepath.Join(h.deps.Config.Home, "agents", agentName)

	// Read agent.yaml to get preset name
	var agentCfg struct {
		Preset string `yaml:"preset"`
	}
	if data, err := os.ReadFile(filepath.Join(agentDir, "agent.yaml")); err == nil {
		yaml.Unmarshal(data, &agentCfg)
	}
	if agentCfg.Preset == "" {
		return nil
	}

	// Read preset from hub cache
	presetPaths := []string{
		filepath.Join(h.deps.Config.Home, "hub-cache", "default", "presets", agentCfg.Preset, "preset.yaml"),
		filepath.Join(h.deps.Config.Home, "presets", agentCfg.Preset+".yaml"),
	}
	var presetData []byte
	for _, p := range presetPaths {
		if d, err := os.ReadFile(p); err == nil {
			presetData = d
			break
		}
	}
	if presetData == nil {
		return nil
	}

	// Parse credential scope declarations
	var preset struct {
		Requires struct {
			Credentials []struct {
				GrantName string `yaml:"grant_name"`
				Scopes    struct {
					Required []string `yaml:"required"`
					Optional []string `yaml:"optional"`
				} `yaml:"scopes"`
			} `yaml:"credentials"`
		} `yaml:"requires"`
	}
	if yaml.Unmarshal(presetData, &preset) != nil {
		return nil
	}

	result := make(map[string]map[string]bool)
	for _, cred := range preset.Requires.Credentials {
		if cred.GrantName == "" || (len(cred.Scopes.Required) == 0 && len(cred.Scopes.Optional) == 0) {
			continue
		}
		scopes := make(map[string]bool)
		for _, s := range cred.Scopes.Required {
			scopes[s] = true
		}
		for _, s := range cred.Scopes.Optional {
			scopes[s] = true
		}
		result[cred.GrantName] = scopes
	}
	return result
}
