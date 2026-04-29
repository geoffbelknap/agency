package runtimebackend

import (
	"context"
	"path/filepath"
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
