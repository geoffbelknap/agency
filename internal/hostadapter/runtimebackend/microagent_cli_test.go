package runtimebackend

import (
	"context"
	"reflect"
	"strings"
	"testing"

	runtimecontract "github.com/geoffbelknap/agency/internal/runtime/contract"
)

func TestMicroagentCLIEnsureCreatesAndStartsWorkspace(t *testing.T) {
	var calls [][]string
	backend := NewMicroagentCLIRuntimeBackend("/tmp/agency", map[string]string{
		"binary_path":     "microagent-test",
		"state_dir":       "/tmp/agency/runtime/microagent",
		"entrypoint":      "/app/entrypoint.sh",
		"rootfs_oci_ref":  "ghcr.io/example/body:v1",
		"mke2fs_path":     "/opt/e2fsprogs/mke2fs",
		"rootfs_size_mib": "1536",
		"memory_mib":      "1024",
		"cpu_count":       "4",
	})
	backend.RunCommand = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		_ = ctx
		calls = append(calls, append([]string{name}, args...))
		return []byte(`{"ok":true}`), nil
	}
	err := backend.Ensure(context.Background(), runtimecontract.RuntimeSpec{
		RuntimeID: "alice",
		Package: runtimecontract.RuntimePackageSpec{
			Image: "agency-body:latest",
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
		{"microagent-test", "create", "--name", "alice", "--image", "ghcr.io/example/body:v1", "--state-dir", "/tmp/agency/runtime/microagent", "--memory", "1024", "--cpus", "4", "--entrypoint", "/app/entrypoint.sh", "--mke2fs", "/opt/e2fsprogs/mke2fs", "--size-mib", "1536", "--env", "AGENCY_AGENT_NAME=alice", "--env", "MICROAGENT_VSOCK_TCP_LISTENERS=3128=3128,8081=8081"},
		{"microagent-test", "start", "alice", "--state-dir", "/tmp/agency/runtime/microagent", "--memory", "1024", "--cpus", "4", "--vsock", "3128=127.0.0.1:19000", "--vsock", "8081=127.0.0.1:19001"},
	}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls = %#v, want %#v", calls, want)
	}
}

func TestMicroagentCLIEnsureRequiresConfiguredBodyImageForLegacyLocalTag(t *testing.T) {
	backend := NewMicroagentCLIRuntimeBackend("/tmp/agency", map[string]string{})
	backend.RunCommand = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		t.Fatal("microagent should not be called without a resolvable OCI image")
		return nil, nil
	}
	err := backend.Ensure(context.Background(), runtimecontract.RuntimeSpec{
		RuntimeID: "alice",
		Package:   runtimecontract.RuntimePackageSpec{Image: "agency-body:latest"},
	})
	if err == nil || !strings.Contains(err.Error(), "rootfs OCI artifact is not configured") {
		t.Fatalf("Ensure error = %v", err)
	}
}

func TestMicroagentCLIEnsureRejectsMutableConfiguredBodyImage(t *testing.T) {
	backend := NewMicroagentCLIRuntimeBackend("/tmp/agency", map[string]string{
		"rootfs_oci_ref": "ghcr.io/example/agency-body:latest",
	})
	backend.RunCommand = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		t.Fatal("microagent should not be called with a mutable OCI ref")
		return nil, nil
	}
	err := backend.Ensure(context.Background(), runtimecontract.RuntimeSpec{
		RuntimeID: "alice",
		Package:   runtimecontract.RuntimePackageSpec{Image: "agency-body:latest"},
	})
	if err == nil || !strings.Contains(err.Error(), "must not use mutable :latest tag") {
		t.Fatalf("Ensure error = %v", err)
	}
}

func TestMicroagentCLIEnsureUsesDirectVersionedImage(t *testing.T) {
	var createArgs []string
	backend := NewMicroagentCLIRuntimeBackend("/tmp/agency", map[string]string{
		"binary_path": "microagent-test",
	})
	backend.RunCommand = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		_ = ctx
		if len(args) > 0 && args[0] == "create" {
			createArgs = append([]string{name}, args...)
		}
		return []byte(`{"ok":true}`), nil
	}
	err := backend.Ensure(context.Background(), runtimecontract.RuntimeSpec{
		RuntimeID: "alice",
		Package: runtimecontract.RuntimePackageSpec{
			Image: "ghcr.io/example/agency-body:v1",
		},
	})
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	wantPrefix := []string{"microagent-test", "create", "--name", "alice", "--image", "ghcr.io/example/agency-body:v1"}
	if len(createArgs) < len(wantPrefix) || !reflect.DeepEqual(createArgs[:len(wantPrefix)], wantPrefix) {
		t.Fatalf("create args = %#v", createArgs)
	}
	if !containsSequence(createArgs, "--entrypoint", "/app/entrypoint.sh") {
		t.Fatalf("create args missing default entrypoint: %#v", createArgs)
	}
}

func TestMicroagentGuestEnvDropsHostOnlyMediationValues(t *testing.T) {
	got := microagentGuestEnv(map[string]string{
		"AGENCY_AGENT_NAME":                           "alice",
		FirecrackerEnforcerProxyTargetEnv:             "http://127.0.0.1:19000",
		FirecrackerEnforcerControlTargetEnv:           "http://127.0.0.1:19001",
		FirecrackerHostServiceTargetEnvBase + "COMMS": "http://127.0.0.1:18080",
		FirecrackerRootFSOverlaysEnv:                  "/tmp/overlay",
	})
	want := []string{"AGENCY_AGENT_NAME=alice"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("guest env = %#v, want %#v", got, want)
	}
}

func TestMicroagentGuestEnvWithVsockBridgeAddsDefaultListeners(t *testing.T) {
	got := microagentGuestEnvWithVsockBridge(map[string]string{
		"AGENCY_AGENT_NAME": "alice",
	}, []string{"3128=127.0.0.1:19000", "8081=127.0.0.1:19001"})
	want := []string{
		"AGENCY_AGENT_NAME=alice",
		"MICROAGENT_VSOCK_TCP_LISTENERS=3128=3128,8081=8081",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("guest env = %#v, want %#v", got, want)
	}
}

func TestMicroagentGuestEnvWithVsockBridgeKeepsExplicitListeners(t *testing.T) {
	got := microagentGuestEnvWithVsockBridge(map[string]string{
		microagentTCPVsockListenersEnv: "9000=9000",
	}, []string{"3128=127.0.0.1:19000"})
	want := []string{"MICROAGENT_VSOCK_TCP_LISTENERS=9000=9000"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("guest env = %#v, want %#v", got, want)
	}
}

func TestMicroagentEnforcerVsockMappings(t *testing.T) {
	got := microagentEnforcerVsockMappings(map[string]string{
		FirecrackerEnforcerProxyTargetEnv:   "http://127.0.0.1:19000",
		FirecrackerEnforcerControlTargetEnv: "http://localhost:19001",
	})
	want := []string{"3128=127.0.0.1:19000", "8081=localhost:19001"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("mappings = %#v, want %#v", got, want)
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

func containsSequence(values []string, want ...string) bool {
	if len(want) == 0 || len(values) < len(want) {
		return false
	}
	for i := 0; i <= len(values)-len(want); i++ {
		if reflect.DeepEqual(values[i:i+len(want)], want) {
			return true
		}
	}
	return false
}
