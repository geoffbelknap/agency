package orchestrate

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// PackConnectorDef declares a connector instance used by the pack.
type PackConnectorDef struct {
	Source string            `yaml:"source" json:"source"`
	Name   string            `yaml:"name" json:"name"`
	Config map[string]string `yaml:"config,omitempty" json:"config,omitempty"`
}

// PackCredentialDef declares a credential required or accepted by the pack.
type PackCredentialDef struct {
	Name        string `yaml:"name" json:"name"`
	Description string `yaml:"description" json:"description"`
	Required    bool   `yaml:"required" json:"required"`
	Secret      bool   `yaml:"secret" json:"secret"`
}

// PackDef represents a pack YAML file.
type PackDef struct {
	Name        string              `yaml:"name" json:"name"`
	Team        PackTeamDef         `yaml:"team" json:"team"`
	Connectors  []PackConnectorDef  `yaml:"connectors,omitempty" json:"connectors,omitempty"`
	Credentials []PackCredentialDef `yaml:"credentials,omitempty" json:"credentials,omitempty"`
}

type PackTeamDef struct {
	Name     string           `yaml:"name" json:"name"`
	Agents   []PackAgentDef   `yaml:"agents" json:"agents"`
	Channels []PackChannelDef `yaml:"channels,omitempty" json:"channels,omitempty"`
}

type PackAgentDef struct {
	Name       string   `yaml:"name" json:"name"`
	Preset     string   `yaml:"preset" json:"preset"`
	Role       string   `yaml:"role,omitempty" json:"role,omitempty"`
	AgentType  string   `yaml:"agent_type,omitempty" json:"agent_type,omitempty"`
	Connectors []string `yaml:"connectors,omitempty" json:"connectors,omitempty"`
}

type PackChannelDef struct {
	Name       string   `yaml:"name" json:"name"`
	Topic      string   `yaml:"topic,omitempty" json:"topic,omitempty"`
	Members    []string `yaml:"members,omitempty" json:"members,omitempty"`
	Visibility string   `yaml:"visibility,omitempty" json:"visibility,omitempty"`
}

// DeployResult tracks what was created during deployment.
type DeployResult struct {
	PackName          string   `json:"pack_name"`
	TeamName          string   `json:"team_name"`
	AgentsCreated     []string `json:"agents_created"`
	AgentsStarted     []string `json:"agents_started"`
	ChannelsCreated   []string `json:"channels_created"`
	ConnectorsCreated []string `json:"connectors_created,omitempty"`
	DeploymentID      string   `json:"deployment_id"`
	DryRun            bool     `json:"dry_run,omitempty"`
}

// LoadPack reads and parses a pack YAML file.
func LoadPack(path string) (*PackDef, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read pack: %w", err)
	}
	var pack PackDef
	if err := yaml.Unmarshal(data, &pack); err != nil {
		return nil, fmt.Errorf("parse pack: %w", err)
	}
	if pack.Name == "" || pack.Team.Name == "" {
		return nil, fmt.Errorf("pack must have name and team.name")
	}
	return &pack, nil
}
