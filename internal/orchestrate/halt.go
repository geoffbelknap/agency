package orchestrate

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"log/slog"

	"github.com/geoffbelknap/agency/internal/comms"
	"github.com/geoffbelknap/agency/internal/credstore"
	agencyDocker "github.com/geoffbelknap/agency/internal/docker"
)

// HaltRecord describes a halt event.
type HaltRecord struct {
	HaltID    string `json:"halt_id"`
	Agent     string `json:"agent"`
	HaltType  string `json:"halt_type"`
	Initiator string `json:"initiator"`
	Reason    string `json:"reason,omitempty"`
	Timestamp string `json:"timestamp"`
	Executed  bool   `json:"executed"`
}

var validHaltTypes = map[string]bool{
	"supervised": true,
	"immediate":  true,
	"graceful":   true,
	"emergency":  true,
}

// HaltController manages halt and resume operations.
type HaltController struct {
	Home         string
	Version      string
	Docker       *agencyDocker.Client
	SourceDir    string
	BuildID      string
	Comms        comms.Client
	CredStore    *credstore.Store
	StopSuppress *StopSuppression
	cli          *client.Client
	log          *slog.Logger
}

func NewHaltController(home, version string, dc *agencyDocker.Client, logger *slog.Logger) (*HaltController, error) {
	var cli *client.Client
	if dc != nil {
		cli = dc.RawClient()
	} else {
		var err error
		cli, err = client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
		if err != nil {
			return nil, err
		}
	}
	return &HaltController{Home: home, Version: version, Docker: dc, cli: cli, log: logger}, nil
}

// Halt stops an agent's containers, records the halt event, and preserves
// halted state in a marker file so status/reporting survive container removal.
// Tenet 9: Every halt is auditable and reversible.
// Tenet 10: Halt authority is asymmetric — resumption >= halt authority.
func (hc *HaltController) Halt(ctx context.Context, agentName, haltType, reason, initiator string) (*HaltRecord, error) {
	if !validHaltTypes[haltType] {
		return nil, fmt.Errorf("unknown halt type %q (valid: supervised, immediate, graceful, emergency)", haltType)
	}
	if initiator == "" {
		initiator = "operator"
	}

	now := time.Now().UTC()
	record := &HaltRecord{
		HaltID:    fmt.Sprintf("halt-%s-%s", now.Format("20060102"), randomHex(8)),
		Agent:     agentName,
		HaltType:  haltType,
		Initiator: initiator,
		Reason:    reason,
		Timestamp: now.Format(time.RFC3339),
	}

	if hc.StopSuppress != nil {
		hc.StopSuppress.Suppress(agentName)
	}

	// Stop containers: workspace first, then enforcer.
	containers := []string{
		fmt.Sprintf("%s-%s-workspace", prefix, agentName),
		fmt.Sprintf("%s-%s-enforcer", prefix, agentName),
	}

	for _, cname := range containers {
		info, err := hc.cli.ContainerInspect(ctx, cname)
		if err != nil {
			continue // Container doesn't exist
		}
		if info.State.Running || info.State.Paused {
			timeout := 30
			if err := hc.cli.ContainerStop(ctx, cname, container.StopOptions{Timeout: &timeout}); err != nil {
				hc.log.Warn("stop failed", "container", cname, "err", err)
			}
		}
		if err := hc.cli.ContainerRemove(ctx, cname, container.RemoveOptions{Force: true}); err != nil {
			hc.log.Warn("remove failed", "container", cname, "err", err)
		}
	}

	record.Executed = true

	// Save halt record
	hc.saveRecord(agentName, record)
	hc.setActiveHalt(agentName, record)

	// Clear current task so it doesn't replay on restart
	contextFile := filepath.Join(hc.Home, "agents", agentName, "state", "session-context.json")
	if fileExists(contextFile) {
		os.WriteFile(contextFile, []byte("{}\n"), 0666)
	}

	hc.log.Info("agent halted", "agent", agentName, "type", haltType, "initiator", initiator)
	return record, nil
}

// Resume restarts a halted agent through the canonical start sequence.
func (hc *HaltController) Resume(ctx context.Context, agentName, initiator string) error {
	if initiator == "" {
		initiator = "operator"
	}

	wsName := fmt.Sprintf("%s-%s-workspace", prefix, agentName)
	info, err := hc.cli.ContainerInspect(ctx, wsName)
	if err == nil && info.State.Running && !info.State.Paused {
		return fmt.Errorf("agent %s is already running", agentName)
	}
	if err != nil && !activeHaltExists(hc.Home, agentName) {
		return fmt.Errorf("agent %s is stopped — use start instead", agentName)
	}
	if err == nil && !info.State.Paused && !activeHaltExists(hc.Home, agentName) {
		return fmt.Errorf("agent %s is stopped — use start instead", agentName)
	}

	if hc.StopSuppress != nil {
		hc.StopSuppress.Suppress(agentName)
	}
	defer func() {
		if hc.StopSuppress != nil {
			hc.StopSuppress.Release(agentName)
		}
	}()

	// Clean up any legacy paused or stopped containers before replaying the
	// full start sequence.
	for _, cname := range []string{
		wsName,
		fmt.Sprintf("%s-%s-enforcer", prefix, agentName),
	} {
		if _, err := hc.cli.ContainerInspect(ctx, cname); err == nil {
			timeout := 30
			_ = hc.cli.ContainerStop(ctx, cname, container.StopOptions{Timeout: &timeout})
			_ = hc.cli.ContainerRemove(ctx, cname, container.RemoveOptions{Force: true})
		}
	}

	ss := &StartSequence{
		AgentName: agentName,
		Home:      hc.Home,
		Version:   hc.Version,
		SourceDir: hc.SourceDir,
		BuildID:   hc.BuildID,
		Docker:    hc.Docker,
		Comms:     hc.Comms,
		Log:       hc.log,
		CredStore: hc.CredStore,
	}
	if _, err := ss.Run(ctx, nil); err != nil {
		return fmt.Errorf("resume start sequence: %w", err)
	}

	hc.clearActiveHalt(agentName)
	hc.log.Info("agent resumed", "agent", agentName, "initiator", initiator)
	return nil
}

// Status returns the halt status of an agent.
func (hc *HaltController) Status(ctx context.Context, agentName string) string {
	wsName := fmt.Sprintf("%s-%s-workspace", prefix, agentName)
	info, err := hc.cli.ContainerInspect(ctx, wsName)
	if err == nil && info.State.Paused {
		return "halted"
	}
	if err == nil && info.State.Running {
		return "running"
	}
	if activeHaltExists(hc.Home, agentName) {
		return "halted"
	}
	return "stopped"
}

// HaltForUnackedConstraint halts an agent because a constraint change was not
// acknowledged within the allowed timeout. This satisfies ASK tenet 6
// (unacknowledged changes are treated as potential compromise) and tenet 8
// (halts are always auditable and reversible).
func (hc *HaltController) HaltForUnackedConstraint(ctx context.Context, agentName, changeID, reason string) error {
	record, err := hc.Halt(ctx, agentName, "immediate", reason, "system:constraint-timeout")
	if err != nil {
		return err
	}
	hc.log.Warn("agent halted for unacked constraint",
		"agent", agentName,
		"change_id", changeID,
		"halt_id", record.HaltID)
	return nil
}

func (hc *HaltController) saveRecord(agentName string, record *HaltRecord) {
	haltsDir := filepath.Join(hc.Home, "agents", agentName, "halts")
	os.MkdirAll(haltsDir, 0755)
	data, _ := json.MarshalIndent(record, "", "  ")
	os.WriteFile(filepath.Join(haltsDir, record.HaltID+".json"), data, 0644)
}

func activeHaltPath(home, agentName string) string {
	return filepath.Join(home, "agents", agentName, "state", "active-halt.json")
}

func activeHaltExists(home, agentName string) bool {
	_, err := os.Stat(activeHaltPath(home, agentName))
	return err == nil
}

func (hc *HaltController) setActiveHalt(agentName string, record *HaltRecord) {
	path := activeHaltPath(hc.Home, agentName)
	_ = os.MkdirAll(filepath.Dir(path), 0755)
	data, _ := json.MarshalIndent(record, "", "  ")
	_ = os.WriteFile(path, data, 0644)
}

func (hc *HaltController) clearActiveHalt(agentName string) {
	_ = os.Remove(activeHaltPath(hc.Home, agentName))
}

// Suppress unused import
var _ = container.StopOptions{}
