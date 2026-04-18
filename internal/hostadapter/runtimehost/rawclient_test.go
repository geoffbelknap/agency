package runtimehost

import (
	"bytes"
	"testing"
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
