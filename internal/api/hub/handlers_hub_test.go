package hub

import (
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
