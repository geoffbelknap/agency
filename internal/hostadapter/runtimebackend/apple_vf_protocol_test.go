package runtimebackend

import (
	"strings"
	"testing"
)

func TestAppleVFHelperRequestJSONArg(t *testing.T) {
	t.Parallel()

	arg, err := (AppleVFHelperRequest{
		RequestID:      "req-1",
		RuntimeID:      "alice",
		Role:           AppleVFRoleWorkload,
		Backend:        BackendAppleVFMicroVM,
		AgencyHomeHash: "home-sha",
		Config: &AppleVFHelperVMConfig{
			KernelPath:      "/kernels/vmlinux",
			RootFSPath:      "/rootfs/alice.ext4",
			StateDir:        "/state/alice",
			MemoryMiB:       512,
			CPUCount:        2,
			EnforcementMode: FirecrackerEnforcementModeHostProcess,
		},
	}).JSONArg()
	if err != nil {
		t.Fatalf("JSONArg() error = %v", err)
	}
	for _, want := range []string{
		`"requestID":"req-1"`,
		`"runtimeID":"alice"`,
		`"role":"workload"`,
		`"backend":"apple-vf-microvm"`,
		`"kernelPath":"/kernels/vmlinux"`,
		`"memoryMiB":512`,
	} {
		if !strings.Contains(arg, want) {
			t.Fatalf("JSONArg() = %s, missing %s", arg, want)
		}
	}
}

func TestParseAppleVFHelperResponseNotImplemented(t *testing.T) {
	t.Parallel()

	resp, err := ParseAppleVFHelperResponse([]byte(`{"agencyHomeHash":"home-sha","arch":"arm64","backend":"apple-vf-microvm","command":"prepare","darwin":"25.4.0","details":{"protocol":"argv-json"},"error":"agency-apple-vf-helper prepare is not implemented","ok":false,"requestID":"req-1","role":"workload","runtimeID":"alice","version":"0.1.0","virtualizationAvailable":true,"vmState":"not_implemented"}`))
	if err != nil {
		t.Fatalf("ParseAppleVFHelperResponse() error = %v", err)
	}
	if resp.OK || resp.Command != AppleVFCommandPrepare || resp.Role != AppleVFRoleWorkload || resp.RuntimeID != "alice" {
		t.Fatalf("unexpected response: %#v", resp)
	}
	if resp.Details["protocol"] != "argv-json" || resp.VMState != "not_implemented" {
		t.Fatalf("unexpected response details: %#v", resp)
	}
}

func TestParseAppleVFHelperPrepareResponse(t *testing.T) {
	t.Parallel()

	resp, err := ParseAppleVFHelperResponse([]byte(`{"agencyHomeHash":"home-sha","arch":"arm64","backend":"apple-vf-microvm","command":"prepare","darwin":"25.4.0","details":{"cpuCount":"2","kernelPath":"/artifacts/Image","memoryMiB":"512","rootfsPath":"/state/rootfs.ext4","stateDir":"/state/vms/alice","validated":"true"},"ok":true,"requestID":"prepare-alice","role":"workload","runtimeID":"alice","version":"0.1.0","virtualizationAvailable":true,"vmState":"prepared"}`))
	if err != nil {
		t.Fatalf("ParseAppleVFHelperResponse() error = %v", err)
	}
	if !resp.OK || resp.Command != AppleVFCommandPrepare || resp.VMState != "prepared" {
		t.Fatalf("unexpected prepare response: %#v", resp)
	}
	if resp.Details["kernelPath"] != "/artifacts/Image" || resp.Details["validated"] != "true" {
		t.Fatalf("unexpected prepare details: %#v", resp.Details)
	}
}

func TestParseAppleVFHelperStartResponse(t *testing.T) {
	t.Parallel()

	resp, err := ParseAppleVFHelperResponse([]byte(`{"agencyHomeHash":"home-sha","arch":"arm64","backend":"apple-vf-microvm","command":"start","darwin":"25.4.0","details":{"pid":"1234","serialLogPath":"/state/vms/alice/serial.log","statePath":"/state/vms/alice/state.json"},"ok":true,"requestID":"start-alice","role":"workload","runtimeID":"alice","version":"0.1.0","virtualizationAvailable":true,"vmState":"starting"}`))
	if err != nil {
		t.Fatalf("ParseAppleVFHelperResponse() error = %v", err)
	}
	if !resp.OK || resp.Command != AppleVFCommandStart || resp.VMState != "starting" {
		t.Fatalf("unexpected start response: %#v", resp)
	}
	if resp.Details["pid"] != "1234" || resp.Details["serialLogPath"] == "" || resp.Details["statePath"] == "" {
		t.Fatalf("unexpected start details: %#v", resp.Details)
	}
}

func TestParseAppleVFHelperEvent(t *testing.T) {
	t.Parallel()

	event, err := ParseAppleVFHelperEvent([]byte(`{"agencyHomeHash":"home-sha","backend":"apple-vf-microvm","detail":"started","requestID":"req-1","role":"workload","runtimeID":"alice","type":"vm_started","version":"0.1.0","vmState":"running"}`))
	if err != nil {
		t.Fatalf("ParseAppleVFHelperEvent() error = %v", err)
	}
	if event.Backend != BackendAppleVFMicroVM || event.Type != "vm_started" || event.Role != AppleVFRoleWorkload || event.VMState != "running" {
		t.Fatalf("unexpected event: %#v", event)
	}
}
