package main

import (
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
