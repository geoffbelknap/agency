package orchestrate

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	hostruntimebackend "github.com/geoffbelknap/agency/internal/hostadapter/runtimebackend"
	runtimecontract "github.com/geoffbelknap/agency/internal/runtime/contract"
)

func TestFirecrackerAgentBodyConnected(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ws/connected/alice" {
			t.Fatalf("path = %q, want /ws/connected/alice", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"agent":"alice","connected":true}`))
	}))
	defer server.Close()

	connected, err := firecrackerAgentBodyConnected(context.Background(), server.URL, "alice")
	if err != nil {
		t.Fatalf("firecrackerAgentBodyConnected returned error: %v", err)
	}
	if !connected {
		t.Fatal("connected = false, want true")
	}
}

func TestFirecrackerAgentBodyConnectedReturnsFalseWhenDisconnected(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"agent":"alice","connected":false}`))
	}))
	defer server.Close()

	connected, err := firecrackerAgentBodyConnected(context.Background(), server.URL, "alice")
	if err != nil {
		t.Fatalf("firecrackerAgentBodyConnected returned error: %v", err)
	}
	if connected {
		t.Fatal("connected = true, want false")
	}
}

func TestFirecrackerComponentBodyReadinessDegradesStatus(t *testing.T) {
	backend := &firecrackerComponentRuntimeBackend{}
	status := runtimecontract.BackendStatus{
		RuntimeID: "alice",
		Healthy:   true,
		Phase:     runtimecontract.RuntimePhaseRunning,
		Details:   map[string]string{},
	}

	status = backend.applyBodyReadiness(context.Background(), "alice", status)

	if status.Healthy {
		t.Fatal("status should be unhealthy when body readiness cannot be checked")
	}
	if status.Phase != runtimecontract.RuntimePhaseDegraded {
		t.Fatalf("phase = %q, want %q", status.Phase, runtimecontract.RuntimePhaseDegraded)
	}
	if status.Details["body_ws_connected"] != "false" {
		t.Fatalf("body_ws_connected = %q, want false", status.Details["body_ws_connected"])
	}
	if status.Details["last_error"] == "" {
		t.Fatal("last_error should explain the failed readiness check")
	}
}

func TestFirecrackerComponentStatusAddsEnforcerComponentDetails(t *testing.T) {
	backend := &firecrackerComponentRuntimeBackend{
		backend: &hostruntimebackend.FirecrackerRuntimeBackend{},
	}
	status := runtimecontract.BackendStatus{
		RuntimeID: "alice",
		Healthy:   true,
		Phase:     runtimecontract.RuntimePhaseRunning,
		Details:   map[string]string{},
	}
	status = backend.applyEnforcerComponentStatus("alice", status)
	if status.Details["enforcer_substrate"] != hostruntimebackend.FirecrackerEnforcementModeHostProcess {
		t.Fatalf("enforcer_substrate = %q", status.Details["enforcer_substrate"])
	}
	if status.Details["enforcer_state"] != "stopped" || status.Details["enforcer_component_state"] != "stopped" {
		t.Fatalf("enforcer status details = %#v", status.Details)
	}
	if status.Phase != runtimecontract.RuntimePhaseDegraded {
		t.Fatalf("phase = %q, want degraded", status.Phase)
	}
}

func TestFirecrackerMicroVMEnforcementModeUsesBackendRuntime(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, "agents", "alice"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte("token: gateway-token\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	backend := &firecrackerComponentRuntimeBackend{
		backend: &hostruntimebackend.FirecrackerRuntimeBackend{
			EnforcementMode: hostruntimebackend.FirecrackerEnforcementModeMicroVM,
		},
		home: home,
	}
	err := backend.EnsureEnforcer(context.Background(), runtimecontract.RuntimeSpec{RuntimeID: "alice"}, false)
	if err == nil || !strings.Contains(err.Error(), "kernel path is not configured") {
		t.Fatalf("EnsureEnforcer error = %v, want backend validation error", err)
	}
}
