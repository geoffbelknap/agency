package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func setupTestEnforcer(t *testing.T) *Enforcer {
	t.Helper()

	dir := t.TempDir()

	// Write routing config
	routingFile := filepath.Join(dir, "routing.yaml")
	os.WriteFile(routingFile, []byte(`
version: "0.1"
providers:
  provider-a:
    api_base: http://localhost:1/v1/
models:
  standard:
    provider: provider-a
    provider_model: provider-a-standard
settings:
  default_timeout: 300
`), 0644)

	// Write API keys
	apiKeysFile := filepath.Join(dir, "api_keys.yaml")
	os.WriteFile(apiKeysFile, []byte(`- key: "test-key-integration"
  name: "workspace"
`), 0644)

	// Write egress domains
	domainsFile := filepath.Join(dir, "egress-domains.yaml")
	os.WriteFile(domainsFile, []byte("mode: denylist\ndomains: []\n"), 0644)

	// Create empty services/agent/keys
	servicesDir := filepath.Join(dir, "services")
	os.MkdirAll(servicesDir, 0755)
	agentDir := filepath.Join(dir, "agent")
	os.MkdirAll(agentDir, 0755)

	auditDir := filepath.Join(dir, "audit")
	os.MkdirAll(auditDir, 0755)

	t.Setenv("ROUTING_CONFIG", routingFile)
	t.Setenv("API_KEYS_FILE", apiKeysFile)
	t.Setenv("EGRESS_DOMAINS_FILE", domainsFile)
	t.Setenv("SERVICES_DIR", servicesDir)
	t.Setenv("AGENT_DIR", agentDir)
	t.Setenv("ENFORCER_LOG_DIR", auditDir)
	t.Setenv("AGENT_NAME", "test-agent")

	e := NewEnforcer()
	t.Cleanup(func() { e.audit.Close() })
	return e
}

func TestIntegrationHealth(t *testing.T) {
	e := setupTestEnforcer(t)
	handler := e.ConnectHandler()

	req := httptest.NewRequest("GET", "/health", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}

	var body map[string]string
	json.Unmarshal(rr.Body.Bytes(), &body)
	if body["status"] != "ok" {
		t.Errorf("expected ok status, got: %v", body)
	}
}

func TestListenAddr(t *testing.T) {
	for _, tt := range []struct {
		host string
		port string
		want string
	}{
		{"", "3128", ":3128"},
		{"127.0.0.1", "3128", "127.0.0.1:3128"},
		{" 127.0.0.1 ", "8081", "127.0.0.1:8081"},
	} {
		if got := listenAddr(tt.host, tt.port); got != tt.want {
			t.Fatalf("listenAddr(%q, %q) = %q, want %q", tt.host, tt.port, got, tt.want)
		}
	}
}

func TestIntegrationHealthNoAuth(t *testing.T) {
	e := setupTestEnforcer(t)
	handler := e.ConnectHandler()

	// Health should work without any auth
	req := httptest.NewRequest("GET", "/health", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("health should bypass auth, got %d", rr.Code)
	}
}

func TestIntegrationUnauthorized(t *testing.T) {
	e := setupTestEnforcer(t)
	handler := e.ConnectHandler()

	req := httptest.NewRequest("GET", "/v1/models", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rr.Code)
	}
}

func TestIntegrationAuthWithAPIKey(t *testing.T) {
	e := setupTestEnforcer(t)
	handler := e.ConnectHandler()

	// LLM endpoint with valid API key but missing model in body
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"messages":[]}`))
	req.Header.Set("Authorization", "Bearer test-key-integration")
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	// Should get 400 (missing model) not 401 (unauthorized)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400 (bad request), got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestIntegrationLLMRouting(t *testing.T) {
	// Create a fake LLM provider
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req map[string]interface{}
		json.Unmarshal(body, &req)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, `{"id":"msg_test","model":"%s","usage":{"input_tokens":10,"output_tokens":20}}`, req["model"])
	}))
	defer provider.Close()

	dir := t.TempDir()
	routingFile := filepath.Join(dir, "routing.yaml")
	os.WriteFile(routingFile, []byte(fmt.Sprintf(`
version: "0.1"
providers:
  provider-a:
    api_base: %s/v1/
models:
  standard:
    provider: provider-a
    provider_model: provider-a-standard
`, provider.URL)), 0644)

	apiKeysFile := filepath.Join(dir, "api_keys.yaml")
	os.WriteFile(apiKeysFile, []byte(`- key: "int-key"\n  name: "ws"\n`), 0644)
	domainsFile := filepath.Join(dir, "egress-domains.yaml")
	os.WriteFile(domainsFile, []byte("mode: denylist\ndomains: []\n"), 0644)
	servicesDir := filepath.Join(dir, "services")
	os.MkdirAll(servicesDir, 0755)
	agentDir := filepath.Join(dir, "agent")
	os.MkdirAll(agentDir, 0755)
	auditDir := filepath.Join(dir, "audit")
	os.MkdirAll(auditDir, 0755)

	t.Setenv("ROUTING_CONFIG", routingFile)
	t.Setenv("API_KEYS_FILE", apiKeysFile)
	t.Setenv("EGRESS_DOMAINS_FILE", domainsFile)
	t.Setenv("SERVICES_DIR", servicesDir)
	t.Setenv("AGENT_DIR", agentDir)
	t.Setenv("ENFORCER_LOG_DIR", auditDir)
	t.Setenv("AGENT_NAME", "test-agent")
	t.Setenv("EGRESS_PROXY", provider.URL)

	e := NewEnforcer()
	defer e.audit.Close()
	handler := e.ConnectHandler()

	body := `{"model":"standard","messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer agency-scoped-test")
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp map[string]interface{}
	json.Unmarshal(rr.Body.Bytes(), &resp)
	if resp["model"] != "provider-a-standard" {
		t.Errorf("expected rewritten model in response, got: %v", resp["model"])
	}
}

func TestIntegrationReload(t *testing.T) {
	dir := t.TempDir()

	routingFile := filepath.Join(dir, "routing.yaml")
	os.WriteFile(routingFile, []byte(`
version: "0.1"
providers: {}
models: {}
`), 0644)

	apiKeysFile := filepath.Join(dir, "api_keys.yaml")
	os.WriteFile(apiKeysFile, []byte(`- key: "old-key"
  name: "ws"
`), 0644)
	domainsFile := filepath.Join(dir, "egress-domains.yaml")
	os.WriteFile(domainsFile, []byte("mode: denylist\ndomains: []\n"), 0644)
	servicesDir := filepath.Join(dir, "services")
	os.MkdirAll(servicesDir, 0755)
	agentDir := filepath.Join(dir, "agent")
	os.MkdirAll(agentDir, 0755)
	auditDir := filepath.Join(dir, "audit")
	os.MkdirAll(auditDir, 0755)

	t.Setenv("ROUTING_CONFIG", routingFile)
	t.Setenv("API_KEYS_FILE", apiKeysFile)
	t.Setenv("EGRESS_DOMAINS_FILE", domainsFile)
	t.Setenv("SERVICES_DIR", servicesDir)
	t.Setenv("AGENT_DIR", agentDir)
	t.Setenv("ENFORCER_LOG_DIR", auditDir)
	t.Setenv("AGENT_NAME", "test-agent")

	e := NewEnforcer()
	defer e.audit.Close()

	// Old key should work
	handler := e.ConnectHandler()
	req := httptest.NewRequest("GET", "/v1/test", nil)
	req.Header.Set("Authorization", "Bearer old-key")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	// Should pass auth (may get other error, but not 401)
	if rr.Code == http.StatusUnauthorized {
		t.Error("old-key should be valid before reload")
	}

	// Update API keys file
	os.WriteFile(apiKeysFile, []byte(`- key: "new-key"
  name: "ws"
`), 0644)

	// Trigger reload
	e.Reload()

	// Old key should fail
	req = httptest.NewRequest("GET", "/v1/test", nil)
	req.Header.Set("Authorization", "Bearer old-key")
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("old-key should fail after reload, got %d", rr.Code)
	}

	// New key should work
	req = httptest.NewRequest("GET", "/v1/test", nil)
	req.Header.Set("Authorization", "Bearer new-key")
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code == http.StatusUnauthorized {
		t.Error("new-key should work after reload")
	}
}
