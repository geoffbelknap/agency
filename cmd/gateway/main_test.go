package main

import (
	"reflect"
	"strings"
	"testing"

	"github.com/geoffbelknap/agency/internal/config"
)

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
