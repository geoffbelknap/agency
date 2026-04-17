package orchestrate

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/geoffbelknap/agency/internal/hostadapter/runtimehost"
	runtimecontract "github.com/geoffbelknap/agency/internal/runtime/contract"
)

type fakeRuntimeBackend struct {
	status        runtimecontract.BackendStatus
	validateErr   error
	capabilityErr error
	inspectErr    error
	ensureCalls   int
	stopCalls     int
	lastSpec      runtimecontract.RuntimeSpec
	stopRuntimeID string
	rotateKey     bool
}

func (f *fakeRuntimeBackend) Name() string { return "fake" }
func (f *fakeRuntimeBackend) Ensure(ctx context.Context, spec runtimecontract.RuntimeSpec) error {
	f.ensureCalls++
	f.lastSpec = spec
	return nil
}
func (f *fakeRuntimeBackend) Stop(ctx context.Context, runtimeID string) error {
	f.stopCalls++
	f.stopRuntimeID = runtimeID
	return nil
}
func (f *fakeRuntimeBackend) Inspect(ctx context.Context, runtimeID string) (runtimecontract.BackendStatus, error) {
	if f.inspectErr != nil {
		return runtimecontract.BackendStatus{}, f.inspectErr
	}
	return f.status, nil
}
func (f *fakeRuntimeBackend) Validate(ctx context.Context, runtimeID string) error {
	return f.validateErr
}
func (f *fakeRuntimeBackend) Capabilities(ctx context.Context) (runtimecontract.BackendCapabilities, error) {
	if f.capabilityErr != nil {
		return runtimecontract.BackendCapabilities{}, f.capabilityErr
	}
	return runtimecontract.BackendCapabilities{
		SupportedTransportTypes: []string{runtimecontract.TransportTypeLoopbackHTTP},
	}, nil
}
func (f *fakeRuntimeBackend) EnsureEnforcer(ctx context.Context, spec runtimecontract.RuntimeSpec, rotateKey bool) error {
	f.lastSpec = spec
	f.rotateKey = rotateKey
	return nil
}
func (f *fakeRuntimeBackend) EnsureWorkspace(ctx context.Context, spec runtimecontract.RuntimeSpec) error {
	f.lastSpec = spec
	return nil
}

func TestRuntimeSupervisorCompileProducesBackendNeutralTransport(t *testing.T) {
	home := t.TempDir()
	agentDir := filepath.Join(home, "agents", "alice")
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, "agent.yaml"), []byte("uuid: ag_123\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rs := NewRuntimeSupervisor(home, "0.1.0", "", "build-1", "", nil, nil, nil, nil)
	spec, err := rs.Compile(context.Background(), "alice")
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	if spec.Backend != defaultRuntimeBackend {
		t.Fatalf("backend = %q, want %q", spec.Backend, defaultRuntimeBackend)
	}
	if spec.Transport.Enforcer.Type != runtimecontract.TransportTypeLoopbackHTTP {
		t.Fatalf("transport type = %q", spec.Transport.Enforcer.Type)
	}
	if spec.Transport.Enforcer.Endpoint == "" {
		t.Fatal("transport endpoint is empty")
	}
	if spec.Transport.Enforcer.TokenRef == "" {
		t.Fatal("token ref is empty")
	}
	if spec.AgentID != "ag_123" {
		t.Fatalf("agent id = %q, want ag_123", spec.AgentID)
	}
}

func TestRuntimeSupervisorValidateFailsClosedWhenTokenMissing(t *testing.T) {
	home := t.TempDir()
	agentDir := filepath.Join(home, "agents", "alice")
	stateDir := filepath.Join(agentDir, "state")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, "agent.yaml"), []byte("uuid: ag_123\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rs := NewRuntimeSupervisor(home, "0.1.0", "", "build-1", "fake", nil, nil, nil, nil)
	fake := &fakeRuntimeBackend{}
	rs.registry.Register("fake", func() (runtimecontract.Backend, error) { return fake, nil })
	spec := runtimecontract.RuntimeSpec{
		RuntimeID: "alice",
		AgentID:   "ag_123",
		Backend:   "fake",
		Transport: runtimecontract.RuntimeTransportSpec{
			Enforcer: runtimecontract.EnforcerTransportSpec{
				Type:     runtimecontract.TransportTypeLoopbackHTTP,
				Endpoint: "http://127.0.0.1:9999",
				TokenRef: filepath.Join(stateDir, "missing-token.yaml"),
			},
		},
		Storage: runtimecontract.RuntimeStorageSpec{
			ConfigPath: agentDir,
			StatePath:  stateDir,
		},
	}
	if err := rs.Reconcile(context.Background(), spec); err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}
	if err := rs.Validate(context.Background(), "alice"); err == nil {
		t.Fatal("Validate returned nil error")
	}
}

func TestRuntimeSupervisorGetProjectsBackendStatus(t *testing.T) {
	home := t.TempDir()
	agentDir := filepath.Join(home, "agents", "alice")
	stateDir := filepath.Join(agentDir, "state")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, "agent.yaml"), []byte("uuid: ag_123\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tokenFile := filepath.Join(stateDir, "token.yaml")
	if err := os.WriteFile(tokenFile, []byte("- key: \"abc\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rs := NewRuntimeSupervisor(home, "0.1.0", "", "build-1", "fake", nil, nil, nil, nil)
	fake := &fakeRuntimeBackend{
		status: runtimecontract.BackendStatus{
			RuntimeID: "alice",
			Healthy:   false,
			Phase:     runtimecontract.RuntimePhaseDegraded,
			Details: map[string]string{
				"enforcer_state": "stopped",
				"last_error":     "lost mediation",
			},
		},
	}
	rs.registry.Register("fake", func() (runtimecontract.Backend, error) { return fake, nil })
	spec := runtimecontract.RuntimeSpec{
		RuntimeID: "alice",
		AgentID:   "ag_123",
		Backend:   "fake",
		Transport: runtimecontract.RuntimeTransportSpec{
			Enforcer: runtimecontract.EnforcerTransportSpec{
				Type:     runtimecontract.TransportTypeLoopbackHTTP,
				Endpoint: "http://127.0.0.1:9999",
				TokenRef: tokenFile,
			},
		},
		Storage: runtimecontract.RuntimeStorageSpec{
			ConfigPath: agentDir,
			StatePath:  stateDir,
		},
	}
	if err := rs.Reconcile(context.Background(), spec); err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}
	status, err := rs.Get(context.Background(), "alice")
	if err != nil {
		t.Fatalf("Get returned error: %v", err)
	}
	if status.Phase != runtimecontract.RuntimePhaseDegraded {
		t.Fatalf("phase = %q", status.Phase)
	}
	if status.Transport.EnforcerConnected {
		t.Fatal("expected enforcer to be disconnected")
	}
	if status.Transport.LastError != "lost mediation" {
		t.Fatalf("last error = %q", status.Transport.LastError)
	}
}

func TestRuntimeSupervisorEnsureEnforcerPassesKeyRotation(t *testing.T) {
	home := t.TempDir()
	agentDir := filepath.Join(home, "agents", "alice")
	stateDir := filepath.Join(agentDir, "state")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, "agent.yaml"), []byte("uuid: ag_123\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rs := NewRuntimeSupervisor(home, "0.1.0", "", "build-1", "fake", nil, nil, nil, nil)
	fake := &fakeRuntimeBackend{
		status: runtimecontract.BackendStatus{
			RuntimeID: "alice",
			Healthy:   true,
			Phase:     runtimecontract.RuntimePhaseRunning,
			Details: map[string]string{
				"enforcer_state": "running",
			},
		},
	}
	rs.registry.Register("fake", func() (runtimecontract.Backend, error) { return fake, nil })
	spec, err := rs.Compile(context.Background(), "alice")
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	spec.Backend = "fake"
	if err := rs.Reconcile(context.Background(), spec); err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}
	if err := rs.EnsureEnforcer(context.Background(), "alice", true); err != nil {
		t.Fatalf("EnsureEnforcer returned error: %v", err)
	}
	if !fake.rotateKey {
		t.Fatal("expected EnsureEnforcer to receive rotateKey=true")
	}
	if fake.lastSpec.RuntimeID != "alice" {
		t.Fatalf("runtime id = %q, want alice", fake.lastSpec.RuntimeID)
	}
}

func TestRuntimeSupervisorGetFallsBackToPersistedStatusWhenInspectFails(t *testing.T) {
	home := t.TempDir()
	agentDir := filepath.Join(home, "agents", "alice")
	stateDir := filepath.Join(agentDir, "state")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, "agent.yaml"), []byte("uuid: ag_123\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tokenFile := filepath.Join(stateDir, "token.yaml")
	if err := os.WriteFile(tokenFile, []byte("- key: \"abc\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rs := NewRuntimeSupervisor(home, "0.1.0", "", "build-1", "fake", nil, nil, nil, nil)
	fake := &fakeRuntimeBackend{inspectErr: errors.New("backend unavailable")}
	rs.registry.Register("fake", func() (runtimecontract.Backend, error) { return fake, nil })

	manifest := runtimeManifest{
		Spec: runtimecontract.RuntimeSpec{
			RuntimeID: "alice",
			AgentID:   "ag_123",
			Backend:   "fake",
			Transport: runtimecontract.RuntimeTransportSpec{
				Enforcer: runtimecontract.EnforcerTransportSpec{
					Type:     runtimecontract.TransportTypeLoopbackHTTP,
					Endpoint: "http://127.0.0.1:9999",
					TokenRef: tokenFile,
				},
			},
			Storage: runtimecontract.RuntimeStorageSpec{
				ConfigPath: agentDir,
				StatePath:  stateDir,
			},
		},
		Status: runtimecontract.RuntimeStatus{
			RuntimeID: "alice",
			AgentID:   "ag_123",
			Phase:     runtimecontract.RuntimePhaseDegraded,
			Healthy:   false,
			Backend:   "fake",
			Transport: runtimecontract.RuntimeTransportStatus{
				Type:              runtimecontract.TransportTypeLoopbackHTTP,
				Endpoint:          "http://127.0.0.1:9999",
				EnforcerConnected: false,
				LastError:         "persisted backend state",
			},
		},
	}
	if err := rs.saveManifest(manifest); err != nil {
		t.Fatalf("saveManifest returned error: %v", err)
	}

	status, err := rs.Get(context.Background(), "alice")
	if err != nil {
		t.Fatalf("Get returned error: %v", err)
	}
	if status.Phase != runtimecontract.RuntimePhaseDegraded {
		t.Fatalf("phase = %q, want degraded", status.Phase)
	}
	if status.Transport.LastError != "persisted backend state" {
		t.Fatalf("last error = %q", status.Transport.LastError)
	}
}

func TestRuntimeSupervisorSaveManifestSkipsDeletedAgent(t *testing.T) {
	home := t.TempDir()
	rs := NewRuntimeSupervisor(home, "0.1.0", "", "build-1", "fake", nil, nil, nil, nil)

	manifest := runtimeManifest{
		Spec: runtimecontract.RuntimeSpec{
			RuntimeID: "deleted-agent",
			AgentID:   "ag_deleted",
			Backend:   "fake",
		},
	}

	if err := rs.saveManifest(manifest); err != nil {
		t.Fatalf("saveManifest returned error: %v", err)
	}
	if _, err := os.Stat(rs.manifestPath("deleted-agent")); !os.IsNotExist(err) {
		t.Fatalf("expected no manifest file, got err=%v", err)
	}
}

func TestRuntimeSupervisorRestartStopsThenEnsures(t *testing.T) {
	home := t.TempDir()
	agentDir := filepath.Join(home, "agents", "alice")
	stateDir := filepath.Join(agentDir, "state")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, "agent.yaml"), []byte("uuid: ag_123\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rs := NewRuntimeSupervisor(home, "0.1.0", "", "build-1", "fake", nil, nil, nil, nil)
	fake := &fakeRuntimeBackend{
		status: runtimecontract.BackendStatus{
			RuntimeID: "alice",
			Healthy:   true,
			Phase:     runtimecontract.RuntimePhaseRunning,
			Details: map[string]string{
				"enforcer_state": "running",
			},
		},
	}
	rs.registry.Register("fake", func() (runtimecontract.Backend, error) { return fake, nil })
	spec, err := rs.Compile(context.Background(), "alice")
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	spec.Backend = "fake"
	if err := rs.Reconcile(context.Background(), spec); err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}

	if err := rs.Restart(context.Background(), "alice"); err != nil {
		t.Fatalf("Restart returned error: %v", err)
	}
	if fake.stopCalls != 1 {
		t.Fatalf("stop calls = %d, want 1", fake.stopCalls)
	}
	if fake.ensureCalls != 1 {
		t.Fatalf("ensure calls = %d, want 1", fake.ensureCalls)
	}
	if fake.stopRuntimeID != "alice" {
		t.Fatalf("stop runtime id = %q, want alice", fake.stopRuntimeID)
	}

	manifest, err := rs.Manifest("alice")
	if err != nil {
		t.Fatalf("Manifest returned error: %v", err)
	}
	if manifest.Status.Phase != runtimecontract.RuntimePhaseRunning {
		t.Fatalf("phase = %q, want running", manifest.Status.Phase)
	}
	if !manifest.Status.Healthy {
		t.Fatal("expected manifest status to be healthy after restart")
	}
}

func TestRuntimeSupervisorProbeBackendRoundTrip(t *testing.T) {
	home := t.TempDir()
	agentDir := filepath.Join(home, "agents", "alice")
	stateDir := filepath.Join(agentDir, "state")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, "agent.yaml"), []byte("uuid: ag_123\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tokenFile := filepath.Join(stateDir, "token.yaml")
	if err := os.WriteFile(tokenFile, []byte("- key: \"abc\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	rs := NewRuntimeSupervisor(home, "0.1.0", "", "build-1", probeRuntimeBackendName, nil, nil, nil, nil)
	spec, err := rs.Compile(context.Background(), "alice")
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	if spec.Backend != probeRuntimeBackendName {
		t.Fatalf("backend = %q, want %q", spec.Backend, probeRuntimeBackendName)
	}
	if err := os.MkdirAll(filepath.Dir(spec.Transport.Enforcer.TokenRef), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(spec.Transport.Enforcer.TokenRef, []byte("- key: \"abc\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := rs.Reconcile(context.Background(), spec); err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}
	if err := rs.Ensure(context.Background(), "alice"); err != nil {
		t.Fatalf("Ensure returned error: %v", err)
	}
	status, err := rs.Get(context.Background(), "alice")
	if err != nil {
		t.Fatalf("Get returned error: %v", err)
	}
	if status.Phase != runtimecontract.RuntimePhaseRunning {
		t.Fatalf("phase = %q, want running", status.Phase)
	}
	if !status.Healthy {
		t.Fatal("expected healthy runtime status")
	}
	if err := rs.Validate(context.Background(), "alice"); err != nil {
		t.Fatalf("Validate returned error: %v", err)
	}
}

func TestRuntimeSupervisorCompilePodmanBackend(t *testing.T) {
	home := t.TempDir()
	agentDir := filepath.Join(home, "agents", "alice")
	if err := os.MkdirAll(filepath.Join(agentDir, "state"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, "agent.yaml"), []byte("uuid: ag_123\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	rs := NewRuntimeSupervisor(home, "0.1.0", "", "build-1", runtimehost.BackendPodman, nil, nil, nil, nil)
	spec, err := rs.Compile(context.Background(), "alice")
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	if spec.Backend != runtimehost.BackendPodman {
		t.Fatalf("backend = %q, want %q", spec.Backend, runtimehost.BackendPodman)
	}
}
