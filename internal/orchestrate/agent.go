package orchestrate

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/log"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/docker/go-connections/nat"
	"github.com/google/uuid"
	"gopkg.in/yaml.v3"

	agencyDocker "github.com/geoffbelknap/agency/internal/docker"
)

var namePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*[a-z0-9]$`)

var reservedNames = map[string]bool{
	"infra-egress": true, "agency": true, "enforcer": true,
	"gateway": true, "workspace": true,
}

// AgentDetail contains the full agent definition and runtime status.
type AgentDetail struct {
	Name        string              `json:"name"`
	Type        string              `json:"type"`
	Status      string              `json:"status"` // running, stopped, paused
	Model       string              `json:"model,omitempty"`
	ModelTier   string              `json:"model_tier,omitempty"`
	Preset      string              `json:"preset,omitempty"`
	Role        string              `json:"role,omitempty"`
	Team        string              `json:"team,omitempty"`
	TrustLevel  int                 `json:"trust_level,omitempty"`
	Workspace   string              `json:"workspace"`
	Enforcer    string              `json:"enforcer"`
	AgentDir    string              `json:"agent_dir"`
	LifecycleID string              `json:"lifecycle_id,omitempty"`
	Constraints     *ConstraintsSummary `json:"constraints,omitempty"`
	Restrictions    []string            `json:"restrictions,omitempty"`
	GrantedCaps     []string            `json:"granted_capabilities,omitempty"`
	GrantedServices []string            `json:"granted_services,omitempty"`
	CurrentTask     *TaskSummary        `json:"current_task,omitempty"`
	UnknownKeys     []string            `json:"unknown_keys,omitempty"` // unknown top-level keys in agent.yaml
	BuildID         string              `json:"build_id,omitempty"`
	Mission         string              `json:"mission,omitempty"`
	MissionStatus   string              `json:"mission_status,omitempty"`
	LastActive      string              `json:"last_active,omitempty"`
}

type ConstraintsSummary struct {
	HardLimits int `json:"hard_limits"`
}

// TaskSummary describes the agent's current or most recent task.
type TaskSummary struct {
	TaskID    string `json:"task_id,omitempty"`
	Content   string `json:"content,omitempty"`
	Timestamp string `json:"timestamp,omitempty"`
	Source    string `json:"source,omitempty"`
}

// AgentManager handles agent CRUD operations.
type AgentManager struct {
	Home          string
	Docker        *agencyDocker.Client
	cli           *client.Client
	log           *log.Logger
	StopSuppress  *StopSuppression
}

func NewAgentManager(home string, dc *agencyDocker.Client, logger *log.Logger) (*AgentManager, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, err
	}
	return &AgentManager{Home: home, Docker: dc, cli: cli, log: logger}, nil
}

// List returns all defined agents with their runtime status.
func (am *AgentManager) List(ctx context.Context) ([]AgentDetail, error) {
	agentsDir := filepath.Join(am.Home, "agents")
	entries, err := os.ReadDir(agentsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []AgentDetail{}, nil
		}
		return nil, err
	}

	// Get running containers in one call
	running := am.getRunningContainers(ctx)

	// Build team membership index once for the entire list — avoids reading all
	// team YAML files once per agent (O(agents*teams) → O(teams)).
	teamIndex := buildTeamIndex(am.Home)

	var agents []AgentDetail
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		agentYAML := filepath.Join(agentsDir, e.Name(), "agent.yaml")
		if _, err := os.Stat(agentYAML); err != nil {
			continue
		}

		detail := am.loadAgentDetail(e.Name(), agentsDir, running, teamIndex)
		agents = append(agents, detail)
	}

	sort.Slice(agents, func(i, j int) bool { return agents[i].Name < agents[j].Name })
	return agents, nil
}

// Show returns details for a single agent.
func (am *AgentManager) Show(ctx context.Context, name string) (*AgentDetail, error) {
	agentsDir := filepath.Join(am.Home, "agents")
	agentDir := filepath.Join(agentsDir, name)
	if _, err := os.Stat(filepath.Join(agentDir, "agent.yaml")); err != nil {
		return nil, fmt.Errorf("agent %q not found", name)
	}

	running := am.getRunningContainers(ctx)
	teamIndex := buildTeamIndex(am.Home)
	detail := am.loadAgentDetail(name, agentsDir, running, teamIndex)
	return &detail, nil
}

// Create creates a new agent from a preset.
func (am *AgentManager) Create(ctx context.Context, name, preset string) error {
	if err := validateAgentName(name); err != nil {
		return err
	}

	agentDir := filepath.Join(am.Home, "agents", name)
	if _, err := os.Stat(agentDir); err == nil {
		return fmt.Errorf("agent %q already exists", name)
	}

	// Read preset if available — presets define identity, hard_limits,
	// escalation, expertise, and model preferences. Without a preset,
	// agents get generic defaults.
	presetData := am.readPreset(preset)

	// Create directory structure — mode 0755 for agent dir (comms container
	// needs access to state/), mode 0700 for memory (sensitive content).
	// ASK compliance: constraints and identity are mounted read-only into
	// containers regardless of host permissions.
	os.MkdirAll(agentDir, 0755)
	for _, sub := range []string{"memory", "memory/preferences", "memory/context", "memory/capabilities"} {
		os.MkdirAll(filepath.Join(agentDir, sub), 0700)
	}

	// Generate agent.yaml
	agentType := "standard"
	if t, ok := presetData["type"].(string); ok && t != "" {
		agentType = t
	}
	agentYAML := map[string]interface{}{
		"version":      "0.1",
		"name":         name,
		"type":         agentType,
		"preset":       preset,
		"lifecycle_id": uuid.New().String(),
		"workspace": map[string]interface{}{
			"ref":   "ubuntu-default",
			"tools": []string{"git", "python3"},
		},
	}
	// Copy expertise from preset to agent.yaml (used for comms routing)
	if expertise, ok := presetData["expertise"]; ok {
		agentYAML["expertise"] = expertise
	}
	// Copy responsiveness from preset
	if resp, ok := presetData["responsiveness"]; ok {
		agentYAML["responsiveness"] = resp
	}
	if err := writeYAML(filepath.Join(agentDir, "agent.yaml"), agentYAML); err != nil {
		return err
	}

	// Generate constraints.yaml — apply preset identity, hard_limits, escalation
	identityBlock := map[string]interface{}{
		"role":    "assistant",
		"purpose": "General assistant",
	}
	if pi, ok := presetData["identity"].(map[string]interface{}); ok {
		if purpose, ok := pi["purpose"].(string); ok && purpose != "" {
			identityBlock["purpose"] = purpose
		}
	}

	hardLimits := []map[string]string{
		{"rule": "never expose credentials, tokens, or secrets in any output", "reason": "credential exposure is a critical security risk"},
		{"rule": "never send data to external services without explicit approval", "reason": "data exfiltration risk"},
		{"rule": "never delete files without explicit confirmation", "reason": "deletions are irreversible"},
	}
	if presetLimits, ok := presetData["hard_limits"].([]interface{}); ok {
		for _, item := range presetLimits {
			if m, ok := item.(map[string]interface{}); ok {
				rule, _ := m["rule"].(string)
				reason, _ := m["reason"].(string)
				if rule != "" {
					hardLimits = append(hardLimits, map[string]string{"rule": rule, "reason": reason})
				}
			}
		}
	}

	escalation := map[string]interface{}{
		"always_escalate":        []string{"authentication and authorization changes"},
		"flag_before_proceeding": []string{"irreversible actions", "unexpected findings"},
	}
	if presetEsc, ok := presetData["escalation"].(map[string]interface{}); ok {
		if ae, ok := presetEsc["always_escalate"]; ok {
			escalation["always_escalate"] = ae
		}
		if fbp, ok := presetEsc["flag_before_proceeding"]; ok {
			escalation["flag_before_proceeding"] = fbp
		}
	}

	constraints := map[string]interface{}{
		"version":     "0.1",
		"agent":       name,
		"identity":    identityBlock,
		"hard_limits": hardLimits,
		"escalation":  escalation,
		"network": map[string]interface{}{
			"egress_mode": "denylist",
		},
	}
	if err := writeYAML(filepath.Join(agentDir, "constraints.yaml"), constraints); err != nil {
		return err
	}

	// Generate identity.md — use preset body if available
	identity := fmt.Sprintf("# %s\n\nYou are %s, a general assistant.\n", name, name)
	if pi, ok := presetData["identity"].(map[string]interface{}); ok {
		if body, ok := pi["body"].(string); ok && body != "" {
			identity = fmt.Sprintf("# %s\n\n%s", name, body)
		}
	}
	os.WriteFile(filepath.Join(agentDir, "identity.md"), []byte(identity), 0644)

	// Generate workspace.yaml
	workspace := map[string]interface{}{
		"version": "0.1",
		"agent":   name,
		"image":   "ubuntu-default",
	}
	if err := writeYAML(filepath.Join(agentDir, "workspace.yaml"), workspace); err != nil {
		return err
	}

	// Generate policy.yaml
	policy := map[string]interface{}{
		"version": "0.1",
	}
	writeYAML(filepath.Join(agentDir, "policy.yaml"), policy)

	// Empty manifests
	os.WriteFile(filepath.Join(agentDir, "services-manifest.json"), []byte("{}"), 0644)
	os.WriteFile(filepath.Join(agentDir, "skills-manifest.json"), []byte("{}"), 0644)

	// State directory — world-writable so comms container can deliver tasks.
	// Docker bind mounts on WSL2/Docker Desktop don't reliably map UIDs,
	// so we need open permissions for cross-container writes.
	// Use explicit Chmod since os.MkdirAll/WriteFile respect umask.
	stateDir := filepath.Join(agentDir, "state")
	os.MkdirAll(stateDir, 0777)
	os.Chmod(stateDir, 0777)
	ctxFile := filepath.Join(stateDir, "session-context.json")
	sigFile := filepath.Join(stateDir, "agent-signals.jsonl")
	os.WriteFile(ctxFile, []byte("{}"), 0666)
	os.Chmod(ctxFile, 0666)
	os.WriteFile(sigFile, []byte(""), 0666)
	os.Chmod(sigFile, 0666)

	am.log.Info("agent created", "name", name, "preset", preset)
	return nil
}

// readPreset loads a preset YAML from hub-cache. Returns empty map if not found.
func (am *AgentManager) readPreset(preset string) map[string]interface{} {
	if preset == "" {
		return nil
	}
	// Check hub-cache for the preset
	paths := []string{
		filepath.Join(am.Home, "hub-cache", "default", "presets", preset, "preset.yaml"),
	}
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		var result map[string]interface{}
		if err := yaml.Unmarshal(data, &result); err == nil {
			return result
		}
	}
	return nil
}

// Delete removes an agent and cleans up all resources.
// Audit logs are archived, never destroyed (ASK tenet 2).
func (am *AgentManager) Delete(ctx context.Context, name string) error {
	if err := validateAgentName(name); err != nil {
		return err
	}
	agentDir := filepath.Join(am.Home, "agents", name)
	if _, err := os.Stat(agentDir); err != nil {
		return fmt.Errorf("agent %q not found", name)
	}

	// Stop containers
	am.stopAgentContainers(ctx, name)

	// Archive audit logs (tenet 2: every action leaves a trace)
	am.archiveAuditLogs(name)

	// Remove workspace volume
	volName := fmt.Sprintf("%s-%s-workspace-data", prefix, name)
	_ = am.cli.VolumeRemove(ctx, volName, true)

	// Remove agent directory
	os.RemoveAll(agentDir)

	// Remove enforcer data
	os.RemoveAll(filepath.Join(am.Home, "infrastructure", "enforcer", "data", name))

	// Remove agent network
	_ = am.cli.NetworkRemove(ctx, fmt.Sprintf("%s-%s-internal", prefix, name))

	am.log.Info("agent deleted", "name", name)
	return nil
}

// JoinChannel joins an agent to a comms channel.
func (am *AgentManager) JoinChannel(ctx context.Context, agentName, channel string) {
	body := map[string]interface{}{"participant": agentName}
	am.Docker.CommsRequest(ctx, "POST", "/channels/"+channel+"/join", body)
}

// StopContainers stops and removes all containers for an agent.
// Marks the agent as suppressed so watchers don't fire spurious alerts.
func (am *AgentManager) StopContainers(ctx context.Context, name string) {
	if am.StopSuppress != nil {
		am.StopSuppress.Suppress(name)
	}
	am.stopAgentContainers(ctx, name)
}

// -- Internal helpers --

func (am *AgentManager) loadAgentDetail(name, agentsDir string, running map[string]containerInfo, teamIndex map[string]string) AgentDetail {
	agentDir := filepath.Join(agentsDir, name)
	d := AgentDetail{
		Name:     name,
		AgentDir: agentDir,
		Status:   "stopped",
	}

	// Read agent.yaml
	agentYAMLPath := filepath.Join(agentDir, "agent.yaml")
	data, err := os.ReadFile(agentYAMLPath)
	if err == nil {
		var ay map[string]interface{}
		if yaml.Unmarshal(data, &ay) == nil {
			d.Type, _ = ay["type"].(string)
			if p, ok := ay["preset"].(string); ok {
				d.Preset = p
			}
			if m, ok := ay["model"].(string); ok {
				d.Model = m
			}
			if mt, ok := ay["model_tier"].(string); ok {
				d.ModelTier = mt
			}
			// Backfill lifecycle_id for agents created before this field existed.
			if _, ok := ay["lifecycle_id"].(string); !ok || ay["lifecycle_id"] == "" {
				newID := uuid.New().String()
				ay["lifecycle_id"] = newID
				writeYAML(agentYAMLPath, ay) //nolint:errcheck
			}
			d.LifecycleID, _ = ay["lifecycle_id"].(string)
			// Detect unknown top-level keys
			for k := range ay {
				if !knownAgentYAMLKeys[k] {
					d.UnknownKeys = append(d.UnknownKeys, k)
				}
			}
			sort.Strings(d.UnknownKeys)
		}
	}

	// Read constraints for restrictions, granted capabilities, role
	cdata, err := os.ReadFile(filepath.Join(agentDir, "constraints.yaml"))
	if err == nil {
		var cy map[string]interface{}
		if yaml.Unmarshal(cdata, &cy) == nil {
			if hl, ok := cy["hard_limits"].([]interface{}); ok {
				d.Constraints = &ConstraintsSummary{
					HardLimits: len(hl),
				}
				for _, h := range hl {
					if hm, ok := h.(map[string]interface{}); ok {
						if rule, ok := hm["rule"].(string); ok {
							d.Restrictions = append(d.Restrictions, rule)
						}
					}
				}
			}
			if identity, ok := cy["identity"].(map[string]interface{}); ok {
				d.Role, _ = identity["role"].(string)
			}
			if gc, ok := cy["granted_capabilities"].([]interface{}); ok {
				for _, c := range gc {
					if s, ok := c.(string); ok {
						d.GrantedCaps = append(d.GrantedCaps, s)
					}
				}
			}
		}
	}

	// Read trust level
	trustData, err := os.ReadFile(filepath.Join(agentDir, "trust.yaml"))
	if err == nil {
		var ty map[string]interface{}
		if yaml.Unmarshal(trustData, &ty) == nil {
			if lvl, ok := ty["level"].(int); ok {
				d.TrustLevel = lvl
			}
		}
	}

	// Read granted services from services.yaml
	svcData, err := os.ReadFile(filepath.Join(agentDir, "services.yaml"))
	if err == nil {
		var sg struct {
			Grants []struct {
				Service string `yaml:"service"`
			} `yaml:"grants"`
		}
		if yaml.Unmarshal(svcData, &sg) == nil {
			for _, g := range sg.Grants {
				if g.Service != "" {
					d.GrantedServices = append(d.GrantedServices, g.Service)
				}
			}
		}
	}

	// Read current task from session context, cross-referenced with heartbeat signals.
	// The body runtime cannot clear current_task from session-context.json because it's
	// mounted read-only (ASK tenet 5). Instead, the gateway (operator-owned process)
	// checks the latest heartbeat: if active_task is null, the task is done and we
	// clear the stale entry from the context file.
	ctxFile := filepath.Join(agentDir, "state", "session-context.json")
	ctxData, err := os.ReadFile(ctxFile)
	if err == nil {
		var sc map[string]interface{}
		if json.Unmarshal(ctxData, &sc) == nil {
			if ct, ok := sc["current_task"].(map[string]interface{}); ok {
				ts := &TaskSummary{}
				ts.TaskID, _ = ct["task_id"].(string)
				ts.Content, _ = ct["content"].(string)
				ts.Timestamp, _ = ct["timestamp"].(string)
				ts.Source, _ = ct["source"].(string)
				if ts.TaskID != "" || ts.Content != "" {
					// Check task_complete signals — if the body runtime has signaled
					// completion for this task, the session-context entry is stale.
					if taskIsComplete(filepath.Join(agentDir, "state", "agent-signals.jsonl"), ts.TaskID) {
						delete(sc, "current_task")
						if updated, err := json.MarshalIndent(sc, "", "  "); err == nil {
							os.WriteFile(ctxFile, updated, 0666)
						}
					} else {
						d.CurrentTask = ts
					}
				}
			}
		}
	}

	// Find team membership via pre-built index (built once per List/Show call)
	d.Team = teamIndex[name]

	// Read mission info if present
	missionPath := filepath.Join(am.Home, "agents", name, "mission.yaml")
	if data, err := os.ReadFile(missionPath); err == nil {
		var m struct {
			Name   string `yaml:"name"`
			Status string `yaml:"status"`
		}
		if yaml.Unmarshal(data, &m) == nil {
			d.Mission = m.Name
			d.MissionStatus = m.Status
		}
	}

	// Runtime status from containers
	wsName := fmt.Sprintf("%s-%s-workspace", prefix, name)
	enfName := fmt.Sprintf("%s-%s-enforcer", prefix, name)

	if ci, ok := running[wsName]; ok {
		d.Workspace = ci.State
		d.Status = ci.State
	} else {
		d.Workspace = "stopped"
	}
	if ci, ok := running[enfName]; ok {
		d.Enforcer = ci.State
		if ci.BuildID != "" {
			d.BuildID = ci.BuildID
		}
	} else {
		d.Enforcer = "stopped"
	}

	// ASK Tenet 3: Mediation is complete. An agent running without its
	// enforcer has no API mediation — report as unhealthy, not running.
	if d.Workspace == "running" && d.Enforcer != "running" {
		d.Status = "unhealthy"
	}

	// Last active: most recent signal timestamp
	signalsPath := filepath.Join(agentDir, "state", "agent-signals.jsonl")
	d.LastActive = lastSignalTimestamp(signalsPath)

	return d
}

// taskIsComplete checks the agent's signals file for a task_complete signal
// matching the given task ID. Returns true if the task has been completed.
// This replaces heartbeat polling — task lifecycle is tracked via explicit
// signals (task_accepted, task_complete) not periodic heartbeats.
//
// Performance: reads only the last 8KB of the file (enough for ~50 JSON lines)
// instead of loading the entire file. Signals files grow unboundedly for
// long-running agents, so tail-reading keeps list calls O(1) in file size.
func taskIsComplete(signalsPath, taskID string) bool {
	f, err := os.Open(signalsPath)
	if err != nil {
		return false
	}
	defer f.Close()

	stat, err := f.Stat()
	if err != nil {
		return false
	}

	// Read last 8KB — enough for ~50 JSON signal lines
	const tailSize = int64(8192)
	readSize := tailSize
	if stat.Size() < readSize {
		readSize = stat.Size()
	}
	if readSize == 0 {
		return false
	}

	buf := make([]byte, readSize)
	if _, err = f.ReadAt(buf, stat.Size()-readSize); err != nil && err != io.EOF {
		return false
	}

	lines := strings.Split(string(buf), "\n")
	// Walk backwards — most recent completion is near the end
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		var sig map[string]interface{}
		if json.Unmarshal([]byte(line), &sig) != nil {
			continue
		}
		if sig["signal_type"] != "task_complete" {
			continue
		}
		sigData, ok := sig["data"].(map[string]interface{})
		if !ok {
			continue
		}
		// Match by task_id, or if no task_id in session-context, any completion counts
		if taskID == "" {
			return true
		}
		if sigTaskID, _ := sigData["task_id"].(string); sigTaskID == taskID {
			return true
		}
	}
	return false
}

// lastSignalTimestamp returns the ISO timestamp of the most recent signal entry,
// or empty string if no signals exist. Reads only the tail of the file.
func lastSignalTimestamp(signalsPath string) string {
	f, err := os.Open(signalsPath)
	if err != nil {
		return ""
	}
	defer f.Close()

	stat, err := f.Stat()
	if err != nil || stat.Size() == 0 {
		return ""
	}

	const tailSize = int64(4096)
	readSize := tailSize
	if stat.Size() < readSize {
		readSize = stat.Size()
	}
	buf := make([]byte, readSize)
	f.ReadAt(buf, stat.Size()-readSize)

	lines := strings.Split(string(buf), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		var sig map[string]interface{}
		if json.Unmarshal([]byte(line), &sig) != nil {
			continue
		}
		if ts, ok := sig["timestamp"].(string); ok && ts != "" {
			return ts
		}
	}
	return ""
}

// buildTeamIndex builds a reverse map of agentName → teamName by reading all
// team YAML files once. Called once per List/Show invocation so that team
// membership is resolved in O(teams) rather than O(agents*teams).
func buildTeamIndex(home string) map[string]string {
	index := make(map[string]string)
	teamsDir := filepath.Join(home, "teams")
	entries, err := os.ReadDir(teamsDir)
	if err != nil {
		return index
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		teamName := entry.Name()
		teamData, err := os.ReadFile(filepath.Join(teamsDir, teamName, "team.yaml"))
		if err != nil {
			continue
		}
		var ty map[string]interface{}
		if yaml.Unmarshal(teamData, &ty) != nil {
			continue
		}
		members, ok := ty["members"].([]interface{})
		if !ok {
			continue
		}
		for _, m := range members {
			if agentName, ok := m.(string); ok && agentName != "" {
				index[agentName] = teamName
			}
		}
	}
	return index
}

// containerInfo holds state and build metadata for a running container.
type containerInfo struct {
	State   string
	BuildID string // value of agency.build.gateway label
}

func (am *AgentManager) getRunningContainers(ctx context.Context) map[string]containerInfo {
	if am.cli == nil {
		return make(map[string]containerInfo)
	}
	result := make(map[string]containerInfo)
	containers, err := am.cli.ContainerList(ctx, container.ListOptions{
		All:     true,
		Filters: filters.NewArgs(filters.Arg("name", prefix+"-")),
	})
	if err != nil {
		return result
	}
	for _, c := range containers {
		for _, n := range c.Names {
			n = strings.TrimPrefix(n, "/")
			ci := containerInfo{State: c.State}
			if bid, ok := c.Labels["agency.build.gateway"]; ok {
				ci.BuildID = bid
			}
			result[n] = ci
		}
	}
	return result
}

func (am *AgentManager) stopAgentContainers(ctx context.Context, name string) {
	containers := []string{
		fmt.Sprintf("%s-%s-workspace", prefix, name),
		fmt.Sprintf("%s-%s-enforcer", prefix, name),
	}
	timeout := 3
	var wg sync.WaitGroup
	for _, cname := range containers {
		cname := cname
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = am.cli.ContainerStop(ctx, cname, container.StopOptions{Timeout: &timeout})
			_ = am.cli.ContainerRemove(ctx, cname, container.RemoveOptions{Force: true})
		}()
	}
	wg.Wait()
}

func (am *AgentManager) archiveAuditLogs(name string) {
	auditDir := filepath.Join(am.Home, "audit", name)
	if _, err := os.Stat(auditDir); err != nil {
		return
	}
	ts := time.Now().UTC().Format("20060102T150405Z")
	archiveDir := filepath.Join(am.Home, "audit", ".archived", fmt.Sprintf("%s-%s", name, ts))
	os.MkdirAll(filepath.Dir(archiveDir), 0755)
	os.Rename(auditDir, archiveDir)
}

func validateAgentName(name string) error {
	if len(name) < 2 {
		return fmt.Errorf("agent name %q is too short (min 2 characters)", name)
	}
	if !namePattern.MatchString(name) {
		return fmt.Errorf("invalid agent name %q: must be lowercase alphanumeric with hyphens", name)
	}
	if reservedNames[name] {
		return fmt.Errorf("agent name %q is reserved", name)
	}
	return nil
}

func writeYAML(path string, v interface{}) error {
	data, err := yaml.Marshal(v)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		// Fallback — should never happen
		for i := range b {
			b[i] = byte(i)
		}
	}
	const hexChars = "0123456789abcdef"
	for i, v := range b {
		b[i] = hexChars[v%16]
	}
	return string(b)
}

// Suppress unused imports
var (
	_ = nat.PortSet{}
	_ = network.EndpointSettings{}
	_ json.RawMessage
)
