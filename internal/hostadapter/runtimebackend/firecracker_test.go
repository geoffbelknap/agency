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
	for _, want := range []string{`"kernel_image_path": "/var/lib/agency/vmlinux"`, `"path_on_host": "/tmp/rootfs.ext4"`, `"guest_cid": 3`} {
		if !strings.Contains(text, want) {
			t.Fatalf("config missing %q:\n%s", want, text)
		}
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
