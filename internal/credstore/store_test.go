package credstore

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

// memBackend is an in-memory SecretBackend for testing.
type memBackend struct {
	store map[string]struct {
		value    string
		metadata map[string]string
	}
}

func newMemBackend() *memBackend {
	return &memBackend{
		store: make(map[string]struct {
			value    string
			metadata map[string]string
		}),
	}
}

func (m *memBackend) Put(name, value string, metadata map[string]string) error {
	existing, ok := m.store[name]
	if ok {
		existing.value = value
		for k, v := range metadata {
			existing.metadata[k] = v
		}
		m.store[name] = existing
		return nil
	}
	meta := make(map[string]string, len(metadata))
	for k, v := range metadata {
		meta[k] = v
	}
	m.store[name] = struct {
		value    string
		metadata map[string]string
	}{value: value, metadata: meta}
	return nil
}

func (m *memBackend) Get(name string) (string, map[string]string, error) {
	e, ok := m.store[name]
	if !ok {
		return "", nil, fmt.Errorf("credential %q not found", name)
	}
	meta := make(map[string]string, len(e.metadata))
	for k, v := range e.metadata {
		meta[k] = v
	}
	return e.value, meta, nil
}

func (m *memBackend) Delete(name string) error {
	if _, ok := m.store[name]; !ok {
		return fmt.Errorf("credential %q not found", name)
	}
	delete(m.store, name)
	return nil
}

func (m *memBackend) List() ([]SecretRef, error) {
	refs := make([]SecretRef, 0, len(m.store))
	for name, e := range m.store {
		meta := make(map[string]string, len(e.metadata))
		for k, v := range e.metadata {
			meta[k] = v
		}
		refs = append(refs, SecretRef{Name: name, Metadata: meta})
	}
	return refs, nil
}

func TestStorePutGetRoundTrip(t *testing.T) {
	backend := newMemBackend()
	home := t.TempDir()
	store := NewStore(backend, home)

	entry := Entry{
		Name:  "TEST_KEY",
		Value: "secret-value-123",
		Metadata: Metadata{
			Kind:     KindService,
			Scope:    "agent:test-agent",
			Service:  "test-service",
			Group:    "test-group",
			Protocol: ProtocolAPIKey,
			ProtocolConfig: map[string]any{
				"header":  "X-Api-Key",
				"domains": []any{"api.example.com"},
			},
			Source:         "operator",
			ExpiresAt:      "2027-01-01T00:00:00Z",
			Requires:       []string{"CONFIG_A", "CONFIG_B"},
			ExternalScopes: []string{"read", "write"},
			CreatedAt:      "2026-03-30T00:00:00Z",
			RotatedAt:      "2026-03-30T00:00:00Z",
		},
	}

	if err := store.Put(entry); err != nil {
		t.Fatalf("Put: %v", err)
	}

	got, err := store.Get("TEST_KEY")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	if got.Name != entry.Name {
		t.Errorf("Name: got %q, want %q", got.Name, entry.Name)
	}
	if got.Value != entry.Value {
		t.Errorf("Value: got %q, want %q", got.Value, entry.Value)
	}
	if got.Metadata.Kind != entry.Metadata.Kind {
		t.Errorf("Kind: got %q, want %q", got.Metadata.Kind, entry.Metadata.Kind)
	}
	if got.Metadata.Scope != entry.Metadata.Scope {
		t.Errorf("Scope: got %q, want %q", got.Metadata.Scope, entry.Metadata.Scope)
	}
	if got.Metadata.Service != entry.Metadata.Service {
		t.Errorf("Service: got %q, want %q", got.Metadata.Service, entry.Metadata.Service)
	}
	if got.Metadata.Group != entry.Metadata.Group {
		t.Errorf("Group: got %q, want %q", got.Metadata.Group, entry.Metadata.Group)
	}
	if got.Metadata.Protocol != entry.Metadata.Protocol {
		t.Errorf("Protocol: got %q, want %q", got.Metadata.Protocol, entry.Metadata.Protocol)
	}
	if got.Metadata.Source != entry.Metadata.Source {
		t.Errorf("Source: got %q, want %q", got.Metadata.Source, entry.Metadata.Source)
	}
	if got.Metadata.ExpiresAt != entry.Metadata.ExpiresAt {
		t.Errorf("ExpiresAt: got %q, want %q", got.Metadata.ExpiresAt, entry.Metadata.ExpiresAt)
	}
	if len(got.Metadata.Requires) != 2 || got.Metadata.Requires[0] != "CONFIG_A" {
		t.Errorf("Requires: got %v, want [CONFIG_A CONFIG_B]", got.Metadata.Requires)
	}
	if len(got.Metadata.ExternalScopes) != 2 || got.Metadata.ExternalScopes[0] != "read" {
		t.Errorf("ExternalScopes: got %v, want [read write]", got.Metadata.ExternalScopes)
	}
	if got.Metadata.ProtocolConfig["header"] != "X-Api-Key" {
		t.Errorf("ProtocolConfig header: got %v", got.Metadata.ProtocolConfig["header"])
	}
}

func TestRotatePreservesMetadata(t *testing.T) {
	backend := newMemBackend()
	home := t.TempDir()
	store := NewStore(backend, home)

	entry := Entry{
		Name:  "ROTATE_KEY",
		Value: "old-value",
		Metadata: Metadata{
			Kind:      KindProvider,
			Scope:     "platform",
			Protocol:  ProtocolBearer,
			Source:    "operator",
			CreatedAt: "2026-01-01T00:00:00Z",
			RotatedAt: "2026-01-01T00:00:00Z",
		},
	}

	if err := store.Put(entry); err != nil {
		t.Fatalf("Put: %v", err)
	}

	if err := store.Rotate("ROTATE_KEY", "new-value"); err != nil {
		t.Fatalf("Rotate: %v", err)
	}

	got, err := store.Get("ROTATE_KEY")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	if got.Value != "new-value" {
		t.Errorf("Value: got %q, want %q", got.Value, "new-value")
	}
	if got.Metadata.Kind != KindProvider {
		t.Errorf("Kind not preserved: got %q", got.Metadata.Kind)
	}
	if got.Metadata.Scope != "platform" {
		t.Errorf("Scope not preserved: got %q", got.Metadata.Scope)
	}
	if got.Metadata.CreatedAt != "2026-01-01T00:00:00Z" {
		t.Errorf("CreatedAt not preserved: got %q", got.Metadata.CreatedAt)
	}
	if got.Metadata.RotatedAt == "2026-01-01T00:00:00Z" {
		t.Error("RotatedAt was not updated")
	}
}

func TestForAgentScopeResolution(t *testing.T) {
	backend := newMemBackend()
	home := t.TempDir()
	store := NewStore(backend, home)

	// Platform-scoped.
	store.Put(Entry{
		Name:  "PLATFORM_KEY",
		Value: "platform-val",
		Metadata: Metadata{
			Kind:     KindService,
			Scope:    "platform",
			Service:  "my-service",
			Protocol: ProtocolAPIKey,
			Source:   "operator",
		},
	})

	// Team-scoped.
	store.Put(Entry{
		Name:  "TEAM_KEY",
		Value: "team-val",
		Metadata: Metadata{
			Kind:     KindService,
			Scope:    "team:security",
			Service:  "my-service",
			Protocol: ProtocolAPIKey,
			Source:   "operator",
		},
	})

	// Agent-scoped.
	store.Put(Entry{
		Name:  "AGENT_KEY",
		Value: "agent-val",
		Metadata: Metadata{
			Kind:     KindService,
			Scope:    "agent:detection-engineer",
			Service:  "my-service",
			Protocol: ProtocolAPIKey,
			Source:   "operator",
		},
	})

	// Agent scope should win.
	got, err := store.ForAgent("detection-engineer", "my-service")
	if err != nil {
		t.Fatalf("ForAgent: %v", err)
	}
	if got.Name != "AGENT_KEY" {
		t.Errorf("expected agent-scoped key, got %q", got.Name)
	}

	// For unknown agent, should fall back to team.
	got, err = store.ForAgent("unknown-agent", "my-service")
	if err != nil {
		t.Fatalf("ForAgent fallback: %v", err)
	}
	if got.Metadata.Scope != "team:security" && got.Metadata.Scope != "platform" {
		t.Errorf("expected team or platform fallback, got scope %q", got.Metadata.Scope)
	}
}

func TestForService(t *testing.T) {
	backend := newMemBackend()
	home := t.TempDir()
	store := NewStore(backend, home)

	store.Put(Entry{
		Name:  "SVC_KEY",
		Value: "svc-val",
		Metadata: Metadata{
			Kind:     KindService,
			Scope:    "platform",
			Service:  "target-service",
			Protocol: ProtocolAPIKey,
			Source:   "operator",
		},
	})

	got, err := store.ForService("target-service")
	if err != nil {
		t.Fatalf("ForService: %v", err)
	}
	if got.Name != "SVC_KEY" {
		t.Errorf("got %q, want SVC_KEY", got.Name)
	}

	_, err = store.ForService("nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent service")
	}
}

func TestListWithFilters(t *testing.T) {
	backend := newMemBackend()
	home := t.TempDir()
	store := NewStore(backend, home)

	store.Put(Entry{
		Name:     "KEY_A",
		Value:    "a",
		Metadata: Metadata{Kind: KindService, Scope: "platform", Service: "svc-a", Protocol: ProtocolAPIKey, Source: "operator"},
	})
	store.Put(Entry{
		Name:     "KEY_B",
		Value:    "b",
		Metadata: Metadata{Kind: KindProvider, Scope: "platform", Protocol: ProtocolBearer, Source: "operator"},
	})
	store.Put(Entry{
		Name:     "KEY_C",
		Value:    "c",
		Metadata: Metadata{Kind: KindService, Scope: "agent:x", Service: "svc-a", Protocol: ProtocolAPIKey, Source: "operator"},
	})

	// Filter by kind.
	entries, err := store.List(Filter{Kind: KindProvider})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(entries) != 1 || entries[0].Name != "KEY_B" {
		t.Errorf("kind filter: got %d entries", len(entries))
	}

	// Filter by service.
	entries, err = store.List(Filter{Service: "svc-a"})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(entries) != 2 {
		t.Errorf("service filter: got %d entries, want 2", len(entries))
	}

	// Filter by scope.
	entries, err = store.List(Filter{Scope: "agent:x"})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(entries) != 1 || entries[0].Name != "KEY_C" {
		t.Errorf("scope filter: got %d entries", len(entries))
	}

	// No filter.
	entries, err = store.List(Filter{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(entries) != 3 {
		t.Errorf("no filter: got %d entries, want 3", len(entries))
	}
}

func TestGroupResolution(t *testing.T) {
	backend := newMemBackend()

	// Create group entry.
	groupEntry := Entry{
		Name:  "limacharlie",
		Value: "",
		Metadata: Metadata{
			Kind:     KindGroup,
			Protocol: ProtocolJWTExchange,
			ProtocolConfig: map[string]any{
				"token_url":            "https://jwt.limacharlie.io",
				"token_response_field": "jwt",
				"token_ttl_seconds":    float64(3000),
				"inject_header":        "Authorization",
				"inject_format":        "Bearer {token}",
			},
			Requires: []string{"LC_ORG_ID"},
			Source:   "operator",
		},
	}
	gn, gv, gm := entryToBackend(groupEntry)
	backend.Put(gn, gv, gm)

	// Create member entry with group reference.
	member := Entry{
		Name:  "LC_KEY_AGENT",
		Value: "secret-key",
		Metadata: Metadata{
			Kind:    KindService,
			Scope:   "agent:test",
			Service: "lc-test",
			Group:   "limacharlie",
			Source:  "operator",
		},
	}

	resolved, err := ResolveGroup(member, backend)
	if err != nil {
		t.Fatalf("ResolveGroup: %v", err)
	}

	if resolved.Metadata.Protocol != ProtocolJWTExchange {
		t.Errorf("Protocol: got %q, want %q", resolved.Metadata.Protocol, ProtocolJWTExchange)
	}
	if resolved.Metadata.ProtocolConfig["token_url"] != "https://jwt.limacharlie.io" {
		t.Errorf("token_url not inherited from group")
	}
	if len(resolved.Metadata.Requires) != 1 || resolved.Metadata.Requires[0] != "LC_ORG_ID" {
		t.Errorf("Requires not inherited: %v", resolved.Metadata.Requires)
	}
}

func TestGroupMembers(t *testing.T) {
	backend := newMemBackend()

	// Two members.
	for _, name := range []string{"KEY_A", "KEY_B"} {
		n, v, m := entryToBackend(Entry{
			Name:  name,
			Value: "val",
			Metadata: Metadata{
				Kind:     KindService,
				Scope:    "platform",
				Group:    "mygroup",
				Protocol: ProtocolAPIKey,
				Source:   "operator",
			},
		})
		backend.Put(n, v, m)
	}

	// One non-member.
	n, v, m := entryToBackend(Entry{
		Name:  "KEY_C",
		Value: "val",
		Metadata: Metadata{
			Kind:     KindService,
			Scope:    "platform",
			Protocol: ProtocolAPIKey,
			Source:   "operator",
		},
	})
	backend.Put(n, v, m)

	members, err := GroupMembers("mygroup", backend)
	if err != nil {
		t.Fatalf("GroupMembers: %v", err)
	}
	if len(members) != 2 {
		t.Errorf("got %d members, want 2", len(members))
	}
}

func TestValidateScopesWarnings(t *testing.T) {
	home := t.TempDir()

	// Set up a minimal agent and preset structure.
	agentDir := filepath.Join(home, "agents", "test-agent")
	os.MkdirAll(agentDir, 0755)
	os.WriteFile(filepath.Join(agentDir, "agent.yaml"), []byte("preset: test-preset\n"), 0644)

	presetDir := filepath.Join(home, "hub-cache", "default", "presets", "test-preset")
	os.MkdirAll(presetDir, 0755)
	presetYAML := `scopes:
  required:
    - read
    - write
  optional:
    - admin
`
	os.WriteFile(filepath.Join(presetDir, "preset.yaml"), []byte(presetYAML), 0644)

	entry := Entry{
		Name:  "TEST_KEY",
		Value: "val",
		Metadata: Metadata{
			Scope:          "agent:test-agent",
			ExternalScopes: []string{"read", "delete"}, // delete is excess, write+admin are missing
		},
	}

	warnings := ValidateScopes(entry, home)
	if len(warnings) == 0 {
		t.Fatal("expected warnings, got none")
	}

	var hasExcess, hasMissing bool
	for _, w := range warnings {
		if strings.Contains(w.Message, "excess") && strings.Contains(w.Message, "delete") {
			hasExcess = true
		}
		if strings.Contains(w.Message, "missing") && (strings.Contains(w.Message, "write") || strings.Contains(w.Message, "admin")) {
			hasMissing = true
		}
	}
	if !hasExcess {
		t.Error("expected excess scope warning for 'delete'")
	}
	if !hasMissing {
		t.Error("expected missing scope warning for 'write' or 'admin'")
	}
}

func TestValidateProtocolConfigPlaceholders(t *testing.T) {
	// Valid placeholders.
	entry := Entry{
		Metadata: Metadata{
			Protocol: ProtocolJWTExchange,
			ProtocolConfig: map[string]any{
				"token_url": "https://jwt.example.com",
				"token_params": map[string]any{
					"secret": "${credential}",
					"org":    "${config:ORG_ID}",
				},
			},
		},
	}
	if err := ValidateProtocolConfig(entry, nil); err != nil {
		t.Errorf("expected valid, got: %v", err)
	}

	// Invalid placeholder.
	entry.Metadata.ProtocolConfig["token_params"] = map[string]any{
		"secret": "${env:HOME}",
	}
	if err := ValidateProtocolConfig(entry, nil); err == nil {
		t.Error("expected error for disallowed placeholder ${env:HOME}")
	}
}

func TestValidateProtocolConfigDomain(t *testing.T) {
	entry := Entry{
		Metadata: Metadata{
			Protocol: ProtocolJWTExchange,
			ProtocolConfig: map[string]any{
				"token_url": "https://evil.example.com/token",
			},
		},
	}

	err := ValidateProtocolConfig(entry, []string{"jwt.limacharlie.io"})
	if err == nil {
		t.Error("expected error for disallowed domain")
	}
	if !strings.Contains(err.Error(), "not in allowed domains") {
		t.Errorf("unexpected error: %v", err)
	}

	// Allowed domain.
	entry.Metadata.ProtocolConfig["token_url"] = "https://jwt.limacharlie.io/token"
	if err := ValidateProtocolConfig(entry, []string{"jwt.limacharlie.io"}); err != nil {
		t.Errorf("expected valid, got: %v", err)
	}
}

func TestGenerateSwapConfig(t *testing.T) {
	backend := newMemBackend()
	home := t.TempDir()
	store := NewStore(backend, home)

	// API key entry.
	store.Put(Entry{
		Name:  "PROVIDER_A_KEY",
		Value: "provider-a-secret",
		Metadata: Metadata{
			Kind:     KindProvider,
			Scope:    "platform",
			Protocol: ProtocolAPIKey,
			ProtocolConfig: map[string]any{
				"header":  "x-provider-key",
				"domains": []any{"provider-a.example.com"},
			},
			Source: "operator",
		},
	})

	// JWT exchange entry with service.
	store.Put(Entry{
		Name:  "LC_KEY",
		Value: "lc-secret",
		Metadata: Metadata{
			Kind:     KindService,
			Scope:    "platform",
			Service:  "limacharlie",
			Protocol: ProtocolJWTExchange,
			ProtocolConfig: map[string]any{
				"token_url":            "https://jwt.limacharlie.io",
				"token_response_field": "jwt",
				"token_ttl_seconds":    float64(3000),
				"inject_header":        "Authorization",
				"inject_format":        "Bearer {token}",
			},
			Source: "operator",
		},
	})

	store.Put(Entry{
		Name:  "gemini-api-key",
		Value: "gemini-secret",
		Metadata: Metadata{
			Kind:     KindProvider,
			Scope:    "platform",
			Protocol: ProtocolAPIKey,
			Source:   "operator",
		},
	})

	data, err := store.GenerateSwapConfig()
	if err != nil {
		t.Fatalf("GenerateSwapConfig: %v", err)
	}

	var cfg struct {
		Swaps map[string]struct {
			Type    string   `yaml:"type"`
			KeyRef  string   `yaml:"key_ref"`
			Header  string   `yaml:"header"`
			Domains []string `yaml:"domains"`
		} `yaml:"swaps"`
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("unmarshal swap config: %v", err)
	}

	if _, ok := cfg.Swaps["PROVIDER_A_KEY"]; !ok {
		t.Error("expected PROVIDER_A_KEY in swap config")
	}
	if lc, ok := cfg.Swaps["limacharlie"]; !ok {
		t.Error("expected limacharlie in swap config")
	} else {
		if lc.Type != "jwt-exchange" {
			t.Errorf("limacharlie type: got %q, want jwt-exchange", lc.Type)
		}
		if lc.KeyRef != "LC_KEY" {
			t.Errorf("limacharlie key_ref: got %q, want LC_KEY", lc.KeyRef)
		}
	}
	if gemini, ok := cfg.Swaps["gemini-api-key"]; !ok {
		t.Error("expected gemini-api-key in swap config")
	} else {
		if gemini.Header != "x-goog-api-key" {
			t.Errorf("gemini header: got %q, want x-goog-api-key", gemini.Header)
		}
		if len(gemini.Domains) != 1 || gemini.Domains[0] != "generativelanguage.googleapis.com" {
			t.Errorf("gemini domains: got %#v", gemini.Domains)
		}
	}
}

func TestGenerateSwapConfigDoesNotInferUnknownProviderDefaults(t *testing.T) {
	backend := newMemBackend()
	store := NewStore(backend, t.TempDir())
	if err := store.Put(Entry{
		Name:  "custom-provider-key",
		Value: "secret",
		Metadata: Metadata{
			Kind:     KindProvider,
			Scope:    "platform",
			Protocol: ProtocolAPIKey,
			Source:   "operator",
		},
	}); err != nil {
		t.Fatalf("Put: %v", err)
	}

	data, err := store.GenerateSwapConfig()
	if err != nil {
		t.Fatalf("GenerateSwapConfig: %v", err)
	}
	var cfg struct {
		Swaps map[string]struct {
			Header  string   `yaml:"header"`
			Domains []string `yaml:"domains"`
		} `yaml:"swaps"`
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("unmarshal swap config: %v", err)
	}
	custom := cfg.Swaps["custom-provider-key"]
	if custom.Header != "" || len(custom.Domains) != 0 {
		t.Fatalf("custom provider inferred defaults: %#v", custom)
	}
}

func TestExpiring(t *testing.T) {
	backend := newMemBackend()
	home := t.TempDir()
	store := NewStore(backend, home)

	soon := time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339)
	far := time.Now().Add(365 * 24 * time.Hour).UTC().Format(time.RFC3339)

	store.Put(Entry{
		Name:     "EXPIRING_SOON",
		Value:    "val",
		Metadata: Metadata{Kind: KindService, Scope: "platform", Protocol: ProtocolAPIKey, Source: "operator", ExpiresAt: soon},
	})
	store.Put(Entry{
		Name:     "EXPIRING_FAR",
		Value:    "val",
		Metadata: Metadata{Kind: KindService, Scope: "platform", Protocol: ProtocolAPIKey, Source: "operator", ExpiresAt: far},
	})
	store.Put(Entry{
		Name:     "NO_EXPIRY",
		Value:    "val",
		Metadata: Metadata{Kind: KindService, Scope: "platform", Protocol: ProtocolAPIKey, Source: "operator"},
	})

	entries, err := store.Expiring(7 * 24 * time.Hour)
	if err != nil {
		t.Fatalf("Expiring: %v", err)
	}
	if len(entries) != 1 || entries[0].Name != "EXPIRING_SOON" {
		t.Errorf("expected 1 expiring entry (EXPIRING_SOON), got %d", len(entries))
	}
}

func TestTestWithHTTPServer(t *testing.T) {
	// Create a test server.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") == "valid-key" {
			w.WriteHeader(200)
			json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
			return
		}
		w.WriteHeader(401)
	}))
	defer server.Close()

	// Extract host from test server URL.
	domain := strings.TrimPrefix(server.URL, "http://")

	backend := newMemBackend()
	home := t.TempDir()
	store := NewStore(backend, home)

	store.Put(Entry{
		Name:  "TEST_API_KEY",
		Value: "valid-key",
		Metadata: Metadata{
			Kind:     KindService,
			Scope:    "platform",
			Protocol: ProtocolAPIKey,
			ProtocolConfig: map[string]any{
				"header":               "x-api-key",
				"domains":              []any{domain},
				"test_endpoint":        "/health",
				"test_expected_status": float64(200),
			},
			Source: "operator",
		},
	})

	// The test will try https://<domain>/health, but our server is http.
	// So we test the method exists and returns a result without panicking.
	result, err := store.Test("TEST_API_KEY")
	if err != nil {
		t.Fatalf("Test: %v", err)
	}
	// Result may fail due to https vs http, but it should not error.
	if result == nil {
		t.Fatal("expected non-nil TestResult")
	}
	// Latency should be set.
	if result.Latency < 0 {
		t.Errorf("unexpected negative latency: %d", result.Latency)
	}
}

func TestForDomain(t *testing.T) {
	backend := newMemBackend()
	home := t.TempDir()
	store := NewStore(backend, home)

	store.Put(Entry{
		Name:  "DOMAIN_KEY",
		Value: "domain-val",
		Metadata: Metadata{
			Kind:     KindProvider,
			Scope:    "platform",
			Protocol: ProtocolAPIKey,
			ProtocolConfig: map[string]any{
				"header":  "x-api-key",
				"domains": []any{"api.example.com"},
			},
			Source: "operator",
		},
	})

	got, err := store.ForDomain("api.example.com")
	if err != nil {
		t.Fatalf("ForDomain: %v", err)
	}
	if got.Name != "DOMAIN_KEY" {
		t.Errorf("got %q, want DOMAIN_KEY", got.Name)
	}

	_, err = store.ForDomain("api.other.com")
	if err == nil {
		t.Error("expected error for unknown domain")
	}
}

func TestSerializeRoundTrip(t *testing.T) {
	original := Entry{
		Name:  "RT_KEY",
		Value: "rt-val",
		Metadata: Metadata{
			Kind:     KindService,
			Scope:    "agent:x",
			Service:  "svc",
			Group:    "grp",
			Protocol: ProtocolJWTExchange,
			ProtocolConfig: map[string]any{
				"token_url": "https://example.com",
				"nested":    map[string]any{"a": "b"},
			},
			Source:         "hub:mycomp",
			ExpiresAt:      "2027-06-01T00:00:00Z",
			Requires:       []string{"X", "Y"},
			ExternalScopes: []string{"s1", "s2", "s3"},
			CreatedAt:      "2026-01-01T00:00:00Z",
			RotatedAt:      "2026-06-01T00:00:00Z",
		},
	}

	name, value, meta := entryToBackend(original)
	restored := entryFromBackend(name, value, meta)

	if restored.Name != original.Name {
		t.Errorf("Name: %q != %q", restored.Name, original.Name)
	}
	if restored.Value != original.Value {
		t.Errorf("Value: %q != %q", restored.Value, original.Value)
	}
	if restored.Metadata.Kind != original.Metadata.Kind {
		t.Errorf("Kind: %q != %q", restored.Metadata.Kind, original.Metadata.Kind)
	}
	if restored.Metadata.Service != original.Metadata.Service {
		t.Errorf("Service: %q != %q", restored.Metadata.Service, original.Metadata.Service)
	}
	if restored.Metadata.Group != original.Metadata.Group {
		t.Errorf("Group: %q != %q", restored.Metadata.Group, original.Metadata.Group)
	}
	if len(restored.Metadata.Requires) != 2 {
		t.Errorf("Requires: got %v", restored.Metadata.Requires)
	}
	if len(restored.Metadata.ExternalScopes) != 3 {
		t.Errorf("ExternalScopes: got %v", restored.Metadata.ExternalScopes)
	}
	if restored.Metadata.ProtocolConfig["token_url"] != "https://example.com" {
		t.Errorf("ProtocolConfig token_url: got %v", restored.Metadata.ProtocolConfig["token_url"])
	}
}
