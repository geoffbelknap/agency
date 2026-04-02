package orchestrate

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/log"
	"gopkg.in/yaml.v3"

	agencyDocker "github.com/geoffbelknap/agency/internal/docker"
	"github.com/geoffbelknap/agency/internal/hub"
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
	Name     string          `yaml:"name" json:"name"`
	Agents   []PackAgentDef  `yaml:"agents" json:"agents"`
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
	PackName           string   `json:"pack_name"`
	TeamName           string   `json:"team_name"`
	AgentsCreated      []string `json:"agents_created"`
	AgentsStarted      []string `json:"agents_started"`
	ChannelsCreated    []string `json:"channels_created"`
	ConnectorsCreated  []string `json:"connectors_created,omitempty"`
	DeploymentID       string   `json:"deployment_id"`
	DryRun             bool     `json:"dry_run,omitempty"`
}

// Deployer manages pack deployment and teardown.
type Deployer struct {
	Home        string
	Version     string
	SourceDir   string // agency_core/ path for dev-mode image builds
	BuildID     string // content-aware build ID for staleness detection
	Docker      *agencyDocker.Client
	Log         *log.Logger
	Credentials map[string]string
}

func NewDeployer(home, version string, dc *agencyDocker.Client, logger *log.Logger) *Deployer {
	return &Deployer{Home: home, Version: version, Docker: dc, Log: logger}
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

// DryRunDeploy validates the pack and returns what would be created without
// actually creating anything.
func (d *Deployer) DryRunDeploy(ctx context.Context, pack *PackDef, onStatus func(string)) (*DeployResult, error) {
	if onStatus == nil {
		onStatus = func(string) {}
	}

	onStatus("Validating pack (dry run)...")

	// Check for agent conflicts — same validation as real deploy.
	for _, agent := range pack.Team.Agents {
		agentDir := filepath.Join(d.Home, "agents", agent.Name)
		if _, err := os.Stat(agentDir); err == nil {
			return nil, fmt.Errorf("agent %q already exists", agent.Name)
		}
	}

	deployID := fmt.Sprintf("%s-%s", pack.Name, time.Now().UTC().Format("20060102-150405"))

	agentNames := make([]string, 0, len(pack.Team.Agents))
	for _, a := range pack.Team.Agents {
		agentNames = append(agentNames, a.Name)
	}

	channelNames := make([]string, 0, len(pack.Team.Channels))
	for _, ch := range pack.Team.Channels {
		channelNames = append(channelNames, ch.Name)
	}

	connectorNames := make([]string, 0, len(pack.Connectors))
	for _, cn := range pack.Connectors {
		connectorNames = append(connectorNames, cn.Name)
	}

	return &DeployResult{
		PackName:          pack.Name,
		TeamName:          pack.Team.Name,
		AgentsCreated:     agentNames,
		AgentsStarted:     agentNames,
		ChannelsCreated:   channelNames,
		ConnectorsCreated: connectorNames,
		DeploymentID:      deployID,
		DryRun:            true,
	}, nil
}

// Deploy creates agents, team, channels, and starts everything.
// Fail-closed: any error rolls back everything created so far.
func (d *Deployer) Deploy(ctx context.Context, pack *PackDef, onStatus func(string)) (*DeployResult, error) {
	if onStatus == nil {
		onStatus = func(string) {}
	}

	if len(pack.Team.Agents) == 0 {
		return nil, fmt.Errorf("pack %q has no agents defined", pack.Name)
	}

	deployID := fmt.Sprintf("%s-%s", pack.Name, time.Now().UTC().Format("20060102-150405"))
	result := &DeployResult{
		PackName:     pack.Name,
		TeamName:     pack.Team.Name,
		DeploymentID: deployID,
	}

	am, err := NewAgentManager(d.Home, d.Docker, d.Log)
	if err != nil {
		return nil, err
	}

	rollback := func() {
		d.Log.Warn("rolling back deploy", "pack", pack.Name)
		for _, name := range result.AgentsCreated {
			am.Delete(ctx, name)
		}
	}
	_ = rollback // used by channel creation below

	// Create agents — skip existing ones for idempotent deploys.
	// Check for agent.yaml specifically (not just the directory) to handle
	// incomplete deletions where the dir exists but config is missing.
	for _, agent := range pack.Team.Agents {
		agentYAML := filepath.Join(d.Home, "agents", agent.Name, "agent.yaml")
		if _, err := os.Stat(agentYAML); err == nil {
			d.Log.Info("agent already exists, skipping", "agent", agent.Name)
			result.AgentsCreated = append(result.AgentsCreated, agent.Name)
			continue
		}
		// Clean up incomplete agent dir if it exists without agent.yaml
		agentDir := filepath.Join(d.Home, "agents", agent.Name)
		if _, err := os.Stat(agentDir); err == nil {
			os.RemoveAll(agentDir)
		}
		onStatus(fmt.Sprintf("Creating agent %s (%s)...", agent.Name, agent.Preset))
		if err := am.Create(ctx, agent.Name, agent.Preset); err != nil {
			return nil, fmt.Errorf("create agent %s: %w", agent.Name, err)
		}
		result.AgentsCreated = append(result.AgentsCreated, agent.Name)
	}

	// Create team directory
	onStatus(fmt.Sprintf("Creating team %s...", pack.Team.Name))
	teamDir := filepath.Join(d.Home, "teams", pack.Team.Name)
	os.MkdirAll(teamDir, 0755)
	memberNames := make([]string, 0, len(pack.Team.Agents))
	for _, a := range pack.Team.Agents {
		memberNames = append(memberNames, a.Name)
	}
	teamConfig := map[string]interface{}{
		"name":    pack.Team.Name,
		"members": memberNames,
	}
	teamData, _ := yaml.Marshal(teamConfig)
	os.WriteFile(filepath.Join(teamDir, "team.yaml"), teamData, 0644)

	// Create channels
	if len(pack.Team.Channels) > 0 {
		onStatus("Creating channels...")
		for _, ch := range pack.Team.Channels {
			body := map[string]interface{}{
				"name":       ch.Name,
				"type":       "team",
				"created_by": "_platform",
				"topic":      ch.Topic,
				"members":    []string{"_platform"},
			}
			_, err := d.Docker.CommsRequest(ctx, "POST", "/channels", body)
			if err != nil {
				// 409 conflict means channel already exists — idempotent, continue
				errStr := err.Error()
				if !strings.Contains(errStr, "409") && !strings.Contains(errStr, "already exists") {
					d.Log.Warn("channel create failed", "channel", ch.Name, "err", err)
					rollback()
					return nil, fmt.Errorf("create channel %s: %w", ch.Name, err)
				}
				d.Log.Info("channel already exists, skipping", "channel", ch.Name)
			}
			result.ChannelsCreated = append(result.ChannelsCreated, ch.Name)

			// Grant access to all pack agents
			grantBody := map[string]interface{}{}
			for _, a := range pack.Team.Agents {
				grantBody["agent"] = a.Name
				if _, gerr := d.Docker.CommsRequest(ctx, "POST", "/channels/"+ch.Name+"/grant-access", grantBody); gerr != nil {
					d.Log.Warn("channel grant-access failed", "channel", ch.Name, "agent", a.Name, "err", gerr)
				}
			}
		}
	}

	// Provision connectors from pack
	if len(pack.Connectors) > 0 {
		hubMgr := hub.NewManager(d.Home)
		credentials := d.Credentials
		if credentials == nil {
			credentials = map[string]string{}
		}
		for _, conn := range pack.Connectors {
			// Resolve ${CREDENTIAL} references in config values
			resolvedConfig := make(map[string]string)
			for k, v := range conn.Config {
				resolved := v
				for credName, credValue := range credentials {
					resolved = strings.ReplaceAll(resolved, "${"+credName+"}", credValue)
				}
				resolvedConfig[k] = resolved
			}

			// Install from hub (skip if already exists)
			inst, err := hubMgr.Install(conn.Source, "connector", "default", conn.Name)
			if err != nil {
				// Check if it already exists — not an error
				if existing := hubMgr.Registry.Resolve(conn.Name); existing != nil {
					inst = existing
				} else {
					onStatus(fmt.Sprintf("Failed to install connector %s: %s", conn.Name, err))
					continue
				}
			}
			result.ConnectorsCreated = append(result.ConnectorsCreated, conn.Name)
			onStatus(fmt.Sprintf("Installed connector %s (%s)", conn.Name, inst.ID))

			// Configure
			instDir := hubMgr.Registry.InstanceDir(conn.Name)
			templateData, _ := os.ReadFile(filepath.Join(instDir, "connector.yaml"))
			schema, _ := hub.ParseConfigSchema(templateData)

			if schema != nil && len(schema.Fields) > 0 {
				configValues, secrets := schema.SplitSecrets(resolvedConfig, inst.Name)
				hub.WriteConfig(instDir, &hub.ConfigValues{
					Instance:        inst.Name,
					ID:              inst.ID,
					SourceComponent: inst.Source,
					Values:          configValues,
				})
				if len(secrets) > 0 {
					hub.WriteSecrets(d.Home, inst.Name, secrets)
				}
				resolved, _ := hubMgr.Registry.ResolvedYAML(conn.Name)
				if resolved != nil {
					os.WriteFile(filepath.Join(instDir, "resolved.yaml"), resolved, 0644)
				}
			}

			hubMgr.Registry.SetState(conn.Name, "active")
			onStatus(fmt.Sprintf("Activated connector %s", conn.Name))
		}
	}

	// Start agents
	for _, agent := range pack.Team.Agents {
		onStatus(fmt.Sprintf("Starting agent %s...", agent.Name))
		ss := &StartSequence{
			AgentName: agent.Name,
			Home:      d.Home,
			Version:   d.Version,
			SourceDir: d.SourceDir,
			BuildID:   d.BuildID,
			Docker:    d.Docker,
			Log:       d.Log,
		}
		_, err := ss.Run(ctx, nil)
		if err != nil {
			d.Log.Warn("agent start failed, continuing", "agent", agent.Name, "err", err)
		} else {
			result.AgentsStarted = append(result.AgentsStarted, agent.Name)
		}
	}

	// Save manifest
	d.saveManifest(pack, result)

	return result, nil
}

// Teardown stops agents, archives channels, and optionally deletes resources.
func (d *Deployer) Teardown(ctx context.Context, packName string, deleteResources bool) error {
	manifestPath := filepath.Join(d.Home, "packs", packName, "manifest.json")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return fmt.Errorf("pack %q not found (no manifest)", packName)
	}

	var manifest map[string]interface{}
	json.Unmarshal(data, &manifest)

	hc, err := NewHaltController(d.Home, d.Version, d.Docker, d.Log)
	if err != nil {
		return err
	}

	// Stop agents
	agents, _ := manifest["agents"].([]interface{})
	for _, a := range agents {
		name, _ := a.(string)
		if name != "" {
			hc.Halt(ctx, name, "supervised", "pack teardown: "+packName, "operator")
		}
	}

	// Archive channels
	channels, _ := manifest["channels"].([]interface{})
	for _, c := range channels {
		name, _ := c.(string)
		if name != "" {
			d.Docker.CommsRequest(ctx, "POST", "/channels/"+name+"/archive", map[string]interface{}{
				"archived_by": "_platform",
			})
		}
	}

	// Deactivate and remove connector instances
	if connectors, ok := manifest["connectors"].([]interface{}); ok {
		hubMgr := hub.NewManager(d.Home)
		for _, c := range connectors {
			connName, _ := c.(string)
			if connName == "" {
				continue
			}

			inst := hubMgr.Registry.Resolve(connName)
			if inst == nil {
				continue
			}

			// Find secret fields for cleanup
			instDir := hubMgr.Registry.InstanceDir(connName)
			cv, _ := hub.ReadConfig(instDir)
			var secretFields []string
			if cv != nil {
				for k, v := range cv.Values {
					if strings.HasPrefix(v, "@scoped:") {
						secretFields = append(secretFields, k)
					}
				}
			}

			// Clean up credentials
			if len(secretFields) > 0 {
				hub.RemoveSecrets(d.Home, connName, secretFields)
			}

			// Remove instance
			hubMgr.Registry.Remove(connName)
		}
	}

	// Delete resources if requested
	if deleteResources {
		am, _ := NewAgentManager(d.Home, d.Docker, d.Log)
		for _, a := range agents {
			name, _ := a.(string)
			if name != "" {
				am.Delete(ctx, name)
			}
		}
		teamName, _ := manifest["team_name"].(string)
		if teamName != "" {
			os.RemoveAll(filepath.Join(d.Home, "teams", teamName))
		}
	}

	d.Log.Info("pack torn down", "pack", packName, "deleted", deleteResources)
	return nil
}

func (d *Deployer) saveManifest(pack *PackDef, result *DeployResult) {
	packDir := filepath.Join(d.Home, "packs", pack.Name)
	os.MkdirAll(packDir, 0755)

	manifest := map[string]interface{}{
		"pack_name":     pack.Name,
		"team_name":     pack.Team.Name,
		"agents":        result.AgentsCreated,
		"channels":      result.ChannelsCreated,
		"connectors":    result.ConnectorsCreated,
		"deployment_id": result.DeploymentID,
		"deployed_at":   time.Now().UTC().Format(time.RFC3339),
	}
	data, _ := json.MarshalIndent(manifest, "", "  ")
	os.WriteFile(filepath.Join(packDir, "manifest.json"), data, 0644)
}
