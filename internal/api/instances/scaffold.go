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

	instanceName := strings.TrimSpace(req.InstanceName)
	if instanceName == "" {
		instanceName = cfg.Name
	}
	nodeID := strings.TrimSpace(req.NodeID)
	if nodeID == "" {
		nodeID = sanitizeNodeID(cfg.Name)
	}
	instConfig := map[string]any{}
	mergeMap(instConfig, connectorDefaultInstanceConfig(cfg))
	mergeMap(instConfig, req.Config)

	nodes := []instancepkg.Node{}
	packageRef := instancepkg.PackageRef{
		Kind:    pkg.Kind,
		Name:    pkg.Name,
		Version: pkg.Version,
	}
	nodeConfig := map[string]any{}
	nodeKind := "connector.ingress"
	if cfg.Source.Type == "none" {
		if pkg.Spec == nil {
			return nil, fmt.Errorf("connector %q does not have installed runtime package metadata", cfg.Name)
		}
		runtimeSpec, _ := pkg.Spec["runtime"].(map[string]any)
		if runtimeSpec == nil || runtimeSpec["executor"] == nil {
			return nil, fmt.Errorf("connector %q does not expose an authority runtime executor", cfg.Name)
		}
		nodeKind = "connector.authority"
		nodeConfig = connectorDefaultAuthorityNodeConfig(cfg, instConfig)
		mergeMap(nodeConfig, req.NodeConfig)
		nodes = append(nodes, instancepkg.Node{
			ID:      nodeID,
			Kind:    nodeKind,
			Package: packageRef,
			Config:  nodeConfig,
		})
	} else {
		mergeMap(nodeConfig, req.NodeConfig)
		nodes = append(nodes, instancepkg.Node{
			ID:      nodeID,
			Kind:    nodeKind,
			Package: packageRef,
			Config:  nodeConfig,
		})
		if packageHasAuthorityRuntime(pkg) {
			nodes = append(nodes, instancepkg.Node{
				ID:      nodeID + "_authority",
				Kind:    "connector.authority",
				Package: packageRef,
				Config:  connectorDefaultAuthorityNodeConfig(cfg, instConfig),
			})
		}
	}

	inst := &instancepkg.Instance{
		Name: instanceName,
		Source: instancepkg.InstanceSource{
			Package: packageRef,
		},
		Nodes:       nodes,
		Credentials: connectorDefaultBindings(cfg),
		Config:      instConfig,
	}
	return inst, nil
}

func connectorDefaultAuthorityNodeConfig(cfg models.ConnectorConfig, instanceConfig map[string]any) map[string]any {
	out := map[string]any{}
	tools := connectorToolNames(cfg, instanceConfig)
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
	return out
}

func connectorDefaultInstanceConfig(cfg models.ConnectorConfig) map[string]any {
	out := map[string]any{}
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
	if cfg.Requires == nil {
		return out
	}
	for _, cred := range cfg.Requires.Credentials {
		name := strings.TrimSpace(cred.Name)
		if name == "" {
			continue
		}
		target := name
		if strings.TrimSpace(cred.GrantName) != "" {
			target = strings.TrimSpace(cred.GrantName)
		}
		out[name] = instancepkg.Binding{
			Type:   "credref",
			Target: "credref:" + target,
		}
	}
	return out
}

func connectorToolNames(cfg models.ConnectorConfig, instanceConfig map[string]any) []string {
	seen := map[string]bool{}
	names := make([]string, 0, len(cfg.Tools))
	for _, tool := range cfg.Tools {
		if !toolEnabled(tool.RequiresConfig, instanceConfig) || strings.TrimSpace(tool.Name) == "" || seen[tool.Name] {
			continue
		}
		seen[tool.Name] = true
		names = append(names, tool.Name)
	}
	if cfg.MCP != nil {
		for _, tool := range cfg.MCP.Tools {
			if !toolEnabled(tool.RequiresConfig, instanceConfig) || strings.TrimSpace(tool.Name) == "" || seen[tool.Name] {
				continue
			}
			seen[tool.Name] = true
			names = append(names, tool.Name)
		}
	}
	return names
}

func toolEnabled(flag string, instanceConfig map[string]any) bool {
	flag = strings.TrimSpace(flag)
	if flag == "" {
		return true
	}
	value, ok := instanceConfig[flag]
	if !ok {
		return false
	}
	enabled, ok := value.(bool)
	return ok && enabled
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

func packageHasAuthorityRuntime(pkg hub.InstalledPackage) bool {
	if pkg.Spec == nil {
		return false
	}
	runtimeSpec, _ := pkg.Spec["runtime"].(map[string]any)
	return runtimeSpec != nil && runtimeSpec["executor"] != nil
}
