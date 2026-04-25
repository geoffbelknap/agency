package main

import (
	"net/http"
	"strings"
	"testing"
)

func TestProviderAdapterOpenAICompatiblePreparesRequest(t *testing.T) {
	adapter := providerAdapterFor(Provider{})
	prepared, err := adapter.PrepareRequest(providerRequestContext{
		RequestPath:   "/v1/chat/completions",
		TargetURL:     "https://provider.example/v1/chat/completions",
		ProviderModel: "provider-model",
		Provider:      Provider{APIBase: "https://provider.example/v1"},
		Body:          map[string]interface{}{"model": "standard"},
	})
	if err != nil {
		t.Fatalf("PrepareRequest returned error: %v", err)
	}
	if prepared.TargetURL != "https://provider.example/v1/chat/completions" {
		t.Fatalf("TargetURL = %q", prepared.TargetURL)
	}
	if !strings.Contains(string(prepared.Body), `"model":"provider-model"`) {
		t.Fatalf("prepared body did not rewrite model: %s", string(prepared.Body))
	}
}

func TestProviderAdapterAnthropicAddsVersionHeader(t *testing.T) {
	adapter := providerAdapterFor(Provider{APIFormat: "anthropic"})
	req, err := http.NewRequest(http.MethodPost, "https://provider.example/messages", nil)
	if err != nil {
		t.Fatal(err)
	}
	adapter.AddHeaders(req)
	if got := req.Header.Get("anthropic-version"); got != "2023-06-01" {
		t.Fatalf("anthropic-version header = %q", got)
	}
}
