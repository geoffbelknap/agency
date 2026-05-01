package orchestrate

import (
	"context"
	"errors"
	"fmt"
	"time"

	agentruntime "github.com/geoffbelknap/agency/internal/hostadapter/agentruntime"
	hostruntimebackend "github.com/geoffbelknap/agency/internal/hostadapter/runtimebackend"
	runtimecontract "github.com/geoffbelknap/agency/internal/runtime/contract"
)

type appleVFComponentRuntimeBackend struct {
	backend   *hostruntimebackend.AppleVFMicroVMRuntimeBackend
	enforcers *agentruntime.HostEnforcerSupervisor
	home      string
	version   string
	sourceDir string
	buildID   string
}

func (b *appleVFComponentRuntimeBackend) Name() string {
	return b.backend.Name()
}

func (b *appleVFComponentRuntimeBackend) Ensure(ctx context.Context, spec runtimecontract.RuntimeSpec) error {
	return b.EnsureWorkspace(ctx, spec)
}

func (b *appleVFComponentRuntimeBackend) EnsureEnforcer(ctx context.Context, spec runtimecontract.RuntimeSpec, rotateKey bool) error {
	proxyHostPort := hostPortFromEndpoint(spec.Package.Env[hostruntimebackend.FirecrackerEnforcerProxyTargetEnv])
	constraintHostPort := hostPortFromEndpoint(spec.Package.Env[hostruntimebackend.FirecrackerEnforcerControlTargetEnv])
	if proxyHostPort == "" || constraintHostPort == "" {
		return fmt.Errorf("apple-vf-microvm enforcer target ports are not configured")
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

func (b *appleVFComponentRuntimeBackend) EnsureWorkspace(ctx context.Context, spec runtimecontract.RuntimeSpec) error {
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

func (b *appleVFComponentRuntimeBackend) ReloadEnforcer(ctx context.Context, spec runtimecontract.RuntimeSpec) error {
	return b.EnsureEnforcer(ctx, spec, true)
}

func (b *appleVFComponentRuntimeBackend) Stop(ctx context.Context, runtimeID string) error {
	var errs []error
	if err := b.backend.Stop(ctx, runtimeID); err != nil {
		errs = append(errs, err)
	}
	if err := b.enforcerSupervisor().Stop(ctx, runtimeID); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

func (b *appleVFComponentRuntimeBackend) Inspect(ctx context.Context, runtimeID string) (runtimecontract.BackendStatus, error) {
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

func (b *appleVFComponentRuntimeBackend) Validate(ctx context.Context, runtimeID string) error {
	if err := b.backend.Validate(ctx, runtimeID); err != nil {
		return err
	}
	if err := b.enforcerSupervisor().HealthCheck(ctx, runtimeID, 10*time.Second); err != nil {
		return err
	}
	connected, err := firecrackerAgentBodyConnected(ctx, appleVFHostServiceURLs()["comms"], runtimeID)
	if err != nil {
		return fmt.Errorf("apple-vf-microvm runtime %q body readiness: %w", runtimeID, err)
	}
	if !connected {
		return fmt.Errorf("apple-vf-microvm runtime %q body websocket is not connected", runtimeID)
	}
	return nil
}

func (b *appleVFComponentRuntimeBackend) Capabilities(ctx context.Context) (runtimecontract.BackendCapabilities, error) {
	return b.backend.Capabilities(ctx)
}

func (b *appleVFComponentRuntimeBackend) enforcerSupervisor() *agentruntime.HostEnforcerSupervisor {
	if b.enforcers == nil {
		b.enforcers = &agentruntime.HostEnforcerSupervisor{}
	}
	return b.enforcers
}

func (b *appleVFComponentRuntimeBackend) applyBodyReadiness(ctx context.Context, runtimeID string, status runtimecontract.BackendStatus) runtimecontract.BackendStatus {
	connected, err := firecrackerAgentBodyConnected(ctx, appleVFHostServiceURLs()["comms"], runtimeID)
	if err == nil && connected {
		status.Details["body_ws_connected"] = "true"
		return status
	}
	status.Healthy = false
	status.Phase = runtimecontract.RuntimePhaseDegraded
	if err != nil {
		status.Details["last_error"] = err.Error()
	} else {
		status.Details["last_error"] = "body websocket is not connected"
	}
	return status
}

func appleVFHostServiceURLs() map[string]string {
	return map[string]string{
		"gateway":   "http://127.0.0.1:" + envPort("AGENCY_GATEWAY_PORT", "8200"),
		"comms":     "http://127.0.0.1:" + envPort("AGENCY_GATEWAY_PROXY_PORT", "8202"),
		"knowledge": "http://127.0.0.1:" + envPort("AGENCY_GATEWAY_PROXY_KNOWLEDGE_PORT", "8204"),
		"web-fetch": "http://127.0.0.1:" + envPort("AGENCY_WEB_FETCH_PORT", "8206"),
		"egress":    "http://127.0.0.1:" + envPort("AGENCY_EGRESS_PROXY_PORT", "8312"),
	}
}
