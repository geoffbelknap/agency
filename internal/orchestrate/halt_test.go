package orchestrate

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	runtimecontract "github.com/geoffbelknap/agency/internal/runtime/contract"
)

func TestHaltControllerResumeUsesRuntimeSupervisor(t *testing.T) {
	home := t.TempDir()
	agentDir := filepath.Join(home, "agents", "alice")
	stateDir := filepath.Join(agentDir, "state")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, "agent.yaml"), []byte("uuid: ag_123\nmodel: gpt-5-mini\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, "constraints.yaml"), []byte("agent: alice\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, "identity.md"), []byte("# Alice\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, "enforcer-auth/api_keys.yaml"), []byte("- key: \"abc\"\n"), 0o644); err == nil {
		t.Fatal("expected write without parent dir to fail")
	}
	if err := os.MkdirAll(filepath.Join(stateDir, "enforcer-auth"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, "enforcer-auth", "api_keys.yaml"), []byte("- key: \"abc\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(activeHaltPath(home, "alice"), []byte(`{"halt_id":"h1"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "capacity.yaml"), []byte("max_agents: 10\nmax_meeseeks: 10\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	rs := NewRuntimeSupervisor(home, "0.1.0", "", "build-1", probeRuntimeBackendName, nil, noopCommsClient{}, nil, nil)
	spec, err := rs.Compile(context.Background(), "alice")
	if err != nil {
		t.Fatal(err)
	}
	if err := rs.Reconcile(context.Background(), spec); err != nil {
		t.Fatal(err)
	}
	hc := &HaltController{
		Home:        home,
		Version:     "0.1.0",
		BackendName: probeRuntimeBackendName,
		Comms:       noopCommsClient{},
		log:         slog.Default(),
		Runtime:     rs,
	}

	if err := hc.Resume(context.Background(), "alice", "operator"); err != nil {
		t.Fatalf("Resume() returned error: %v", err)
	}
	status, err := rs.Get(context.Background(), "alice")
	if err != nil {
		t.Fatal(err)
	}
	if status.Phase != runtimecontract.RuntimePhaseRunning {
		t.Fatalf("phase = %q, want running", status.Phase)
	}
	if activeHaltExists(home, "alice") {
		t.Fatal("expected active halt to be cleared")
	}
}

type noopCommsClient struct{}

func (noopCommsClient) CommsRequest(context.Context, string, string, interface{}) ([]byte, error) {
	return []byte(`{"ok":true}`), nil
}
