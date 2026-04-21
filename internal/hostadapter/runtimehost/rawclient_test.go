package runtimehost

import (
	"bytes"
	"strings"
	"testing"

	dockercontainer "github.com/docker/docker/api/types/container"
	dockernetwork "github.com/docker/docker/api/types/network"
)

func TestNerdctlJSONOutputPrefersStdout(t *testing.T) {
	stdout := []byte(" \n[{\"Id\":\"sha256:1\"}]\n")
	stderr := []byte("ignored")
	got := nerdctlJSONOutput(stdout, stderr)
	if !bytes.Equal(got, []byte("[{\"Id\":\"sha256:1\"}]")) {
		t.Fatalf("nerdctlJSONOutput() = %q", got)
	}
}

func TestNerdctlJSONOutputFallsBackToStderr(t *testing.T) {
	stdout := []byte(" \n ")
	stderr := []byte("\n[{\"Name\":\"agency-net\"}]\n")
	got := nerdctlJSONOutput(stdout, stderr)
	if !bytes.Equal(got, []byte("[{\"Name\":\"agency-net\"}]")) {
		t.Fatalf("nerdctlJSONOutput() = %q", got)
	}
}

func TestSupportsEventStream(t *testing.T) {
	if (&RawClient{backend: BackendDocker}).SupportsEventStream() != true {
		t.Fatal("docker client should support event streams")
	}
	if (&RawClient{backend: BackendContainerd, nerdctl: &nerdctlConfig{}}).SupportsEventStream() != false {
		t.Fatal("containerd nerdctl client should not report event stream support")
	}
	if (&RawClient{backend: BackendAppleContainer, appleContainer: &appleContainerConfig{}}).SupportsEventStream() != false {
		t.Fatal("apple-container client should not report event stream support before event mapping exists")
	}
}

func TestValidateContainerdAddressRejectsDockerCompatibleSocket(t *testing.T) {
	err := validateContainerdAddress("unix:///run/user/1000/containerd-rootless/api.sock")
	if err == nil {
		t.Fatal("expected Docker-compatible api.sock to be rejected")
	}
}

func TestValidateContainerdAddressAcceptsNativeSocket(t *testing.T) {
	if err := validateContainerdAddress("unix:///run/user/1000/containerd/containerd.sock"); err != nil {
		t.Fatalf("expected native socket to pass validation, got %v", err)
	}
}

func TestValidateAppleContainerPlatform(t *testing.T) {
	if err := validateAppleContainerPlatform("darwin", "arm64"); err != nil {
		t.Fatalf("expected darwin/arm64 to pass validation, got %v", err)
	}
	if err := validateAppleContainerPlatform("linux", "arm64"); err == nil {
		t.Fatal("expected non-macOS host to be rejected")
	}
	if err := validateAppleContainerPlatform("darwin", "amd64"); err == nil {
		t.Fatal("expected non-Apple-silicon host to be rejected")
	}
}

func TestValidateAppleContainerConfigRejectsSocketShapedKeys(t *testing.T) {
	for _, key := range []string{"host", "socket", "native_socket", "address", "containerd_address", "namespace", "data_root"} {
		t.Run(key, func(t *testing.T) {
			if err := validateAppleContainerConfig(map[string]string{key: "value"}); err == nil {
				t.Fatalf("expected %s to be rejected", key)
			}
		})
	}
}

func TestValidateAppleContainerConfigAcceptsBinaryOverride(t *testing.T) {
	if err := validateAppleContainerConfig(map[string]string{"binary": "/usr/local/bin/container"}); err != nil {
		t.Fatalf("expected binary override to pass validation, got %v", err)
	}
}

func TestNerdctlCreateArgsIncludeLogConfig(t *testing.T) {
	config := &dockercontainer.Config{Image: "agency-body:latest"}
	hostConfig := &dockercontainer.HostConfig{
		LogConfig: dockercontainer.LogConfig{
			Type:   "json-file",
			Config: map[string]string{"max-size": "10m", "max-file": "3"},
		},
	}
	args, err := nerdctlCreateArgs(config, hostConfig, nil, nil, "test")
	if err != nil {
		t.Fatalf("nerdctlCreateArgs() error = %v", err)
	}
	joined := strings.Join(args, " ")
	for _, want := range []string{"--log-driver json-file", "--log-opt max-file=3", "--log-opt max-size=10m"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("nerdctlCreateArgs() missing %q in %q", want, joined)
		}
	}
}

func TestNormalizeContainerNetworkNamesResolvesSyntheticNames(t *testing.T) {
	networks := map[string]*dockernetwork.EndpointSettings{
		"unknown-eth0":      {NetworkID: "net-1"},
		"agency-egress-int": {},
	}
	got := normalizeContainerNetworkNames(networks, func(id string) string {
		if id == "net-1" {
			return "agency-gateway"
		}
		return ""
	})
	want := []string{"agency-egress-int", "agency-gateway"}
	if len(got) != len(want) {
		t.Fatalf("normalizeContainerNetworkNames() len = %d, want %d (%v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("normalizeContainerNetworkNames()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestContainerNetworkNamesFromLabels(t *testing.T) {
	config := &dockercontainer.Config{
		Labels: map[string]string{
			"nerdctl/networks": "[\"agency-gateway\",\"agency-agent-internal\",\"agency-egress-int\"]",
		},
	}
	got := containerNetworkNamesFromLabels(config)
	want := []string{"agency-agent-internal", "agency-egress-int", "agency-gateway"}
	if len(got) != len(want) {
		t.Fatalf("containerNetworkNamesFromLabels() len = %d, want %d (%v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("containerNetworkNamesFromLabels()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestHasNonSyntheticNetworkName(t *testing.T) {
	if hasNonSyntheticNetworkName([]string{"unknown-eth0", "eth1"}) {
		t.Fatal("expected only synthetic network names to be treated as synthetic")
	}
	if !hasNonSyntheticNetworkName([]string{"unknown-eth0", "agency-gateway"}) {
		t.Fatal("expected a real network name to be detected")
	}
}
