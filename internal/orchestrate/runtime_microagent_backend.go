package orchestrate

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	agentruntime "github.com/geoffbelknap/agency/internal/hostadapter/agentruntime"
	hostruntimebackend "github.com/geoffbelknap/agency/internal/hostadapter/runtimebackend"
	runtimecontract "github.com/geoffbelknap/agency/internal/runtime/contract"
)

type microagentComponentRuntimeBackend struct {
	backend   *hostruntimebackend.MicroagentCLIRuntimeBackend
	enforcers *agentruntime.HostEnforcerSupervisor
	home      string
	version   string
	sourceDir string
	buildID   string
}

func (b *microagentComponentRuntimeBackend) Name() string {
	return b.backend.Name()
}

func (b *microagentComponentRuntimeBackend) Ensure(ctx context.Context, spec runtimecontract.RuntimeSpec) error {
	return b.EnsureWorkspace(ctx, spec)
}

func (b *microagentComponentRuntimeBackend) EnsureEnforcer(ctx context.Context, spec runtimecontract.RuntimeSpec, rotateKey bool) error {
	proxyHostPort := hostPortFromEndpoint(spec.Package.Env[hostruntimebackend.FirecrackerEnforcerProxyTargetEnv])
	constraintHostPort := hostPortFromEndpoint(spec.Package.Env[hostruntimebackend.FirecrackerEnforcerControlTargetEnv])
	if proxyHostPort == "" || constraintHostPort == "" {
		return fmt.Errorf("microagent enforcer target ports are not configured")
	}
	enforcer := &Enforcer{
		AgentName:          spec.RuntimeID,
		ContainerName:      fmt.Sprintf("%s-%s-enforcer", prefix, spec.RuntimeID),
		Home:               b.home,
		Version:            b.version,
		SourceDir:          b.sourceDir,
		BuildID:            b.buildID,
		ProxyHostPort:      proxyHostPort,
		ConstraintHostPort: constraintHostPort,
		LifecycleID:        spec.Revision.InstanceRevision,
		PrincipalUUID:      spec.AgentID,
	}
	launchSpec, err := enforcer.BuildLaunchSpec(ctx, rotateKey)
	if err != nil {
		return err
	}
	launchSpec.ProxyHostPort = proxyHostPort
	launchSpec.ConstraintHostPort = constraintHostPort
	if err := b.enforcerSupervisor().Start(ctx, launchSpec, appleVFHostServiceURLs()); err != nil {
		return err
	}
	if err := b.enforcerSupervisor().HealthCheck(ctx, spec.RuntimeID, 30*time.Second); err != nil {
		_ = b.enforcerSupervisor().Stop(context.Background(), spec.RuntimeID)
		return err
	}
	return nil
}

func (b *microagentComponentRuntimeBackend) EnsureWorkspace(ctx context.Context, spec runtimecontract.RuntimeSpec) error {
	if spec.Package.Env == nil {
		spec.Package.Env = map[string]string{}
	}
	if scopedKey := readScopedAPIKey(spec.Transport.Enforcer.TokenRef); scopedKey != "" {
		spec.Package.Env["AGENCY_LLM_API_KEY"] = scopedKey
	}
	if err := b.EnsureEnforcer(ctx, spec, false); err != nil {
		return err
	}
	if err := b.backend.Ensure(ctx, spec); err != nil {
		_ = b.enforcerSupervisor().Stop(context.Background(), spec.RuntimeID)
		return err
	}
	return nil
}

func (b *microagentComponentRuntimeBackend) ReloadEnforcer(ctx context.Context, spec runtimecontract.RuntimeSpec) error {
	return b.EnsureEnforcer(ctx, spec, true)
}

func (b *microagentComponentRuntimeBackend) Stop(ctx context.Context, runtimeID string) error {
	var errs []error
	if err := b.backend.Stop(ctx, runtimeID); err != nil {
		errs = append(errs, err)
	}
	if err := b.enforcerSupervisor().Stop(ctx, runtimeID); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

func (b *microagentComponentRuntimeBackend) Inspect(ctx context.Context, runtimeID string) (runtimecontract.BackendStatus, error) {
	status, err := b.backend.Inspect(ctx, runtimeID)
	if err != nil {
		return status, err
	}
	if status.Details == nil {
		status.Details = map[string]string{}
	}
	status.Details["enforcer_substrate"] = hostruntimebackend.FirecrackerEnforcementModeHostProcess
	enforcerStatus, enforcerErr := b.enforcerSupervisor().Inspect(runtimeID)
	if enforcerErr != nil {
		status.Details["enforcer_state"] = "stopped"
		status.Details["enforcer_component_state"] = "stopped"
	} else {
		status.Details["enforcer_state"] = enforcerStatus.State
		status.Details["enforcer_component_state"] = enforcerStatus.State
		status.Details["enforcer_pid"] = fmt.Sprintf("%d", enforcerStatus.PID)
		if enforcerStatus.LastError != "" {
			status.Details["last_error"] = enforcerStatus.LastError
		}
	}
	if status.Healthy && status.Details["enforcer_state"] != agentruntime.HostEnforcerStateRunning {
		status.Healthy = false
		status.Phase = runtimecontract.RuntimePhaseDegraded
		status.Details["last_error"] = "workload VM is running without an enforcer"
	}
	if status.Healthy {
		status = b.applyBodyReadiness(ctx, runtimeID, status)
	}
	return status, nil
}

func (b *microagentComponentRuntimeBackend) Validate(ctx context.Context, runtimeID string) error {
	if err := b.backend.Validate(ctx, runtimeID); err != nil {
		return err
	}
	if err := b.enforcerSupervisor().HealthCheck(ctx, runtimeID, 10*time.Second); err != nil {
		return err
	}
	connected, err := firecrackerAgentBodyConnected(ctx, appleVFHostServiceURLs()["comms"], runtimeID)
	if err != nil {
		return fmt.Errorf("microagent runtime %q body readiness: %w", runtimeID, err)
	}
	if !connected {
		return fmt.Errorf("microagent runtime %q body websocket is not connected", runtimeID)
	}
	return nil
}

func (b *microagentComponentRuntimeBackend) Capabilities(ctx context.Context) (runtimecontract.BackendCapabilities, error) {
	return b.backend.Capabilities(ctx)
}

func (b *microagentComponentRuntimeBackend) enforcerSupervisor() *agentruntime.HostEnforcerSupervisor {
	if b.enforcers == nil {
		b.enforcers = &agentruntime.HostEnforcerSupervisor{}
	}
	return b.enforcers
}

func (b *microagentComponentRuntimeBackend) applyBodyReadiness(ctx context.Context, runtimeID string, status runtimecontract.BackendStatus) runtimecontract.BackendStatus {
	connected, err := firecrackerAgentBodyConnected(ctx, appleVFHostServiceURLs()["comms"], runtimeID)
	if err == nil && connected {
		status.Details["body_ws_connected"] = "true"
		return status
	}
	status.Healthy = false
	status.Phase = runtimecontract.RuntimePhaseDegraded
	status.Details["body_ws_connected"] = "false"
	if err != nil {
		status.Details["last_error"] = strings.TrimSpace(err.Error())
	} else {
		status.Details["last_error"] = "body websocket is not connected"
	}
	return status
}

func newMicroagentComponentRuntimeBackend(home, version, sourceDir, buildID string, cfg map[string]string) *microagentComponentRuntimeBackend {
	backend := hostruntimebackend.NewMicroagentCLIRuntimeBackend(home, cfg)
	return &microagentComponentRuntimeBackend{
		backend: backend,
		enforcers: &agentruntime.HostEnforcerSupervisor{
			BinaryPath: strings.TrimSpace(cfg["enforcer_binary_path"]),
			StateDir:   filepath.Join(backend.StateDir, "host-enforcers"),
		},
		home:      home,
		version:   version,
		sourceDir: sourceDir,
		buildID:   buildID,
	}
}
