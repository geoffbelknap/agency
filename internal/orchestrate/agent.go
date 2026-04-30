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
	"time"

	"github.com/google/uuid"
	"gopkg.in/yaml.v3"
	"log/slog"

	"github.com/geoffbelknap/agency/internal/comms"
	"github.com/geoffbelknap/agency/internal/hostadapter/runtimehost"
	runtimecontract "github.com/geoffbelknap/agency/internal/runtime/contract"
)

var namePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*[a-z0-9]$`)

var reservedNames = map[string]bool{
	"infra-egress": true, "agency": true, "enforcer": true,
	"gateway": true, "workspace": true,
}

// AgentDetail contains the full agent definition and runtime status.
type AgentDetail struct {
	Name            string              `json:"name"`
	Type            string              `json:"type"`
	Status          string              `json:"status"` // running, stopped, paused
	Model           string              `json:"model,omitempty"`
	ModelTier       string              `json:"model_tier,omitempty"`
	Preset          string              `json:"preset,omitempty"`
	Role            string              `json:"role,omitempty"`
	Team            string              `json:"team,omitempty"`
	TrustLevel      int                 `json:"trust_level,omitempty"`
	Workspace       string              `json:"workspace"`
	Enforcer        string              `json:"enforcer"`
	AgentDir        string              `json:"agent_dir"`
	LifecycleID     string              `json:"lifecycle_id,omitempty"`
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
	Home         string
	Backend      *runtimehost.BackendHandle
	Comms        comms.Client
	Version      string
	SourceDir    string
	BuildID      string
	BackendName  string
	Runtime      *RuntimeSupervisor
	log          *slog.Logger
	StopSuppress *StopSuppression
	infra        *Infra // optional — nil in tests without infra
}

func NewAgentManager(home string, dc *runtimehost.BackendHandle, logger *slog.Logger) (*AgentManager, error) {
	return &AgentManager{Home: home, Backend: dc, Comms: dc, log: logger}, nil
}

// SetInfra attaches the infrastructure manager (and its principal registry)
// to the agent manager. Must be called after both are initialised.
func (am *AgentManager) SetInfra(inf *Infra) {
	am.infra = inf
}

// List returns all defined agents with their runtime status.
func (am *AgentManager) List(ctx context.Context) ([]AgentDetail, error) {
	agentsDir := filepath.Join(am.Home, "agents")
	names, err := am.Names(ctx)
	if err != nil {
		return nil, err
	}

	// Build team membership index once for the entire list — avoids reading all
	// team YAML files once per agent (O(agents*teams) → O(teams)).
	teamIndex := buildTeamIndex(am.Home)

	agents := make([]AgentDetail, 0, len(names))
	for _, name := range names {
		detail := am.loadAgentDetail(ctx, name, agentsDir, teamIndex)
		agents = append(agents, detail)
	}

	sort.Slice(agents, func(i, j int) bool { return agents[i].Name < agents[j].Name })
	return agents, nil
}

// Names returns all defined agent names without loading runtime detail.
func (am *AgentManager) Names(ctx context.Context) ([]string, error) {
	agentsDir := filepath.Join(am.Home, "agents")
	entries, err := os.ReadDir(agentsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, err
	}

	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !e.IsDir() {
			continue
		}
		agentYAML := filepath.Join(agentsDir, e.Name(), "agent.yaml")
		if _, err := os.Stat(agentYAML); err != nil {
			continue
		}
		names = append(names, e.Name())
	}
	sort.Strings(names)
	return names, nil
}

// Show returns details for a single agent.
func (am *AgentManager) Show(ctx context.Context, name string) (*AgentDetail, error) {
	agentsDir := filepath.Join(am.Home, "agents")
	agentDir := filepath.Join(agentsDir, name)
	if _, err := os.Stat(filepath.Join(agentDir, "agent.yaml")); err != nil {
		return nil, fmt.Errorf("agent %q not found", name)
	}

	teamIndex := buildTeamIndex(am.Home)
	detail := am.loadAgentDetail(ctx, name, agentsDir, teamIndex)
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

	// Register in principal registry — gives the agent a stable UUID that
	// survives renames, restarts, and shows up in audit trails and knowledge
	// graph references. Fallback to local generation for test paths where
	// infra/registry may be nil.
	agentUUID := am.registerAgentPrincipal(name)
	if agentUUID == "" {
		agentUUID = uuid.New().String()
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
		"uuid":         agentUUID,
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
	if tier, ok := presetData["model_tier"]; ok {
		agentYAML["model_tier"] = tier
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
		"version":              "0.1",
		"agent":                name,
		"identity":             identityBlock,
		"granted_capabilities": []string{"provider-web-search"},
		"hard_limits":          hardLimits,
		"escalation":           escalation,
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
	// Some VM-backed host backends do not reliably map bind-mount UIDs, so we
	// need open permissions for cross-runtime writes.
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

func (am *AgentManager) registerAgentPrincipal(name string) string {
	if am.infra == nil || am.infra.Registry == nil {
		return ""
	}
	agentUUID, regErr := am.infra.Registry.Register("agent", name)
	if regErr != nil {
		if _, retireErr := am.infra.Registry.RetireByName("agent", name); retireErr == nil {
			agentUUID, regErr = am.infra.Registry.Register("agent", name)
		}
	}
	if regErr != nil {
		if am.log != nil {
			am.log.Warn("registry: agent registration failed, using local UUID", "err", regErr)
		}
		return ""
	}
	if snapErr := am.infra.WriteRegistrySnapshot(); snapErr != nil && am.log != nil {
		am.log.Warn("registry: snapshot write failed", "err", snapErr)
	}
	return agentUUID
}

func (am *AgentManager) retireAgentPrincipal(name string) {
	if am.infra == nil || am.infra.Registry == nil {
		return
	}
	if _, err := am.infra.Registry.RetireByName("agent", name); err != nil {
		if am.log != nil {
			am.log.Warn("registry: agent principal retirement failed", "agent", name, "err", err)
		}
		return
	}
	if err := am.infra.WriteRegistrySnapshot(); err != nil && am.log != nil {
		am.log.Warn("registry: snapshot write failed", "err", err)
	}
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

	// Stop runtime
	am.StopAgentRuntime(ctx, name)

	// Archive audit logs (tenet 2: every action leaves a trace)
	am.archiveAuditLogs(name)

	// Remove workspace volume
	if am.Backend != nil {
		_ = runtimehost.RemoveRuntimeArtifacts(ctx, am.Backend, name)
	}

	// Remove agent directory
	if err := removePathAll(agentDir); err != nil {
		return fmt.Errorf("remove agent directory: %w", err)
	}

	// Remove enforcer data
	if err := removePathAll(filepath.Join(am.Home, "infrastructure", "enforcer", "data", name)); err != nil {
		return fmt.Errorf("remove enforcer data: %w", err)
	}

	am.retireAgentPrincipal(name)

	am.log.Info("agent deleted", "name", name)
	return nil
}

// JoinChannel joins an agent to a comms channel.
func (am *AgentManager) JoinChannel(ctx context.Context, agentName, channel string) {
	body := map[string]interface{}{"participant": agentName}
	am.Comms.CommsRequest(ctx, "POST", "/channels/"+channel+"/join", body)
}

// StopAgentRuntime stops and removes the runtime for an agent.
// Marks the agent as suppressed so watchers don't fire spurious alerts.
func (am *AgentManager) StopAgentRuntime(ctx context.Context, name string) {
	if am.StopSuppress != nil {
		am.StopSuppress.Suppress(name)
	}
	if am.Runtime != nil {
		_ = am.Runtime.Stop(ctx, name)
		return
	}
	am.stopBackendRuntime(ctx, name)
}

// -- Internal helpers --

func (am *AgentManager) loadAgentDetail(ctx context.Context, name, agentsDir string, teamIndex map[string]string) AgentDetail {
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

	applyPersistedAgentStatus(ctx, &d, am, name)
	if activeHaltExists(am.Home, name) && d.Status != "running" {
		d.Status = "halted"
	}

	// Last active: most recent signal timestamp
	signalsPath := filepath.Join(agentDir, "state", "agent-signals.jsonl")
	d.LastActive = lastSignalTimestamp(signalsPath)

	return d
}

func applyRuntimeStatus(d *AgentDetail, status runtimecontract.RuntimeStatus) {
	switch status.Phase {
	case runtimecontract.RuntimePhaseRunning:
		d.Status = "running"
		d.Workspace = "running"
		d.Enforcer = "running"
	case runtimecontract.RuntimePhaseStopped:
		d.Status = "stopped"
		d.Workspace = "stopped"
		d.Enforcer = "stopped"
	case runtimecontract.RuntimePhaseDegraded:
		d.Status = "unhealthy"
		d.Workspace = "running"
		if status.Transport.EnforcerConnected {
			d.Enforcer = "running"
		} else {
			d.Enforcer = "stopped"
		}
	case runtimecontract.RuntimePhaseStarting, runtimecontract.RuntimePhaseReconciled, runtimecontract.RuntimePhaseCompiled:
		d.Status = "starting"
		d.Workspace = "stopped"
		if status.Transport.EnforcerConnected {
			d.Enforcer = "running"
		} else {
			d.Enforcer = "stopped"
		}
	default:
		d.Status = "stopped"
		d.Workspace = "stopped"
		if status.Transport.EnforcerConnected {
			d.Enforcer = "running"
		} else {
			d.Enforcer = "stopped"
		}
	}
}

func applyPersistedAgentStatus(ctx context.Context, d *AgentDetail, am *AgentManager, name string) {
	if am.Runtime != nil {
		statusCtx, cancel := context.WithTimeout(ctx, 150*time.Millisecond)
		defer cancel()
		if status, err := am.Runtime.Get(statusCtx, name); err == nil {
			applyRuntimeStatus(d, status)
			return
		}
		if manifest, err := am.Runtime.Manifest(name); err == nil && manifest.Status.RuntimeID != "" {
			applyRuntimeStatus(d, manifest.Status)
			return
		}
	}
	d.Status = "stopped"
	d.Workspace = "stopped"
	d.Enforcer = "stopped"
	if activeHaltExists(am.Home, name) {
		d.Status = "halted"
	}
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

func (am *AgentManager) stopBackendRuntime(ctx context.Context, name string) {
	if am.Backend == nil {
		return
	}
	_ = runtimehost.StopRuntime(ctx, am.Backend, name)
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

func removePathAll(path string) error {
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	// Normalize permissions before deletion so backend-created files do not
	// silently leave ghost agent directories behind.
	_ = filepath.Walk(path, func(current string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			_ = os.Chmod(current, 0o755)
			return nil
		}
		_ = os.Chmod(current, 0o644)
		return nil
	})

	if err := os.RemoveAll(path); err != nil {
		return err
	}
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("%s still exists after removal", path)
	} else if !os.IsNotExist(err) {
		return err
	}
	return nil
}

var _ json.RawMessage
