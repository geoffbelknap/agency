package hub

import (
	"bytes"
	"context"
	"encoding/json"
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

const testOpenAIProviderYAML = `
name: openai
routing:
  api_base: https://api.openai.com/v1
  auth_header: Authorization
  auth_prefix: "Bearer "
  models:
    gpt-4o:
      provider_model: gpt-4o
      capabilities: [tools]
  tiers:
    standard: gpt-4o
`

func TestHubRemoveProviderCleansRouting(t *testing.T) {
	home := t.TempDir()
	mgr := hubpkg.NewManager(home)

	if _, err := mgr.Registry.Create("openai", "provider", "default/openai"); err != nil {
		t.Fatalf("create provider instance: %v", err)
	}
	if err := hubpkg.MergeProviderRouting(home, "openai", []byte(testOpenAIProviderYAML)); err != nil {
		t.Fatalf("merge provider routing: %v", err)
	}

	r := chi.NewRouter()
	RegisterRoutes(r, Deps{Config: &config.Config{Home: home}})

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/hub/openai", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
	}
	if mgr.Registry.Resolve("openai") != nil {
		t.Fatal("provider registry instance should have been removed")
	}

	data, err := os.ReadFile(filepath.Join(home, "infrastructure", "routing.yaml"))
	if err != nil {
		t.Fatalf("read routing.yaml: %v", err)
	}
	if strings.Contains(string(data), "openai") || strings.Contains(string(data), "gpt-4o") {
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
	if err := os.MkdirAll(filepath.Join(home, "connectors"), 0755); err != nil {
		t.Fatalf("mkdir connectors: %v", err)
	}
	published := filepath.Join(home, "connectors", inst.Name+".yaml")
	if err := os.WriteFile(published, []byte("kind: connector\nname: fixture-connector\n"), 0644); err != nil {
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
	if err := os.MkdirAll(filepath.Join(home, "connectors"), 0755); err != nil {
		t.Fatalf("mkdir connectors: %v", err)
	}
	published := filepath.Join(home, "connectors", inst.Name+".yaml")
	if err := os.WriteFile(published, []byte("kind: connector\nname: fixture-connector\n"), 0644); err != nil {
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
`), 0644); err != nil {
		t.Fatalf("write pack template: %v", err)
	}
	connDir := mgr.Registry.InstanceDir(conn.Name)
	if err := os.WriteFile(filepath.Join(connDir, "connector.yaml"), []byte("kind: connector\nname: fixture-connector\n"), 0644); err != nil {
		t.Fatalf("write connector template: %v", err)
	}
	svcDir := mgr.Registry.InstanceDir(svc.Name)
	if err := os.WriteFile(filepath.Join(svcDir, "service.yaml"), []byte("kind: service\nname: fixture-service\n"), 0644); err != nil {
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

	if err := os.MkdirAll(filepath.Join(home, "connectors"), 0755); err != nil {
		t.Fatalf("mkdir connectors: %v", err)
	}
	published := filepath.Join(home, "connectors", conn.Name+".yaml")
	if err := os.WriteFile(published, []byte("kind: connector\nname: fixture-connector\n"), 0644); err != nil {
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

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/hub/"+pack.Name, nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
	}
	if mgr.Registry.Resolve(pack.Name) != nil || mgr.Registry.Resolve(conn.Name) != nil || mgr.Registry.Resolve(svc.Name) != nil {
		t.Fatal("expected pack and auto-installed dependencies to be removed")
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

func TestHubConfigureActiveConnectorSignalsIntakeAndWritesResolvedYAML(t *testing.T) {
	home := t.TempDir()
	mgr := hubpkg.NewManager(home)
	inst, err := mgr.Registry.Create("fixture-connector", "connector", "local/fixture-connector")
	if err != nil {
		t.Fatalf("create connector instance: %v", err)
	}
	if err := mgr.Registry.SetState(inst.Name, "active"); err != nil {
		t.Fatalf("activate connector instance: %v", err)
	}
	instDir := mgr.Registry.InstanceDir(inst.Name)
	template := `kind: connector
name: fixture-connector
config:
  - name: target_agent
    required: true
source:
  type: webhook
routes:
  - match:
      kind: test
    target:
      agent: ${target_agent}
`
	if err := os.WriteFile(filepath.Join(instDir, "connector.yaml"), []byte(template), 0644); err != nil {
		t.Fatalf("write connector template: %v", err)
	}

	signal := &recordingSignalSender{}
	r := chi.NewRouter()
	RegisterRoutes(r, Deps{
		Config: &config.Config{Home: home},
		Signal: signal,
		Audit:  logs.NewWriter(home),
		Logger: slog.Default(),
	})

	body, err := json.Marshal(map[string]map[string]string{
		"config": {"target_agent": "fixture-agent"},
	})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	req := httptest.NewRequest(http.MethodPut, "/api/v1/hub/"+inst.Name+"/config", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
	}
	resolvedPath := filepath.Join(instDir, "resolved.yaml")
	resolved, err := os.ReadFile(resolvedPath)
	if err != nil {
		t.Fatalf("read resolved connector yaml: %v", err)
	}
	content := string(resolved)
	if strings.Contains(content, "${target_agent}") {
		t.Fatalf("connector placeholder should be resolved, got:\n%s", content)
	}
	if strings.Contains(content, "\nconfig:") {
		t.Fatalf("runtime connector yaml should not include hub config schema, got:\n%s", content)
	}
	publishedPath := filepath.Join(home, "connectors", inst.Name+".yaml")
	published, err := os.ReadFile(publishedPath)
	if err != nil {
		t.Fatalf("read published connector yaml: %v", err)
	}
	publishedContent := string(published)
	if strings.Contains(publishedContent, "${target_agent}") {
		t.Fatalf("published connector placeholder should be resolved, got:\n%s", publishedContent)
	}
	if strings.Contains(publishedContent, "\nconfig:") {
		t.Fatalf("published connector yaml should not include hub config schema, got:\n%s", publishedContent)
	}
	if len(signal.calls) != 1 {
		t.Fatalf("expected 1 intake signal, got %d", len(signal.calls))
	}
	if signal.calls[0].container != "agency-infra-intake" || signal.calls[0].signal != "SIGHUP" {
		t.Fatalf("unexpected signal call: %+v", signal.calls[0])
	}
}
