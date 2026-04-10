package config

import "testing"

func TestProviderCredentialName(t *testing.T) {
	tests := map[string]string{
		"anthropic": "anthropic-api-key",
		"openai":    "openai-api-key",
		"gemini":    "gemini-api-key",
		"custom":    "CUSTOM_API_KEY",
	}

	for provider, want := range tests {
		if got := ProviderCredentialName(provider); got != want {
			t.Fatalf("ProviderCredentialName(%q) = %q, want %q", provider, got, want)
		}
	}
}
