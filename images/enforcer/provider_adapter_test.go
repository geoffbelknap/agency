package main

import (
	"net/http"
	"strings"
	"testing"
)

func TestProviderAdapterOpenAICompatiblePreparesRequest(t *testing.T) {
	adapter := providerAdapterFor("custom", Provider{APIBase: "https://provider.example/v1"})
	prepared, err := adapter.PrepareRequest(providerRequestContext{
		RequestPath:   "/v1/chat/completions",
		TargetURL:     "https://provider.example/v1/chat/completions",
		ProviderModel: "provider-model",
		Provider:      Provider{APIBase: "https://provider.example/v1"},
		Body:          map[string]interface{}{"model": "standard", "stream": true},
		Stream:        true,
	})
	if err != nil {
		t.Fatalf("PrepareRequest returned error: %v", err)
	}
	if prepared.TargetURL != "https://provider.example/v1/chat/completions" {
		t.Fatalf("TargetURL = %q", prepared.TargetURL)
	}
	body := string(prepared.Body)
	if !strings.Contains(body, `"model":"provider-model"`) {
		t.Fatalf("prepared body did not rewrite model: %s", body)
	}
	if !strings.Contains(body, `"include_usage":true`) {
		t.Fatalf("prepared streaming body did not request usage: %s", body)
	}
}

func TestProviderAdapterAnthropicPreparesHeadersAndRejectsResponses(t *testing.T) {
	adapter := providerAdapterFor("anthropic", Provider{APIFormat: "anthropic"})
	if _, err := adapter.PrepareRequest(providerRequestContext{RequestPath: "/v1/responses", ProviderName: "anthropic"}); err == nil {
		t.Fatal("expected responses endpoint rejection")
	}
	req, err := http.NewRequest(http.MethodPost, "https://provider.example/messages", nil)
	if err != nil {
		t.Fatal(err)
	}
	adapter.AddHeaders(req)
	if got := req.Header.Get("anthropic-version"); got != "2023-06-01" {
		t.Fatalf("anthropic-version header = %q", got)
	}
}

func TestProviderAdapterInfersBuiltInProviderFormat(t *testing.T) {
	if _, ok := providerAdapterFor("anthropic", Provider{}).(anthropicProviderAdapter); !ok {
		t.Fatal("expected legacy anthropic provider name to use Anthropic adapter")
	}
	if _, ok := providerAdapterFor("google", Provider{}).(geminiProviderAdapter); !ok {
		t.Fatal("expected legacy google provider name to use Gemini adapter")
	}
}

func TestProviderAdapterExplicitFormatOverridesProviderName(t *testing.T) {
	if _, ok := providerAdapterFor("anthropic", Provider{APIFormat: "openai"}).(openAICompatibleProviderAdapter); !ok {
		t.Fatal("expected explicit openai api_format to use OpenAI-compatible adapter")
	}
}

func TestProviderAdapterGeminiStreamURL(t *testing.T) {
	adapter := providerAdapterFor("google", Provider{APIFormat: "gemini"})
	prepared, err := adapter.PrepareRequest(providerRequestContext{
		RequestPath:   "/v1/chat/completions",
		TargetURL:     "https://generativelanguage.googleapis.com/v1beta/models/gemini:generateContent",
		ProviderModel: "gemini",
		Provider:      Provider{APIFormat: "gemini"},
		Body: map[string]interface{}{
			"model":    "standard",
			"stream":   true,
			"messages": []interface{}{map[string]interface{}{"role": "user", "content": "hi"}},
		},
		Stream: true,
	})
	if err != nil {
		t.Fatalf("PrepareRequest returned error: %v", err)
	}
	if !strings.Contains(prepared.TargetURL, ":streamGenerateContent") || !strings.Contains(prepared.TargetURL, "alt=sse") {
		t.Fatalf("Gemini stream TargetURL = %q", prepared.TargetURL)
	}
}
