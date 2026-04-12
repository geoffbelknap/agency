package manifestgen

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/geoffbelknap/agency/internal/capabilities"
	"github.com/geoffbelknap/agency/internal/models"
	runpkg "github.com/geoffbelknap/agency/internal/runtime"
)

type Generator struct {
	Home             string
	Logger           *slog.Logger
	LoadPresetScopes func(agentName string) map[string]map[string]bool
}

func (g Generator) GenerateAgentManifest(agentName string) error {
	if !validName(agentName) {
		return fmt.Errorf("invalid agent name")
	}
	agentDir := filepath.Join(g.Home, "agents", agentName)

	var constraints map[string]interface{}
	if data, err := os.ReadFile(filepath.Join(agentDir, "constraints.yaml")); err == nil {
		_ = yaml.Unmarshal(data, &constraints)
	}

	grantedList, _ := constraints["granted_capabilities"].([]interface{})
	granted := map[string]bool{}
	for _, item := range grantedList {
		if svcName, ok := item.(string); ok {
			granted[svcName] = true
		}
	}

	presetScopes := map[string]map[string]bool(nil)
	if g.LoadPresetScopes != nil {
		presetScopes = g.LoadPresetScopes(agentName)
	} else {
		presetScopes = g.defaultLoadPresetScopes(agentName)
	}

	reg := capabilities.NewRegistry(g.Home)
	allCaps := reg.List()
	enabledServices := map[string]capabilities.Entry{}
	for _, cap := range allCaps {
		if cap.Kind == "service" && cap.State != "disabled" {
			enabledServices[cap.Name] = cap
		}
	}

	var services []map[string]interface{}
	loadedServices := map[string]bool{}

	loadServiceDef := func(svcName string) {
		if loadedServices[svcName] {
			return
		}
		candidates := []string{
			filepath.Join(g.Home, "registry", "services", svcName+".yaml"),
			filepath.Join(g.Home, "services", svcName+".yaml"),
			filepath.Join(g.Home, "registry", "services", svcName, "service.yaml"),
			filepath.Join(g.Home, "services", svcName, "service.yaml"),
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
						continue
					}
					toolName, _ := toolMap["name"].(string)
					g.logger().Info("tool filtered by scope",
						"agent", agentName,
						"service", svcName,
						"tool", toolName,
						"required_scope", scope)
				}
				svcDef["tools"] = filtered
			}
		}

		services = append(services, svcDef)
		loadedServices[svcName] = true
	}

	for svcName, cap := range enabledServices {
		accessible := false
		if cap.State == "available" {
			accessible = true
		} else if cap.State == "restricted" {
			for _, allowedAgent := range cap.Agents {
				if allowedAgent == agentName {
					accessible = true
					break
				}
			}
		}
		if granted[svcName] {
			accessible = true
		}
		if accessible {
			loadServiceDef(svcName)
		}
	}

	for svcName := range granted {
		loadServiceDef(svcName)
	}

	for _, svcDir := range []string{
		filepath.Join(g.Home, "services"),
		filepath.Join(g.Home, "registry", "services"),
	} {
		entries, err := os.ReadDir(svcDir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			var svcName string
			if entry.IsDir() {
				svcName = entry.Name()
			} else if strings.HasSuffix(entry.Name(), ".yaml") {
				svcName = strings.TrimSuffix(entry.Name(), ".yaml")
			} else {
				continue
			}
			if granted[svcName] {
				loadServiceDef(svcName)
			}
		}
	}

	projected, err := g.projectRuntimeServices(agentName)
	if err != nil {
		return err
	}
	services = append(services, projected...)

	manifest := map[string]interface{}{
		"version":  1,
		"agent":    agentName,
		"services": services,
	}
	manifestData, _ := json.Marshal(manifest)
	if err := os.WriteFile(filepath.Join(agentDir, "services-manifest.json"), manifestData, 0o644); err != nil {
		return fmt.Errorf("write services-manifest.json: %w", err)
	}

	var grantEntries []map[string]interface{}
	for _, svcMap := range services {
		svcName, _ := svcMap["service"].(string)
		if svcName == "" {
			continue
		}
		entry := map[string]interface{}{
			"service":    svcName,
			"granted_by": "operator",
		}
		if scopes := presetScopes[svcName]; scopes != nil {
			scopeList := make([]string, 0, len(scopes))
			for scope := range scopes {
				scopeList = append(scopeList, scope)
			}
			entry["allowed_scopes"] = scopeList
		}
		grantEntries = append(grantEntries, entry)
	}
	grantsData, _ := yaml.Marshal(map[string]interface{}{
		"agent":  agentName,
		"grants": grantEntries,
	})
	if err := os.WriteFile(filepath.Join(agentDir, "services.yaml"), grantsData, 0o644); err != nil {
		return fmt.Errorf("write services.yaml: %w", err)
	}

	g.logger().Info("agent manifest generated",
		"agent", agentName,
		"services", len(services))
	return nil
}

func (g Generator) projectRuntimeServices(agentName string) ([]map[string]interface{}, error) {
	cfg, err := g.loadAgentConfig(agentName)
	if err != nil {
		return nil, err
	}
	if len(cfg.Instances.Attach) == 0 {
		return nil, nil
	}

	var services []map[string]interface{}
	for _, attachment := range cfg.Instances.Attach {
		instanceDir := filepath.Join(g.Home, "instances", attachment.InstanceID)
		manifest, err := runpkg.NewStore(instanceDir).LoadManifest()
		if err != nil {
			g.logger().Warn("skipping runtime projection without manifest",
				"agent", agentName,
				"instance_id", attachment.InstanceID,
				"node_id", attachment.NodeID,
				"err", err)
			continue
		}
		node := findRuntimeNode(manifest, attachment.NodeID)
		if node == nil || node.Kind != "connector.authority" || node.Executor == nil {
			continue
		}
		actions := projectedActions(manifest, node, attachment.Actions)
		if len(actions) == 0 {
			continue
		}
		serviceName := sanitizeIdentifier("runtime_" + manifest.Metadata.InstanceName + "_" + node.NodeID)
		var tools []map[string]interface{}
		for _, action := range actions {
			toolName := sanitizeIdentifier("instance_" + manifest.Metadata.InstanceName + "_" + node.NodeID + "_" + action)
			parameters := []interface{}{}
			if requirement, ok := node.ConsentRequirements[action]; ok && manifest.Source.ConsentDeploymentID != "" {
				parameters = append(parameters,
					map[string]interface{}{
						"name":        requirement.TokenInputField,
						"type":        "string",
						"description": "Consent token authorizing this action",
					},
					map[string]interface{}{
						"name":        requirement.TargetInputField,
						"type":        "string",
						"description": "Consent target for this action",
					},
				)
			}
			tools = append(tools, map[string]interface{}{
				"name":        toolName,
				"description": fmt.Sprintf("Invoke %s on instance %s node %s", action, manifest.Metadata.InstanceName, node.NodeID),
				"method":      "POST",
				"path": fmt.Sprintf(
					"/api/v1/instances/%s/runtime/nodes/%s/actions/%s",
					attachment.InstanceID,
					node.NodeID,
					action,
				),
				"parameters":  parameters,
				"passthrough": true,
			})
		}
		services = append(services, map[string]interface{}{
			"service":      serviceName,
			"display_name": fmt.Sprintf("Runtime %s/%s", manifest.Metadata.InstanceName, node.NodeID),
			"api_base":     "http://enforcer:8081/mediation/runtime",
			"description":  fmt.Sprintf("Projected authority tools from instance %s node %s", manifest.Metadata.InstanceName, node.NodeID),
			"credential": map[string]interface{}{
				"env_var":       "AGENCY_GATEWAY_TOKEN",
				"header":        "Authorization",
				"scoped_prefix": "agency-scoped-runtime",
			},
			"scoped_token": "runtime",
			"tools":        tools,
		})
	}
	return services, nil
}

func (g Generator) loadAgentConfig(agentName string) (*models.AgentConfig, error) {
	agentPath := filepath.Join(g.Home, "agents", agentName, "agent.yaml")
	data, err := os.ReadFile(agentPath)
	if err != nil {
		return nil, fmt.Errorf("read agent.yaml: %w", err)
	}
	var cfg models.AgentConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse agent.yaml: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("validate agent.yaml: %w", err)
	}
	return &cfg, nil
}

func (g Generator) AttachedAgents(instanceID string) ([]string, error) {
	if strings.TrimSpace(instanceID) == "" {
		return nil, fmt.Errorf("instance id is required")
	}
	agentsDir := filepath.Join(g.Home, "agents")
	entries, err := os.ReadDir(agentsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var attached []string
	for _, entry := range entries {
		if !entry.IsDir() || !validName(entry.Name()) {
			continue
		}
		cfg, err := g.loadAgentConfig(entry.Name())
		if err != nil {
			continue
		}
		for _, attachment := range cfg.Instances.Attach {
			if attachment.InstanceID == instanceID {
				attached = append(attached, entry.Name())
				break
			}
		}
	}
	sort.Strings(attached)
	return attached, nil
}

func projectedActions(manifest *runpkg.Manifest, node *runpkg.RuntimeNode, requested []string) []string {
	allowed := map[string]bool{}
	for _, action := range node.Tools {
		if contains(node.ConsentActions, action) {
			if manifest == nil || manifest.Source.ConsentDeploymentID == "" {
				continue
			}
			if _, ok := node.ConsentRequirements[action]; !ok {
				continue
			}
		}
		allowed[action] = true
	}
	if node.Executor != nil {
		for action := range allowed {
			if _, ok := node.Executor.Actions[action]; !ok {
				delete(allowed, action)
			}
		}
	}

	var actions []string
	if len(requested) > 0 {
		for _, action := range requested {
			if allowed[action] {
				actions = append(actions, action)
			}
		}
	} else {
		for action := range allowed {
			actions = append(actions, action)
		}
	}
	sort.Strings(actions)
	return actions
}

func findRuntimeNode(manifest *runpkg.Manifest, nodeID string) *runpkg.RuntimeNode {
	for i := range manifest.Runtime.Nodes {
		if manifest.Runtime.Nodes[i].NodeID == nodeID {
			return &manifest.Runtime.Nodes[i]
		}
	}
	return nil
}

func sanitizeIdentifier(value string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(value) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			continue
		}
		b.WriteByte('_')
	}
	return strings.Trim(b.String(), "_")
}

func nestedStr(m map[string]interface{}, keys ...string) (string, bool) {
	cur := m
	for i, key := range keys {
		value, ok := cur[key]
		if !ok {
			return "", false
		}
		if i == len(keys)-1 {
			s, ok := value.(string)
			return s, ok
		}
		next, ok := value.(map[string]interface{})
		if !ok {
			return "", false
		}
		cur = next
	}
	return "", false
}

func validName(name string) bool {
	if strings.TrimSpace(name) == "" {
		return false
	}
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			continue
		}
		return false
	}
	return true
}

func contains(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}

func (g Generator) logger() *slog.Logger {
	if g.Logger != nil {
		return g.Logger
	}
	return slog.Default()
}

func (g Generator) defaultLoadPresetScopes(agentName string) map[string]map[string]bool {
	agentDir := filepath.Join(g.Home, "agents", agentName)
	var agentCfg struct {
		Preset string `yaml:"preset"`
	}
	if data, err := os.ReadFile(filepath.Join(agentDir, "agent.yaml")); err == nil {
		_ = yaml.Unmarshal(data, &agentCfg)
	}
	if agentCfg.Preset == "" {
		return nil
	}

	presetPaths := []string{
		filepath.Join(g.Home, "hub-cache", "default", "presets", agentCfg.Preset, "preset.yaml"),
		filepath.Join(g.Home, "presets", agentCfg.Preset+".yaml"),
	}
	var presetData []byte
	for _, path := range presetPaths {
		if data, err := os.ReadFile(path); err == nil {
			presetData = data
			break
		}
	}
	if presetData == nil {
		return nil
	}

	var preset struct {
		Requires struct {
			Credentials []struct {
				GrantName     string   `yaml:"grant_name"`
				AllowedScopes []string `yaml:"allowed_scopes"`
			} `yaml:"credentials"`
		} `yaml:"requires"`
	}
	if err := yaml.Unmarshal(presetData, &preset); err != nil {
		return nil
	}

	result := make(map[string]map[string]bool)
	for _, cred := range preset.Requires.Credentials {
		if cred.GrantName == "" {
			continue
		}
		if len(cred.AllowedScopes) == 0 {
			result[cred.GrantName] = nil
			continue
		}
		allowed := make(map[string]bool, len(cred.AllowedScopes))
		for _, scope := range cred.AllowedScopes {
			allowed[scope] = true
		}
		result[cred.GrantName] = allowed
	}
	if len(result) == 0 {
		return nil
	}
	return result
}
