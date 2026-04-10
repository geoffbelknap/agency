package infra

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/geoffbelknap/agency/internal/config"
	"github.com/geoffbelknap/agency/internal/credstore"
)

func TestCredentialNameCandidates(t *testing.T) {
	got := credentialNameCandidates("GEMINI_API_KEY")
	want := []string{"GEMINI_API_KEY", "gemini-api-key"}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d: %#v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("candidate[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestRoutingConfigConfiguredRequiresUsableCredential(t *testing.T) {
	tmp := t.TempDir()
	writeRoutingConfig(t, tmp)
	h := &handler{deps: Deps{
		Config:    &config.Config{Home: tmp},
		CredStore: newTestCredStore(t, tmp),
	}}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/infra/routing/config", nil)
	rec := httptest.NewRecorder()
	h.routingConfig(rec, req)

	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var body map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if configured, _ := body["configured"].(bool); configured {
		t.Fatal("routing should not be configured without provider credential")
	}

	if err := h.deps.CredStore.Put(credstore.Entry{
		Name:  "gemini-api-key",
		Value: "test-key",
		Metadata: credstore.Metadata{
			Kind:     credstore.KindProvider,
			Scope:    "platform",
			Protocol: credstore.ProtocolAPIKey,
		},
	}); err != nil {
		t.Fatalf("put credential: %v", err)
	}

	rec = httptest.NewRecorder()
	h.routingConfig(rec, req)
	if rec.Code != 200 {
		t.Fatalf("expected 200 after credential, got %d", rec.Code)
	}
	body = map[string]interface{}{}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode response after credential: %v", err)
	}
	if configured, _ := body["configured"].(bool); !configured {
		t.Fatal("routing should be configured when provider credential exists under canonical name")
	}
}

func TestListProvidersRecognizesEnvVarCredential(t *testing.T) {
	tmp := t.TempDir()
	writeHubProvider(t, tmp)
	store := newTestCredStore(t, tmp)
	if err := store.Put(credstore.Entry{
		Name:  "GEMINI_API_KEY",
		Value: "test-key",
		Metadata: credstore.Metadata{
			Kind:     credstore.KindProvider,
			Scope:    "platform",
			Protocol: credstore.ProtocolAPIKey,
		},
	}); err != nil {
		t.Fatalf("put credential: %v", err)
	}
	h := &handler{deps: Deps{
		Config:    &config.Config{Home: tmp},
		CredStore: store,
	}}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/infra/providers", nil)
	rec := httptest.NewRecorder()
	h.listProviders(rec, req)

	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var body []map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(body) != 1 {
		t.Fatalf("providers = %d, want 1: %#v", len(body), body)
	}
	if configured, _ := body[0]["credential_configured"].(bool); !configured {
		t.Fatalf("credential_configured = false, want true: %#v", body[0])
	}
}

func newTestCredStore(t *testing.T, home string) *credstore.Store {
	t.Helper()
	backend, err := credstore.NewFileBackend(
		filepath.Join(home, "credentials", "store.enc"),
		filepath.Join(home, "credentials", "key"),
	)
	if err != nil {
		t.Fatalf("create credential backend: %v", err)
	}
	return credstore.NewStore(backend, home)
}

func writeRoutingConfig(t *testing.T, home string) {
	t.Helper()
	dir := filepath.Join(home, "infrastructure")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("mkdir routing dir: %v", err)
	}
	data := []byte(`providers:
  gemini:
    api_base: https://generativelanguage.googleapis.com/v1beta/openai
    auth_env: GEMINI_API_KEY
models:
  gemini-2.5-flash:
    provider: gemini
    provider_model: gemini-2.5-flash
tiers:
  fast:
    - model: gemini-2.5-flash
`)
	if err := os.WriteFile(filepath.Join(dir, "routing.yaml"), data, 0644); err != nil {
		t.Fatalf("write routing: %v", err)
	}
}

func writeHubProvider(t *testing.T, home string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte(`hub:
  sources:
    - name: default
      url: https://github.com/geoffbelknap/agency-hub.git
`), 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	dir := filepath.Join(home, "hub-cache", "default", "providers", "gemini")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("mkdir provider dir: %v", err)
	}
	data := []byte(`name: gemini
display_name: Google Gemini
description: Gemini models
category: cloud
credential:
  name: gemini-api-key
  label: API Key
  env_var: GEMINI_API_KEY
`)
	if err := os.WriteFile(filepath.Join(dir, "provider.yaml"), data, 0644); err != nil {
		t.Fatalf("write provider: %v", err)
	}
}
