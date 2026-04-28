package runtimebackend

import (
	"context"
	"fmt"

	"github.com/docker/docker/api/types/container"

	"github.com/geoffbelknap/agency/internal/hostadapter/runtimehost"
	runtimecontract "github.com/geoffbelknap/agency/internal/runtime/contract"
)

type ContainerRuntimeBackend struct {
	BackendName        string
	Backend            *runtimehost.Client
	EnsureAgentNetwork func(ctx context.Context, runtimeID string) error
	EnsureEnforcerFn   func(ctx context.Context, spec runtimecontract.RuntimeSpec, rotateKey bool) error
	EnsureWorkspaceFn  func(ctx context.Context, spec runtimecontract.RuntimeSpec) error
}

func (b *ContainerRuntimeBackend) Name() string {
	if name := runtimehost.NormalizeContainerBackend(b.BackendName); name != "" {
		return name
	}
	return runtimehost.BackendDocker
}

func (b *ContainerRuntimeBackend) Ensure(ctx context.Context, spec runtimecontract.RuntimeSpec) error {
	if err := b.EnsureEnforcer(ctx, spec, false); err != nil {
		return err
	}
	return b.EnsureWorkspace(ctx, spec)
}

func (b *ContainerRuntimeBackend) ReloadEnforcer(ctx context.Context, spec runtimecontract.RuntimeSpec) error {
	sharedCli, err := b.rawClient()
	if err != nil {
		return err
	}
	return sharedCli.ContainerKill(ctx, fmt.Sprintf("agency-%s-enforcer", spec.RuntimeID), "SIGHUP")
}

func (b *ContainerRuntimeBackend) EnsureEnforcer(ctx context.Context, spec runtimecontract.RuntimeSpec, rotateKey bool) error {
	if b.EnsureAgentNetwork != nil {
		if err := b.EnsureAgentNetwork(ctx, spec.RuntimeID); err != nil {
			return err
		}
	}
	if b.EnsureEnforcerFn == nil {
		return fmt.Errorf("%s enforcer ensure is not configured", b.Name())
	}
	return b.EnsureEnforcerFn(ctx, spec, rotateKey)
}

func (b *ContainerRuntimeBackend) EnsureWorkspace(ctx context.Context, spec runtimecontract.RuntimeSpec) error {
	if b.EnsureWorkspaceFn == nil {
		return fmt.Errorf("%s workspace ensure is not configured", b.Name())
	}
	return b.EnsureWorkspaceFn(ctx, spec)
}

func (b *ContainerRuntimeBackend) Stop(ctx context.Context, runtimeID string) error {
	sharedCli, err := b.rawClient()
	if err != nil {
		return err
	}
	for _, name := range []string{
		fmt.Sprintf("agency-%s-workspace", runtimeID),
		fmt.Sprintf("agency-%s-enforcer", runtimeID),
	} {
		timeout := 10
		_ = sharedCli.ContainerStop(ctx, name, container.StopOptions{Timeout: &timeout})
		_ = sharedCli.ContainerRemove(ctx, name, container.RemoveOptions{Force: true})
	}
	return nil
}

func (b *ContainerRuntimeBackend) Inspect(ctx context.Context, runtimeID string) (runtimecontract.BackendStatus, error) {
	sharedCli, err := b.rawClient()
	if err != nil {
		return runtimecontract.BackendStatus{}, err
	}
	wsState := inspectContainerState(ctx, sharedCli, fmt.Sprintf("agency-%s-workspace", runtimeID))
	enfState := inspectContainerState(ctx, sharedCli, fmt.Sprintf("agency-%s-enforcer", runtimeID))
	status := runtimecontract.BackendStatus{
		RuntimeID: runtimeID,
		Details: map[string]string{
			"workspace_state": wsState,
			"enforcer_state":  enfState,
		},
	}
	switch {
	case wsState == "running" && enfState == "running":
		status.Phase = runtimecontract.RuntimePhaseRunning
		status.Healthy = true
	case wsState == "running" && enfState != "running":
		status.Phase = runtimecontract.RuntimePhaseDegraded
		status.Details["last_error"] = "workspace is running without an enforcer"
	case wsState != "running" && enfState == "running":
		status.Phase = runtimecontract.RuntimePhaseStarting
	case wsState == "exited" || enfState == "exited":
		status.Phase = runtimecontract.RuntimePhaseFailed
		status.Details["last_error"] = "runtime container exited unexpectedly"
	default:
		status.Phase = runtimecontract.RuntimePhaseStopped
	}
	return status, nil
}

func (b *ContainerRuntimeBackend) Validate(ctx context.Context, runtimeID string) error {
	status, err := b.Inspect(ctx, runtimeID)
	if err != nil {
		return err
	}
	if status.Details["enforcer_state"] == "running" {
		return nil
	}
	return fmt.Errorf("runtime %q enforcer is not running", runtimeID)
}

func (b *ContainerRuntimeBackend) Capabilities(ctx context.Context) (runtimecontract.BackendCapabilities, error) {
	_ = ctx
	if b.Backend == nil {
		return runtimecontract.BackendCapabilities{}, fmt.Errorf("%s is not available", b.Name())
	}
	return runtimecontract.BackendCapabilities{
		SupportedTransportTypes: []string{runtimecontract.TransportTypeLoopbackHTTP},
		SupportsComposeLike:     false,
		SupportsRootless:        b.Name() == runtimehost.BackendPodman,
		Isolation:               runtimecontract.IsolationContainer,
		RequiresKVM:             false,
		SupportsSnapshots:       false,
	}, nil
}

func (b *ContainerRuntimeBackend) rawClient() (*runtimehost.RawClient, error) {
	if b.Backend == nil {
		return nil, fmt.Errorf("%s is not available", b.Name())
	}
	return b.Backend.RawClient(), nil
}

func inspectContainerState(ctx context.Context, cli *runtimehost.RawClient, name string) string {
	info, err := cli.ContainerInspect(ctx, name)
	if err != nil {
		return "stopped"
	}
	switch {
	case info.State == nil:
		return "stopped"
	case info.State.Running:
		return "running"
	case info.State.Paused:
		return "paused"
	case info.State.Status != "":
		return info.State.Status
	default:
		return "stopped"
	}
}
