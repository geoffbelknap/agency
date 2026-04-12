package hub

import (
	"bytes"
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
)

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
