package runtimebackend

import (
	"context"
	"strings"
	"testing"

	runtimecontract "github.com/geoffbelknap/agency/internal/runtime/contract"
)

func TestFirecrackerRuntimeBackendSkeleton(t *testing.T) {
	backend := &FirecrackerRuntimeBackend{}
	if backend.Name() != BackendFirecracker {
		t.Fatalf("Name() = %q, want %q", backend.Name(), BackendFirecracker)
	}
	if err := backend.Ensure(context.Background(), runtimecontract.RuntimeSpec{}); err == nil || !strings.Contains(err.Error(), "Ensure not implemented") {
		t.Fatalf("Ensure() error = %v", err)
	}
	if err := backend.Stop(context.Background(), "alice"); err == nil || !strings.Contains(err.Error(), "Stop not implemented") {
		t.Fatalf("Stop() error = %v", err)
	}
	if _, err := backend.Inspect(context.Background(), "alice"); err == nil || !strings.Contains(err.Error(), "Inspect not implemented") {
		t.Fatalf("Inspect() error = %v", err)
	}
	if err := backend.Validate(context.Background(), "alice"); err == nil || !strings.Contains(err.Error(), "Validate not implemented") {
		t.Fatalf("Validate() error = %v", err)
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
