package runtimebackend

import (
	"context"
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
