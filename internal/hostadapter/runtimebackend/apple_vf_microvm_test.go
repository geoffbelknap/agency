package runtimebackend

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	runtimecontract "github.com/geoffbelknap/agency/internal/runtime/contract"
)

func TestAppleVFMicroVMBackendSkeleton(t *testing.T) {
	home := t.TempDir()
	backend := NewAppleVFMicroVMRuntimeBackend(home, map[string]string{})
	if backend.Name() != BackendAppleVFMicroVM {
		t.Fatalf("Name() = %q, want %q", backend.Name(), BackendAppleVFMicroVM)
	}
	if backend.StateDir != filepath.Join(home, "runtime", "apple-vf-microvm") {
		t.Fatalf("StateDir = %q", backend.StateDir)
	}
	if backend.Images == nil {
		t.Fatal("Images = nil, want shared OCI rootfs image store")
	}
	if backend.Images.StateDir != backend.StateDir {
		t.Fatalf("image store state dir = %q, want %q", backend.Images.StateDir, backend.StateDir)
	}
	caps, err := backend.Capabilities(context.Background())
	if err != nil {
		t.Fatalf("Capabilities() error = %v", err)
	}
	if caps.Isolation != runtimecontract.IsolationMicroVM {
		t.Fatalf("Isolation = %q, want %q", caps.Isolation, runtimecontract.IsolationMicroVM)
	}
	if caps.RequiresKVM {
		t.Fatal("RequiresKVM = true, want false")
	}
	if !caps.RequiresAppleVirtualization {
		t.Fatal("RequiresAppleVirtualization = false, want true")
	}
	if caps.SupportsSnapshots {
		t.Fatal("SupportsSnapshots = true, want false until implemented")
	}
	if len(caps.SupportedTransportTypes) != 1 || caps.SupportedTransportTypes[0] != runtimecontract.TransportTypeVsockHTTP {
		t.Fatalf("SupportedTransportTypes = %#v", caps.SupportedTransportTypes)
	}
}

func TestAppleVFMicroVMBackendConfiguresSharedRootFSImageStore(t *testing.T) {
	home := t.TempDir()
	backend := NewAppleVFMicroVMRuntimeBackend(home, map[string]string{
		"state_dir":                filepath.Join(home, "apple-vf-state"),
		"podman_path":              "/usr/local/bin/podman",
		"mke2fs_path":              "/usr/local/sbin/mke2fs",
		"rootfs_size_mib":          "2048",
		"vsock_bridge_binary_path": "/usr/local/bin/agency-vsock-http-bridge",
	})
	if backend.Images.StateDir != filepath.Join(home, "apple-vf-state") {
		t.Fatalf("image store state dir = %q", backend.Images.StateDir)
	}
	if backend.Images.PodmanPath != "/usr/local/bin/podman" {
		t.Fatalf("podman path = %q", backend.Images.PodmanPath)
	}
	if backend.Images.Mke2fsPath != "/usr/local/sbin/mke2fs" {
		t.Fatalf("mke2fs path = %q", backend.Images.Mke2fsPath)
	}
	if backend.Images.SizeMiB != 2048 {
		t.Fatalf("rootfs size = %d", backend.Images.SizeMiB)
	}
	if backend.Images.VsockBridgeBinary != "/usr/local/bin/agency-vsock-http-bridge" {
		t.Fatalf("vsock bridge binary = %q", backend.Images.VsockBridgeBinary)
	}
	if backend.Images.OverlayBaseDir != home {
		t.Fatalf("overlay base dir = %q, want %q", backend.Images.OverlayBaseDir, home)
	}
}

func TestAppleVFMicroVMPrepareRootFSUsesOCIImageStore(t *testing.T) {
	stateDir := t.TempDir()
	commands := &fakeFirecrackerImageCommands{
		outputs: map[string][]byte{
			"podman image inspect --format {{.Digest}} agency-body:latest":                                      []byte("sha256:abc123\n"),
			"podman image inspect --format {{json .Config.Entrypoint}}|{{json .Config.Cmd}} agency-body:latest": []byte("null|[\"/app/entrypoint.sh\"]\n"),
			"podman create agency-body:latest":                                                                  []byte("source-id\n"),
		},
	}
	backend := &AppleVFMicroVMRuntimeBackend{
		StateDir: stateDir,
		Images:   &MicroVMImageStore{StateDir: stateDir, commands: commands},
	}
	rootfs, err := backend.PrepareRootFS(context.Background(), runtimecontract.RuntimeSpec{
		RuntimeID: "alice",
		Package:   runtimecontract.RuntimePackageSpec{Image: "agency-body:latest"},
	})
	if err != nil {
		t.Fatalf("PrepareRootFS returned error: %v", err)
	}
	if rootfs.Path != filepath.Join(stateDir, "tasks", "alice", "rootfs.ext4") {
		t.Fatalf("rootfs path = %q", rootfs.Path)
	}
	data, err := os.ReadFile(rootfs.Path)
	if err != nil {
		t.Fatalf("read rootfs: %v", err)
	}
	if string(data) != "ext4" {
		t.Fatalf("rootfs contents = %q", string(data))
	}
	if !commands.exported["source-id"] {
		t.Fatal("expected image filesystem export")
	}
}

func TestParseAppleVFHelperHealth(t *testing.T) {
	t.Parallel()

	health, err := ParseAppleVFHelperHealth([]byte(`{"arch":"arm64","backend":"apple-vf-microvm","command":"health","darwin":"25.4.0","ok":true,"version":"0.1.0","virtualizationAvailable":true}`))
	if err != nil {
		t.Fatalf("ParseAppleVFHelperHealth() error = %v", err)
	}
	if !health.OK || health.Backend != BackendAppleVFMicroVM || health.Arch != "arm64" || !health.VirtualizationAvailable {
		t.Fatalf("unexpected health: %#v", health)
	}
}

func TestParseAppleVFHelperHealthFailure(t *testing.T) {
	t.Parallel()

	health, err := ParseAppleVFHelperHealth([]byte(`{"arch":"x86_64","backend":"apple-vf-microvm","command":"health","darwin":"25.4.0","ok":false,"version":"0.1.0","virtualizationAvailable":false,"error":"Apple Virtualization.framework does not report VM support on this host"}`))
	if err != nil {
		t.Fatalf("ParseAppleVFHelperHealth() error = %v", err)
	}
	if health.OK || !strings.Contains(health.Error, "Virtualization.framework") {
		t.Fatalf("unexpected health failure: %#v", health)
	}
}
