package main

import (
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/geoffbelknap/agency/internal/config"
	hostruntimebackend "github.com/geoffbelknap/agency/internal/hostadapter/runtimebackend"
)

func TestSelectRuntimeBackendDefaultsToStrategicBackend(t *testing.T) {
	t.Setenv("AGENCY_HOME", t.TempDir())
	t.Setenv("AGENCY_RUNTIME_BACKEND", "")
	t.Setenv("AGENCY_CONTAINER_BACKEND", "")
	backend, cfg, err := selectRuntimeBackend("", false)
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
	} else if cfg != nil {
		t.Fatalf("cfg = %#v, want nil", cfg)
	}
}

func TestSelectRuntimeBackendRequiresExperimentalForTransitionalBackend(t *testing.T) {
	t.Setenv("AGENCY_RUNTIME_BACKEND", "")
	t.Setenv("AGENCY_CONTAINER_BACKEND", "")
	_, _, err := selectRuntimeBackend("podman", false)
	if err == nil || !strings.Contains(err.Error(), "transitional") {
		t.Fatalf("selectRuntimeBackend(podman) error = %v", err)
	}
}

func TestSelectRuntimeBackendRequiresExperimentalForNonDefaultStrategicBackend(t *testing.T) {
	t.Setenv("AGENCY_RUNTIME_BACKEND", "")
	t.Setenv("AGENCY_CONTAINER_BACKEND", "")
	backend := hostruntimebackend.BackendAppleVFMicroVM
	if runtime.GOOS == "darwin" {
		backend = hostruntimebackend.BackendFirecracker
	}
	_, _, err := selectRuntimeBackend(backend, false)
	if err == nil || !strings.Contains(err.Error(), "not the default") {
		t.Fatalf("selectRuntimeBackend(%s) error = %v", backend, err)
	}
}

func TestValidateConfiguredBackendRejectsGenericContainerdHostKey(t *testing.T) {
	err := validateConfiguredBackend(&config.Config{
		Hub: config.HubConfig{
			DeploymentBackend: "containerd",
			DeploymentBackendConfig: map[string]string{
				"host": "/run/containerd/containerd.sock",
			},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "native containerd socket") {
		t.Fatalf("validateConfiguredBackend() error = %v", err)
	}
}

func TestWithAppleContainerHelperConfig(t *testing.T) {
	t.Setenv("AGENCY_APPLE_CONTAINER_HELPER_BIN", "/tmp/agency-apple-container-helper")
	t.Setenv("AGENCY_APPLE_CONTAINER_WAIT_HELPER_BIN", "/tmp/agency-apple-container-wait-helper")
	got := withAppleContainerHelperConfig("apple-container", map[string]string{"binary": "/opt/homebrew/bin/container"})
	want := map[string]string{
		"binary":             "/opt/homebrew/bin/container",
		"helper_binary":      "/tmp/agency-apple-container-helper",
		"wait_helper_binary": "/tmp/agency-apple-container-wait-helper",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("helper config = %#v, want %#v", got, want)
	}
	if got := withAppleContainerHelperConfig("docker", nil); got != nil {
		t.Fatalf("docker config = %#v, want nil", got)
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

func TestValidateConfiguredBackendAcceptsNativeContainerdSocketKey(t *testing.T) {
	if err := validateConfiguredBackend(&config.Config{
		Hub: config.HubConfig{
			DeploymentBackend: "containerd",
			DeploymentBackendConfig: map[string]string{
				"native_socket": "/run/user/1000/containerd/containerd.sock",
			},
		},
	}); err != nil {
		t.Fatalf("validateConfiguredBackend() error = %v", err)
	}
}
