package runtimebackend

import "testing"

func TestNormalizeRuntimeBackendMapsLegacyMicroVMsToMicroagent(t *testing.T) {
	for _, name := range []string{"", "auto", BackendFirecracker, BackendAppleVFMicroVM, BackendMicroagent} {
		if got := NormalizeRuntimeBackend(name); got != BackendMicroagent {
			t.Fatalf("NormalizeRuntimeBackend(%q) = %q, want %q", name, got, BackendMicroagent)
		}
	}
}

func TestNormalizeRuntimeBackendPreservesContainerNames(t *testing.T) {
	for _, name := range []string{"docker", "podman", "containerd", "apple-container"} {
		if got := NormalizeRuntimeBackend(name); got != name {
			t.Fatalf("NormalizeRuntimeBackend(%q) = %q, want %q", name, got, name)
		}
	}
}
