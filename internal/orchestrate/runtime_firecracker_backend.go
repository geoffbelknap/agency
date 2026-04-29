package orchestrate

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
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
	return b.EnsureWorkspace(ctx, spec)
}

func (b *firecrackerComponentRuntimeBackend) EnsureEnforcer(ctx context.Context, spec runtimecontract.RuntimeSpec, rotateKey bool) error {
	if b.enforcementMode() == hostruntimebackend.FirecrackerEnforcementModeMicroVM {
		if !rotateKey && b.enforcerMicroVMRunning(ctx, spec.RuntimeID) {
			return nil
		}
		enforcerSpec, err := b.compileEnforcerMicroVMSpec(ctx, spec, rotateKey)
		if err != nil {
			return err
		}
		return b.backend.Ensure(ctx, enforcerSpec.RuntimeSpec(spec))
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

func (b *firecrackerComponentRuntimeBackend) enforcerMicroVMRunning(ctx context.Context, runtimeID string) bool {
	status, err := b.backend.Inspect(ctx, firecrackerComponentRuntimeID(runtimeID, firecrackerComponentEnforcer))
	return err == nil && status.Healthy
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
	if b.enforcementMode() == hostruntimebackend.FirecrackerEnforcementModeMicroVM {
		if err := b.applyEnforcerMicroVMTargets(spec.RuntimeID, spec.Package.Env); err != nil {
			return err
		}
	}
	return b.backend.Ensure(ctx, spec)
}

func (b *firecrackerComponentRuntimeBackend) ReloadEnforcer(ctx context.Context, spec runtimecontract.RuntimeSpec) error {
	_ = ctx
	return b.enforcerSupervisor().Signal(spec.RuntimeID, syscall.SIGHUP)
}

func (b *firecrackerComponentRuntimeBackend) applyEnforcerMicroVMTargets(runtimeID string, env map[string]string) error {
	if b.backend.Vsock == nil {
		return fmt.Errorf("firecracker enforcer microVM bridge is not running")
	}
	bridge := b.backend.Vsock.Bridge(firecrackerComponentRuntimeID(runtimeID, firecrackerComponentEnforcer))
	if bridge == nil {
		return fmt.Errorf("firecracker enforcer microVM bridge is not running")
	}
	env[hostruntimebackend.FirecrackerEnforcerProxyTargetEnv] = hostruntimebackend.FirecrackerGuestVsockTarget(bridge.UDSBase, 3128)
	env[hostruntimebackend.FirecrackerEnforcerControlTargetEnv] = hostruntimebackend.FirecrackerGuestVsockTarget(bridge.UDSBase, 8081)
	return nil
}

func (b *firecrackerComponentRuntimeBackend) Stop(ctx context.Context, runtimeID string) error {
	err := b.backend.Stop(ctx, runtimeID)
	if b.enforcementMode() == hostruntimebackend.FirecrackerEnforcementModeMicroVM {
		enforcerErr := b.backend.Stop(ctx, firecrackerComponentRuntimeID(runtimeID, firecrackerComponentEnforcer))
		if err != nil {
			return err
		}
		return enforcerErr
	}
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
	status = b.applyEnforcerComponentStatus(runtimeID, status)
	if status.Healthy {
		status = b.applyBodyReadiness(ctx, runtimeID, status)
	}
	return status, nil
}

func (b *firecrackerComponentRuntimeBackend) applyEnforcerComponentStatus(runtimeID string, status runtimecontract.BackendStatus) runtimecontract.BackendStatus {
	if status.Details == nil {
		status.Details = map[string]string{}
	}
	status.Details["enforcer_substrate"] = b.enforcementMode()
	if b.enforcementMode() == hostruntimebackend.FirecrackerEnforcementModeMicroVM {
		enforcerStatus, enforcerErr := b.backend.Inspect(context.Background(), firecrackerComponentRuntimeID(runtimeID, firecrackerComponentEnforcer))
		if enforcerErr != nil {
			status.Details["enforcer_state"] = "stopped"
			status.Details["enforcer_component_state"] = "stopped"
		} else {
			status.Details["enforcer_state"] = enforcerStatus.Details["workload_vm_state"]
			status.Details["enforcer_component_state"] = enforcerStatus.Details["workload_vm_state"]
			status.Details["enforcer_pid"] = enforcerStatus.Details["workload_pid"]
			if enforcerStatus.Details["last_error"] != "" {
				status.Details["last_error"] = enforcerStatus.Details["last_error"]
			}
		}
		if status.Healthy && status.Details["enforcer_component_state"] != hostruntimebackend.FirecrackerVMRunning {
			status.Healthy = false
			status.Phase = runtimecontract.RuntimePhaseDegraded
			status.Details["last_error"] = "workload VM is running without an enforcer microVM"
		}
		return status
	}
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
	return status
}

func (b *firecrackerComponentRuntimeBackend) Validate(ctx context.Context, runtimeID string) error {
	if err := b.backend.Validate(ctx, runtimeID); err != nil {
		return err
	}
	if b.enforcementMode() == hostruntimebackend.FirecrackerEnforcementModeMicroVM {
		if err := b.backend.Validate(ctx, firecrackerComponentRuntimeID(runtimeID, firecrackerComponentEnforcer)); err != nil {
			return err
		}
	} else {
		if err := b.enforcerSupervisor().HealthCheck(ctx, runtimeID, 10*time.Second); err != nil {
			return err
		}
	}
	connected, err := firecrackerAgentBodyConnected(ctx, b.hostServiceURLs()["comms"], runtimeID)
	if err != nil {
		return fmt.Errorf("firecracker runtime %q body readiness: %w", runtimeID, err)
	}
	if !connected {
		return fmt.Errorf("firecracker runtime %q body websocket is not connected", runtimeID)
	}
	return nil
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

func (b *firecrackerComponentRuntimeBackend) applyBodyReadiness(ctx context.Context, runtimeID string, status runtimecontract.BackendStatus) runtimecontract.BackendStatus {
	connected, err := firecrackerAgentBodyConnected(ctx, b.hostServiceURLs()["comms"], runtimeID)
	if err == nil && connected {
		status.Details["body_ws_connected"] = "true"
		return status
	}
	status.Healthy = false
	status.Phase = runtimecontract.RuntimePhaseDegraded
	status.Details["body_ws_connected"] = "false"
	if err != nil {
		status.Details["last_error"] = "agent body websocket readiness check failed: " + err.Error()
	} else {
		status.Details["last_error"] = "agent body websocket is not connected"
	}
	return status
}

func firecrackerAgentBodyConnected(ctx context.Context, commsURL, runtimeID string) (bool, error) {
	commsURL = strings.TrimRight(strings.TrimSpace(commsURL), "/")
	if commsURL == "" {
		return false, fmt.Errorf("comms URL is not configured")
	}
	endpoint := commsURL + "/ws/connected/" + url.PathEscape(runtimeID)
	reqCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, endpoint, nil)
	if err != nil {
		return false, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return false, fmt.Errorf("comms readiness returned HTTP %d", resp.StatusCode)
	}
	var body struct {
		Connected bool `json:"connected"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return false, err
	}
	return body.Connected, nil
}
