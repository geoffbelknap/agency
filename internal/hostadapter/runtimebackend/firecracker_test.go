package runtimebackend

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	runtimecontract "github.com/geoffbelknap/agency/internal/runtime/contract"
)

func TestFirecrackerRuntimeBackendSkeleton(t *testing.T) {
	backend := &FirecrackerRuntimeBackend{}
	if backend.Name() != BackendFirecracker {
		t.Fatalf("Name() = %q, want %q", backend.Name(), BackendFirecracker)
	}
	if err := backend.Ensure(context.Background(), runtimecontract.RuntimeSpec{}); err == nil || !strings.Contains(err.Error(), "kernel path is not configured") {
		t.Fatalf("Ensure() error = %v", err)
	}
	if err := backend.Stop(context.Background(), "alice"); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if _, err := backend.Inspect(context.Background(), "alice"); err == nil || !strings.Contains(err.Error(), "not tracked") {
		t.Fatalf("Inspect() error = %v", err)
	}
	if err := backend.Validate(context.Background(), "alice"); err == nil || !strings.Contains(err.Error(), "not tracked") {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestNewFirecrackerRuntimeBackendUsesConfig(t *testing.T) {
	home := t.TempDir()
	backend := NewFirecrackerRuntimeBackend(home, map[string]string{
		"binary_path":              "/usr/local/bin/firecracker",
		"kernel_path":              "/var/lib/agency/vmlinux",
		"state_dir":                filepath.Join(home, "fc-state"),
		"memory_mib":               "768",
		"rootfs_size_mib":          "2048",
		"stop_timeout":             "250ms",
		"enforcement_mode":         FirecrackerEnforcementModeMicroVM,
		"vsock_bridge_binary_path": "/usr/local/bin/agency-vsock-http-bridge",
	})
	if backend.BinaryPath != "/usr/local/bin/firecracker" {
		t.Fatalf("binary path = %q", backend.BinaryPath)
	}
	if backend.KernelPath != "/var/lib/agency/vmlinux" {
		t.Fatalf("kernel path = %q", backend.KernelPath)
	}
	if backend.StateDir != filepath.Join(home, "fc-state") {
		t.Fatalf("state dir = %q", backend.StateDir)
	}
	if backend.MemoryMiB != 768 {
		t.Fatalf("memory = %d", backend.MemoryMiB)
	}
	if backend.Images.SizeMiB != 2048 {
		t.Fatalf("rootfs size = %d", backend.Images.SizeMiB)
	}
	if backend.Images.VsockBridgeBinary != "/usr/local/bin/agency-vsock-http-bridge" {
		t.Fatalf("vsock bridge binary = %q", backend.Images.VsockBridgeBinary)
	}
	if backend.Tasks.StopTimeout.String() != "250ms" {
		t.Fatalf("stop timeout = %s", backend.Tasks.StopTimeout)
	}
	if backend.EnforcementMode != FirecrackerEnforcementModeMicroVM {
		t.Fatalf("enforcement mode = %q", backend.EnforcementMode)
	}
}

func TestFirecrackerRuntimeBackendDefaultsToHostProcessEnforcement(t *testing.T) {
	backend := NewFirecrackerRuntimeBackend(t.TempDir(), nil)
	if backend.EnforcementMode != FirecrackerEnforcementModeHostProcess {
		t.Fatalf("enforcement mode = %q, want %q", backend.EnforcementMode, FirecrackerEnforcementModeHostProcess)
	}
	if err := backend.validateConfig(); err != nil {
		t.Fatalf("validateConfig returned error: %v", err)
	}
}

func TestFirecrackerRuntimeBackendRejectsUnsupportedEnforcementMode(t *testing.T) {
	backend := NewFirecrackerRuntimeBackend(t.TempDir(), map[string]string{"enforcement_mode": "inside-agent"})
	if _, err := backend.Capabilities(context.Background()); err == nil || !strings.Contains(err.Error(), "unsupported enforcement_mode") {
		t.Fatalf("Capabilities() error = %v", err)
	}
}

func TestFirecrackerRuntimeBackendWritesConfig(t *testing.T) {
	dir := t.TempDir()
	backend := &FirecrackerRuntimeBackend{
		KernelPath: "/var/lib/agency/vmlinux",
		StateDir:   dir,
	}
	path, err := backend.writeConfig(runtimecontract.RuntimeSpec{RuntimeID: "alice"}, "/tmp/rootfs.ext4", filepath.Join(dir, "alice", "vsock.sock"))
	if err != nil {
		t.Fatalf("writeConfig returned error: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, want := range []string{`"kernel_image_path": "/var/lib/agency/vmlinux"`, `"path_on_host": "/tmp/rootfs.ext4"`, `"guest_cid": 3`, `"mem_size_mib": 512`} {
		if !strings.Contains(text, want) {
			t.Fatalf("config missing %q:\n%s", want, text)
		}
	}
}

func TestFirecrackerRuntimeBackendWritesConfiguredMemory(t *testing.T) {
	dir := t.TempDir()
	backend := &FirecrackerRuntimeBackend{
		KernelPath: "/var/lib/agency/vmlinux",
		StateDir:   dir,
		MemoryMiB:  768,
	}
	path, err := backend.writeConfig(runtimecontract.RuntimeSpec{RuntimeID: "alice"}, "/tmp/rootfs.ext4", filepath.Join(dir, "alice", "vsock.sock"))
	if err != nil {
		t.Fatalf("writeConfig returned error: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"mem_size_mib": 768`) {
		t.Fatalf("config missing configured memory:\n%s", string(data))
	}
}

func TestFirecrackerRuntimeBackendCleanupRuntimeState(t *testing.T) {
	dir := t.TempDir()
	backend := &FirecrackerRuntimeBackend{StateDir: dir}
	for _, path := range []string{
		filepath.Join(dir, "alice", "firecracker.json"),
		filepath.Join(dir, "tasks", "alice", "rootfs.ext4"),
		filepath.Join(dir, "pids", "alice.pid"),
		filepath.Join(dir, "images", "base.ext4"),
		filepath.Join(dir, "logs", "alice.log"),
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := backend.cleanupRuntimeState("alice"); err != nil {
		t.Fatalf("cleanupRuntimeState returned error: %v", err)
	}
	for _, path := range []string{
		filepath.Join(dir, "alice"),
		filepath.Join(dir, "tasks", "alice"),
		filepath.Join(dir, "pids", "alice.pid"),
	} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("expected %s to be removed, err=%v", path, err)
		}
	}
	for _, path := range []string{
		filepath.Join(dir, "images", "base.ext4"),
		filepath.Join(dir, "logs", "alice.log"),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected %s to remain: %v", path, err)
		}
	}
}

func TestFirecrackerGuestEnvRemovesHostOnlyTargets(t *testing.T) {
	env := firecrackerGuestEnv(map[string]string{
		"AGENCY_AGENT_NAME":                  "alice",
		"AGENCY_TRANSPORT_ENFORCER_ENDPOINT": "vsock://2:8081",
		FirecrackerEnforcerProxyTargetEnv:    "http://127.0.0.1:19000",
		FirecrackerEnforcerControlTargetEnv:  "http://127.0.0.1:19001",
	})
	if env["AGENCY_AGENT_NAME"] != "alice" {
		t.Fatalf("guest env missing agent name: %#v", env)
	}
	if env["AGENCY_TRANSPORT_ENFORCER_ENDPOINT"] != "vsock://2:8081" {
		t.Fatalf("guest env missing transport endpoint: %#v", env)
	}
	for _, key := range []string{FirecrackerEnforcerProxyTargetEnv, FirecrackerEnforcerControlTargetEnv} {
		if _, ok := env[key]; ok {
			t.Fatalf("guest env leaked host-only key %s: %#v", key, env)
		}
	}
}

func TestFirecrackerRuntimeBackendInspectDegradesWhenBridgeMissing(t *testing.T) {
	dir := t.TempDir()
	supervisor := &FirecrackerVMSupervisor{
		BinaryPath: "/bin/sh",
		LogDir:     filepath.Join(dir, "logs"),
		PIDDir:     filepath.Join(dir, "pids"),
	}
	backend := &FirecrackerRuntimeBackend{
		StateDir: dir,
		Tasks:    supervisor,
		Vsock:    &FirecrackerVsockListenerFactory{StateDir: dir},
	}
	spec := runtimecontract.RuntimeSpec{RuntimeID: "alice"}
	if err := supervisor.Start(context.Background(), spec, []string{"-c", "sleep 30"}); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	defer backend.Stop(context.Background(), "alice") //nolint:errcheck
	if err := waitForFirecrackerVM(t, supervisor, "alice", func(status FirecrackerVMStatus) bool {
		return status.State == FirecrackerVMRunning
	}); err != nil {
		t.Fatal(err)
	}
	status, err := backend.Inspect(context.Background(), "alice")
	if err != nil {
		t.Fatalf("Inspect returned error: %v", err)
	}
	if status.Phase != runtimecontract.RuntimePhaseDegraded {
		t.Fatalf("phase = %q, want degraded", status.Phase)
	}
	if status.Details["vsock_bridge_state"] != "stopped" {
		t.Fatalf("vsock bridge state = %q", status.Details["vsock_bridge_state"])
	}
	if status.Details["workload_vm_state"] != FirecrackerVMRunning {
		t.Fatalf("workload_vm_state = %q, want %q", status.Details["workload_vm_state"], FirecrackerVMRunning)
	}
	if status.Details["workload_pid"] == "" || status.Details["workload_pid"] == "0" {
		t.Fatalf("workload_pid = %q, want running process id", status.Details["workload_pid"])
	}
	if status.Details["last_error"] != "vsock bridge is not running" {
		t.Fatalf("last error = %q", status.Details["last_error"])
	}
}

func TestFirecrackerRuntimeBackendInspectDegradesWhenBridgeSocketMissing(t *testing.T) {
	dir := t.TempDir()
	supervisor := &FirecrackerVMSupervisor{
		BinaryPath: "/bin/sh",
		LogDir:     filepath.Join(dir, "logs"),
		PIDDir:     filepath.Join(dir, "pids"),
	}
	factory := &FirecrackerVsockListenerFactory{
		StateDir: dir,
		bridges: map[string]*FirecrackerVsockBridge{
			"alice": {
				RuntimeID: "alice",
				Paths: map[int]string{
					8081: filepath.Join(dir, "alice", "vsock.sock_8081"),
				},
			},
		},
	}
	backend := &FirecrackerRuntimeBackend{
		StateDir: dir,
		Tasks:    supervisor,
		Vsock:    factory,
	}
	spec := runtimecontract.RuntimeSpec{RuntimeID: "alice"}
	if err := supervisor.Start(context.Background(), spec, []string{"-c", "sleep 30"}); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	defer backend.Stop(context.Background(), "alice") //nolint:errcheck
	if err := waitForFirecrackerVM(t, supervisor, "alice", func(status FirecrackerVMStatus) bool {
		return status.State == FirecrackerVMRunning
	}); err != nil {
		t.Fatal(err)
	}
	status, err := backend.Inspect(context.Background(), "alice")
	if err != nil {
		t.Fatalf("Inspect returned error: %v", err)
	}
	if status.Phase != runtimecontract.RuntimePhaseDegraded {
		t.Fatalf("phase = %q, want degraded", status.Phase)
	}
	if status.Details["vsock_bridge_state"] != "degraded" {
		t.Fatalf("vsock bridge state = %q", status.Details["vsock_bridge_state"])
	}
}

func TestFirecrackerRuntimeBackendCapabilities(t *testing.T) {
	caps, err := (&FirecrackerRuntimeBackend{}).Capabilities(context.Background())
	if err != nil {
		t.Fatalf("Capabilities() returned error: %v", err)
	}
	if len(caps.SupportedTransportTypes) != 1 || caps.SupportedTransportTypes[0] != runtimecontract.TransportTypeVsockHTTP {
		t.Fatalf("supported transports = %#v", caps.SupportedTransportTypes)
	}
	if caps.SupportsRootless {
		t.Fatal("SupportsRootless = true, want false")
	}
	if caps.SupportsComposeLike {
		t.Fatal("SupportsComposeLike = true, want false")
	}
	if caps.Isolation != runtimecontract.IsolationMicroVM {
		t.Fatalf("Isolation = %q, want %q", caps.Isolation, runtimecontract.IsolationMicroVM)
	}
	if !caps.RequiresKVM {
		t.Fatal("RequiresKVM = false, want true")
	}
	if !caps.SupportsSnapshots {
		t.Fatal("SupportsSnapshots = false, want true")
	}
}

func TestFirecrackerEnforcerTargetsDefaultCompatibility(t *testing.T) {
	targets, err := firecrackerEnforcerTargets(runtimecontract.RuntimeSpec{
		Transport: runtimecontract.RuntimeTransportSpec{
			Enforcer: runtimecontract.EnforcerTransportSpec{Endpoint: "http://127.0.0.1:19128"},
		},
	})
	if err != nil {
		t.Fatalf("firecrackerEnforcerTargets returned error: %v", err)
	}
	if len(targets) != 1 || targets[9999] != "127.0.0.1:19128" {
		t.Fatalf("targets = %#v", targets)
	}
}

func TestFirecrackerEnforcerTargetsProxyAndControl(t *testing.T) {
	targets, err := firecrackerEnforcerTargets(runtimecontract.RuntimeSpec{
		Package: runtimecontract.RuntimePackageSpec{Env: map[string]string{
			FirecrackerEnforcerProxyTargetEnv:   "http://127.0.0.1:19128",
			FirecrackerEnforcerControlTargetEnv: "http://127.0.0.1:19081",
		}},
	})
	if err != nil {
		t.Fatalf("firecrackerEnforcerTargets returned error: %v", err)
	}
	if len(targets) != 2 || targets[3128] != "127.0.0.1:19128" || targets[8081] != "127.0.0.1:19081" {
		t.Fatalf("targets = %#v", targets)
	}
}

func TestParseFirecrackerEnforcementMode(t *testing.T) {
	for _, tt := range []struct {
		raw  string
		want string
	}{
		{"", FirecrackerEnforcementModeHostProcess},
		{"host-process", FirecrackerEnforcementModeHostProcess},
		{"microvm", FirecrackerEnforcementModeMicroVM},
		{"MICROVM", FirecrackerEnforcementModeMicroVM},
	} {
		got, err := parseFirecrackerEnforcementMode(tt.raw)
		if err != nil {
			t.Fatalf("parseFirecrackerEnforcementMode(%q) returned error: %v", tt.raw, err)
		}
		if got != tt.want {
			t.Fatalf("parseFirecrackerEnforcementMode(%q) = %q, want %q", tt.raw, got, tt.want)
		}
	}
	if _, err := parseFirecrackerEnforcementMode("shared"); err == nil {
		t.Fatal("expected unsupported enforcement mode to fail")
	}
}
