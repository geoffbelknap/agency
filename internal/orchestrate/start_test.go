package orchestrate

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveModelTierPrefersConfiguredProvider(t *testing.T) {
	home := t.TempDir()
	t.Setenv("GEMINI_API_KEY", "test-key")
	writeFile(t, filepath.Join(home, "config.yaml"), "llm_provider: gemini\n")
	writeFile(t, filepath.Join(home, "infrastructure", "routing.yaml"), `providers:
  anthropic:
    api_base: https://api.anthropic.com/v1
    auth_env: ANTHROPIC_API_KEY
  gemini:
    api_base: https://generativelanguage.googleapis.com/v1beta/openai
    auth_env: GEMINI_API_KEY
models:
  claude-sonnet:
    provider: anthropic
    provider_model: claude-sonnet-4-20250514
  gemini-2.5-pro:
    provider: gemini
    provider_model: gemini-2.5-pro
tiers:
  standard:
    - model: claude-sonnet
      preference: 0
    - model: gemini-2.5-pro
      preference: 1
settings:
  default_tier: standard
`)

	ss := &StartSequence{Home: home}
	if got := ss.resolveModelTier("standard"); got != "gemini-2.5-pro" {
		t.Fatalf("resolveModelTier() = %q, want gemini-2.5-pro", got)
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

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
