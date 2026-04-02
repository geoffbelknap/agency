// agency-gateway/internal/models/workspace.go
package models

import "fmt"

// WorkspaceBase defines the base container image and runtime settings.
type WorkspaceBase struct {
	Image      string `yaml:"image" validate:"required"`
	User       string `yaml:"user" default:"agent"`
	Filesystem string `yaml:"filesystem" default:"readonly-root"`
}

// WorkspaceProvides describes the capabilities exposed by a workspace.
type WorkspaceProvides struct {
	Tools   []string `yaml:"tools"`
	Network string   `yaml:"network" default:"mediated"`
}

// WorkspaceResources defines container resource limits for a workspace.
type WorkspaceResources struct {
	Memory string `yaml:"memory" default:"2GB"`
	CPU    string `yaml:"cpu" default:"1.0"`
	Tmpfs  string `yaml:"tmpfs" default:"512MB"`
}

// WorkspaceSecurity defines the security posture for a workspace container.
type WorkspaceSecurity struct {
	Capabilities    string `yaml:"capabilities" validate:"required,oneof=none" default:"none"`
	Seccomp         string `yaml:"seccomp" validate:"required,oneof=default-strict default" default:"default-strict"`
	NoNewPrivileges bool   `yaml:"no_new_privileges" default:"true"`
}

// WorkspaceConfig is the schema for workspace.yaml template files.
type WorkspaceConfig struct {
	Name      string             `yaml:"name" validate:"required"`
	Version   string             `yaml:"version" default:"1.0"`
	Base      WorkspaceBase      `yaml:"base"`
	Provides  WorkspaceProvides  `yaml:"provides"`
	Resources WorkspaceResources `yaml:"resources"`
	Security  WorkspaceSecurity  `yaml:"security"`
}

// ExtraMount defines an additional host-to-container bind mount.
type ExtraMount struct {
	Source string `yaml:"source" validate:"required"`
	Target string `yaml:"target" validate:"required"`
}

// Validate checks that both Source and Target are absolute paths.
func (e *ExtraMount) Validate() error {
	if len(e.Source) == 0 || e.Source[0] != '/' {
		return fmt.Errorf("source must be an absolute path")
	}
	if len(e.Target) == 0 || e.Target[0] != '/' {
		return fmt.Errorf("target must be an absolute path")
	}
	return nil
}

// AgentWorkspaceConfig is the schema for workspace.yaml files under agents/.
type AgentWorkspaceConfig struct {
	Version      string       `yaml:"version" default:"0.1"`
	Agent        string       `yaml:"agent" validate:"required"`
	WorkspaceRef string       `yaml:"workspace_ref" validate:"required"`
	ProjectDir   *string      `yaml:"project_dir"`
	ExtraMounts  []ExtraMount `yaml:"extra_mounts"`
}
