// agency-gateway/internal/models/pack.go
package models

import "fmt"

// PackAgent represents a single agent entry in a pack team.
type PackAgent struct {
	Name      string   `yaml:"name" validate:"required"`
	Preset    string   `yaml:"preset" validate:"required"`
	Workspace *string  `yaml:"workspace"`
	Role      string   `yaml:"role" validate:"omitempty,oneof=standard coordinator function" default:"standard"`
	AgentType *string  `yaml:"agent_type"`
	Host      *string  `yaml:"host"`
	Skills    []string `yaml:"skills"`
	Connectors []string `yaml:"connectors"`
}

// PackChannel represents a communication channel defined in a pack.
type PackChannel struct {
	Name    string `yaml:"name" validate:"required"`
	Topic   string `yaml:"topic"`
	Private bool   `yaml:"private"`
}

// PackCredential represents a credential required by a pack.
type PackCredential struct {
	Name        string `yaml:"name" validate:"required"`
	Description string `yaml:"description"`
	Required    bool   `yaml:"required" default:"true"`
}

// PackMissionAssignment binds a mission to an agent within a pack.
type PackMissionAssignment struct {
	Mission string `yaml:"mission" validate:"required"`
	Agent   string `yaml:"agent" validate:"required"`
}

// PackRequires lists the dependencies of a pack.
type PackRequires struct {
	Connectors []string `yaml:"connectors"`
	Presets    []string `yaml:"presets"`
	Services   []string `yaml:"services"`
	Skills     []string `yaml:"skills"`
	Workspaces []string `yaml:"workspaces"`
	Policies   []string `yaml:"policies"`
}

// PackTeam defines the team composition within a pack.
type PackTeam struct {
	Name     string        `yaml:"name" validate:"required"`
	Agents   []PackAgent   `yaml:"agents" validate:"required"`
	Channels []PackChannel `yaml:"channels"`
}

// Validate implements cross-field validation for PackTeam.
// Checks that agents is non-empty and that agent/channel names are unique.
func (pt *PackTeam) Validate() error {
	if len(pt.Agents) == 0 {
		return fmt.Errorf("pack must define at least one agent")
	}

	agentSeen := make(map[string]bool)
	for _, a := range pt.Agents {
		if agentSeen[a.Name] {
			return fmt.Errorf("duplicate agent names: %s", a.Name)
		}
		agentSeen[a.Name] = true
	}

	channelSeen := make(map[string]bool)
	for _, c := range pt.Channels {
		if channelSeen[c.Name] {
			return fmt.Errorf("duplicate channel names: %s", c.Name)
		}
		channelSeen[c.Name] = true
	}

	return nil
}

// PackConfig is the schema for pack.yaml files.
type PackConfig struct {
	Kind                 string           `yaml:"kind" validate:"required,oneof=pack" default:"pack"`
	Name                 string           `yaml:"name" validate:"required"`
	Version              string           `yaml:"version" default:"1.0.0"`
	Description          string           `yaml:"description"`
	Author               string           `yaml:"author"`
	License              string           `yaml:"license,omitempty"`
	Requires             PackRequires     `yaml:"requires"`
	Team                 PackTeam         `yaml:"team"`
	Credentials          []PackCredential `yaml:"credentials"`
	Policy               map[string]interface{} `yaml:"policy"`
	RecommendedConnectors []string        `yaml:"recommended_connectors"`
	MissionAssignments   []PackMissionAssignment `yaml:"mission_assignments"`
}

// Validate implements cross-field validation for PackConfig.
// Applies per-agent defaults and delegates to PackTeam for structural checks.
func (pc *PackConfig) Validate() error {
	for i := range pc.Team.Agents {
		if pc.Team.Agents[i].Role == "" {
			pc.Team.Agents[i].Role = "standard"
		}
	}
	return pc.Team.Validate()
}
