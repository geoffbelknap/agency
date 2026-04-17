package orchestrate

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"log/slog"

	"gopkg.in/yaml.v3"

	"github.com/geoffbelknap/agency/internal/comms"
	"github.com/geoffbelknap/agency/internal/credstore"
	hostruntimebackend "github.com/geoffbelknap/agency/internal/hostadapter/runtimebackend"
	"github.com/geoffbelknap/agency/internal/hostadapter/runtimehost"
	runtimebackend "github.com/geoffbelknap/agency/internal/runtime/backend"
	runtimecontract "github.com/geoffbelknap/agency/internal/runtime/contract"
)

const defaultRuntimeBackend = runtimehost.BackendDocker

type RuntimeSupervisor struct {
	Home        string
	Version     string
	SourceDir   string
	BuildID     string
	BackendName string
	Docker      *runtimehost.DockerHandle
	Comms       comms.Client
	Log         *slog.Logger
	CredStore   *credstore.Store

	registry *runtimebackend.Registry
}

type runtimeManifest struct {
	Spec          runtimecontract.RuntimeSpec         `yaml:"spec" json:"spec"`
	Status        runtimecontract.RuntimeStatus       `yaml:"status" json:"status"`
	BackendStatus runtimecontract.BackendStatus       `yaml:"backendStatus,omitempty" json:"backendStatus,omitempty"`
	Capabilities  runtimecontract.BackendCapabilities `yaml:"capabilities,omitempty" json:"capabilities,omitempty"`
	CompiledAt    time.Time                           `yaml:"compiledAt" json:"compiledAt"`
	UpdatedAt     time.Time                           `yaml:"updatedAt" json:"updatedAt"`
}

type componentBackend interface {
	runtimecontract.Backend
	EnsureEnforcer(ctx context.Context, spec runtimecontract.RuntimeSpec, rotateKey bool) error
	EnsureWorkspace(ctx context.Context, spec runtimecontract.RuntimeSpec) error
}

type enforcerReloadBackend interface {
	ReloadEnforcer(ctx context.Context, spec runtimecontract.RuntimeSpec) error
}

func NewRuntimeSupervisor(home, version, sourceDir, buildID, backendName string, dc *runtimehost.DockerHandle, comms comms.Client, logger *slog.Logger, credStore *credstore.Store) *RuntimeSupervisor {
	rs := &RuntimeSupervisor{
		Home:        home,
		Version:     version,
		SourceDir:   sourceDir,
		BuildID:     buildID,
		BackendName: normalizeRuntimeBackendName(backendName),
		Docker:      dc,
		Comms:       comms,
		Log:         logger,
		CredStore:   credStore,
		registry:    runtimebackend.NewRegistry(),
	}
	registerContainerBackend := func(backendName string) {
		rs.registry.Register(backendName, func() (runtimecontract.Backend, error) {
			return &hostruntimebackend.DockerRuntimeBackend{
				BackendName: backendName,
				Docker:      rs.Docker,
				EnsureAgentNetwork: func(ctx context.Context, runtimeID string) error {
					if rs.Docker == nil {
						return fmt.Errorf("%s is not available", backendName)
					}
					infra, err := NewInfra(rs.Home, rs.Version, rs.Docker, rs.Log, nil)
					if err != nil {
						return err
					}
					infra.SourceDir = rs.SourceDir
					infra.BuildID = rs.BuildID
					return infra.EnsureAgentNetwork(ctx, fmt.Sprintf("%s-%s-internal", prefix, runtimeID))
				},
				EnsureEnforcerFn: func(ctx context.Context, spec runtimecontract.RuntimeSpec, rotateKey bool) error {
					if rs.Docker == nil {
						return fmt.Errorf("%s is not available", backendName)
					}
					sharedCli := rs.Docker.RawClient()
					enf, err := NewEnforcerWithClient(spec.RuntimeID, rs.Home, rs.Version, rs.Log, nil, sharedCli)
					if err != nil {
						return err
					}
					enf.SourceDir = rs.SourceDir
					enf.BuildID = rs.BuildID
					enf.ConstraintHostPort = hostPortFromEndpoint(spec.Transport.Enforcer.Endpoint)
					if rotateKey {
						_, err = enf.StartWithKeyRotation(ctx)
					} else {
						_, err = enf.Start(ctx)
					}
					if err != nil {
						return err
					}
					return enf.HealthCheck(ctx, 30*time.Second)
				},
				EnsureWorkspaceFn: func(ctx context.Context, spec runtimecontract.RuntimeSpec) error {
					if rs.Docker == nil {
						return fmt.Errorf("%s is not available", backendName)
					}
					sharedCli := rs.Docker.RawClient()
					ws, err := NewWorkspaceWithClient(spec.RuntimeID, rs.Home, rs.Version, rs.Log, sharedCli)
					if err != nil {
						return err
					}
					ws.Backend = backendName
					ws.SourceDir = rs.SourceDir
					ws.BuildID = rs.BuildID
					return ws.Start(ctx, StartOptions{
						ScopedKey:  readScopedAPIKey(spec.Transport.Enforcer.TokenRef),
						Model:      spec.Package.Env["AGENCY_MODEL"],
						AdminModel: spec.Package.Env["AGENCY_ADMIN_MODEL"],
						Env:        spec.Package.Env,
					})
				},
			}, nil
		})
	}
	registerContainerBackend(defaultRuntimeBackend)
	registerContainerBackend(runtimehost.BackendPodman)
	rs.registry.Register(probeRuntimeBackendName, func() (runtimecontract.Backend, error) {
		return &probeRuntimeBackend{home: rs.Home}, nil
	})
	return rs
}

func normalizeRuntimeBackendName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return defaultRuntimeBackend
	}
	return name
}

func (rs *RuntimeSupervisor) Compile(ctx context.Context, runtimeID string) (runtimecontract.RuntimeSpec, error) {
	_ = ctx
	agentID := strings.TrimSpace(runtimeID)
	if agentID == "" {
		return runtimecontract.RuntimeSpec{}, fmt.Errorf("runtime id is required")
	}
	if _, err := os.Stat(filepath.Join(rs.Home, "agents", agentID, "agent.yaml")); err != nil {
		return runtimecontract.RuntimeSpec{}, fmt.Errorf("agent %q not found", agentID)
	}
	endpoint, err := allocateLoopbackEndpoint()
	if err != nil {
		return runtimecontract.RuntimeSpec{}, fmt.Errorf("allocate loopback endpoint: %w", err)
	}
	spec := runtimecontract.RuntimeSpec{
		RuntimeID: agentID,
		AgentID:   agentUUIDOrName(filepath.Join(rs.Home, "agents", agentID, "agent.yaml"), agentID),
		Backend:   normalizeRuntimeBackendName(rs.BackendName),
		Package: runtimecontract.RuntimePackageSpec{
			Image: bodyImage,
			Env: map[string]string{
				"AGENCY_AGENT_NAME":                  agentID,
				"AGENCY_MODEL":                       rs.resolveModel(agentID),
				"AGENCY_ADMIN_MODEL":                 rs.resolveAdminModel(agentID),
				"PATH":                               "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
				"NO_PROXY":                           "enforcer,localhost,127.0.0.1",
				"AGENCY_ENFORCER_PROXY_URL":          "http://enforcer:3128",
				"AGENCY_ENFORCER_URL":                "http://enforcer:3128/v1",
				"OPENAI_API_BASE":                    "http://enforcer:3128/v1",
				"AGENCY_ENFORCER_CONTROL_URL":        "http://enforcer:8081",
				"AGENCY_ENFORCER_HEALTH_URL":         "http://enforcer:3128/health",
				"AGENCY_COMMS_URL":                   "http://enforcer:8081/mediation/comms",
				"AGENCY_KNOWLEDGE_URL":               "http://enforcer:8081/mediation/knowledge",
				"AGENCY_TRANSPORT_ENFORCER_TYPE":     runtimecontract.TransportTypeLoopbackHTTP,
				"AGENCY_TRANSPORT_ENFORCER_ENDPOINT": endpoint,
			},
		},
		Transport: runtimecontract.RuntimeTransportSpec{
			Enforcer: runtimecontract.EnforcerTransportSpec{
				Type:     runtimecontract.TransportTypeLoopbackHTTP,
				Endpoint: endpoint,
				AuthMode: "bearer",
				TokenRef: filepath.Join(rs.Home, "agents", agentID, "state", "enforcer-auth", "api_keys.yaml"),
			},
		},
		Storage: runtimecontract.RuntimeStorageSpec{
			ConfigPath:    filepath.Join(rs.Home, "agents", agentID),
			StatePath:     filepath.Join(rs.Home, "agents", agentID, "state"),
			WorkspacePath: rs.workspacePath(agentID),
		},
		Lifecycle: runtimecontract.RuntimeLifecycleSpec{
			RestartPolicy: "unless-stopped",
			RecoverState:  true,
		},
		Health: runtimecontract.RuntimeHealthSpec{
			HealthPath:    "/health",
			HeartbeatFile: filepath.Join(rs.Home, "agents", agentID, "state", "agent-signals.jsonl"),
		},
		Revision: runtimecontract.RuntimeRevisionSpec{
			InstanceRevision: time.Now().UTC().Format(time.RFC3339),
		},
	}
	return spec, nil
}

func (rs *RuntimeSupervisor) workspacePath(agentID string) string {
	if normalizeRuntimeBackendName(rs.BackendName) == probeRuntimeBackendName {
		return filepath.Join(rs.Home, "agents", agentID, "workspace")
	}
	return "/workspace"
}

func (rs *RuntimeSupervisor) Reconcile(ctx context.Context, spec runtimecontract.RuntimeSpec) error {
	_ = ctx
	status := runtimecontract.RuntimeStatus{
		RuntimeID: spec.RuntimeID,
		AgentID:   spec.AgentID,
		Phase:     runtimecontract.RuntimePhaseReconciled,
		Healthy:   false,
		Backend:   spec.Backend,
		Transport: runtimecontract.RuntimeTransportStatus{
			Type:     spec.Transport.Enforcer.Type,
			Endpoint: spec.Transport.Enforcer.Endpoint,
		},
	}
	now := time.Now().UTC()
	caps, _ := rs.capabilities(context.Background(), spec.Backend)
	return rs.saveManifest(runtimeManifest{
		Spec:         spec,
		Status:       status,
		Capabilities: caps,
		CompiledAt:   now,
		UpdatedAt:    now,
	})
}

func (rs *RuntimeSupervisor) Ensure(ctx context.Context, runtimeID string) error {
	spec, err := rs.loadSpec(runtimeID)
	if err != nil {
		return err
	}
	backend, err := rs.backend(spec.Backend)
	if err != nil {
		return err
	}
	if err := backend.Ensure(ctx, spec); err != nil {
		return err
	}
	return rs.refreshStatus(ctx, spec)
}

func (rs *RuntimeSupervisor) EnsureEnforcer(ctx context.Context, runtimeID string, rotateKey bool) error {
	spec, err := rs.loadOrCompile(ctx, runtimeID)
	if err != nil {
		return err
	}
	backend, err := rs.componentBackend(spec.Backend)
	if err != nil {
		return err
	}
	if err := backend.EnsureEnforcer(ctx, spec, rotateKey); err != nil {
		return err
	}
	return rs.refreshStatus(ctx, spec)
}

func (rs *RuntimeSupervisor) EnsureWorkspace(ctx context.Context, runtimeID string) error {
	spec, err := rs.loadOrCompile(ctx, runtimeID)
	if err != nil {
		return err
	}
	backend, err := rs.componentBackend(spec.Backend)
	if err != nil {
		return err
	}
	if err := backend.EnsureWorkspace(ctx, spec); err != nil {
		return err
	}
	return rs.refreshStatus(ctx, spec)
}

func (rs *RuntimeSupervisor) ReloadEnforcer(ctx context.Context, runtimeID string) error {
	spec, err := rs.loadSpec(runtimeID)
	if err != nil {
		return err
	}
	backend, err := rs.backend(spec.Backend)
	if err != nil {
		return err
	}
	reloader, ok := backend.(enforcerReloadBackend)
	if !ok {
		return nil
	}
	return reloader.ReloadEnforcer(ctx, spec)
}

func (rs *RuntimeSupervisor) Restart(ctx context.Context, runtimeID string) error {
	if err := rs.Stop(ctx, runtimeID); err != nil {
		return err
	}
	spec, err := rs.Compile(ctx, runtimeID)
	if err != nil {
		return err
	}
	if err := rs.Reconcile(ctx, spec); err != nil {
		return err
	}
	return rs.Ensure(ctx, runtimeID)
}

func (rs *RuntimeSupervisor) Stop(ctx context.Context, runtimeID string) error {
	spec, err := rs.loadSpec(runtimeID)
	if err != nil {
		spec = runtimecontract.RuntimeSpec{RuntimeID: runtimeID, Backend: normalizeRuntimeBackendName(rs.BackendName)}
	}
	backend, err := rs.backend(spec.Backend)
	if err != nil {
		return err
	}
	if err := backend.Stop(ctx, runtimeID); err != nil {
		return err
	}
	manifest, _ := rs.loadManifest(runtimeID)
	if manifest.Spec.RuntimeID == "" {
		manifest.Spec = spec
	}
	manifest.Status = runtimecontract.RuntimeStatus{
		RuntimeID: runtimeID,
		AgentID:   manifest.Spec.AgentID,
		Phase:     runtimecontract.RuntimePhaseStopped,
		Healthy:   false,
		Backend:   spec.Backend,
		Transport: runtimecontract.RuntimeTransportStatus{
			Type:     manifest.Spec.Transport.Enforcer.Type,
			Endpoint: manifest.Spec.Transport.Enforcer.Endpoint,
		},
	}
	manifest.UpdatedAt = time.Now().UTC()
	return rs.saveManifest(manifest)
}

func (rs *RuntimeSupervisor) Get(ctx context.Context, runtimeID string) (runtimecontract.RuntimeStatus, error) {
	manifest, err := rs.loadManifest(runtimeID)
	if err != nil {
		return runtimecontract.RuntimeStatus{}, err
	}
	spec := manifest.Spec
	backend, err := rs.backend(spec.Backend)
	if err != nil {
		if manifest.Status.RuntimeID != "" {
			return manifest.Status, nil
		}
		return runtimecontract.RuntimeStatus{}, err
	}
	backendStatus, err := backend.Inspect(ctx, runtimeID)
	if err != nil {
		if manifest.Status.RuntimeID != "" {
			return manifest.Status, nil
		}
		return runtimecontract.RuntimeStatus{}, err
	}
	status := runtimecontract.RuntimeStatus{
		RuntimeID: runtimeID,
		AgentID:   spec.AgentID,
		Phase:     backendStatus.Phase,
		Healthy:   backendStatus.Healthy,
		Backend:   spec.Backend,
		Transport: runtimecontract.RuntimeTransportStatus{
			Type:              spec.Transport.Enforcer.Type,
			Endpoint:          spec.Transport.Enforcer.Endpoint,
			EnforcerConnected: backendStatus.Details["enforcer_state"] == "running",
			LastError:         backendStatus.Details["last_error"],
		},
	}
	manifest.Spec = spec
	manifest.Status = status
	manifest.BackendStatus = backendStatus
	manifest.UpdatedAt = time.Now().UTC()
	manifest.CompiledAt = time.Now().UTC()
	_ = rs.saveManifest(manifest)
	return status, nil
}

func (rs *RuntimeSupervisor) Validate(ctx context.Context, runtimeID string) error {
	spec, err := rs.loadSpec(runtimeID)
	if err != nil {
		return err
	}
	if strings.TrimSpace(spec.Transport.Enforcer.Endpoint) == "" {
		return fmt.Errorf("runtime %q transport endpoint is not configured", runtimeID)
	}
	if spec.Transport.Enforcer.Type != runtimecontract.TransportTypeLoopbackHTTP {
		return fmt.Errorf("runtime %q transport type %q is not supported", runtimeID, spec.Transport.Enforcer.Type)
	}
	if strings.TrimSpace(spec.Transport.Enforcer.TokenRef) == "" {
		return fmt.Errorf("runtime %q token ref is not configured", runtimeID)
	}
	if _, err := os.Stat(spec.Transport.Enforcer.TokenRef); err != nil {
		return fmt.Errorf("runtime %q token ref: %w", runtimeID, err)
	}
	if _, err := os.Stat(spec.Storage.ConfigPath); err != nil {
		return fmt.Errorf("runtime %q config path: %w", runtimeID, err)
	}
	if _, err := os.Stat(spec.Storage.StatePath); err != nil {
		return fmt.Errorf("runtime %q state path: %w", runtimeID, err)
	}
	backend, err := rs.backend(spec.Backend)
	if err != nil {
		return err
	}
	return backend.Validate(ctx, runtimeID)
}

func (rs *RuntimeSupervisor) RuntimeAvailable(ctx context.Context) error {
	_, err := rs.capabilities(ctx, normalizeRuntimeBackendName(rs.BackendName))
	return err
}

func (rs *RuntimeSupervisor) Manifest(runtimeID string) (runtimeManifest, error) {
	return rs.loadManifest(runtimeID)
}

func (rs *RuntimeSupervisor) capabilities(ctx context.Context, backendName string) (runtimecontract.BackendCapabilities, error) {
	backend, err := rs.backend(normalizeRuntimeBackendName(backendName))
	if err != nil {
		return runtimecontract.BackendCapabilities{}, err
	}
	return backend.Capabilities(ctx)
}

func (rs *RuntimeSupervisor) backend(name string) (runtimecontract.Backend, error) {
	return rs.registry.Build(normalizeRuntimeBackendName(name))
}

func (rs *RuntimeSupervisor) componentBackend(name string) (componentBackend, error) {
	b, err := rs.backend(name)
	if err != nil {
		return nil, err
	}
	cb, ok := b.(componentBackend)
	if !ok {
		return nil, fmt.Errorf("runtime backend %q does not support component lifecycle", b.Name())
	}
	return cb, nil
}

func (rs *RuntimeSupervisor) loadOrCompile(ctx context.Context, runtimeID string) (runtimecontract.RuntimeSpec, error) {
	spec, err := rs.loadSpec(runtimeID)
	if err == nil {
		return spec, nil
	}
	spec, err = rs.Compile(ctx, runtimeID)
	if err != nil {
		return runtimecontract.RuntimeSpec{}, err
	}
	if err := rs.Reconcile(ctx, spec); err != nil {
		return runtimecontract.RuntimeSpec{}, err
	}
	return spec, nil
}

func (rs *RuntimeSupervisor) refreshStatus(ctx context.Context, spec runtimecontract.RuntimeSpec) error {
	status, err := rs.Get(ctx, spec.RuntimeID)
	if err != nil {
		return err
	}
	manifest, _ := rs.loadManifest(spec.RuntimeID)
	manifest.Spec = spec
	manifest.Status = status
	manifest.CompiledAt = time.Now().UTC()
	return rs.saveManifest(manifest)
}

func (rs *RuntimeSupervisor) loadSpec(runtimeID string) (runtimecontract.RuntimeSpec, error) {
	manifest, err := rs.loadManifest(runtimeID)
	if err != nil {
		return runtimecontract.RuntimeSpec{}, err
	}
	if manifest.Spec.RuntimeID == "" {
		return runtimecontract.RuntimeSpec{}, fmt.Errorf("runtime %q manifest is empty", runtimeID)
	}
	return manifest.Spec, nil
}

func (rs *RuntimeSupervisor) loadManifest(runtimeID string) (runtimeManifest, error) {
	path := rs.manifestPath(runtimeID)
	data, err := os.ReadFile(path)
	if err != nil {
		return runtimeManifest{}, err
	}
	var manifest runtimeManifest
	if err := yaml.Unmarshal(data, &manifest); err != nil {
		return runtimeManifest{}, fmt.Errorf("parse runtime manifest: %w", err)
	}
	return manifest, nil
}

func (rs *RuntimeSupervisor) saveManifest(manifest runtimeManifest) error {
	if strings.TrimSpace(manifest.Spec.RuntimeID) == "" {
		return fmt.Errorf("runtime manifest missing runtime id")
	}
	agentPath := filepath.Join(rs.Home, "agents", manifest.Spec.RuntimeID, "agent.yaml")
	if _, err := os.Stat(agentPath); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("stat agent definition: %w", err)
	}
	path := rs.manifestPath(manifest.Spec.RuntimeID)
	if manifest.UpdatedAt.IsZero() {
		manifest.UpdatedAt = time.Now().UTC()
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create runtime dir: %w", err)
	}
	data, err := yaml.Marshal(manifest)
	if err != nil {
		return fmt.Errorf("marshal runtime manifest: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write runtime manifest: %w", err)
	}
	return nil
}

func (rs *RuntimeSupervisor) manifestPath(runtimeID string) string {
	return filepath.Join(rs.Home, "agents", runtimeID, "runtime", "manifest.yaml")
}

func (rs *RuntimeSupervisor) resolveModel(agentName string) string {
	model := readAgentStringField(filepath.Join(rs.Home, "agents", agentName, "agent.yaml"), "model")
	if model != "" {
		return model
	}
	return "claude-sonnet"
}

func (rs *RuntimeSupervisor) resolveAdminModel(agentName string) string {
	model := readAgentStringField(filepath.Join(rs.Home, "agents", agentName, "agent.yaml"), "admin_model")
	if model != "" {
		return model
	}
	return ""
}

func readAgentStringField(path, field string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var raw map[string]any
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return ""
	}
	value, _ := raw[field].(string)
	return strings.TrimSpace(value)
}

func agentUUIDOrName(path, fallback string) string {
	id := readAgentStringField(path, "uuid")
	if id == "" {
		return fallback
	}
	return id
}

func allocateLoopbackEndpoint() (string, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	addr := listener.Addr().String()
	if err := listener.Close(); err != nil {
		return "", err
	}
	return "http://" + addr, nil
}
