package runtimehost

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func shortTempSocketPath(t *testing.T, name string) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "agency-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return filepath.Join(dir, name)
}

func TestResolveBackendHostUsesConfiguredSocket(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "podman.sock")
	host := resolveBackendHost(BackendPodman, map[string]string{"socket": socket})
	want := "unix://" + socket
	if host != want {
		t.Fatalf("host = %q, want %q", host, want)
	}
}

func TestRawClientEndpointTracksResolvedHost(t *testing.T) {
	socket := shortTempSocketPath(t, "podman.sock")
	client, err := NewRawClientForBackend(BackendPodman, map[string]string{"host": socket})
	if err != nil {
		t.Fatal(err)
	}
	want := "unix://" + socket
	if got := client.Endpoint(); got != want {
		t.Fatalf("Endpoint() = %q, want %q", got, want)
	}
}

func TestResolveBackendHostUsesContainerdNativeSocketConfig(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "containerd.sock")
	got := resolveBackendHost(BackendContainerd, map[string]string{"native_socket": socket})
	want := "unix://" + socket
	if got != want {
		t.Fatalf("resolveBackendHost(containerd native_socket) = %q, want %q", got, want)
	}
}

func TestResolveBackendHostUsesContainerdAddressConfig(t *testing.T) {
	got := resolveBackendHost(BackendContainerd, map[string]string{"address": "unix:///tmp/containerd.sock"})
	if got != "unix:///tmp/containerd.sock" {
		t.Fatalf("resolveBackendHost(containerd address) = %q", got)
	}
}

func TestResolveBackendHostUsesContainerdEnv(t *testing.T) {
	t.Setenv("CONTAINERD_HOST", "unix:///tmp/containerd-compat.sock")
	if got := resolveBackendHost(BackendContainerd, nil); got != "unix:///tmp/containerd-compat.sock" {
		t.Fatalf("resolveBackendHost(containerd) = %q", got)
	}
}

func TestResolveBackendHostIgnoresLegacyContainerdKeys(t *testing.T) {
	t.Setenv("CONTAINERD_HOST", "")
	t.Setenv("CONTAINER_HOST", "")
	socket := filepath.Join(t.TempDir(), "legacy-containerd.sock")
	got := resolveBackendHost(BackendContainerd, map[string]string{"socket": socket, "host": socket})
	want := "unix:///run/containerd/containerd.sock"
	if got != want {
		t.Fatalf("resolveBackendHost(containerd legacy keys) = %q, want %q", got, want)
	}
}

func TestResolveBackendHostUsesNativeContainerdDefault(t *testing.T) {
	t.Setenv("CONTAINERD_HOST", "")
	t.Setenv("CONTAINER_HOST", "")
	if got := resolveBackendHost(BackendContainerd, nil); got != "unix:///run/containerd/containerd.sock" {
		t.Fatalf("resolveBackendHost(containerd default) = %q", got)
	}
}

func TestResolvedBackendModeContainerdRootless(t *testing.T) {
	got := ResolvedBackendMode(BackendContainerd, map[string]string{
		"native_socket": "/run/user/1000/containerd/containerd.sock",
	})
	if got != "rootless" {
		t.Fatalf("ResolvedBackendMode(containerd rootless) = %q", got)
	}
}

func TestResolvedBackendModeContainerdRootful(t *testing.T) {
	got := ResolvedBackendMode(BackendContainerd, map[string]string{
		"native_socket": "/run/containerd/containerd.sock",
	})
	if got != "rootful" {
		t.Fatalf("ResolvedBackendMode(containerd rootful) = %q", got)
	}
}

func TestResolvedBackendModePodmanRootless(t *testing.T) {
	got := ResolvedBackendMode(BackendPodman, map[string]string{"socket": "/run/user/1000/podman/podman.sock"})
	if got != "rootless" {
		t.Fatalf("ResolvedBackendMode(podman rootless) = %q", got)
	}
}

func TestResolvedBackendModePodmanRootful(t *testing.T) {
	got := ResolvedBackendMode(BackendPodman, map[string]string{"socket": "/run/podman/podman.sock"})
	if got != "rootful" {
		t.Fatalf("ResolvedBackendMode(podman rootful) = %q", got)
	}
}

func TestResolvedBackendModeDockerIsEmpty(t *testing.T) {
	got := ResolvedBackendMode(BackendDocker, map[string]string{"host": "/var/run/docker.sock"})
	if got != "" {
		t.Fatalf("ResolvedBackendMode(docker) = %q, want empty", got)
	}
}

func TestResolvedBackendModeAppleContainer(t *testing.T) {
	got := ResolvedBackendMode(BackendAppleContainer, nil)
	if got != "macos-vm" {
		t.Fatalf("ResolvedBackendMode(apple-container) = %q, want macos-vm", got)
	}
}

func TestValidateBackendConfigRejectsContainerdLegacyHostKey(t *testing.T) {
	err := ValidateBackendConfig(BackendContainerd, map[string]string{
		"host": "/run/containerd/containerd.sock",
	})
	if err == nil || !strings.Contains(err.Error(), "does not accept generic host config") {
		t.Fatalf("ValidateBackendConfig(containerd host) error = %v", err)
	}
}

func TestValidateBackendConfigRejectsContainerdLegacySocketKey(t *testing.T) {
	err := ValidateBackendConfig(BackendContainerd, map[string]string{
		"socket": "/run/containerd/containerd.sock",
	})
	if err == nil || !strings.Contains(err.Error(), "does not accept generic socket config") {
		t.Fatalf("ValidateBackendConfig(containerd socket) error = %v", err)
	}
}

func TestValidateBackendConfigAcceptsNativeContainerdSocketKey(t *testing.T) {
	if err := ValidateBackendConfig(BackendContainerd, map[string]string{
		"native_socket": "/run/user/1000/containerd/containerd.sock",
	}); err != nil {
		t.Fatalf("ValidateBackendConfig(containerd native_socket) error = %v", err)
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
	if got := NormalizeContainerBackend("container"); got != BackendAppleContainer {
		t.Fatalf("NormalizeContainerBackend(\"container\") = %q, want %q", got, BackendAppleContainer)
	}
	if !IsContainerBackend("podman") {
		t.Fatal("expected podman to be a container backend")
	}
	if !IsContainerBackend("containerd") {
		t.Fatal("expected containerd to be a container backend")
	}
	if !IsContainerBackend("apple-container") {
		t.Fatal("expected apple-container to be a container backend")
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

func TestInfraStatusFormattingHelpers(t *testing.T) {
	if got := shortContainerID("1234567890abcdef"); got != "1234567890ab" {
		t.Fatalf("shortContainerID() = %q", got)
	}
	if got := formatContainerUptime(0, "running", "Up 2 hours (healthy)"); got != "2 hours" {
		t.Fatalf("formatContainerUptime(status) = %q", got)
	}
	if got := formatContainerUptime(0, "exited", "Exited 3 minutes ago"); got != "" {
		t.Fatalf("formatContainerUptime(exited) = %q, want empty", got)
	}
	if got := infraHealthFromStatus("Up 2 minutes (unhealthy)"); got != "unhealthy" {
		t.Fatalf("infraHealthFromStatus(unhealthy) = %q", got)
	}
	if got := infraHealthFromStatus("Up 2 minutes (healthy)"); got != "healthy" {
		t.Fatalf("infraHealthFromStatus(healthy) = %q", got)
	}
	if got := infraHealthFromStatus("running"); got != "none" {
		t.Fatalf("infraHealthFromStatus(running) = %q", got)
	}
}

func TestInfraContainerNameUsesInstance(t *testing.T) {
	t.Setenv("AGENCY_INFRA_INSTANCE", " local-test ")
	if got := infraContainerName("comms"); got != "agency-infra-comms-local-test" {
		t.Fatalf("infraContainerName() = %q", got)
	}
	if !validInfraComponent("comms") {
		t.Fatal("comms should be a valid infra component")
	}
	if validInfraComponent("../comms") {
		t.Fatal("path-like component should not be valid")
	}
}
