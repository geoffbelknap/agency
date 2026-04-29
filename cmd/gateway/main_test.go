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
	if cfg != nil {
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
