package api

import (
	"testing"

	authzcore "github.com/geoffbelknap/agency/internal/authz"
	"github.com/geoffbelknap/agency/internal/config"
	"github.com/geoffbelknap/agency/internal/hub"
)

// TestStartup_NilDocker_ReturnsError verifies that passing a nil Docker client
// causes Startup to return an error rather than a partially-initialized result.
// This exercises the hard-fail semantics for core component initialization.
func TestStartup_NilDocker_ReturnsError(t *testing.T) {
	cfg := &config.Config{Home: t.TempDir(), Version: "test"}
	_, err := Startup(cfg, nil, nil)
	if err == nil {
		t.Fatal("expected error when docker client is nil")
	}
}

func TestStartup_NilDocker_AllowedForNonDockerBackend(t *testing.T) {
	cfg := &config.Config{
		Home:    t.TempDir(),
		Version: "test",
		Hub: config.HubConfig{
			DeploymentBackend: "probe",
		},
	}
	result, err := Startup(cfg, nil, nil)
	if err != nil {
		t.Fatalf("Startup() returned error: %v", err)
	}
	if result.Runtime == nil || result.AgentManager == nil || result.HaltController == nil {
		t.Fatalf("unexpected startup result: %#v", result)
	}
}

func TestStartup_NilDocker_ReturnsErrorForPodmanBackend(t *testing.T) {
	cfg := &config.Config{
		Home:    t.TempDir(),
		Version: "test",
		Hub: config.HubConfig{
			DeploymentBackend: "podman",
		},
	}
	_, err := Startup(cfg, nil, nil)
	if err == nil {
		t.Fatal("expected error when podman client is nil")
	}
}

func TestInitV2Dependencies_RegistersV2Stores(t *testing.T) {
	cfg := &config.Config{Home: t.TempDir(), Version: "test"}
	hubMgr := hub.NewManager(cfg.Home)

	deps := initV2Dependencies(cfg, hubMgr)
	if deps.HubRegistry == nil {
		t.Fatal("HubRegistry is nil")
	}
	if deps.InstanceStore == nil {
		t.Fatal("InstanceStore is nil")
	}
	if deps.AuthzResolver == nil {
		t.Fatal("AuthzResolver is nil")
	}

	if _, ok := interface{}(deps.AuthzResolver).(*authzcore.Resolver); !ok {
		t.Fatal("AuthzResolver has wrong type")
	}
	if deps.HubRegistry != hubMgr.Registry {
		t.Fatal("HubRegistry does not reuse startup hub manager registry")
	}
}
