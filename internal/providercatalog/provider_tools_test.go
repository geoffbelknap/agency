package providercatalog

import "testing"

func TestProviderToolsInventoryLoads(t *testing.T) {
	inv, err := ProviderTools()
	if err != nil {
		t.Fatal(err)
	}
	if inv.Version == "" {
		t.Fatal("expected inventory version")
	}
	for _, cap := range []string{
		"provider-web-search",
		"provider-url-context",
		"provider-file-search",
		"provider-code-execution",
		"provider-computer-use",
		"provider-shell",
		"provider-text-editor",
		"provider-memory",
		"provider-mcp",
		"provider-image-generation",
		"provider-google-maps",
		"provider-tool-search",
		"provider-apply-patch",
	} {
		entry, ok := inv.Capabilities[cap]
		if !ok {
			t.Fatalf("missing capability %s", cap)
		}
		if entry.Risk == "" || entry.Execution == "" {
			t.Fatalf("capability %s missing risk/execution metadata: %#v", cap, entry)
		}
		for _, provider := range []string{"openai", "anthropic", "google"} {
			if _, ok := entry.Providers[provider]; !ok {
				t.Fatalf("capability %s missing provider %s", cap, provider)
			}
		}
	}
}

func TestProviderYAMLToolCapabilitiesExistInInventory(t *testing.T) {
	inv, err := ProviderTools()
	if err != nil {
		t.Fatal(err)
	}
	providers, err := List()
	if err != nil {
		t.Fatal(err)
	}
	for _, provider := range providers {
		routing, ok := provider.Routing["models"].(map[string]interface{})
		if !ok {
			continue
		}
		for modelName, rawModel := range routing {
			model, ok := rawModel.(map[string]interface{})
			if !ok {
				continue
			}
			caps, _ := model["provider_tool_capabilities"].([]interface{})
			for _, rawCap := range caps {
				capability, _ := rawCap.(string)
				if capability == "" {
					continue
				}
				if _, ok := inv.Capabilities[capability]; !ok {
					t.Fatalf("%s/%s declares unknown provider tool capability %q", provider.Name, modelName, capability)
				}
			}
			pricing, _ := model["provider_tool_pricing"].(map[string]interface{})
			for capability := range pricing {
				if _, ok := inv.Capabilities[capability]; !ok {
					t.Fatalf("%s/%s declares unknown provider tool pricing capability %q", provider.Name, modelName, capability)
				}
			}
		}
	}
}

func TestProviderYAMLDoesNotExposeAgencyHarnessedTools(t *testing.T) {
	inv, err := ProviderTools()
	if err != nil {
		t.Fatal(err)
	}
	providers, err := List()
	if err != nil {
		t.Fatal(err)
	}
	for _, provider := range providers {
		routing, ok := provider.Routing["models"].(map[string]interface{})
		if !ok {
			continue
		}
		for modelName, rawModel := range routing {
			model, ok := rawModel.(map[string]interface{})
			if !ok {
				continue
			}
			caps, _ := model["provider_tool_capabilities"].([]interface{})
			for _, rawCap := range caps {
				capability, _ := rawCap.(string)
				entry, ok := inv.Capabilities[capability]
				if !ok {
					continue
				}
				if entry.Execution == "agency_harnessed" {
					t.Fatalf("%s/%s exposes agency-harnessed provider tool capability %q", provider.Name, modelName, capability)
				}
			}
		}
	}
}

func TestInventoryDoesNotUseStaleHarnessRequiredStatus(t *testing.T) {
	inv, err := ProviderTools()
	if err != nil {
		t.Fatal(err)
	}
	for capability, entry := range inv.Capabilities {
		for provider, meta := range entry.Providers {
			if meta.Status == "harness_required" {
				t.Fatalf("%s/%s uses stale harness_required status", capability, provider)
			}
		}
	}
}

func TestProviderYAMLUsesCanonicalProviderPrincipals(t *testing.T) {
	providers, err := List()
	if err != nil {
		t.Fatal(err)
	}
	for _, provider := range providers {
		if provider.Name == "gemini" {
			t.Fatal("gemini provider principal should not exist; use google with api_format gemini")
		}
		routingProviders, _ := provider.Routing["providers"].(map[string]interface{})
		for name := range routingProviders {
			if name == "gemini" {
				t.Fatalf("%s routing declares stale gemini provider principal", provider.Name)
			}
		}
		routingModels, _ := provider.Routing["models"].(map[string]interface{})
		for modelName, rawModel := range routingModels {
			model, _ := rawModel.(map[string]interface{})
			modelProvider, _ := model["provider"].(string)
			if modelProvider == "gemini" {
				t.Fatalf("%s/%s references stale gemini provider principal", provider.Name, modelName)
			}
			if modelProvider != "" {
				if _, ok := routingProviders[modelProvider]; !ok {
					t.Fatalf("%s/%s references unknown provider principal %q", provider.Name, modelName, modelProvider)
				}
			}
		}
	}
}

func TestCanonicalProviderPrincipalForGoogle(t *testing.T) {
	if _, _, err := Get("google"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Get("gemini"); err == nil {
		t.Fatal("gemini provider principal should not exist; use google with api_format gemini")
	}
	if _, _, err := Get("openai-compatible"); err == nil {
		t.Fatal("openai-compatible pseudo-provider should not exist; use an explicit provider adapter")
	}
}
