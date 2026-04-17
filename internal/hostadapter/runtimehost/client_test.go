package runtimehost

import (
	"path/filepath"
	"testing"
)

func TestResolveBackendHostUsesConfiguredSocket(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "podman.sock")
	host := resolveBackendHost(BackendPodman, map[string]string{"socket": socket})
	want := "unix://" + socket
	if host != want {
		t.Fatalf("host = %q, want %q", host, want)
	}
}

func TestResolveBackendHostUsesContainerdEnv(t *testing.T) {
	t.Setenv("CONTAINERD_HOST", "unix:///tmp/containerd-compat.sock")
	if got := resolveBackendHost(BackendContainerd, nil); got != "unix:///tmp/containerd-compat.sock" {
		t.Fatalf("resolveBackendHost(containerd) = %q", got)
	}
}

func TestResolveBackendHostUsesNativeContainerdDefault(t *testing.T) {
	t.Setenv("CONTAINERD_HOST", "")
	t.Setenv("CONTAINER_HOST", "")
	if got := resolveBackendHost(BackendContainerd, nil); got != "unix:///run/containerd/containerd.sock" {
		t.Fatalf("resolveBackendHost(containerd default) = %q", got)
	}
}

func TestNormalizeContainerBackend(t *testing.T) {
	if got := NormalizeContainerBackend(""); got != BackendDocker {
		t.Fatalf("NormalizeContainerBackend(\"\") = %q, want %q", got, BackendDocker)
	}
	if got := NormalizeContainerBackend("PODMAN"); got != BackendPodman {
		t.Fatalf("NormalizeContainerBackend(\"PODMAN\") = %q, want %q", got, BackendPodman)
	}
	if got := NormalizeContainerBackend("CONTAINERD"); got != BackendContainerd {
		t.Fatalf("NormalizeContainerBackend(\"CONTAINERD\") = %q, want %q", got, BackendContainerd)
	}
	if !IsContainerBackend("podman") {
		t.Fatal("expected podman to be a container backend")
	}
	if !IsContainerBackend("containerd") {
		t.Fatal("expected containerd to be a container backend")
	}
	if IsContainerBackend("probe") {
		t.Fatal("probe should not be a container backend")
	}
}

func TestHostGatewayAliasesEnv(t *testing.T) {
	if got := HostGatewayAliasesEnv(BackendDocker); got != "host.docker.internal,host.containers.internal" {
		t.Fatalf("HostGatewayAliasesEnv(docker) = %q", got)
	}
	if got := HostGatewayAliasesEnv(BackendPodman); got != "host.containers.internal,host.docker.internal" {
		t.Fatalf("HostGatewayAliasesEnv(podman) = %q", got)
	}
}
