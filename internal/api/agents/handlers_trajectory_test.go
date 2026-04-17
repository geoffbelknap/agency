package agents

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/geoffbelknap/agency/internal/orchestrate"
	runtimecontract "github.com/geoffbelknap/agency/internal/runtime/contract"
)

func TestTrajectoryURLUsesRuntimeTransportEndpoint(t *testing.T) {
	home := t.TempDir()
	agentDir := filepath.Join(home, "agents", "agent")
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
	rs := orchestrate.NewRuntimeSupervisor(home, "0.1.0", "", "build-1", "probe", nil, nil, nil, nil)
	spec := runtimecontract.RuntimeSpec{
		RuntimeID: "agent",
		AgentID:   "ag_123",
		Backend:   "probe",
		Transport: runtimecontract.RuntimeTransportSpec{
			Enforcer: runtimecontract.EnforcerTransportSpec{
				Type:     runtimecontract.TransportTypeLoopbackHTTP,
				Endpoint: "http://127.0.0.1:9911",
				TokenRef: tokenFile,
			},
		},
		Storage: runtimecontract.RuntimeStorageSpec{
			ConfigPath: agentDir,
			StatePath:  stateDir,
		},
	}
	if err := rs.Reconcile(context.Background(), spec); err != nil {
		t.Fatal(err)
	}
	h := &handler{deps: Deps{AgentManager: &orchestrate.AgentManager{Home: home, Runtime: rs}}}
	got, err := h.trajectoryURL("agent")
	if err != nil {
		t.Fatalf("trajectoryURL() returned error: %v", err)
	}
	if got != "http://127.0.0.1:9911/trajectory" {
		t.Fatalf("trajectoryURL() = %q, want %q", got, "http://127.0.0.1:9911/trajectory")
	}
}

func TestTrajectoryURLFallsBackToLegacyEnforcerName(t *testing.T) {
	h := &handler{}
	got, err := h.trajectoryURL("agent")
	if err != nil {
		t.Fatalf("trajectoryURL() returned error: %v", err)
	}
	if got != "http://agency-agent-enforcer:8081/trajectory" {
		t.Fatalf("trajectoryURL() = %q", got)
	}
}

func TestHostResultsDirUsesRuntimeWorkspacePath(t *testing.T) {
	home := t.TempDir()
	agentDir := filepath.Join(home, "agents", "agent")
	stateDir := filepath.Join(agentDir, "state")
	workspaceDir := filepath.Join(agentDir, "workspace")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(workspaceDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, "agent.yaml"), []byte("uuid: ag_123\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tokenFile := filepath.Join(stateDir, "token.yaml")
	if err := os.WriteFile(tokenFile, []byte("- key: \"abc\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rs := orchestrate.NewRuntimeSupervisor(home, "0.1.0", "", "build-1", "probe", nil, nil, nil, nil)
	spec := runtimecontract.RuntimeSpec{
		RuntimeID: "agent",
		AgentID:   "ag_123",
		Backend:   "probe",
		Transport: runtimecontract.RuntimeTransportSpec{
			Enforcer: runtimecontract.EnforcerTransportSpec{
				Type:     runtimecontract.TransportTypeLoopbackHTTP,
				Endpoint: "http://127.0.0.1:9911",
				TokenRef: tokenFile,
			},
		},
		Storage: runtimecontract.RuntimeStorageSpec{
			ConfigPath:    agentDir,
			StatePath:     stateDir,
			WorkspacePath: workspaceDir,
		},
	}
	if err := rs.Reconcile(context.Background(), spec); err != nil {
		t.Fatal(err)
	}
	h := &handler{deps: Deps{AgentManager: &orchestrate.AgentManager{Home: home, Runtime: rs}}}
	got, ok := h.hostResultsDir("agent")
	if !ok {
		t.Fatal("hostResultsDir() = false, want true")
	}
	want := filepath.Join(workspaceDir, ".results")
	if got != want {
		t.Fatalf("hostResultsDir() = %q, want %q", got, want)
	}
}

func TestHostResultsDirRejectsContainerWorkspacePath(t *testing.T) {
	home := t.TempDir()
	agentDir := filepath.Join(home, "agents", "agent")
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
	rs := orchestrate.NewRuntimeSupervisor(home, "0.1.0", "", "build-1", "docker", nil, nil, nil, nil)
	spec := runtimecontract.RuntimeSpec{
		RuntimeID: "agent",
		AgentID:   "ag_123",
		Backend:   "docker",
		Transport: runtimecontract.RuntimeTransportSpec{
			Enforcer: runtimecontract.EnforcerTransportSpec{
				Type:     runtimecontract.TransportTypeLoopbackHTTP,
				Endpoint: "http://127.0.0.1:9911",
				TokenRef: tokenFile,
			},
		},
		Storage: runtimecontract.RuntimeStorageSpec{
			ConfigPath:    agentDir,
			StatePath:     stateDir,
			WorkspacePath: "/workspace",
		},
	}
	if err := rs.Reconcile(context.Background(), spec); err != nil {
		t.Fatal(err)
	}
	h := &handler{deps: Deps{AgentManager: &orchestrate.AgentManager{Home: home, Runtime: rs}}}
	if _, ok := h.hostResultsDir("agent"); ok {
		t.Fatal("hostResultsDir() = true, want false")
	}
}
