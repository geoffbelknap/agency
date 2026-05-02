package runtimebackend

import (
	"context"
	"reflect"
	"testing"

	runtimecontract "github.com/geoffbelknap/agency/internal/runtime/contract"
)

func TestMicroagentCLIEnsureCreatesAndStartsWorkspace(t *testing.T) {
	var calls [][]string
	backend := NewMicroagentCLIRuntimeBackend("/tmp/agency", map[string]string{
		"binary_path": "microagent-test",
		"state_dir":   "/tmp/agency/runtime/microagent",
		"entrypoint":  "/app/entrypoint.sh",
		"memory_mib":  "1024",
		"cpu_count":   "4",
	})
	backend.RunCommand = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		_ = ctx
		calls = append(calls, append([]string{name}, args...))
		return []byte(`{"ok":true}`), nil
	}
	err := backend.Ensure(context.Background(), runtimecontract.RuntimeSpec{
		RuntimeID: "alice",
		Package: runtimecontract.RuntimePackageSpec{
			Image: "ghcr.io/example/body:v1",
			Env: map[string]string{
				"AGENCY_AGENT_NAME":                 "alice",
				FirecrackerEnforcerProxyTargetEnv:   "http://127.0.0.1:19000",
				FirecrackerEnforcerControlTargetEnv: "http://127.0.0.1:19001",
			},
		},
	})
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	want := [][]string{
		{"microagent-test", "create", "--name", "alice", "--image", "ghcr.io/example/body:v1", "--state-dir", "/tmp/agency/runtime/microagent", "--memory", "1024", "--cpus", "4", "--entrypoint", "/app/entrypoint.sh", "--env", "AGENCY_AGENT_NAME=alice"},
		{"microagent-test", "start", "alice", "--state-dir", "/tmp/agency/runtime/microagent", "--memory", "1024", "--cpus", "4"},
	}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls = %#v, want %#v", calls, want)
	}
}

func TestMicroagentCLIInspectMapsRunningStatus(t *testing.T) {
	backend := NewMicroagentCLIRuntimeBackend("/tmp/agency", map[string]string{})
	backend.RunCommand = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		_ = ctx
		return []byte(`{"ok":true,"event":{"state":"running"}}`), nil
	}
	status, err := backend.Inspect(context.Background(), "alice")
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	if !status.Healthy || status.Phase != runtimecontract.RuntimePhaseRunning {
		t.Fatalf("status = %#v", status)
	}
	if status.Details["state_dir"] != "/tmp/agency/runtime/microagent" {
		t.Fatalf("state_dir = %q", status.Details["state_dir"])
	}
}

func TestMicroagentCLIStopStopsAndDeletesWorkspace(t *testing.T) {
	var calls [][]string
	backend := NewMicroagentCLIRuntimeBackend("/tmp/agency", map[string]string{"binary_path": "microagent-test"})
	backend.RunCommand = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		_ = ctx
		calls = append(calls, append([]string{name}, args...))
		return []byte(`{"ok":true}`), nil
	}
	if err := backend.Stop(context.Background(), "alice"); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	want := [][]string{
		{"microagent-test", "stop", "alice", "--state-dir", "/tmp/agency/runtime/microagent"},
		{"microagent-test", "delete", "alice", "--state-dir", "/tmp/agency/runtime/microagent"},
	}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls = %#v, want %#v", calls, want)
	}
}
