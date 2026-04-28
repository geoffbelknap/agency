package runtimebackend

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	runtimecontract "github.com/geoffbelknap/agency/internal/runtime/contract"
)

func TestFirecrackerRuntimeBackendIntegration(t *testing.T) {
	if os.Getenv("AGENCY_FIRECRACKER_INTEGRATION") != "1" {
		t.Skip("set AGENCY_FIRECRACKER_INTEGRATION=1 to run the Firecracker integration test")
	}
	if _, err := os.Stat("/dev/kvm"); err != nil {
		t.Skipf("/dev/kvm unavailable: %v", err)
	}
	binaryPath := os.Getenv("AGENCY_FIRECRACKER_BIN")
	kernelPath := os.Getenv("AGENCY_FIRECRACKER_KERNEL")
	imageRef := os.Getenv("AGENCY_FIRECRACKER_TEST_IMAGE")
	if binaryPath == "" || kernelPath == "" || imageRef == "" {
		t.Skip("AGENCY_FIRECRACKER_BIN, AGENCY_FIRECRACKER_KERNEL, and AGENCY_FIRECRACKER_TEST_IMAGE are required")
	}
	backend := NewFirecrackerRuntimeBackend(t.TempDir(), map[string]string{
		"binary_path": binaryPath,
		"kernel_path": kernelPath,
		"state_dir":   filepath.Join(t.TempDir(), "state"),
	})
	spec := runtimecontract.RuntimeSpec{
		RuntimeID: "firecracker-integration",
		Backend:   BackendFirecracker,
		Package:   runtimecontract.RuntimePackageSpec{Image: imageRef},
		Transport: runtimecontract.RuntimeTransportSpec{
			Enforcer: runtimecontract.EnforcerTransportSpec{
				Type:     runtimecontract.TransportTypeVsockHTTP,
				Endpoint: "127.0.0.1:9999",
			},
		},
		Lifecycle: runtimecontract.RuntimeLifecycleSpec{RestartPolicy: "never"},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err := backend.Ensure(ctx, spec); err != nil {
		t.Fatalf("Ensure returned error: %v", err)
	}
	t.Cleanup(func() {
		_ = backend.Stop(context.Background(), spec.RuntimeID)
	})
	if err := backend.Validate(ctx, spec.RuntimeID); err != nil {
		t.Fatalf("Validate returned error: %v", err)
	}
}
