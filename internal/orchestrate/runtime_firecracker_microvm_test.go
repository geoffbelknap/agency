package orchestrate

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	agentruntime "github.com/geoffbelknap/agency/internal/hostadapter/agentruntime"
	hostruntimebackend "github.com/geoffbelknap/agency/internal/hostadapter/runtimebackend"
	runtimecontract "github.com/geoffbelknap/agency/internal/runtime/contract"
)

func TestFirecrackerCompileEnforcerMicroVMSpec(t *testing.T) {
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
		home:    home,
		buildID: "build-1",
	}
	spec, err := backend.compileEnforcerMicroVMSpec(context.Background(), runtimecontract.RuntimeSpec{
		RuntimeID: "alice",
		AgentID:   "principal-1",
		Revision:  runtimecontract.RuntimeRevisionSpec{InstanceRevision: "rev-1"},
		Package: runtimecontract.RuntimePackageSpec{Env: map[string]string{
			hostruntimebackend.FirecrackerEnforcerProxyTargetEnv:   "http://127.0.0.1:19128",
			hostruntimebackend.FirecrackerEnforcerControlTargetEnv: "http://127.0.0.1:19081",
		}},
	}, false)
	if err != nil {
		t.Fatalf("compileEnforcerMicroVMSpec returned error: %v", err)
	}
	if spec.RuntimeID != "alice-enforcer" || spec.ParentRuntimeID != "alice" || spec.Role != firecrackerComponentEnforcer {
		t.Fatalf("unexpected identity fields: %#v", spec)
	}
	if spec.Image != enforcerImage {
		t.Fatalf("image = %q, want %q", spec.Image, enforcerImage)
	}
	for key, want := range map[string]string{
		"AGENT_NAME":                "alice",
		"BUILD_ID":                  "build-1",
		"GATEWAY_TOKEN":             "gateway-token",
		"ENFORCER_PORT":             agentruntime.EnforcerProxyPort,
		"CONSTRAINT_WS_PORT":        agentruntime.EnforcerConstraintPort,
		"ENFORCER_BIND_ADDR":        "0.0.0.0",
		"CONSTRAINT_WS_BIND_ADDR":   "0.0.0.0",
		"GATEWAY_URL":               "http://127.0.0.1:8200",
		"COMMS_URL":                 "http://127.0.0.1:8202",
		"KNOWLEDGE_URL":             "http://127.0.0.1:8204",
		"WEB_FETCH_URL":             "http://127.0.0.1:8206",
		"EGRESS_PROXY":              "http://127.0.0.1:8312",
		"AGENCY_VSOCK_HTTP_BRIDGES": "127.0.0.1:8200=2:8200,127.0.0.1:8202=2:8202,127.0.0.1:8204=2:8204,127.0.0.1:8206=2:8206,127.0.0.1:8312=2:8312",
	} {
		if got := spec.Env[key]; got != want {
			t.Fatalf("env[%s] = %q, want %q", key, got, want)
		}
	}
	for _, key := range []string{
		hostruntimebackend.FirecrackerEnforcerProxyTargetEnv,
		hostruntimebackend.FirecrackerEnforcerControlTargetEnv,
	} {
		if _, ok := spec.Env[key]; ok {
			t.Fatalf("microVM enforcer env leaked host-only key %s: %#v", key, spec.Env)
		}
	}
	if _, ok := spec.HostServicePorts[8200]; !ok {
		t.Fatalf("host service ports missing gateway: %#v", spec.HostServicePorts)
	}
	if got := spec.Env[hostruntimebackend.FirecrackerHostServiceTargetEnv(8200)]; got != "http://127.0.0.1:8200" {
		t.Fatalf("gateway target env = %q, want http://127.0.0.1:8200", got)
	}
	if !hasFirecrackerEnforcerMount(spec.Mounts, agentruntime.EnforcerMount{
		HostPath:  filepath.Join(home, "agents", "alice", "state", "enforcer-auth"),
		GuestPath: "/agency/enforcer/auth",
		Mode:      "ro",
	}) {
		t.Fatalf("missing scoped auth mount in %#v", spec.Mounts)
	}
}

func TestFirecrackerComponentRuntimeID(t *testing.T) {
	if got := firecrackerComponentRuntimeID("alice", firecrackerComponentWorkload); got != "alice" {
		t.Fatalf("workload runtime id = %q, want alice", got)
	}
	if got := firecrackerComponentRuntimeID("alice", firecrackerComponentEnforcer); got != "alice-enforcer" {
		t.Fatalf("enforcer runtime id = %q, want alice-enforcer", got)
	}
}

func hasFirecrackerEnforcerMount(mounts []agentruntime.EnforcerMount, want agentruntime.EnforcerMount) bool {
	for _, mount := range mounts {
		if mount == want {
			return true
		}
	}
	return false
}
