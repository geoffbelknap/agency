package hub

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"gopkg.in/yaml.v3"

	"github.com/geoffbelknap/agency/internal/config"
	hubpkg "github.com/geoffbelknap/agency/internal/hub"
	"github.com/geoffbelknap/agency/internal/logs"
)

type signalCall struct {
	container string
	signal    string
}

type recordingSignalSender struct {
	calls []signalCall
}

func (r *recordingSignalSender) SignalContainer(_ context.Context, containerName, signal string) error {
	r.calls = append(r.calls, signalCall{container: containerName, signal: signal})
	return nil
}

const testProviderYAML = `
name: provider-a
routing:
  api_base: https://provider-a.example.com/v1
  auth_header: Authorization
  auth_prefix: "Bearer "
  models:
    provider-a-standard:
      provider_model: provider-a-model-v1
      capabilities: [tools]
  tiers:
    standard: provider-a-standard
`

func TestHubRemoveProviderCleansRouting(t *testing.T) {
	home := t.TempDir()
	mgr := hubpkg.NewManager(home)

	if _, err := mgr.Registry.Create("provider-a", "provider", "default/provider-a"); err != nil {
		t.Fatalf("create provider instance: %v", err)
	}
	if err := hubpkg.MergeProviderRouting(home, "provider-a", []byte(testProviderYAML)); err != nil {
		t.Fatalf("merge provider routing: %v", err)
	}

	r := chi.NewRouter()
	RegisterRoutes(r, Deps{Config: &config.Config{Home: home}})

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/hub/provider-a", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
	}
	if mgr.Registry.Resolve("provider-a") != nil {
		t.Fatal("provider registry instance should have been removed")
	}

	data, err := os.ReadFile(filepath.Join(home, "infrastructure", "routing.yaml"))
	if err != nil {
		t.Fatalf("read routing.yaml: %v", err)
	}
	if strings.Contains(string(data), "provider-a") || strings.Contains(string(data), "provider-a-standard") {
		t.Fatalf("provider routing remained after remove:\n%s", string(data))
	}

	var cfg map[string]interface{}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("parse routing.yaml: %v", err)
	}
	if providers, _ := cfg["providers"].(map[string]interface{}); len(providers) != 0 {
		t.Fatalf("providers = %+v, want empty", providers)
	}
}

func TestIsLocalOrTLSAllowsLocalHostBehindProxy(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:18200/api/v1/hub/connectors/example/configure", nil)
	req.RemoteAddr = "172.18.0.2:54321"

	if !isLocalOrTLS(req) {
		t.Fatal("local host request should be allowed even when remote addr is a local proxy")
	}
}

func TestIsLocalOrTLSAllowsLocalOriginBehindProxy(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "http://agency-gateway:8200/api/v1/hub/connectors/example/configure", nil)
	req.RemoteAddr = "172.18.0.2:54321"
	req.Host = "agency-gateway:8200"
	req.Header.Set("Origin", "http://localhost:18280")

	if !isLocalOrTLS(req) {
		t.Fatal("local browser origin should be allowed behind the local web proxy")
	}
}

func TestIsLocalOrTLSRejectsExternalPlaintext(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "http://agency.example.com/api/v1/hub/connectors/example/configure", nil)
	req.RemoteAddr = "203.0.113.10:54321"
	req.Host = "agency.example.com"
	req.Header.Set("Origin", "http://evil.example")

	if isLocalOrTLS(req) {
		t.Fatal("external plaintext request should be rejected")
	}
}

func TestHubDeactivateConnectorSignalsIntakeAndRemovesPublishedYAML(t *testing.T) {
	home := t.TempDir()
	mgr := hubpkg.NewManager(home)
	inst, err := mgr.Registry.Create("fixture-connector", "connector", "local/fixture-connector")
	if err != nil {
		t.Fatalf("create connector instance: %v", err)
	}
	if err := mgr.Registry.SetState(inst.Name, "active"); err != nil {
		t.Fatalf("activate connector instance: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(home, "connectors"), 0o755); err != nil {
		t.Fatalf("mkdir connectors: %v", err)
	}
	published := filepath.Join(home, "connectors", inst.Name+".yaml")
	if err := os.WriteFile(published, []byte("kind: connector\nname: fixture-connector\n"), 0o644); err != nil {
		t.Fatalf("write published connector: %v", err)
	}

	signal := &recordingSignalSender{}
	r := chi.NewRouter()
	RegisterRoutes(r, Deps{
		Config: &config.Config{Home: home},
		Signal: signal,
		Audit:  logs.NewWriter(home),
		Logger: slog.Default(),
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/hub/"+inst.Name+"/deactivate", strings.NewReader("{}"))
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
	}
	if _, err := os.Stat(published); !os.IsNotExist(err) {
		t.Fatalf("published connector yaml should be removed, stat err = %v", err)
	}
	if len(signal.calls) != 1 {
		t.Fatalf("expected 1 intake signal, got %d", len(signal.calls))
	}
	if signal.calls[0].container != "agency-infra-intake" || signal.calls[0].signal != "SIGHUP" {
		t.Fatalf("unexpected signal call: %+v", signal.calls[0])
	}
}

func TestHubRemoveConnectorSignalsIntakeAndRemovesPublishedYAML(t *testing.T) {
	home := t.TempDir()
	mgr := hubpkg.NewManager(home)
	inst, err := mgr.Registry.Create("fixture-connector", "connector", "local/fixture-connector")
	if err != nil {
		t.Fatalf("create connector instance: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(home, "connectors"), 0o755); err != nil {
		t.Fatalf("mkdir connectors: %v", err)
	}
	published := filepath.Join(home, "connectors", inst.Name+".yaml")
	if err := os.WriteFile(published, []byte("kind: connector\nname: fixture-connector\n"), 0o644); err != nil {
		t.Fatalf("write published connector: %v", err)
	}

	signal := &recordingSignalSender{}
	r := chi.NewRouter()
	RegisterRoutes(r, Deps{
		Config: &config.Config{Home: home},
		Signal: signal,
		Audit:  logs.NewWriter(home),
		Logger: slog.Default(),
	})

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/hub/"+inst.Name, nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
	}
	if mgr.Registry.Resolve(inst.Name) != nil {
		t.Fatal("connector instance should be removed")
	}
	if _, err := os.Stat(published); !os.IsNotExist(err) {
		t.Fatalf("published connector yaml should be removed, stat err = %v", err)
	}
	if len(signal.calls) != 1 {
		t.Fatalf("expected 1 intake signal, got %d", len(signal.calls))
	}
	if signal.calls[0].container != "agency-infra-intake" || signal.calls[0].signal != "SIGHUP" {
		t.Fatalf("unexpected signal call: %+v", signal.calls[0])
	}
}

func TestHubDeactivateConnector_NonDockerSkipsInfraSignal(t *testing.T) {
	home := t.TempDir()
	mgr := hubpkg.NewManager(home)
	inst, err := mgr.Registry.Create("fixture-connector", "connector", "local/fixture-connector")
	if err != nil {
		t.Fatalf("create connector instance: %v", err)
	}
	if err := mgr.Registry.SetState(inst.Name, "active"); err != nil {
		t.Fatalf("activate connector instance: %v", err)
	}

	signal := &recordingSignalSender{}
	r := chi.NewRouter()
	RegisterRoutes(r, Deps{
		Config: &config.Config{
			Home: home,
			Hub: config.HubConfig{
				DeploymentBackend: "probe",
			},
		},
		Signal: signal,
		Audit:  logs.NewWriter(home),
		Logger: slog.Default(),
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/hub/"+inst.Name+"/deactivate", strings.NewReader("{}"))
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
	}
	if len(signal.calls) != 0 {
		t.Fatalf("expected no infra signal for non-docker backend, got %+v", signal.calls)
	}
}

func TestHubDeployPackNonDockerBackendUnavailable(t *testing.T) {
	home := t.TempDir()
	r := chi.NewRouter()
	RegisterRoutes(r, Deps{
		Config: &config.Config{
			Home: home,
			Hub: config.HubConfig{
				DeploymentBackend: "probe",
			},
		},
		Audit:  logs.NewWriter(home),
		Logger: slog.Default(),
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/hub/deploy", bytes.NewBufferString(`{"pack":{"name":"demo"}}`))
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
	}
}

func TestHubTeardownPackNonDockerBackendUnavailable(t *testing.T) {
	home := t.TempDir()
	r := chi.NewRouter()
	RegisterRoutes(r, Deps{
		Config: &config.Config{
			Home: home,
			Hub: config.HubConfig{
				DeploymentBackend: "probe",
			},
		},
		Audit:  logs.NewWriter(home),
		Logger: slog.Default(),
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/hub/teardown/demo", bytes.NewBufferString(`{}`))
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
	}
}

func TestHubRemovePackCleansAutoInstalledConnectorDependencies(t *testing.T) {
	home := t.TempDir()
	mgr := hubpkg.NewManager(home)

	pack, err := mgr.Registry.Create("fixture-pack", "pack", "local/fixture-pack")
	if err != nil {
		t.Fatalf("create pack instance: %v", err)
	}
	conn, err := mgr.Registry.Create("fixture-connector", "connector", "local/fixture-connector")
	if err != nil {
		t.Fatalf("create connector instance: %v", err)
	}
	svc, err := mgr.Registry.Create("fixture-service", "service", "local/fixture-service")
	if err != nil {
		t.Fatalf("create service instance: %v", err)
	}

	packDir := mgr.Registry.InstanceDir(pack.Name)
	if err := os.WriteFile(filepath.Join(packDir, "pack.yaml"), []byte(`kind: pack
name: fixture-pack
requires:
  connectors:
    - fixture-connector
  services:
    - fixture-service
`), 0o644); err != nil {
		t.Fatalf("write pack template: %v", err)
	}
	connDir := mgr.Registry.InstanceDir(conn.Name)
	if err := os.WriteFile(filepath.Join(connDir, "connector.yaml"), []byte("kind: connector\nname: fixture-connector\n"), 0o644); err != nil {
		t.Fatalf("write connector template: %v", err)
	}
	svcDir := mgr.Registry.InstanceDir(svc.Name)
	if err := os.WriteFile(filepath.Join(svcDir, "service.yaml"), []byte("kind: service\nname: fixture-service\n"), 0o644); err != nil {
		t.Fatalf("write service template: %v", err)
	}

	if err := mgr.Registry.MarkAutoInstalled(conn.Name, true); err != nil {
		t.Fatalf("mark connector auto-installed: %v", err)
	}
	if err := mgr.Registry.MarkAutoInstalled(svc.Name, true); err != nil {
		t.Fatalf("mark service auto-installed: %v", err)
	}
	if err := mgr.Registry.AddRequiredBy(conn.Name, pack.Name); err != nil {
		t.Fatalf("link connector dependency: %v", err)
	}
	if err := mgr.Registry.AddRequiredBy(svc.Name, pack.Name); err != nil {
		t.Fatalf("link service dependency: %v", err)
	}

	signal := &recordingSignalSender{}
	r := chi.NewRouter()
	RegisterRoutes(r, Deps{
		Config: &config.Config{Home: home},
		Signal: signal,
		Audit:  logs.NewWriter(home),
		Logger: slog.Default(),
	})

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/hub/"+pack.Name, nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
	}
	if mgr.Registry.Resolve(pack.Name) != nil {
		t.Fatal("pack instance should be removed")
	}
	if mgr.Registry.Resolve(conn.Name) != nil {
		t.Fatal("auto-installed connector dependency should be removed")
	}
	if mgr.Registry.Resolve(svc.Name) != nil {
		t.Fatal("auto-installed service dependency should be removed")
	}
}

func TestHubRemovePackKeepsSharedDependencies(t *testing.T) {
	home := t.TempDir()
	mgr := hubpkg.NewManager(home)

	packA, _ := mgr.Registry.Create("pack-a", "pack", "local/pack-a")
	packB, _ := mgr.Registry.Create("pack-b", "pack", "local/pack-b")
	conn, _ := mgr.Registry.Create("shared-connector", "connector", "local/shared-connector")
	if err := mgr.Registry.MarkAutoInstalled(conn.Name, true); err != nil {
		t.Fatalf("mark auto-installed: %v", err)
	}
	if err := mgr.Registry.AddRequiredBy(conn.Name, packA.Name); err != nil {
		t.Fatalf("link packA: %v", err)
	}
	if err := mgr.Registry.AddRequiredBy(conn.Name, packB.Name); err != nil {
		t.Fatalf("link packB: %v", err)
	}

	packADir := mgr.Registry.InstanceDir(packA.Name)
	if err := os.WriteFile(filepath.Join(packADir, "pack.yaml"), []byte("kind: pack\nname: pack-a\n"), 0o644); err != nil {
		t.Fatalf("write packA: %v", err)
	}

	r := chi.NewRouter()
	RegisterRoutes(r, Deps{Config: &config.Config{Home: home}})

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/hub/"+packA.Name, nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
	}
	if mgr.Registry.Resolve(conn.Name) == nil {
		t.Fatal("shared dependency should remain installed")
	}
}

func TestHubInstallRejectsDeploymentEnabledPack(t *testing.T) {
	home := t.TempDir()
	writeHubConfig(t, home)
	writeDeploymentEnabledPack(t, home, "community-admin")

	r := chi.NewRouter()
	RegisterRoutes(r, Deps{Config: &config.Config{Home: home}})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/hub/install", bytes.NewBufferString(`{"component":"community-admin","kind":"pack"}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "deployment-enabled") {
		t.Fatalf("body = %s, want deployment guidance", rr.Body.String())
	}
}

func TestHubConfigureRejectsDeploymentManagedInstance(t *testing.T) {
	home := t.TempDir()
	mgr := hubpkg.NewManager(home)
	inst, err := mgr.Registry.Create("slack-interactivity", "connector", "official/slack-interactivity")
	if err != nil {
		t.Fatalf("create instance: %v", err)
	}
	if err := mgr.Registry.SetDeploymentBinding(inst.ID, "dep-123", "connector"); err != nil {
		t.Fatalf("set deployment binding: %v", err)
	}
	instDir := mgr.Registry.InstanceDir(inst.ID)
	if err := os.WriteFile(filepath.Join(instDir, "connector.yaml"), []byte("name: slack-interactivity\n"), 0o644); err != nil {
		t.Fatalf("write template: %v", err)
	}

	r := chi.NewRouter()
	RegisterRoutes(r, Deps{Config: &config.Config{Home: home}})

	req := httptest.NewRequest(http.MethodPut, "/api/v1/hub/"+inst.Name+"/config", bytes.NewBufferString(`{"config":{"foo":"bar"}}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusConflict {
		t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "deployment") {
		t.Fatalf("body = %s, want deployment guidance", rr.Body.String())
	}
}

func TestHubRemoveRejectsDeploymentManagedInstance(t *testing.T) {
	home := t.TempDir()
	mgr := hubpkg.NewManager(home)
	inst, err := mgr.Registry.Create("google-drive-admin", "connector", "official/google-drive-admin")
	if err != nil {
		t.Fatalf("create instance: %v", err)
	}
	if err := mgr.Registry.SetDeploymentBinding(inst.ID, "dep-456", "connector"); err != nil {
		t.Fatalf("set deployment binding: %v", err)
	}

	r := chi.NewRouter()
	RegisterRoutes(r, Deps{Config: &config.Config{Home: home}})

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/hub/"+inst.Name, nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusConflict {
		t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "deployment") {
		t.Fatalf("body = %s, want deployment guidance", rr.Body.String())
	}
}

func writeHubConfig(t *testing.T, home string) {
	t.Helper()
	data := []byte("hub:\n  sources:\n    - name: official\n      type: oci\n      registry: ghcr.io/geoffbelknap/agency-hub\n")
	if err := os.WriteFile(filepath.Join(home, "config.yaml"), data, 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
}

func writeDeploymentEnabledPack(t *testing.T, home, name string) {
	t.Helper()
	packDir := filepath.Join(home, "hub-cache", "official", "packs", name)
	if err := os.MkdirAll(packDir, 0o755); err != nil {
		t.Fatalf("mkdir pack dir: %v", err)
	}
	packYAML := "name: " + name + "\nversion: 1.0.0\ndescription: test pack\n"
	if err := os.WriteFile(filepath.Join(packDir, "pack.yaml"), []byte(packYAML), 0o644); err != nil {
		t.Fatalf("write pack.yaml: %v", err)
	}
	schemaYAML := "schema_version: 1\ndeployment:\n  name: " + name + "\ninstances:\n  pack:\n    component: " + name + "\n"
	if err := os.WriteFile(filepath.Join(packDir, "deployment_schema.yaml"), []byte(schemaYAML), 0o644); err != nil {
		t.Fatalf("write deployment schema: %v", err)
	}
}
