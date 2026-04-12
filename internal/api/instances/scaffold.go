package instances

import (
	"fmt"
	"strings"

	"github.com/geoffbelknap/agency/internal/hub"
	instancepkg "github.com/geoffbelknap/agency/internal/instances"
	"github.com/geoffbelknap/agency/internal/models"
)

type packageInstantiateRequest struct {
	Kind         string         `json:"kind"`
	Name         string         `json:"name"`
	InstanceName string         `json:"instance_name,omitempty"`
	NodeID       string         `json:"node_id,omitempty"`
	Config       map[string]any `json:"config,omitempty"`
	NodeConfig   map[string]any `json:"node_config,omitempty"`
}

func scaffoldInstanceFromPackage(pkg hub.InstalledPackage, req packageInstantiateRequest) (*instancepkg.Instance, error) {
	switch pkg.Kind {
	case "connector":
		return scaffoldConnectorInstance(pkg, req)
	default:
		return nil, fmt.Errorf("package kind %q does not support instance scaffolding yet", pkg.Kind)
	}
}

func scaffoldConnectorInstance(pkg hub.InstalledPackage, req packageInstantiateRequest) (*instancepkg.Instance, error) {
	var cfg models.ConnectorConfig
	if err := models.Load(pkg.Path, &cfg); err != nil {
		return nil, fmt.Errorf("load connector package: %w", err)
	}
	if cfg.Source.Type != "none" {
		return nil, fmt.Errorf("connector %q requires ingress/runtime composition and cannot be scaffolded as a single authority instance yet", cfg.Name)
	}
	if pkg.Spec == nil {
		return nil, fmt.Errorf("connector %q does not have installed runtime package metadata", cfg.Name)
	}
	runtimeSpec, _ := pkg.Spec["runtime"].(map[string]any)
	if runtimeSpec == nil || runtimeSpec["executor"] == nil {
		return nil, fmt.Errorf("connector %q does not expose an authority runtime executor", cfg.Name)
	}

	instanceName := strings.TrimSpace(req.InstanceName)
	if instanceName == "" {
		instanceName = cfg.Name
	}
	nodeID := strings.TrimSpace(req.NodeID)
	if nodeID == "" {
		nodeID = sanitizeNodeID(cfg.Name)
	}

	nodeConfig := connectorDefaultNodeConfig(cfg)
	mergeMap(nodeConfig, req.NodeConfig)
	instConfig := map[string]any{}
	mergeMap(instConfig, req.Config)

	inst := &instancepkg.Instance{
		Name: instanceName,
		Source: instancepkg.InstanceSource{
			Package: instancepkg.PackageRef{
				Kind:    pkg.Kind,
				Name:    pkg.Name,
				Version: pkg.Version,
			},
		},
		Nodes: []instancepkg.Node{{
			ID:   nodeID,
			Kind: "connector.authority",
			Package: instancepkg.PackageRef{
				Kind:    pkg.Kind,
				Name:    pkg.Name,
				Version: pkg.Version,
			},
			Config: nodeConfig,
		}},
		Credentials: connectorDefaultBindings(cfg),
		Config:      instConfig,
	}
	return inst, nil
}

func connectorDefaultNodeConfig(cfg models.ConnectorConfig) map[string]any {
	out := map[string]any{}
	tools := connectorToolNames(cfg)
	if len(tools) > 0 {
		items := make([]any, 0, len(tools))
		for _, name := range tools {
			items = append(items, name)
		}
		out["tools"] = items
	}
	if cfg.MCP != nil && strings.TrimSpace(cfg.MCP.Credential) != "" {
		out["credential_bindings"] = []any{cfg.MCP.Credential}
	}
	for key, raw := range cfg.Config {
		field, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if def, ok := field["default"]; ok {
			out[key] = def
		}
	}
	return out
}

func connectorDefaultBindings(cfg models.ConnectorConfig) map[string]instancepkg.Binding {
	out := map[string]instancepkg.Binding{}
	if cfg.MCP == nil || strings.TrimSpace(cfg.MCP.Credential) == "" {
		return out
	}
	target := cfg.MCP.Credential
	if cfg.Requires != nil {
		for _, cred := range cfg.Requires.Credentials {
			if strings.TrimSpace(cred.Name) == cfg.MCP.Credential && strings.TrimSpace(cred.GrantName) != "" {
				target = cred.GrantName
				break
			}
		}
	}
	out[cfg.MCP.Credential] = instancepkg.Binding{
		Type:   "credref",
		Target: "credref:" + target,
	}
	return out
}

func connectorToolNames(cfg models.ConnectorConfig) []string {
	var tools []models.ConnectorMCPTool
	if len(cfg.Tools) > 0 {
		tools = cfg.Tools
	} else if cfg.MCP != nil {
		tools = cfg.MCP.Tools
	}
	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		if strings.TrimSpace(tool.Name) == "" {
			continue
		}
		names = append(names, tool.Name)
	}
	return names
}

func mergeMap(dst map[string]any, src map[string]any) {
	for key, value := range src {
		dst[key] = value
	}
}

func sanitizeNodeID(name string) string {
	name = strings.TrimSpace(strings.ToLower(name))
	name = strings.ReplaceAll(name, "-", "_")
	name = strings.ReplaceAll(name, " ", "_")
	if name == "" {
		return "node"
	}
	return name
}
