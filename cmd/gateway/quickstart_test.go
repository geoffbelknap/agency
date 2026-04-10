package main

import (
	"testing"

	"github.com/geoffbelknap/agency/internal/config"
)

func TestNormalizeQuickstartProvider(t *testing.T) {
	tests := map[string]string{
		"":           "",
		"anthropic":  "anthropic",
		"OpenAI":     "openai",
		"gemini":     "gemini",
		"  GEMINI  ": "gemini",
	}

	for input, want := range tests {
		if got := normalizeProvider(input); got != want {
			t.Fatalf("normalizeProvider(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestSupportedQuickstartProvider(t *testing.T) {
	for _, provider := range []string{"anthropic", "openai", "gemini"} {
		if !isSupportedQuickstartProvider(provider) {
			t.Fatalf("%s should be supported", provider)
		}
	}
	if isSupportedQuickstartProvider("google") {
		t.Fatal("google should not be supported as a provider identity; use gemini")
	}
}

func TestQuickstartPromptDecision(t *testing.T) {
	tests := []struct {
		name             string
		provider         string
		key              string
		providerExplicit bool
		want             bool
	}{
		{name: "no provider prompts", want: true},
		{name: "detected provider without key skips prompt", provider: "anthropic", want: false},
		{name: "explicit provider without key prompts", provider: "anthropic", providerExplicit: true, want: true},
		{name: "explicit provider with key skips prompt for direct validation", provider: "anthropic", key: "sk-test", providerExplicit: true, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shouldPromptForQuickstartKey(tt.provider, tt.key, tt.providerExplicit)
			if got != tt.want {
				t.Fatalf("shouldPromptForQuickstartKey() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestQuickstartRestartDecision(t *testing.T) {
	key := config.KeyEntry{Provider: "anthropic", EnvVar: "ANTHROPIC_API_KEY", Key: "sk-test"}
	tests := []struct {
		name                string
		gatewayRunning      bool
		configExistedBefore bool
		pendingKeys         []config.KeyEntry
		want                bool
	}{
		{name: "stopped gateway never restarts", configExistedBefore: true, pendingKeys: []config.KeyEntry{key}, want: false},
		{name: "running gateway with unchanged config stays up", gatewayRunning: true, configExistedBefore: true, want: false},
		{name: "running gateway with new config restarts", gatewayRunning: true, want: true},
		{name: "running gateway with new key restarts", gatewayRunning: true, configExistedBefore: true, pendingKeys: []config.KeyEntry{key}, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shouldRestartGatewayForQuickstart(tt.gatewayRunning, tt.configExistedBefore, tt.pendingKeys)
			if got != tt.want {
				t.Fatalf("shouldRestartGatewayForQuickstart() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestHubInstallAlreadyExists(t *testing.T) {
	if !isHubInstallAlreadyExists(assertErr("create instance: instance with name \"gemini\" already exists (id=9ac2cbab)")) {
		t.Fatal("already exists hub install error should be treated as idempotent success")
	}
	if isHubInstallAlreadyExists(assertErr("hub cache unavailable")) {
		t.Fatal("unrelated hub install errors should still fail")
	}
}

type assertErr string

func (e assertErr) Error() string { return string(e) }

func TestLocalWebURLForHost(t *testing.T) {
	tests := map[string]string{
		"":          "http://localhost:8280",
		"localhost": "http://localhost:8280",
		"127.0.0.1": "http://127.0.0.1:8280",
	}

	for input, want := range tests {
		if got := localWebURLForHost(input); got != want {
			t.Fatalf("localWebURLForHost(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestQuickstartDMURLForAgent(t *testing.T) {
	tests := []struct {
		name      string
		baseURL   string
		agentName string
		want      string
	}{
		{name: "empty agent falls back to channels", baseURL: "http://localhost:8280", want: "http://localhost:8280/channels"},
		{name: "agent dm", baseURL: "http://localhost:8280", agentName: "henry", want: "http://localhost:8280/channels/dm-henry"},
		{name: "trims trailing slash", baseURL: "http://localhost:8280/", agentName: "security-analyst", want: "http://localhost:8280/channels/dm-security-analyst"},
		{name: "escapes unsafe names defensively", baseURL: "http://localhost:8280", agentName: "bad name", want: "http://localhost:8280/channels/dm-bad%20name"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := quickstartDMURLForAgent(tt.baseURL, tt.agentName); got != tt.want {
				t.Fatalf("quickstartDMURLForAgent(%q, %q) = %q, want %q", tt.baseURL, tt.agentName, got, tt.want)
			}
		})
	}
}
