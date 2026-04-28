package orchestrate

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"syscall"
	"time"

	agentruntime "github.com/geoffbelknap/agency/internal/hostadapter/agentruntime"
	hostruntimebackend "github.com/geoffbelknap/agency/internal/hostadapter/runtimebackend"
	runtimecontract "github.com/geoffbelknap/agency/internal/runtime/contract"
)

type firecrackerComponentRuntimeBackend struct {
	backend   *hostruntimebackend.FirecrackerRuntimeBackend
	enforcers *agentruntime.HostEnforcerSupervisor
	home      string
	version   string
	sourceDir string
	buildID   string
	log       *slog.Logger
}

func (b *firecrackerComponentRuntimeBackend) Name() string {
	return b.backend.Name()
}

func (b *firecrackerComponentRuntimeBackend) Ensure(ctx context.Context, spec runtimecontract.RuntimeSpec) error {
	if err := b.EnsureEnforcer(ctx, spec, false); err != nil {
		return err
	}
	return b.EnsureWorkspace(ctx, spec)
}

func (b *firecrackerComponentRuntimeBackend) EnsureEnforcer(ctx context.Context, spec runtimecontract.RuntimeSpec, rotateKey bool) error {
	if b.enforcementMode() == hostruntimebackend.FirecrackerEnforcementModeMicroVM {
		return fmt.Errorf("firecracker enforcer microVM mode is not implemented")
	}
	proxyHostPort := hostPortFromEndpoint(spec.Package.Env[hostruntimebackend.FirecrackerEnforcerProxyTargetEnv])
	constraintHostPort := hostPortFromEndpoint(spec.Package.Env[hostruntimebackend.FirecrackerEnforcerControlTargetEnv])
	if proxyHostPort == "" || constraintHostPort == "" {
		return fmt.Errorf("firecracker enforcer target ports are not configured")
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
	if err := b.enforcerSupervisor().Start(ctx, launchSpec, b.hostServiceURLs()); err != nil {
		return err
	}
	return b.enforcerSupervisor().HealthCheck(ctx, spec.RuntimeID, 30*time.Second)
}

func (b *firecrackerComponentRuntimeBackend) EnsureWorkspace(ctx context.Context, spec runtimecontract.RuntimeSpec) error {
	if spec.Package.Env == nil {
		spec.Package.Env = map[string]string{}
	}
	if scopedKey := readScopedAPIKey(spec.Transport.Enforcer.TokenRef); scopedKey != "" {
		spec.Package.Env["AGENCY_LLM_API_KEY"] = scopedKey
	}
	if err := b.EnsureEnforcer(ctx, spec, false); err != nil {
		return err
	}
	return b.backend.Ensure(ctx, spec)
}

func (b *firecrackerComponentRuntimeBackend) ReloadEnforcer(ctx context.Context, spec runtimecontract.RuntimeSpec) error {
	_ = ctx
	return b.enforcerSupervisor().Signal(spec.RuntimeID, syscall.SIGHUP)
}

func (b *firecrackerComponentRuntimeBackend) Stop(ctx context.Context, runtimeID string) error {
	err := b.backend.Stop(ctx, runtimeID)
	enforcerErr := b.enforcerSupervisor().Stop(ctx, runtimeID)
	if err != nil {
		return err
	}
	return enforcerErr
}

func (b *firecrackerComponentRuntimeBackend) Inspect(ctx context.Context, runtimeID string) (runtimecontract.BackendStatus, error) {
	status, err := b.backend.Inspect(ctx, runtimeID)
	if err != nil {
		return status, err
	}
	if status.Details == nil {
		status.Details = map[string]string{}
	}
	enforcerStatus, enforcerErr := b.enforcerSupervisor().Inspect(runtimeID)
	if enforcerErr != nil {
		status.Details["enforcer_state"] = "stopped"
	} else {
		status.Details["enforcer_state"] = enforcerStatus.State
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
	return status, nil
}

func (b *firecrackerComponentRuntimeBackend) Validate(ctx context.Context, runtimeID string) error {
	if err := b.backend.Validate(ctx, runtimeID); err != nil {
		return err
	}
	return b.enforcerSupervisor().HealthCheck(ctx, runtimeID, 10*time.Second)
}

func (b *firecrackerComponentRuntimeBackend) Capabilities(ctx context.Context) (runtimecontract.BackendCapabilities, error) {
	return b.backend.Capabilities(ctx)
}

func (b *firecrackerComponentRuntimeBackend) enforcementMode() string {
	mode := strings.TrimSpace(b.backend.EnforcementMode)
	if mode == "" {
		return hostruntimebackend.FirecrackerEnforcementModeHostProcess
	}
	return mode
}

func (b *firecrackerComponentRuntimeBackend) enforcerSupervisor() *agentruntime.HostEnforcerSupervisor {
	if b.enforcers == nil {
		b.enforcers = &agentruntime.HostEnforcerSupervisor{}
	}
	return b.enforcers
}

func (b *firecrackerComponentRuntimeBackend) hostServiceURLs() map[string]string {
	return map[string]string{
		"gateway":   "http://127.0.0.1:" + envPort("AGENCY_GATEWAY_PORT", "8200"),
		"comms":     "http://127.0.0.1:" + envPort("AGENCY_GATEWAY_PROXY_PORT", "8202"),
		"knowledge": "http://127.0.0.1:" + envPort("AGENCY_GATEWAY_PROXY_KNOWLEDGE_PORT", "8204"),
		"web-fetch": "http://127.0.0.1:" + envPort("AGENCY_WEB_FETCH_PORT", "8206"),
		"egress":    "http://127.0.0.1:" + envPort("AGENCY_EGRESS_PROXY_PORT", "8312"),
	}
}
