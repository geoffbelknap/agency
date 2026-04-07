package orchestrate

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"log/slog"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"

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
	Home    string
	Version string
	Docker  *agencyDocker.Client
	cli     *client.Client
	log     *slog.Logger
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

// Halt pauses an agent's containers and records the halt event.
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

	// Pause containers: workspace first, then enforcer
	containers := []string{
		fmt.Sprintf("%s-%s-workspace", prefix, agentName),
		fmt.Sprintf("%s-%s-enforcer", prefix, agentName),
	}

	for _, cname := range containers {
		info, err := hc.cli.ContainerInspect(ctx, cname)
		if err != nil {
			continue // Container doesn't exist
		}
		if info.State.Running {
			if err := hc.cli.ContainerPause(ctx, cname); err != nil {
				hc.log.Warn("pause failed", "container", cname, "err", err)
			}
		}
	}

	record.Executed = true

	// Save halt record
	hc.saveRecord(agentName, record)

	// Clear current task so it doesn't replay on restart
	contextFile := filepath.Join(hc.Home, "agents", agentName, "state", "session-context.json")
	if fileExists(contextFile) {
		os.WriteFile(contextFile, []byte("{}\n"), 0666)
	}

	hc.log.Info("agent halted", "agent", agentName, "type", haltType, "initiator", initiator)
	return record, nil
}

// Resume unpauses a halted agent's containers.
// Reconnects infrastructure first, then unpauses sidecars before workspace
// so enforcement is active first.
func (hc *HaltController) Resume(ctx context.Context, agentName, initiator string) error {
	if initiator == "" {
		initiator = "operator"
	}

	wsName := fmt.Sprintf("%s-%s-workspace", prefix, agentName)
	info, err := hc.cli.ContainerInspect(ctx, wsName)
	if err != nil {
		return fmt.Errorf("agent %s not found — use start instead", agentName)
	}

	if !info.State.Paused {
		if info.State.Running {
			return fmt.Errorf("agent %s is already running", agentName)
		}
		return fmt.Errorf("agent %s is stopped — use start instead", agentName)
	}

	// Verify enforcer is present
	enfName := fmt.Sprintf("%s-%s-enforcer", prefix, agentName)
	enfInfo, err := hc.cli.ContainerInspect(ctx, enfName)
	if err != nil || (!enfInfo.State.Running && !enfInfo.State.Paused) {
		return fmt.Errorf("cannot resume %s: enforcer not running — restart the agent", agentName)
	}

	// Unpause sidecars first (enforcement active before workspace)
	for _, cname := range []string{enfName} {
		_ = hc.cli.ContainerUnpause(ctx, cname)
	}

	// Unpause workspace
	if err := hc.cli.ContainerUnpause(ctx, wsName); err != nil {
		return fmt.Errorf("unpause workspace: %w", err)
	}

	hc.log.Info("agent resumed", "agent", agentName, "initiator", initiator)
	return nil
}

// Status returns the halt status of an agent.
func (hc *HaltController) Status(ctx context.Context, agentName string) string {
	wsName := fmt.Sprintf("%s-%s-workspace", prefix, agentName)
	info, err := hc.cli.ContainerInspect(ctx, wsName)
	if err != nil {
		return "stopped"
	}
	if info.State.Paused {
		return "halted"
	}
	if info.State.Running {
		return "running"
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

// Suppress unused import
var _ = container.StopOptions{}
