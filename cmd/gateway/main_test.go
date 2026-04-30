package main

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/geoffbelknap/agency/internal/backendhealth"
	"github.com/geoffbelknap/agency/internal/config"
	hostruntimebackend "github.com/geoffbelknap/agency/internal/hostadapter/runtimebackend"
	"github.com/geoffbelknap/agency/internal/hostadapter/runtimehost"
)

func TestSelectRuntimeBackendDefaultsToStrategicBackend(t *testing.T) {
	t.Setenv("AGENCY_HOME", t.TempDir())
	t.Setenv("AGENCY_RUNTIME_BACKEND", "")
	backend, cfg, err := selectRuntimeBackend("")
	if err != nil {
		t.Fatalf("selectRuntimeBackend() error = %v", err)
	}
	want := hostruntimebackend.BackendFirecracker
	if runtime.GOOS == "darwin" {
		want = hostruntimebackend.BackendAppleVFMicroVM
	}
	if backend != want {
		t.Fatalf("backend = %q, want %q", backend, want)
	}
	if want == hostruntimebackend.BackendAppleVFMicroVM {
		if cfg["kernel_path"] == "" {
			t.Fatalf("cfg = %#v, want default apple-vf kernel path", cfg)
		}
	} else if cfg["binary_path"] == "" || cfg["kernel_path"] == "" {
		t.Fatalf("cfg = %#v, want default firecracker artifact paths", cfg)
	}
}

func TestSelectRuntimeBackendRejectsContainerBackend(t *testing.T) {
	t.Setenv("AGENCY_RUNTIME_BACKEND", "")
	_, _, err := selectRuntimeBackend("podman")
	if err == nil || !strings.Contains(err.Error(), "not supported") {
		t.Fatalf("selectRuntimeBackend(podman) error = %v", err)
	}
}

func TestSelectRuntimeBackendAllowsExplicitMicroVMBackendsWithoutExperimentalFlag(t *testing.T) {
	t.Setenv("AGENCY_RUNTIME_BACKEND", "")
	for _, backend := range []string{hostruntimebackend.BackendFirecracker, hostruntimebackend.BackendAppleVFMicroVM} {
		got, _, err := selectRuntimeBackend(backend)
		if err != nil {
			t.Fatalf("selectRuntimeBackend(%s) error = %v", backend, err)
		}
		if got != backend {
			t.Fatalf("selectRuntimeBackend(%s) = %q, want %q", backend, got, backend)
		}
	}
}

func TestValidateConfiguredBackendRejectsContainerBackend(t *testing.T) {
	err := validateConfiguredBackend(&config.Config{
		Hub: config.HubConfig{
			DeploymentBackend: "containerd",
			DeploymentBackendConfig: map[string]string{
				"host": "/run/containerd/containerd.sock",
			},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "no longer supported") {
		t.Fatalf("validateConfiguredBackend() error = %v", err)
	}
}

func TestWithAppleVFArtifactConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AGENCY_HOME", home)
	t.Setenv("AGENCY_APPLE_VF_KERNEL", "")
	t.Setenv("AGENCY_APPLE_VF_HELPER_BIN", "")
	t.Setenv("AGENCY_APPLE_VF_ENFORCER_BIN", "")
	t.Setenv("AGENCY_APPLE_VF_VSOCK_BRIDGE_BIN", "")
	t.Setenv("AGENCY_MKE2FS", "")
	got := withAppleVFArtifactConfig(hostruntimebackend.BackendAppleVFMicroVM, nil)
	want := hostruntimebackend.DefaultAppleVFKernelPath(home)
	if got["kernel_path"] != want {
		t.Fatalf("kernel path = %q, want %q", got["kernel_path"], want)
	}

	got = withAppleVFArtifactConfig(hostruntimebackend.BackendAppleVFMicroVM, map[string]string{"kernel_path": "/custom/Image"})
	if got["kernel_path"] != "/custom/Image" {
		t.Fatalf("kernel path override = %q", got["kernel_path"])
	}

	t.Setenv("AGENCY_APPLE_VF_KERNEL", "/env/Image")
	got = withAppleVFArtifactConfig(hostruntimebackend.BackendAppleVFMicroVM, nil)
	if got["kernel_path"] != "/env/Image" {
		t.Fatalf("kernel path env = %q", got["kernel_path"])
	}
	t.Setenv("AGENCY_APPLE_VF_HELPER_BIN", "/env/helper")
	t.Setenv("AGENCY_APPLE_VF_ENFORCER_BIN", "/env/enforcer")
	t.Setenv("AGENCY_APPLE_VF_VSOCK_BRIDGE_BIN", "/env/bridge")
	t.Setenv("AGENCY_MKE2FS", "/env/mke2fs")
	got = withAppleVFArtifactConfig(hostruntimebackend.BackendAppleVFMicroVM, nil)
	for key, want := range map[string]string{
		"helper_binary":            "/env/helper",
		"enforcer_binary_path":     "/env/enforcer",
		"vsock_bridge_binary_path": "/env/bridge",
		"mke2fs_path":              "/env/mke2fs",
	} {
		if got[key] != want {
			t.Fatalf("%s = %q, want %q", key, got[key], want)
		}
	}
	got = withAppleVFArtifactConfig(hostruntimebackend.BackendAppleVFMicroVM, map[string]string{
		"helper_binary": "/custom/helper",
		"mke2fs_path":   "/custom/mke2fs",
	})
	if got["helper_binary"] != "/custom/helper" || got["mke2fs_path"] != "/custom/mke2fs" {
		t.Fatalf("configured Apple VF paths were not preserved: %#v", got)
	}
	if got := withAppleVFArtifactConfig(hostruntimebackend.BackendFirecracker, nil); got != nil {
		t.Fatalf("firecracker cfg = %#v, want nil", got)
	}
}

func TestWithFirecrackerArtifactConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AGENCY_HOME", home)
	t.Setenv("AGENCY_FIRECRACKER_BIN", "")
	t.Setenv("AGENCY_FIRECRACKER_KERNEL", "")
	t.Setenv("AGENCY_FIRECRACKER_ENFORCER_BIN", "")
	t.Setenv("AGENCY_FIRECRACKER_VSOCK_BRIDGE_BIN", "")
	t.Setenv("AGENCY_MKE2FS", "")

	got := withFirecrackerArtifactConfig(hostruntimebackend.BackendFirecracker, nil)
	if got["binary_path"] == "" || !strings.Contains(got["binary_path"], "firecracker-v1.12.1-") {
		t.Fatalf("binary path = %q, want pinned firecracker artifact path", got["binary_path"])
	}
	if got["kernel_path"] != filepath.Join(home, "runtime", "firecracker", "artifacts", "vmlinux") {
		t.Fatalf("kernel path = %q", got["kernel_path"])
	}
	if got["mke2fs_path"] == "" {
		t.Fatalf("mke2fs path was not defaulted: %#v", got)
	}

	got = withFirecrackerArtifactConfig(hostruntimebackend.BackendFirecracker, map[string]string{"binary_path": "/custom/firecracker"})
	if got["binary_path"] != "/custom/firecracker" {
		t.Fatalf("binary path override = %q", got["binary_path"])
	}

	t.Setenv("AGENCY_FIRECRACKER_BIN", "/env/firecracker")
	t.Setenv("AGENCY_FIRECRACKER_KERNEL", "/env/vmlinux")
	t.Setenv("AGENCY_FIRECRACKER_ENFORCER_BIN", "/env/enforcer")
	t.Setenv("AGENCY_FIRECRACKER_VSOCK_BRIDGE_BIN", "/env/bridge")
	t.Setenv("AGENCY_MKE2FS", "/env/mke2fs")
	got = withFirecrackerArtifactConfig(hostruntimebackend.BackendFirecracker, nil)
	for key, want := range map[string]string{
		"binary_path":              "/env/firecracker",
		"kernel_path":              "/env/vmlinux",
		"enforcer_binary_path":     "/env/enforcer",
		"vsock_bridge_binary_path": "/env/bridge",
		"mke2fs_path":              "/env/mke2fs",
	} {
		if got[key] != want {
			t.Fatalf("%s = %q, want %q", key, got[key], want)
		}
	}
	if got := withFirecrackerArtifactConfig(hostruntimebackend.BackendAppleVFMicroVM, nil); got != nil {
		t.Fatalf("apple-vf cfg = %#v, want nil", got)
	}
}

func TestRouteBackendHealthDropsTypedNilStatus(t *testing.T) {
	var status *runtimehost.Status
	if got := routeBackendHealth(status); got != nil {
		t.Fatalf("routeBackendHealth(nil) = %#v, want nil", got)
	}
	var recorder backendhealth.Recorder = runtimehost.NewStatus(nil)
	if recorder == nil {
		t.Fatal("test setup: typed recorder unexpectedly nil")
	}
	if got := routeBackendHealth(runtimehost.NewStatus(nil)); got == nil {
		t.Fatal("routeBackendHealth(non-nil) = nil")
	}
}

func TestValidateConfiguredBackendAcceptsMicroVMBackend(t *testing.T) {
	if err := validateConfiguredBackend(&config.Config{
		Hub: config.HubConfig{
			DeploymentBackend: hostruntimebackend.BackendFirecracker,
		},
	}); err != nil {
		t.Fatalf("validateConfiguredBackend() error = %v", err)
	}
}
