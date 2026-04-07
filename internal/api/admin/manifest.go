package admin

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/geoffbelknap/agency/internal/capabilities"
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
	agentDir := filepath.Join(h.deps.Config.Home, "agents", agentName)

	// --- Read granted capabilities from constraints.yaml ---
	constraintsPath := filepath.Join(agentDir, "constraints.yaml")
	var constraints map[string]interface{}
	if data, err := os.ReadFile(constraintsPath); err == nil {
		yaml.Unmarshal(data, &constraints)
	}

	grantedList, _ := constraints["granted_capabilities"].([]interface{})
	granted := map[string]bool{}
	for _, g := range grantedList {
		if s, ok := g.(string); ok {
			granted[s] = true
		}
	}

	// --- Load preset scope declarations for tool filtering ---
	presetScopes := h.loadPresetScopes(agentName)

	// --- Build enabled-service map from capability registry ---
	reg := capabilities.NewRegistry(h.deps.Config.Home)
	allCaps := reg.List()
	enabledServices := map[string]capabilities.Entry{}
	for _, cap := range allCaps {
		if cap.Kind == "service" && cap.State != "disabled" {
			enabledServices[cap.Name] = cap
		}
	}

	// --- Collect service definitions ---
	var services []map[string]interface{}
	loadedServices := map[string]bool{}

	// loadServiceDef reads a service definition and appends it to services.
	loadServiceDef := func(svcName string) {
		if loadedServices[svcName] {
			return
		}
		// Try registry first, then flat services dir, then directory-based layout
		candidates := []string{
			filepath.Join(h.deps.Config.Home, "registry", "services", svcName+".yaml"),
			filepath.Join(h.deps.Config.Home, "services", svcName+".yaml"),
			filepath.Join(h.deps.Config.Home, "registry", "services", svcName, "service.yaml"),
			filepath.Join(h.deps.Config.Home, "services", svcName, "service.yaml"),
		}
		var data []byte
		for _, path := range candidates {
			if d, err := os.ReadFile(path); err == nil {
				data = d
				break
			}
		}
		if data == nil {
			return
		}
		var svcDef map[string]interface{}
		if yaml.Unmarshal(data, &svcDef) != nil {
			return
		}
		scopedPrefix, _ := nestedStr(svcDef, "credential", "scoped_prefix")
		if scopedPrefix == "" {
			scopedPrefix = "agency-scoped-" + svcName
		}
		svcDef["scoped_token"] = scopedPrefix + "-" + agentName

		// Filter tools by scope when preset declares scopes for this service
		if allowedScopes := presetScopes[svcName]; allowedScopes != nil {
			if tools, ok := svcDef["tools"].([]interface{}); ok {
				var filtered []interface{}
				for _, t := range tools {
					toolMap, ok := t.(map[string]interface{})
					if !ok {
						filtered = append(filtered, t)
						continue
					}
					scope, _ := toolMap["scope"].(string)
					if scope == "" || allowedScopes[scope] {
						filtered = append(filtered, t)
					} else {
						toolName, _ := toolMap["name"].(string)
						h.deps.Logger.Info("tool filtered by scope",
							"agent", agentName,
							"service", svcName,
							"tool", toolName,
							"required_scope", scope)
					}
				}
				svcDef["tools"] = filtered
			}
		}

		services = append(services, svcDef)
		loadedServices[svcName] = true
	}

	// 1. Add enabled capabilities (from cap registry) that the agent can access
	for svcName, cap := range enabledServices {
		accessible := false
		if cap.State == "available" {
			accessible = true
		} else if cap.State == "restricted" {
			for _, a := range cap.Agents {
				if a == agentName {
					accessible = true
					break
				}
			}
		}
		if granted[svcName] {
			accessible = true
		}
		if !accessible {
			continue
		}
		loadServiceDef(svcName)
	}

	// 2. Add granted services not in the cap registry (e.g., per-agent scoped
	// service definitions created by agency grant)
	for svcName := range granted {
		loadServiceDef(svcName)
	}

	// --- Also scan service directories for granted services (handles both
	// flat files and directory-based layouts that loadServiceDef by name
	// might miss if the filename differs from the grant name) ---
	svcDirs := []string{
		filepath.Join(h.deps.Config.Home, "services"),
		filepath.Join(h.deps.Config.Home, "registry", "services"),
	}
	for _, svcDir := range svcDirs {
		entries, err := os.ReadDir(svcDir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			var svcName string
			if e.IsDir() {
				svcName = e.Name()
			} else if strings.HasSuffix(e.Name(), ".yaml") {
				svcName = strings.TrimSuffix(e.Name(), ".yaml")
			} else {
				continue
			}
			if !granted[svcName] {
				continue
			}
			loadServiceDef(svcName)
		}
	}

	// --- Write services-manifest.json ---
	manifest := map[string]interface{}{
		"version":  1,
		"agent":    agentName,
		"services": services,
	}
	manifestData, _ := json.Marshal(manifest)
	if err := os.WriteFile(filepath.Join(agentDir, "services-manifest.json"), manifestData, 0644); err != nil {
		return fmt.Errorf("write services-manifest.json: %w", err)
	}

	// --- Write services.yaml with allowed_scopes ---
	var grantEntries []map[string]interface{}
	for _, svcMap := range services {
		svcName, _ := svcMap["service"].(string)
		if svcName != "" {
			entry := map[string]interface{}{
				"service":    svcName,
				"granted_by": "operator",
			}
			if scopes := presetScopes[svcName]; scopes != nil {
				scopeList := make([]string, 0, len(scopes))
				for s := range scopes {
					scopeList = append(scopeList, s)
				}
				entry["allowed_scopes"] = scopeList
			}
			grantEntries = append(grantEntries, entry)
		}
	}
	grantsYAML := map[string]interface{}{
		"agent":  agentName,
		"grants": grantEntries,
	}
	grantsData, _ := yaml.Marshal(grantsYAML)
	if err := os.WriteFile(filepath.Join(agentDir, "services.yaml"), grantsData, 0644); err != nil {
		return fmt.Errorf("write services.yaml: %w", err)
	}

	h.deps.Logger.Info("agent manifest generated",
		"agent", agentName,
		"services", len(services))
	return nil
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
