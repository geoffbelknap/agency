package api

import (
	"log/slog"
	"path/filepath"
	"testing"

	authzcore "github.com/geoffbelknap/agency/internal/authz"
	"github.com/geoffbelknap/agency/internal/config"
	hostruntimebackend "github.com/geoffbelknap/agency/internal/hostadapter/runtimebackend"
	"github.com/geoffbelknap/agency/internal/hub"
)

// TestStartup_NilDocker_ReturnsError verifies that passing a nil backend client
// causes Startup to return an error rather than a partially-initialized result.
// This exercises the hard-fail semantics for core component initialization.
func TestStartup_NilDocker_ReturnsError(t *testing.T) {
	cfg := &config.Config{
		Home:    t.TempDir(),
		Version: "test",
		Hub: config.HubConfig{
			DeploymentBackend: "docker",
		},
	}
	_, err := Startup(cfg, nil, nil)
	if err == nil {
		t.Fatal("expected error when backend client is nil")
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
	result, err := Startup(cfg, nil, slog.Default())
	if err != nil {
		t.Fatalf("Startup() returned error: %v", err)
	}
	if result.Runtime == nil || result.AgentManager == nil || result.HaltController == nil {
		t.Fatalf("unexpected startup result: %#v", result)
	}
}

func TestStartup_DefaultsMicroagentRuntimeArtifacts(t *testing.T) {
	home := t.TempDir()
	sourceDir := filepath.Join(home, "share", "agency")
	cfg := &config.Config{
		Home:      home,
		Version:   "0.3.15",
		SourceDir: sourceDir,
		Hub: config.HubConfig{
			DeploymentBackend: hostruntimebackend.BackendMicroagent,
		},
	}

	result, err := Startup(cfg, nil, slog.Default())
	if err != nil {
		t.Fatalf("Startup() returned error: %v", err)
	}

	got := result.Runtime.BackendConfig
	if got["enforcer_binary_path"] != filepath.Join(sourceDir, "bin", "agency-enforcer-host") {
		t.Fatalf("enforcer_binary_path = %q", got["enforcer_binary_path"])
	}
	if got["binary_path"] != "microagent" {
		t.Fatalf("binary_path = %q", got["binary_path"])
	}
	if got["rootfs_oci_ref"] != "ghcr.io/geoffbelknap/agency-runtime-body:v0.3.15" {
		t.Fatalf("rootfs_oci_ref = %q", got["rootfs_oci_ref"])
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

func TestStartup_NilDocker_ReturnsErrorForContainerdBackend(t *testing.T) {
	cfg := &config.Config{
		Home:    t.TempDir(),
		Version: "test",
		Hub: config.HubConfig{
			DeploymentBackend: "containerd",
		},
	}
	_, err := Startup(cfg, nil, nil)
	if err == nil {
		t.Fatal("expected error when containerd client is nil")
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
