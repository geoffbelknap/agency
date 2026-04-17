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
