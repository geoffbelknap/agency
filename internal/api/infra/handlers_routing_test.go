package infra

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/geoffbelknap/agency/internal/config"
	"github.com/geoffbelknap/agency/internal/credstore"
	"github.com/geoffbelknap/agency/internal/providercatalog"
)

func TestCredentialNameCandidates(t *testing.T) {
	got := credentialNameCandidates("PROVIDER_A_API_KEY")
	want := []string{"PROVIDER_A_API_KEY", "provider-a-api-key"}
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
		Name:  "provider-a-api-key",
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
	if len(body) == 0 {
		t.Fatalf("providers = %d, want at least 1: %#v", len(body), body)
	}
	foundGoogle := false
	for _, provider := range body {
		if name, _ := provider["name"].(string); name != "google" {
			continue
		}
		foundGoogle = true
		if configured, _ := provider["credential_configured"].(bool); !configured {
			t.Fatalf("google credential_configured = false, want true: %#v", provider)
		}
		if selectable, _ := provider["quickstart_selectable"].(bool); !selectable {
			t.Fatalf("google quickstart_selectable = false, want true: %#v", provider)
		}
		if order, _ := provider["quickstart_order"].(float64); order != 1 {
			t.Fatalf("google quickstart_order = %v, want 1: %#v", order, provider)
		}
		if recommended, _ := provider["quickstart_recommended"].(bool); !recommended {
			t.Fatalf("google quickstart_recommended = false, want true: %#v", provider)
		}
	}
	if !foundGoogle {
		t.Fatalf("expected bundled google provider in list: %#v", body)
	}
}

func TestProviderToolsInventoryEndpoint(t *testing.T) {
	h := &handler{deps: Deps{Config: &config.Config{Home: t.TempDir()}}}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/infra/provider-tools", nil)
	rec := httptest.NewRecorder()
	h.providerTools(rec, req)

	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var body map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	caps, ok := body["capabilities"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected capabilities map: %#v", body)
	}
	webSearch, ok := caps["provider-web-search"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected provider-web-search capability: %#v", caps)
	}
	providers, ok := webSearch["providers"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected provider map: %#v", webSearch)
	}
	if _, ok := providers["google"]; !ok {
		t.Fatalf("expected google provider entry: %#v", providers)
	}
}

func TestProviderProbeURLUsesAPIBaseOverride(t *testing.T) {
	doc, _, err := providercatalog.Get("ollama")
	if err != nil {
		t.Fatalf("load provider: %v", err)
	}
	doc.Quickstart = &providercatalog.QuickstartConfig{
		Probe: &providercatalog.QuickstartProbeConfig{
			Method: http.MethodGet,
			URL:    "http://localhost:11434/v1/models",
		},
	}

	got, err := providerProbeURL(doc, doc.Quickstart.Probe, "http://127.0.0.1:11435/v1")
	if err != nil {
		t.Fatalf("providerProbeURL: %v", err)
	}
	if got != "http://127.0.0.1:11435/v1/models" {
		t.Fatalf("probe url = %q, want %q", got, "http://127.0.0.1:11435/v1/models")
	}
}

func TestPerformProviderProbeUsesDeclaredAuthHeader(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("x-api-key"); got != "test-key" {
			t.Fatalf("x-api-key = %q, want test-key", got)
		}
		if r.URL.Path != "/v1/messages" {
			t.Fatalf("path = %q, want /v1/messages", r.URL.Path)
		}
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer upstream.Close()

	doc := providercatalog.ProviderDoc{
		Name: "provider-a",
		Routing: map[string]interface{}{
			"auth_header": "x-api-key",
			"auth_prefix": "",
		},
		Quickstart: &providercatalog.QuickstartConfig{
			Probe: &providercatalog.QuickstartProbeConfig{
				Method:          http.MethodPost,
				URL:             upstream.URL + "/v1/messages",
				SuccessStatuses: []int{http.StatusOK, http.StatusTooManyRequests},
			},
		},
	}

	status, message, err := performProviderProbe(doc, doc.Quickstart.Probe, "test-key", "")
	if err != nil {
		t.Fatalf("performProviderProbe: %v", err)
	}
	if status != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want %d", status, http.StatusTooManyRequests)
	}
	if message == "" {
		t.Fatal("message should not be empty")
	}
}

func TestInstallProviderHonorsAPIBaseOverride(t *testing.T) {
	tmp := t.TempDir()
	h := &handler{deps: Deps{
		Config: &config.Config{Home: tmp},
	}}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/infra/providers/ollama/install", strings.NewReader(`{"api_base":"http://127.0.0.1:11435/v1"}`))
	rec := httptest.NewRecorder()
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("name", "ollama")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	h.installProvider(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	cfg := loadRoutingConfig(tmp)
	if cfg == nil {
		t.Fatal("expected routing config to be written")
	}
	provider, ok := cfg.Providers["ollama"]
	if !ok {
		t.Fatalf("expected ollama provider in routing config: %#v", cfg.Providers)
	}
	if provider.APIBase != "http://127.0.0.1:11435/v1" {
		t.Fatalf("api_base = %q, want %q", provider.APIBase, "http://127.0.0.1:11435/v1")
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
  provider-a:
    api_base: https://provider-a.example.com/v1
    auth_env: PROVIDER_A_API_KEY
models:
  provider-a-fast:
    provider: provider-a
    provider_model: provider-a-model-v1
tiers:
  fast:
    - model: provider-a-fast
`)
	if err := os.WriteFile(filepath.Join(dir, "routing.yaml"), data, 0644); err != nil {
		t.Fatalf("write routing: %v", err)
	}
}
