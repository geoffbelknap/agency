package orchestrate

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	runtimecontract "github.com/geoffbelknap/agency/internal/runtime/contract"
)

const probeRuntimeBackendName = "probe"

type probeRuntimeBackend struct {
	home string
}

type probeRuntimeState struct {
	WorkspaceState string `json:"workspace_state"`
	EnforcerState  string `json:"enforcer_state"`
	LastError      string `json:"last_error,omitempty"`
}

func (b *probeRuntimeBackend) Name() string {
	return probeRuntimeBackendName
}

func (b *probeRuntimeBackend) Ensure(ctx context.Context, spec runtimecontract.RuntimeSpec) error {
	if err := b.EnsureEnforcer(ctx, spec, false); err != nil {
		return err
	}
	return b.EnsureWorkspace(ctx, spec)
}

func (b *probeRuntimeBackend) EnsureEnforcer(ctx context.Context, spec runtimecontract.RuntimeSpec, rotateKey bool) error {
	_ = ctx
	_ = rotateKey
	state := b.loadState(spec.RuntimeID)
	state.EnforcerState = "running"
	if state.WorkspaceState == "" {
		state.WorkspaceState = "stopped"
	}
	state.LastError = ""
	return b.saveState(spec.RuntimeID, state)
}

func (b *probeRuntimeBackend) EnsureWorkspace(ctx context.Context, spec runtimecontract.RuntimeSpec) error {
	_ = ctx
	state := b.loadState(spec.RuntimeID)
	if state.EnforcerState == "" {
		state.EnforcerState = "running"
	}
	state.WorkspaceState = "running"
	state.LastError = ""
	return b.saveState(spec.RuntimeID, state)
}

func (b *probeRuntimeBackend) ReloadEnforcer(ctx context.Context, spec runtimecontract.RuntimeSpec) error {
	_ = ctx
	_ = spec
	return nil
}

func (b *probeRuntimeBackend) Stop(ctx context.Context, runtimeID string) error {
	_ = ctx
	return b.saveState(runtimeID, probeRuntimeState{
		WorkspaceState: "stopped",
		EnforcerState:  "stopped",
	})
}

func (b *probeRuntimeBackend) Inspect(ctx context.Context, runtimeID string) (runtimecontract.BackendStatus, error) {
	_ = ctx
	state := b.loadState(runtimeID)
	if state.WorkspaceState == "" {
		state.WorkspaceState = "stopped"
	}
	if state.EnforcerState == "" {
		state.EnforcerState = "stopped"
	}
	status := runtimecontract.BackendStatus{
		RuntimeID: runtimeID,
		Details: map[string]string{
			"workspace_state": state.WorkspaceState,
			"enforcer_state":  state.EnforcerState,
		},
	}
	if state.LastError != "" {
		status.Details["last_error"] = state.LastError
	}
	switch {
	case state.WorkspaceState == "running" && state.EnforcerState == "running":
		status.Phase = runtimecontract.RuntimePhaseRunning
		status.Healthy = true
	case state.WorkspaceState == "running" && state.EnforcerState != "running":
		status.Phase = runtimecontract.RuntimePhaseDegraded
		status.Details["last_error"] = firstNonEmpty(state.LastError, "workspace is running without an enforcer")
	case state.WorkspaceState != "running" && state.EnforcerState == "running":
		status.Phase = runtimecontract.RuntimePhaseStarting
	case state.WorkspaceState == "failed" || state.EnforcerState == "failed":
		status.Phase = runtimecontract.RuntimePhaseFailed
		status.Details["last_error"] = firstNonEmpty(state.LastError, "probe runtime entered failed state")
	default:
		status.Phase = runtimecontract.RuntimePhaseStopped
	}
	return status, nil
}

func (b *probeRuntimeBackend) Validate(ctx context.Context, runtimeID string) error {
	status, err := b.Inspect(ctx, runtimeID)
	if err != nil {
		return err
	}
	if status.Details["enforcer_state"] != "running" {
		return fmt.Errorf("runtime %q enforcer is not running", runtimeID)
	}
	return nil
}

func (b *probeRuntimeBackend) Capabilities(ctx context.Context) (runtimecontract.BackendCapabilities, error) {
	_ = ctx
	return runtimecontract.BackendCapabilities{
		SupportedTransportTypes: []string{runtimecontract.TransportTypeLoopbackHTTP},
		SupportsComposeLike:     false,
		SupportsRootless:        true,
	}, nil
}

func (b *probeRuntimeBackend) statePath(runtimeID string) string {
	return filepath.Join(b.home, "agents", runtimeID, "state", "runtime-probe-state.json")
}

func (b *probeRuntimeBackend) loadState(runtimeID string) probeRuntimeState {
	data, err := os.ReadFile(b.statePath(runtimeID))
	if err != nil {
		return probeRuntimeState{}
	}
	var state probeRuntimeState
	if json.Unmarshal(data, &state) != nil {
		return probeRuntimeState{}
	}
	return state
}

func (b *probeRuntimeBackend) saveState(runtimeID string, state probeRuntimeState) error {
	path := b.statePath(runtimeID)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
