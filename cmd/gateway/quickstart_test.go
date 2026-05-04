package main

import (
	"path/filepath"
	"testing"

	"github.com/geoffbelknap/agency/internal/config"
)

func TestAgencyHomeFlagFromArgs(t *testing.T) {
	tests := map[string]struct {
		args []string
		want string
	}{
		"long separate":  {args: []string{"--agency-home", "/tmp/agency-home", "infra", "status"}, want: "/tmp/agency-home"},
		"long equals":    {args: []string{"infra", "status", "--agency-home=/tmp/agency-home"}, want: "/tmp/agency-home"},
		"short separate": {args: []string{"-H", "/tmp/agency-home", "-q", "list"}, want: "/tmp/agency-home"},
		"short joined":   {args: []string{"list", "-H/tmp/agency-home"}, want: "/tmp/agency-home"},
		"none":           {args: []string{"list"}, want: ""},
		"after terminator": {
			args: []string{"--", "--agency-home", "/tmp/agency-home"},
			want: "",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			if got := agencyHomeFlagFromArgs(tt.args); got != tt.want {
				t.Fatalf("agencyHomeFlagFromArgs() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNormalizeAgencyHomeFlag(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	got, err := normalizeAgencyHomeFlag("relative-home")
	if err != nil {
		t.Fatalf("normalize relative: %v", err)
	}
	if !filepath.IsAbs(got) {
		t.Fatalf("relative home normalized to non-absolute path %q", got)
	}

	got, err = normalizeAgencyHomeFlag("~/agency-home")
	if err != nil {
		t.Fatalf("normalize tilde: %v", err)
	}
	want := filepath.Join(tmp, "agency-home")
	if got != want {
		t.Fatalf("tilde home = %q, want %q", got, want)
	}
}

func TestNormalizeQuickstartProvider(t *testing.T) {
	tests := map[string]string{
		"":           "",
		"anthropic":  "anthropic",
		"OpenAI":     "openai",
		"google":     "google",
		"  GOOGLE  ": "google",
	}

	for input, want := range tests {
		if got := normalizeProvider(input); got != want {
			t.Fatalf("normalizeProvider(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestSupportedQuickstartProvider(t *testing.T) {
	for _, provider := range []string{"anthropic", "openai", "google"} {
		if !isSupportedQuickstartProvider(provider) {
			t.Fatalf("%s should be supported", provider)
		}
	}
	if isSupportedQuickstartProvider("gemini") {
		t.Fatal("gemini should not be supported as a provider identity; use google")
	}
}

func TestQuickstartProviderDescriptorsComeFromCatalogMetadata(t *testing.T) {
	descriptors := quickstartProviderDescriptors()
	if len(descriptors) != 3 {
		t.Fatalf("len(descriptors) = %d, want 3", len(descriptors))
	}
	if descriptors[0].Name != "google" || !descriptors[0].Recommended {
		t.Fatalf("descriptors[0] = %#v, want google recommended first", descriptors[0])
	}
	if descriptors[1].Name != "anthropic" || descriptors[2].Name != "openai" {
		t.Fatalf("descriptor order = %#v, want google/anthropic/openai", descriptors)
	}
	if probe := quickstartProviderProbe("google"); probe == nil || probe.URL == "" || probe.Method == "" {
		t.Fatalf("quickstartProviderProbe(google) = %#v, want configured probe", probe)
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

func TestPromptProviderChoiceMapping(t *testing.T) {
	tests := map[string]string{
		"":  "google",
		"1": "google",
		"2": "anthropic",
		"3": "openai",
		"9": "google",
	}

	for input, want := range tests {
		if got := quickstartProviderForChoice(input); got != want {
			t.Fatalf("quickstartProviderForChoice(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestQuickstartRestartDecision(t *testing.T) {
	key := config.KeyEntry{Provider: "provider-a", EnvVar: "PROVIDER_A_API_KEY", Key: "sk-test"}
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

func TestQuickstartRuntimeErrorDetails(t *testing.T) {
	err := assertErr("microagent runtime artifacts are not ready\n  - KVM device access: fix it")
	got := quickstartRuntimeErrorDetails(err)
	want := "  - KVM device access: fix it"
	if got != want {
		t.Fatalf("quickstartRuntimeErrorDetails() = %q, want %q", got, want)
	}
}

func TestQuickstartRuntimeErrorDetailsWithoutDetails(t *testing.T) {
	if got := quickstartRuntimeErrorDetails(assertErr("runtime artifacts are not ready")); got != "" {
		t.Fatalf("quickstartRuntimeErrorDetails() = %q, want empty", got)
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
