package hub

import (
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestGenerateSwapConfig_FromServiceDefs(t *testing.T) {
	home := t.TempDir()

	svcDir := filepath.Join(home, "registry", "services")
	os.MkdirAll(svcDir, 0755)
	os.WriteFile(filepath.Join(svcDir, "nextdns-api.yaml"), []byte(`
service: nextdns-api
api_base: https://api.nextdns.io
credential:
  env_var: NEXTDNS_API_KEY
  header: X-Api-Key
  scoped_prefix: agency-scoped-nextdns
`), 0644)

	result, err := GenerateSwapConfig(home)
	if err != nil {
		t.Fatalf("GenerateSwapConfig failed: %v", err)
	}

	var cfg SwapConfigFile
	if err := yaml.Unmarshal(result, &cfg); err != nil {
		t.Fatalf("parse result: %v", err)
	}

	swap, ok := cfg.Swaps["nextdns-api"]
	if !ok {
		t.Fatal("expected nextdns-api swap entry")
	}
	if swap.Type != "api-key" {
		t.Errorf("expected type api-key, got %s", swap.Type)
	}
	if len(swap.Domains) != 1 || swap.Domains[0] != "api.nextdns.io" {
		t.Errorf("expected domain api.nextdns.io, got %v", swap.Domains)
	}
	if swap.Header != "X-Api-Key" {
		t.Errorf("expected header X-Api-Key, got %s", swap.Header)
	}
	if swap.KeyRef != "NEXTDNS_API_KEY" {
		t.Errorf("expected key_ref NEXTDNS_API_KEY, got %s", swap.KeyRef)
	}
}

func TestGenerateSwapConfig_FromRouting(t *testing.T) {
	home := t.TempDir()
	os.MkdirAll(filepath.Join(home, "registry", "services"), 0755)

	infraDir := filepath.Join(home, "infrastructure")
	os.MkdirAll(infraDir, 0755)
	os.WriteFile(filepath.Join(infraDir, "routing.yaml"), []byte(`
version: '0.1'
providers:
  provider-a:
    api_base: https://provider-a.example.com/v1/
    auth_env: PROVIDER_A_API_KEY
    auth_header: x-api-key
    auth_prefix: ""
`), 0644)

	result, err := GenerateSwapConfig(home)
	if err != nil {
		t.Fatalf("GenerateSwapConfig failed: %v", err)
	}

	var cfg SwapConfigFile
	if err := yaml.Unmarshal(result, &cfg); err != nil {
		t.Fatalf("parse result: %v", err)
	}

	swap, ok := cfg.Swaps["provider-a"]
	if !ok {
		t.Fatal("expected provider-a swap entry")
	}
	if swap.Type != "api-key" {
		t.Errorf("expected type api-key, got %s", swap.Type)
	}
	if swap.Header != "x-api-key" {
		t.Errorf("expected header x-api-key, got %s", swap.Header)
	}
	if swap.KeyRef != "PROVIDER_A_API_KEY" {
		t.Errorf("expected key_ref PROVIDER_A_API_KEY, got %s", swap.KeyRef)
	}
}

func TestGenerateSwapConfig_FromJWTSwap(t *testing.T) {
	home := t.TempDir()
	os.MkdirAll(filepath.Join(home, "registry", "services"), 0755)

	secretsDir := filepath.Join(home, "secrets")
	os.MkdirAll(secretsDir, 0755)
	os.WriteFile(filepath.Join(secretsDir, "jwt-swap.yaml"), []byte(`
limacharlie-api:
  token_url: "https://jwt.limacharlie.io"
  token_params:
    oid: "${LC_ORG_ID}"
    secret: "${credential}"
  token_response_field: jwt
  token_ttl_seconds: 3000
  inject_header: Authorization
  inject_format: "Bearer {token}"
  match_domains:
    - api.limacharlie.io
`), 0644)

	result, err := GenerateSwapConfig(home)
	if err != nil {
		t.Fatalf("GenerateSwapConfig failed: %v", err)
	}

	var cfg SwapConfigFile
	if err := yaml.Unmarshal(result, &cfg); err != nil {
		t.Fatalf("parse result: %v", err)
	}

	swap, ok := cfg.Swaps["limacharlie-api"]
	if !ok {
		t.Fatal("expected limacharlie-api swap entry")
	}
	if swap.Type != "jwt-exchange" {
		t.Errorf("expected type jwt-exchange, got %s", swap.Type)
	}

	// Auto-generated body-key-swap for the JWT token URL domain
	bodySwap, ok := cfg.Swaps["limacharlie-api-jwt"]
	if !ok {
		t.Fatal("expected auto-generated limacharlie-api-jwt body-key-swap entry")
	}
	if bodySwap.Type != "body-key-swap" {
		t.Errorf("expected type body-key-swap, got %s", bodySwap.Type)
	}
	if len(bodySwap.Domains) != 1 || bodySwap.Domains[0] != "jwt.limacharlie.io" {
		t.Errorf("expected domain jwt.limacharlie.io, got %v", bodySwap.Domains)
	}
	if bodySwap.KeyRef != "limacharlie-api" {
		t.Errorf("expected key_ref limacharlie-api, got %s", bodySwap.KeyRef)
	}
	if bodySwap.BodyField != "secret" {
		t.Errorf("expected body_field secret, got %s", bodySwap.BodyField)
	}
}

func TestGenerateSwapConfig_WritesFile(t *testing.T) {
	home := t.TempDir()
	os.MkdirAll(filepath.Join(home, "registry", "services"), 0755)
	os.MkdirAll(filepath.Join(home, "infrastructure"), 0755)

	if err := WriteSwapConfig(home); err != nil {
		t.Fatalf("WriteSwapConfig failed: %v", err)
	}

	path := filepath.Join(home, "infrastructure", "credential-swaps.yaml")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("swap config file not created: %v", err)
	}
}
