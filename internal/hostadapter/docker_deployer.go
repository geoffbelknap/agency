package hostadapter

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/geoffbelknap/agency/internal/credstore"
	"github.com/geoffbelknap/agency/internal/hostadapter/runtimehost"
	"github.com/geoffbelknap/agency/internal/hub"
	"github.com/geoffbelknap/agency/internal/orchestrate"
)

var deployNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*[a-z0-9]$`)

type dockerDeployer struct {
	Home        string
	Version     string
	SourceDir   string
	BuildID     string
	Docker      *runtimehost.Client
	Logger      logger
	Credentials map[string]string
	CredStore   *credstore.Store
}

type logger interface {
	Info(msg string, args ...any)
	Warn(msg string, args ...any)
}

func (d *dockerDeployer) secretPutter(instanceName string) hub.SecretPutter {
	return func(name, value string) error {
		if d.CredStore == nil {
			return fmt.Errorf("credential store not initialized")
		}
		now := time.Now().UTC().Format(time.RFC3339)
		return d.CredStore.Put(credstore.Entry{
			Name:  name,
			Value: value,
			Metadata: credstore.Metadata{
				Kind:      credstore.KindService,
				Scope:     "platform",
				Service:   instanceName,
				Protocol:  credstore.ProtocolAPIKey,
				Source:    "hub",
				CreatedAt: now,
				RotatedAt: now,
			},
		})
	}
}

func (d *dockerDeployer) secretDeleter() hub.SecretDeleter {
	return func(name string) error {
		if d.CredStore == nil {
			return fmt.Errorf("credential store not initialized")
		}
		return d.CredStore.Delete(name)
	}
}

func dockerInfraContainerName(component string) string {
	name := "agency-infra-" + component
	if instance := strings.TrimSpace(os.Getenv("AGENCY_INFRA_INSTANCE")); instance != "" {
		name += "-" + instance
	}
	return name
}

func (d *dockerDeployer) dryRun(ctx context.Context, pack *orchestrate.PackDef, onStatus func(string)) (*orchestrate.DeployResult, error) {
	_ = ctx
	if onStatus == nil {
		onStatus = func(string) {}
	}
	onStatus("Validating pack (dry run)...")
	for _, agent := range pack.Team.Agents {
		agentDir := filepath.Join(d.Home, "agents", agent.Name)
		if _, err := os.Stat(agentDir); err == nil {
			return nil, fmt.Errorf("agent %q already exists", agent.Name)
		}
	}
	deployID := fmt.Sprintf("%s-%s", pack.Name, time.Now().UTC().Format("20060102-150405"))
	result := &orchestrate.DeployResult{
		PackName:     pack.Name,
		TeamName:     pack.Team.Name,
		DeploymentID: deployID,
		DryRun:       true,
	}
	for _, a := range pack.Team.Agents {
		result.AgentsCreated = append(result.AgentsCreated, a.Name)
		result.AgentsStarted = append(result.AgentsStarted, a.Name)
	}
	for _, ch := range pack.Team.Channels {
		result.ChannelsCreated = append(result.ChannelsCreated, ch.Name)
	}
	for _, cn := range pack.Connectors {
		result.ConnectorsCreated = append(result.ConnectorsCreated, cn.Name)
	}
	return result, nil
}

func (d *dockerDeployer) deploy(ctx context.Context, pack *orchestrate.PackDef, onStatus func(string)) (*orchestrate.DeployResult, error) {
	if onStatus == nil {
		onStatus = func(string) {}
	}
	if len(pack.Team.Agents) == 0 {
		return nil, fmt.Errorf("pack %q has no agents defined", pack.Name)
	}
	deployID := fmt.Sprintf("%s-%s", pack.Name, time.Now().UTC().Format("20060102-150405"))
	result := &orchestrate.DeployResult{
		PackName:     pack.Name,
		TeamName:     pack.Team.Name,
		DeploymentID: deployID,
	}

	am, err := orchestrate.NewAgentManager(d.Home, d.Docker, nil)
	if err != nil {
		return nil, err
	}
	am.Version = d.Version
	am.SourceDir = d.SourceDir
	am.BuildID = d.BuildID
	am.BackendName = "docker"

	rollback := func() {
		if d.Logger != nil {
			d.Logger.Warn("rolling back deploy", "pack", pack.Name)
		}
		for _, name := range result.AgentsCreated {
			am.Delete(ctx, name)
		}
	}

	for _, agent := range pack.Team.Agents {
		agentYAML := filepath.Join(d.Home, "agents", agent.Name, "agent.yaml")
		if _, err := os.Stat(agentYAML); err == nil {
			if d.Logger != nil {
				d.Logger.Info("agent already exists, skipping", "agent", agent.Name)
			}
			result.AgentsCreated = append(result.AgentsCreated, agent.Name)
			continue
		}
		agentDir := filepath.Join(d.Home, "agents", agent.Name)
		if _, err := os.Stat(agentDir); err == nil {
			_ = os.RemoveAll(agentDir)
		}
		onStatus(fmt.Sprintf("Creating agent %s (%s)...", agent.Name, agent.Preset))
		if err := am.Create(ctx, agent.Name, agent.Preset); err != nil {
			return nil, fmt.Errorf("create agent %s: %w", agent.Name, err)
		}
		result.AgentsCreated = append(result.AgentsCreated, agent.Name)
	}

	teamDir := filepath.Join(d.Home, "teams", pack.Team.Name)
	_ = os.MkdirAll(teamDir, 0o755)
	memberNames := make([]string, 0, len(pack.Team.Agents))
	for _, a := range pack.Team.Agents {
		memberNames = append(memberNames, a.Name)
	}
	teamData, _ := yaml.Marshal(map[string]any{"name": pack.Team.Name, "members": memberNames})
	_ = os.WriteFile(filepath.Join(teamDir, "team.yaml"), teamData, 0o644)

	if len(pack.Team.Channels) > 0 {
		onStatus("Creating channels...")
		for _, ch := range pack.Team.Channels {
			body := map[string]any{
				"name":       ch.Name,
				"type":       "team",
				"created_by": "_platform",
				"topic":      ch.Topic,
				"members":    []string{"_platform"},
			}
			if _, err := d.Docker.CommsRequest(ctx, "POST", "/channels", body); err != nil {
				errStr := err.Error()
				if !strings.Contains(errStr, "409") && !strings.Contains(errStr, "already exists") {
					rollback()
					return nil, fmt.Errorf("create channel %s: %w", ch.Name, err)
				}
			}
			result.ChannelsCreated = append(result.ChannelsCreated, ch.Name)
			for _, a := range pack.Team.Agents {
				_, _ = d.Docker.CommsRequest(ctx, "POST", "/channels/"+ch.Name+"/grant-access", map[string]any{"agent": a.Name})
			}
		}
	}

	if len(pack.Connectors) > 0 {
		hubMgr := hub.NewManager(d.Home)
		credentials := d.Credentials
		if credentials == nil {
			credentials = map[string]string{}
		}
		for _, conn := range pack.Connectors {
			resolvedConfig := make(map[string]string)
			for k, v := range conn.Config {
				resolved := v
				for credName, credValue := range credentials {
					resolved = strings.ReplaceAll(resolved, "${"+credName+"}", credValue)
				}
				resolvedConfig[k] = resolved
			}

			inst, err := hubMgr.Install(conn.Source, "connector", "default", conn.Name)
			if err != nil {
				if existing := hubMgr.Registry.Resolve(conn.Name); existing != nil {
					inst = existing
				} else {
					onStatus(fmt.Sprintf("Failed to install connector %s: %s", conn.Name, err))
					continue
				}
			}
			result.ConnectorsCreated = append(result.ConnectorsCreated, conn.Name)
			instDir := hubMgr.Registry.InstanceDir(conn.Name)
			templateData, _ := os.ReadFile(filepath.Join(instDir, "connector.yaml"))
			schema, _ := hub.ParseConfigSchema(templateData)
			if schema != nil && len(schema.Fields) > 0 {
				configValues, secrets := schema.SplitSecrets(resolvedConfig, inst.Name)
				_ = hub.WriteConfig(instDir, &hub.ConfigValues{
					Instance:        inst.Name,
					ID:              inst.ID,
					SourceComponent: inst.Source,
					Values:          configValues,
				})
				if len(secrets) > 0 {
					hub.WriteSecrets(d.Home, inst.Name, secrets, d.secretPutter(inst.Name))
				}
				if resolved, _ := hubMgr.Registry.ResolvedYAML(conn.Name); resolved != nil {
					_ = os.WriteFile(filepath.Join(instDir, "resolved.yaml"), resolved, 0o644)
				}
			}
			_ = hubMgr.Registry.SetState(conn.Name, "active")
		}
	}

	for _, agent := range pack.Team.Agents {
		ss := &orchestrate.StartSequence{
			AgentName:   agent.Name,
			Home:        d.Home,
			Version:     d.Version,
			SourceDir:   d.SourceDir,
			BuildID:     d.BuildID,
			BackendName: "docker",
			Docker:      d.Docker,
			Comms:       d.Docker,
		}
		if _, err := ss.Run(ctx, nil); err == nil {
			result.AgentsStarted = append(result.AgentsStarted, agent.Name)
		}
	}

	d.saveManifest(pack, result)
	return result, nil
}

func (d *dockerDeployer) teardown(ctx context.Context, packName string, deleteResources bool) error {
	manifestPath := filepath.Join(d.Home, "packs", packName, "manifest.json")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return fmt.Errorf("pack %q not found (no manifest)", packName)
	}
	var manifest map[string]any
	_ = json.Unmarshal(data, &manifest)

	hc, err := orchestrate.NewHaltController(d.Home, d.Version, d.Docker, nil)
	if err != nil {
		return err
	}
	agents, _ := manifest["agents"].([]any)
	for _, a := range agents {
		if name, _ := a.(string); name != "" {
			_, _ = hc.Halt(ctx, name, "supervised", "pack teardown: "+packName, "operator")
		}
	}
	channels, _ := manifest["channels"].([]any)
	for _, c := range channels {
		if name, _ := c.(string); name != "" {
			_, _ = d.Docker.CommsRequest(ctx, "POST", "/channels/"+name+"/archive", map[string]any{"archived_by": "_platform"})
		}
	}

	if connectors, ok := manifest["connectors"].([]any); ok {
		hubMgr := hub.NewManager(d.Home)
		needsIntakeSignal := false
		for _, c := range connectors {
			connName, _ := c.(string)
			if connName == "" {
				continue
			}
			inst := hubMgr.Registry.Resolve(connName)
			if inst == nil {
				continue
			}
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
			if len(secretFields) > 0 {
				hub.RemoveSecrets(d.Home, connName, secretFields, d.secretDeleter())
			}
			removed, err := hubMgr.RemoveWithDependencies(connName)
			if err != nil {
				continue
			}
			for _, removedInst := range removed {
				if removedInst.Kind == "connector" {
					_ = os.Remove(filepath.Join(d.Home, "connectors", removedInst.Name+".yaml"))
					needsIntakeSignal = true
				}
			}
		}
		if needsIntakeSignal {
			_ = d.Docker.SignalContainer(ctx, dockerInfraContainerName("intake"), "SIGHUP")
		}
	}

	if deleteResources {
		am, _ := orchestrate.NewAgentManager(d.Home, d.Docker, nil)
		for _, a := range agents {
			if name, _ := a.(string); name != "" {
				am.Delete(ctx, name)
			}
		}
		if teamName, _ := manifest["team_name"].(string); teamName != "" && deployNamePattern.MatchString(teamName) {
			_ = os.RemoveAll(filepath.Join(d.Home, "teams", teamName))
		}
	}
	return nil
}

func (d *dockerDeployer) saveManifest(pack *orchestrate.PackDef, result *orchestrate.DeployResult) {
	if !deployNamePattern.MatchString(pack.Name) {
		return
	}
	packDir := filepath.Join(d.Home, "packs", pack.Name)
	_ = os.MkdirAll(packDir, 0o755)
	manifest := map[string]any{
		"pack_name":     pack.Name,
		"team_name":     pack.Team.Name,
		"agents":        result.AgentsCreated,
		"channels":      result.ChannelsCreated,
		"connectors":    result.ConnectorsCreated,
		"deployment_id": result.DeploymentID,
		"deployed_at":   time.Now().UTC().Format(time.RFC3339),
	}
	data, _ := json.MarshalIndent(manifest, "", "  ")
	_ = os.WriteFile(filepath.Join(packDir, "manifest.json"), data, 0o644)
}
