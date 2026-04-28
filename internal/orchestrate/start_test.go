package orchestrate

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/geoffbelknap/agency/internal/models"
)

func TestResolveModelTierPrefersConfiguredProvider(t *testing.T) {
	home := t.TempDir()
	t.Setenv("PROVIDER_B_API_KEY", "test-key")
	writeFile(t, filepath.Join(home, "config.yaml"), "llm_provider: provider-b\n")
	writeFile(t, filepath.Join(home, "infrastructure", "routing.yaml"), `providers:
  provider-a:
    api_base: https://provider-a.example.com/v1
    auth_env: PROVIDER_A_API_KEY
  provider-b:
    api_base: https://provider-b.example.com/v1
    auth_env: PROVIDER_B_API_KEY
models:
  standard:
    provider: provider-a
    provider_model: provider-a-model-v1
  provider-b-standard:
    provider: provider-b
    provider_model: provider-b-model-v1
tiers:
  standard:
    - model: standard
      preference: 0
    - model: provider-b-standard
      preference: 1
settings:
  default_tier: standard
`)

	ss := &StartSequence{Home: home}
	if got := ss.resolveModelTier("standard"); got != "provider-b-standard" {
		t.Fatalf("resolveModelTier() = %q, want provider-b-standard", got)
	}
}

type staticCommsClient struct {
	responses map[string][]byte
}

func (c staticCommsClient) CommsRequest(_ context.Context, method, path string, _ interface{}) ([]byte, error) {
	key := method + " " + path
	if data, ok := c.responses[key]; ok {
		return data, nil
	}
	return []byte(`{"ok":true}`), nil
}

func TestWaitForCommsWebSocketReturnsWhenConnected(t *testing.T) {
	ss := &StartSequence{
		AgentName: "alpha",
		Comms: staticCommsClient{responses: map[string][]byte{
			"GET /ws/connected/alpha": []byte(`{"agent":"alpha","connected":true}`),
		}},
	}

	if err := ss.waitForCommsWebSocket(context.Background()); err != nil {
		t.Fatalf("waitForCommsWebSocket() returned error: %v", err)
	}
}

func TestWaitForCommsWebSocketTreatsLegacyResponseAsReady(t *testing.T) {
	ss := &StartSequence{
		AgentName: "alpha",
		Comms: staticCommsClient{responses: map[string][]byte{
			"GET /ws/connected/alpha": []byte(`{"ok":true}`),
		}},
	}

	if err := ss.waitForCommsWebSocket(context.Background()); err != nil {
		t.Fatalf("waitForCommsWebSocket() returned error: %v", err)
	}
}

func TestResolveModelTierInfersDefaultsWhenTiersMissing(t *testing.T) {
	home := t.TempDir()
	t.Setenv("PROVIDER_A_API_KEY", "test-key")
	writeFile(t, filepath.Join(home, "infrastructure", "routing.yaml"), `providers:
  provider-a:
    api_base: https://provider-a.example.com/v1
    auth_env: PROVIDER_A_API_KEY
models:
  standard:
    provider: provider-a
    provider_model: provider-a-model-v1
`)

	ss := &StartSequence{Home: home}
	if got := ss.resolveModelTier("standard"); got != "standard" {
		t.Fatalf("resolveModelTier() = %q, want standard", got)
	}
}

func TestDefaultModelTier(t *testing.T) {
	home := t.TempDir()
	writeFile(t, filepath.Join(home, "infrastructure", "routing.yaml"), `settings:
  default_tier: fast
`)

	ss := &StartSequence{Home: home}
	if got := ss.defaultModelTier(); got != "fast" {
		t.Fatalf("defaultModelTier() = %q, want fast", got)
	}
}

func TestPhase3ConstraintsFailsWhenNoCredentialedModelResolves(t *testing.T) {
	home := t.TempDir()
	agentName := "alpha"
	writeFile(t, filepath.Join(home, "config.yaml"), "llm_provider: provider-b\n")
	writeFile(t, filepath.Join(home, "infrastructure", "routing.yaml"), `providers:
  provider-a:
    api_base: https://provider-a.example.com/v1
    auth_env: PROVIDER_A_API_KEY
  provider-b:
    api_base: https://provider-b.example.com/v1
    auth_env: PROVIDER_B_API_KEY
models:
  standard:
    provider: provider-a
    provider_model: provider-a-model-v1
  provider-b-standard:
    provider: provider-b
    provider_model: provider-b-model-v1
tiers:
  standard:
    - model: standard
      preference: 0
    - model: provider-b-standard
      preference: 1
settings:
  default_tier: standard
`)
	if err := os.MkdirAll(filepath.Join(home, "agents", agentName), 0755); err != nil {
		t.Fatalf("mkdir agent dir: %v", err)
	}

	ss := &StartSequence{
		Home:            home,
		AgentName:       agentName,
		Log:             slog.Default(),
		agentConfig:     map[string]interface{}{"model_tier": "standard"},
		constraintsData: map[string]interface{}{},
	}
	err := ss.phase3Constraints()
	if err == nil {
		t.Fatal("phase3Constraints returned nil error")
	}
	if !strings.Contains(err.Error(), "no credentialed model is available") {
		t.Fatalf("phase3Constraints error = %q, want credentialed model error", err.Error())
	}
	if ss.model == "standard" {
		t.Fatal("phase3Constraints fell back to uncredentialed standard model")
	}
}

func TestInferredTierEntriesStandardPrefersCapabilityRichModel(t *testing.T) {
	models := map[string]models.ModelConfig{
		"balanced": {
			CostPerMTokIn:            2,
			CostPerMTokOut:           4,
			Capabilities:             []string{"tools", "vision", "reasoning"},
			ProviderToolCapabilities: []string{"provider-web-search"},
		},
		"premium": {
			CostPerMTokIn:  5,
			CostPerMTokOut: 10,
			Capabilities:   []string{"tools", "vision"},
		},
	}

	entries := inferredTierEntries(models, "standard")
	if len(entries) != 2 {
		t.Fatalf("len(entries) = %d, want 2", len(entries))
	}
	if entries[0].Model != "balanced" {
		t.Fatalf("entries[0].Model = %q, want balanced", entries[0].Model)
	}
}

func TestInferredTierEntriesFastPrefersLowerCostModel(t *testing.T) {
	models := map[string]models.ModelConfig{
		"low-cost": {
			CostPerMTokIn:  0.1,
			CostPerMTokOut: 0.2,
			Capabilities:   []string{"tools"},
		},
		"high-cost": {
			CostPerMTokIn:  2,
			CostPerMTokOut: 4,
			Capabilities:   []string{"tools", "vision", "reasoning"},
		},
	}

	entries := inferredTierEntries(models, "fast")
	if len(entries) != 2 {
		t.Fatalf("len(entries) = %d, want 2", len(entries))
	}
	if entries[0].Model != "low-cost" {
		t.Fatalf("entries[0].Model = %q, want low-cost", entries[0].Model)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
