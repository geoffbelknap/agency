package runtimebackend

import (
	"context"
	"fmt"
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
	if backend.KernelPath != filepath.Join(home, "runtime", "apple-vf-microvm", "artifacts", "Image") {
		t.Fatalf("KernelPath = %q", backend.KernelPath)
	}
	if backend.RootFSBuilder == nil {
		t.Fatal("RootFSBuilder = nil, want ORAS rootfs builder")
	}
	builder, ok := backend.RootFSBuilder.(*MicroVMOCIRootFSBuilder)
	if !ok {
		t.Fatalf("RootFSBuilder = %T, want *MicroVMOCIRootFSBuilder", backend.RootFSBuilder)
	}
	if builder.StateDir != backend.StateDir {
		t.Fatalf("rootfs builder state dir = %q, want %q", builder.StateDir, backend.StateDir)
	}
	if builder.Platform.OS != "linux" || builder.Platform.Architecture != "arm64" {
		t.Fatalf("rootfs builder platform = %#v, want linux/arm64", builder.Platform)
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

func TestAppleVFMicroVMBackendPreservesConfiguredKernelPath(t *testing.T) {
	backend := NewAppleVFMicroVMRuntimeBackend(t.TempDir(), map[string]string{
		"kernel_path": "/custom/Image",
	})
	if backend.KernelPath != "/custom/Image" {
		t.Fatalf("KernelPath = %q, want configured path", backend.KernelPath)
	}
}

func TestAppleVFMicroVMBackendConfiguresORASRootFSBuilder(t *testing.T) {
	home := t.TempDir()
	backend := NewAppleVFMicroVMRuntimeBackend(home, map[string]string{
		"state_dir":                filepath.Join(home, "apple-vf-state"),
		"mke2fs_path":              "/usr/local/sbin/mke2fs",
		"rootfs_size_mib":          "2048",
		"vsock_bridge_binary_path": "/usr/local/bin/agency-vsock-http-bridge",
		"rootfs_oci_ref":           "ghcr.io/example/agency-runtime-body:v1",
	})
	builder, ok := backend.RootFSBuilder.(*MicroVMOCIRootFSBuilder)
	if !ok {
		t.Fatalf("RootFSBuilder = %T, want *MicroVMOCIRootFSBuilder", backend.RootFSBuilder)
	}
	if builder.StateDir != filepath.Join(home, "apple-vf-state") {
		t.Fatalf("rootfs builder state dir = %q", builder.StateDir)
	}
	if builder.Mke2fsPath != "/usr/local/sbin/mke2fs" {
		t.Fatalf("mke2fs path = %q", builder.Mke2fsPath)
	}
	if builder.SizeMiB != 2048 {
		t.Fatalf("rootfs size = %d", builder.SizeMiB)
	}
	if builder.VsockBridgeBinary != "/usr/local/bin/agency-vsock-http-bridge" {
		t.Fatalf("vsock bridge binary = %q", builder.VsockBridgeBinary)
	}
	if builder.OverlayBaseDir != home {
		t.Fatalf("overlay base dir = %q, want %q", builder.OverlayBaseDir, home)
	}
	if backend.BodyImageRef != "ghcr.io/example/agency-runtime-body:v1" {
		t.Fatalf("body image ref = %q", backend.BodyImageRef)
	}
}

func TestAppleVFMicroVMPrepareRootFSUsesOCIBuilder(t *testing.T) {
	stateDir := t.TempDir()
	builder := &fakeAppleVFRootFSBuilder{}
	backend := &AppleVFMicroVMRuntimeBackend{
		StateDir:      stateDir,
		BodyImageRef:  "ghcr.io/example/agency-runtime-body:v1",
		RootFSBuilder: builder,
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
	if builder.imageRef != "ghcr.io/example/agency-runtime-body:v1" {
		t.Fatalf("rootfs builder image ref = %q", builder.imageRef)
	}
}

func TestAppleVFMicroVMPrepareHelperRequest(t *testing.T) {
	home := t.TempDir()
	backend := NewAppleVFMicroVMRuntimeBackend(home, map[string]string{
		"kernel_path":      "/artifacts/Image",
		"memory_mib":       "768",
		"cpu_count":        "4",
		"enforcement_mode": FirecrackerEnforcementModeHostProcess,
	})
	req := backend.PrepareHelperRequest(runtimecontract.RuntimeSpec{
		RuntimeID: "alice",
		Package: runtimecontract.RuntimePackageSpec{Env: map[string]string{
			FirecrackerEnforcerProxyTargetEnv:   "http://127.0.0.1:19128",
			FirecrackerEnforcerControlTargetEnv: "http://127.0.0.1:19081",
		}},
	}, MicroVMRootFS{
		Path: filepath.Join(home, "runtime", "apple-vf-microvm", "tasks", "alice", "rootfs.ext4"),
	})
	if req.RequestID != "prepare-alice" || req.RuntimeID != "alice" || req.Role != AppleVFRoleWorkload || req.Backend != BackendAppleVFMicroVM {
		t.Fatalf("unexpected request identity: %#v", req)
	}
	if req.AgencyHomeHash == "" {
		t.Fatalf("agency home hash is empty: %#v", req)
	}
	if req.Config == nil {
		t.Fatal("Config = nil")
	}
	if req.Config.KernelPath != "/artifacts/Image" {
		t.Fatalf("kernel path = %q", req.Config.KernelPath)
	}
	if req.Config.RootFSPath == "" || !strings.HasSuffix(req.Config.RootFSPath, "rootfs.ext4") {
		t.Fatalf("rootfs path = %q", req.Config.RootFSPath)
	}
	if req.Config.StateDir != filepath.Join(backend.StateDir, "vms", "alice") {
		t.Fatalf("state dir = %q", req.Config.StateDir)
	}
	if req.Config.MemoryMiB != 768 || req.Config.CPUCount != 4 || req.Config.EnforcementMode != FirecrackerEnforcementModeHostProcess {
		t.Fatalf("unexpected config: %#v", req.Config)
	}
	if len(req.Config.VsockListeners) != 2 {
		t.Fatalf("vsock listeners = %#v, want proxy/control listeners", req.Config.VsockListeners)
	}
	if req.Config.VsockListeners[0] != (AppleVFHelperVsockListener{Port: 3128, Target: "127.0.0.1:19128"}) {
		t.Fatalf("proxy listener = %#v", req.Config.VsockListeners[0])
	}
	if req.Config.VsockListeners[1] != (AppleVFHelperVsockListener{Port: 8081, Target: "127.0.0.1:19081"}) {
		t.Fatalf("control listener = %#v", req.Config.VsockListeners[1])
	}
}

func TestAppleVFMicroVMEnsureInspectStopUseHelper(t *testing.T) {
	stateDir := t.TempDir()
	logPath := filepath.Join(stateDir, "helper.log")
	helper := writeAppleVFHelperScript(t, logPath)
	backend := &AppleVFMicroVMRuntimeBackend{
		HelperBinary:  helper,
		KernelPath:    "/artifacts/Image",
		StateDir:      stateDir,
		MemoryMiB:     512,
		CPUCount:      2,
		BodyImageRef:  "ghcr.io/example/agency-runtime-body:v1",
		RootFSBuilder: &fakeAppleVFRootFSBuilder{},
	}
	spec := runtimecontract.RuntimeSpec{
		RuntimeID: "alice",
		Package: runtimecontract.RuntimePackageSpec{
			Image: "agency-body:latest",
			Env: map[string]string{
				FirecrackerEnforcerProxyTargetEnv:   "http://127.0.0.1:19128",
				FirecrackerEnforcerControlTargetEnv: "http://127.0.0.1:19081",
			},
		},
	}
	if err := backend.Ensure(context.Background(), spec); err != nil {
		t.Fatalf("Ensure returned error: %v", err)
	}
	status, err := backend.Inspect(context.Background(), "alice")
	if err != nil {
		t.Fatalf("Inspect returned error: %v", err)
	}
	if !status.Healthy || status.Phase != runtimecontract.RuntimePhaseRunning {
		t.Fatalf("status = %#v, want healthy running", status)
	}
	if err := backend.Validate(context.Background(), "alice"); err != nil {
		t.Fatalf("Validate returned error: %v", err)
	}
	if err := backend.Stop(context.Background(), "alice"); err != nil {
		t.Fatalf("Stop returned error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(stateDir, "tasks", "alice")); !os.IsNotExist(err) {
		t.Fatalf("task state still exists after stop: %v", err)
	}
	log, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read helper log: %v", err)
	}
	for _, want := range []string{"start --request-json", "inspect --request-json", "stop --request-json", "delete --request-json"} {
		if !strings.Contains(string(log), want) {
			t.Fatalf("helper log = %q, missing %q", string(log), want)
		}
	}
}

func TestAppleVFMicroVMStopCleansTaskStateWhenHelperStateIsMissing(t *testing.T) {
	stateDir := t.TempDir()
	taskDir := filepath.Join(stateDir, "tasks", "alice")
	if err := os.MkdirAll(taskDir, 0o755); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(stateDir, "helper.log")
	helper := writeAppleVFStopMissingHelperScript(t, logPath)
	backend := &AppleVFMicroVMRuntimeBackend{
		HelperBinary: helper,
		StateDir:     stateDir,
	}
	if err := backend.Stop(context.Background(), "alice"); err != nil {
		t.Fatalf("Stop returned error: %v", err)
	}
	if _, err := os.Stat(taskDir); !os.IsNotExist(err) {
		t.Fatalf("task state still exists after stop: %v", err)
	}
	log, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read helper log: %v", err)
	}
	for _, want := range []string{"stop --request-json", "delete --request-json"} {
		if !strings.Contains(string(log), want) {
			t.Fatalf("helper log = %q, missing %q", string(log), want)
		}
	}
}

func TestAppleVFMicroVMEnsureCleansUpAfterStartFailure(t *testing.T) {
	stateDir := t.TempDir()
	logPath := filepath.Join(stateDir, "helper.log")
	helper := writeAppleVFStartFailureHelperScript(t, logPath)
	backend := &AppleVFMicroVMRuntimeBackend{
		HelperBinary:  helper,
		KernelPath:    "/artifacts/Image",
		StateDir:      stateDir,
		MemoryMiB:     512,
		CPUCount:      2,
		BodyImageRef:  "ghcr.io/example/agency-runtime-body:v1",
		RootFSBuilder: &fakeAppleVFRootFSBuilder{},
	}
	spec := runtimecontract.RuntimeSpec{
		RuntimeID: "alice",
		Package: runtimecontract.RuntimePackageSpec{
			Image: "agency-body:latest",
			Env: map[string]string{
				FirecrackerEnforcerProxyTargetEnv:   "http://127.0.0.1:19128",
				FirecrackerEnforcerControlTargetEnv: "http://127.0.0.1:19081",
			},
		},
	}
	err := backend.Ensure(context.Background(), spec)
	if err == nil || !strings.Contains(err.Error(), "VM failed to start") {
		t.Fatalf("Ensure error = %v, want helper start failure", err)
	}
	if _, err := os.Stat(filepath.Join(stateDir, "tasks", "alice")); !os.IsNotExist(err) {
		t.Fatalf("task state still exists after start failure: %v", err)
	}
	log, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read helper log: %v", err)
	}
	for _, want := range []string{"start --request-json", "delete --request-json"} {
		if !strings.Contains(string(log), want) {
			t.Fatalf("helper log = %q, missing %q", string(log), want)
		}
	}
}

func TestAppleVFOCIImageRefMapsLegacyBodyTag(t *testing.T) {
	t.Parallel()

	backend := &AppleVFMicroVMRuntimeBackend{BodyImageRef: "ghcr.io/example/agency-runtime-body:v1"}
	got, err := backend.appleVFOCIImageRef("agency-body:latest")
	if err != nil {
		t.Fatalf("appleVFOCIImageRef returned error: %v", err)
	}
	if got != "ghcr.io/example/agency-runtime-body:v1" {
		t.Fatalf("appleVFOCIImageRef legacy tag = %q", got)
	}
	got, err = backend.appleVFOCIImageRef("ghcr.io/example/custom:tag")
	if err != nil {
		t.Fatalf("appleVFOCIImageRef custom returned error: %v", err)
	}
	if got != "ghcr.io/example/custom:tag" {
		t.Fatalf("appleVFOCIImageRef custom ref = %q", got)
	}
}

func TestAppleVFOCIImageRefFailsClosedWithoutConfiguredArtifact(t *testing.T) {
	t.Parallel()

	backend := &AppleVFMicroVMRuntimeBackend{}
	_, err := backend.appleVFOCIImageRef("agency-body:latest")
	if err == nil || !strings.Contains(err.Error(), "rootfs_oci_ref") {
		t.Fatalf("appleVFOCIImageRef error = %v, want rootfs_oci_ref guidance", err)
	}
}

func TestAppleVFOCIImageRefRejectsLatestTag(t *testing.T) {
	t.Parallel()

	backend := &AppleVFMicroVMRuntimeBackend{BodyImageRef: "ghcr.io/example/agency-runtime-body:latest"}
	_, err := backend.appleVFOCIImageRef("agency-body:latest")
	if err == nil || !strings.Contains(err.Error(), "must not use mutable :latest") {
		t.Fatalf("appleVFOCIImageRef error = %v, want latest rejection", err)
	}
}

type fakeAppleVFRootFSBuilder struct {
	imageRef string
}

func (b *fakeAppleVFRootFSBuilder) Build(_ context.Context, imageRef, outPath string, _ map[string]string) (MicroVMOCIRootFSResult, error) {
	b.imageRef = imageRef
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return MicroVMOCIRootFSResult{}, err
	}
	if err := os.WriteFile(outPath, []byte("ext4"), 0o644); err != nil {
		return MicroVMOCIRootFSResult{}, err
	}
	return MicroVMOCIRootFSResult{
		ImageRef:   imageRef,
		RootFSPath: outPath,
		InitPath:   "/sbin/agency-init",
	}, nil
}

func writeAppleVFHelperScript(t *testing.T, logPath string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "apple-vf-helper")
	script := fmt.Sprintf(`#!/bin/sh
echo "$@" >> %q
case "$1" in
  start)
    echo '{"arch":"arm64","backend":"apple-vf-microvm","command":"start","darwin":"25.4.0","details":{"pid":"1234","stateDir":"/state/alice"},"ok":true,"requestID":"start-alice","role":"workload","runtimeID":"alice","version":"0.1.0","virtualizationAvailable":true,"vmState":"starting"}'
    ;;
  inspect)
    echo '{"arch":"arm64","backend":"apple-vf-microvm","command":"inspect","darwin":"25.4.0","details":{"pid":"1234","stateDir":"/state/alice"},"ok":true,"requestID":"inspect-alice","role":"workload","runtimeID":"alice","version":"0.1.0","virtualizationAvailable":true,"vmState":"running"}'
    ;;
  stop)
    echo '{"arch":"arm64","backend":"apple-vf-microvm","command":"stop","darwin":"25.4.0","details":{"pid":"1234","stateDir":"/state/alice"},"ok":true,"requestID":"stop-alice","role":"workload","runtimeID":"alice","version":"0.1.0","virtualizationAvailable":true,"vmState":"stopped"}'
    ;;
  delete)
    echo '{"arch":"arm64","backend":"apple-vf-microvm","command":"delete","darwin":"25.4.0","details":{"stateDir":"/state/alice"},"ok":true,"requestID":"runtime-alice","role":"workload","runtimeID":"alice","version":"0.1.0","virtualizationAvailable":true,"vmState":"deleted"}'
    ;;
  *)
    echo '{"arch":"arm64","backend":"apple-vf-microvm","command":"health","darwin":"25.4.0","ok":true,"version":"0.1.0","virtualizationAvailable":true}'
    ;;
esac
`, logPath)
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write helper script: %v", err)
	}
	return path
}

func writeAppleVFStopMissingHelperScript(t *testing.T, logPath string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "apple-vf-helper")
	script := fmt.Sprintf(`#!/bin/sh
echo "$@" >> %q
case "$1" in
  stop)
    echo '{"arch":"arm64","backend":"apple-vf-microvm","command":"stop","darwin":"25.4.0","error":"open /state/alice/state.json: no such file or directory","ok":false,"requestID":"runtime-alice","role":"workload","runtimeID":"alice","version":"0.1.0","virtualizationAvailable":true,"vmState":"stop_failed"}'
    exit 1
    ;;
  delete)
    echo '{"arch":"arm64","backend":"apple-vf-microvm","command":"delete","darwin":"25.4.0","details":{"stateDir":"/state/alice"},"ok":true,"requestID":"runtime-alice","role":"workload","runtimeID":"alice","version":"0.1.0","virtualizationAvailable":true,"vmState":"deleted"}'
    ;;
  *)
    echo '{"arch":"arm64","backend":"apple-vf-microvm","command":"health","darwin":"25.4.0","ok":true,"version":"0.1.0","virtualizationAvailable":true}'
    ;;
esac
`, logPath)
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write helper script: %v", err)
	}
	return path
}

func writeAppleVFStartFailureHelperScript(t *testing.T, logPath string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "apple-vf-helper")
	script := fmt.Sprintf(`#!/bin/sh
echo "$@" >> %q
case "$1" in
  start)
    echo '{"arch":"arm64","backend":"apple-vf-microvm","command":"start","darwin":"25.4.0","error":"VM failed to start","ok":false,"requestID":"start-alice","role":"workload","runtimeID":"alice","version":"0.1.0","virtualizationAvailable":true,"vmState":"start_failed"}'
    exit 1
    ;;
  delete)
    echo '{"arch":"arm64","backend":"apple-vf-microvm","command":"delete","darwin":"25.4.0","details":{"stateDir":"/state/alice"},"ok":true,"requestID":"runtime-alice","role":"workload","runtimeID":"alice","version":"0.1.0","virtualizationAvailable":true,"vmState":"deleted"}'
    ;;
  *)
    echo '{"arch":"arm64","backend":"apple-vf-microvm","command":"health","darwin":"25.4.0","ok":true,"version":"0.1.0","virtualizationAvailable":true}'
    ;;
esac
`, logPath)
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write helper script: %v", err)
	}
	return path
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

func TestAppleVFHelperHealthStatusDoesNotInheritAgencyHome(t *testing.T) {
	t.Setenv("AGENCY_HOME", "/tmp/agency-home-that-breaks-helper-health")
	helper := filepath.Join(t.TempDir(), "helper")
	script := `#!/bin/sh
if [ -n "${AGENCY_HOME:-}" ]; then
  echo '{"arch":"arm64","backend":"apple-vf-microvm","command":"health","darwin":"25.4.0","error":"AGENCY_HOME leaked","ok":false,"version":"0.1.0","virtualizationAvailable":false}'
  exit 1
fi
echo '{"arch":"arm64","backend":"apple-vf-microvm","command":"health","darwin":"25.4.0","ok":true,"version":"0.1.0","virtualizationAvailable":true}'
`
	if err := os.WriteFile(helper, []byte(script), 0o755); err != nil {
		t.Fatalf("write helper: %v", err)
	}
	health, err := AppleVFHelperHealthStatus(context.Background(), helper)
	if err != nil {
		t.Fatalf("AppleVFHelperHealthStatus() error = %v", err)
	}
	if !health.OK || !health.VirtualizationAvailable {
		t.Fatalf("unexpected health: %#v", health)
	}
}
