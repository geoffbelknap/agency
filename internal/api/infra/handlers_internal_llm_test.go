package infra

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/geoffbelknap/agency/internal/config"
	"github.com/geoffbelknap/agency/internal/logs"
	"github.com/geoffbelknap/agency/internal/models"
)

func TestValidInfraCallers(t *testing.T) {
	allowed := []string{"knowledge-synthesizer", "knowledge-curator"}
	for _, c := range allowed {
		if !validInfraCallers[c] {
			t.Errorf("expected %q to be a valid infra caller", c)
		}
	}

	rejected := []string{"agent-body", "unknown", "", "admin"}
	for _, c := range rejected {
		if validInfraCallers[c] {
			t.Errorf("expected %q to NOT be a valid infra caller", c)
		}
	}
}

func TestInfraTranslateToAnthropic(t *testing.T) {
	input := `{
		"model": "provider-a-fast",
		"messages": [
			{"role": "system", "content": "You are a knowledge extractor."},
			{"role": "user", "content": "Extract entities from this text."}
		],
		"max_tokens": 4096
	}`

	result, err := infraTranslateToAnthropic([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(result, &parsed); err != nil {
		t.Fatalf("invalid JSON output: %v", err)
	}

	// System should be extracted to top-level
	system, ok := parsed["system"].([]interface{})
	if !ok || len(system) != 1 {
		t.Error("expected system to be extracted as array with 1 element")
	}

	// Messages should only contain user message
	messages, ok := parsed["messages"].([]interface{})
	if !ok || len(messages) != 1 {
		t.Error("expected messages to contain only user message")
	}

	// max_tokens should be preserved
	if mt, ok := parsed["max_tokens"].(float64); !ok || mt != 4096 {
		t.Errorf("expected max_tokens=4096, got %v", parsed["max_tokens"])
	}
}

func TestInfraTranslateFromAnthropic(t *testing.T) {
	input := `{
		"id": "msg_123",
		"type": "message",
		"role": "assistant",
		"model": "provider-a-fast",
		"content": [
			{"type": "text", "text": "Here are the entities."}
		],
		"stop_reason": "end_turn",
		"usage": {
			"input_tokens": 100,
			"output_tokens": 50
		}
	}`

	result, err := infraTranslateFromAnthropic([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(result, &parsed); err != nil {
		t.Fatalf("invalid JSON output: %v", err)
	}

	if parsed["object"] != "chat.completion" {
		t.Errorf("expected object=chat.completion, got %v", parsed["object"])
	}

	choices, ok := parsed["choices"].([]interface{})
	if !ok || len(choices) != 1 {
		t.Fatal("expected 1 choice")
	}

	choice := choices[0].(map[string]interface{})
	msg := choice["message"].(map[string]interface{})
	if msg["content"] != "Here are the entities." {
		t.Errorf("unexpected content: %v", msg["content"])
	}
	if msg["stop_reason"] != "end_turn" {
		t.Errorf("expected message stop_reason=end_turn, got %v", msg["stop_reason"])
	}
	if choice["finish_reason"] != "stop" {
		t.Errorf("expected finish_reason=stop, got %v", choice["finish_reason"])
	}
	if choice["stop_reason"] != "end_turn" {
		t.Errorf("expected choice stop_reason=end_turn, got %v", choice["stop_reason"])
	}

	usage := parsed["usage"].(map[string]interface{})
	if usage["prompt_tokens"].(float64) != 100 {
		t.Errorf("expected prompt_tokens=100, got %v", usage["prompt_tokens"])
	}
	if usage["completion_tokens"].(float64) != 50 {
		t.Errorf("expected completion_tokens=50, got %v", usage["completion_tokens"])
	}
}

func TestExtractUsageFromResponse(t *testing.T) {
	body := `{"usage": {"prompt_tokens": 150, "completion_tokens": 75}}`
	in, out := extractUsageFromResponse([]byte(body))
	if in != 150 {
		t.Errorf("expected input_tokens=150, got %d", in)
	}
	if out != 75 {
		t.Errorf("expected output_tokens=75, got %d", out)
	}
}

func TestExtractUsageFromResponse_Empty(t *testing.T) {
	in, out := extractUsageFromResponse([]byte(`{}`))
	if in != 0 || out != 0 {
		t.Errorf("expected 0,0 for empty response, got %d,%d", in, out)
	}
}

func TestInternalLLMAdapterOpenAICompatible(t *testing.T) {
	provider := &models.ProviderConfig{
		APIBase:    "https://provider.example/v1",
		AuthHeader: "Authorization",
		AuthPrefix: "Token ",
	}
	model := &models.ModelConfig{Provider: "custom", ProviderModel: "provider-model"}
	adapter := internalLLMAdapterFor(provider, model)
	target, body, err := adapter.PrepareRequest(provider, model, map[string]interface{}{"model": "standard"})
	if err != nil {
		t.Fatalf("PrepareRequest: %v", err)
	}
	if target != "https://provider.example/v1/chat/completions" {
		t.Fatalf("target = %q", target)
	}
	if !bytes.Contains(body, []byte(`"model":"provider-model"`)) {
		t.Fatalf("body did not rewrite model: %s", string(body))
	}
	req := httptest.NewRequest(http.MethodPost, target, nil)
	adapter.AddAuthHeaders(req, provider, "secret")
	if got := req.Header.Get("Authorization"); got != "Token secret" {
		t.Fatalf("Authorization = %q", got)
	}
}

func TestInternalLLMAdapterGeminiUsesRawKey(t *testing.T) {
	provider := &models.ProviderConfig{
		APIBase:    "https://generativelanguage.googleapis.com/v1beta",
		APIFormat:  "gemini",
		AuthHeader: "x-goog-api-key",
	}
	model := &models.ModelConfig{Provider: "google", ProviderModel: "gemini-2.5-flash"}
	adapter := internalLLMAdapterFor(provider, model)
	target, body, err := adapter.PrepareRequest(provider, model, map[string]interface{}{
		"model":    "fast",
		"messages": []interface{}{map[string]interface{}{"role": "user", "content": "hello"}},
	})
	if err != nil {
		t.Fatalf("PrepareRequest: %v", err)
	}
	if target != "https://generativelanguage.googleapis.com/v1beta/models/gemini-2.5-flash:generateContent" {
		t.Fatalf("target = %q", target)
	}
	if bytes.Contains(body, []byte(`"messages"`)) || !bytes.Contains(body, []byte(`"contents"`)) {
		t.Fatalf("body was not translated to Gemini shape: %s", string(body))
	}
	req := httptest.NewRequest(http.MethodPost, target, nil)
	adapter.AddAuthHeaders(req, provider, "secret")
	if got := req.Header.Get("x-goog-api-key"); got != "secret" {
		t.Fatalf("x-goog-api-key = %q", got)
	}
}

func TestInternalLLMGeminiNativeUsesAPIKeyHeaderWithoutBearer(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("GEMINI_API_KEY", "gemini-secret")

	var gotPath, gotKey string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotKey = r.Header.Get("x-goog-api-key")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"candidates":[{"content":{"parts":[{"text":"ok"}]}}],
			"usageMetadata":{"promptTokenCount":3,"candidatesTokenCount":2,"totalTokenCount":5}
		}`))
	}))
	defer upstream.Close()

	infraDir := filepath.Join(tmp, "infrastructure")
	if err := os.MkdirAll(infraDir, 0700); err != nil {
		t.Fatal(err)
	}
	routing := `providers:
  google:
    api_base: ` + upstream.URL + `/v1beta
    api_format: gemini
    auth_header: x-goog-api-key
    auth_env: GEMINI_API_KEY
models:
  gemini-flash:
    provider: google
    provider_model: gemini-2.5-flash
`
	if err := os.WriteFile(filepath.Join(infraDir, "routing.yaml"), []byte(routing), 0600); err != nil {
		t.Fatal(err)
	}

	h := &handler{deps: Deps{
		Config: &config.Config{Home: tmp, Token: "token"},
		Audit:  logs.NewWriter(tmp),
	}}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/infra/internal/llm", bytes.NewReader([]byte(`{
		"model":"gemini-flash",
		"messages":[{"role":"user","content":"hello"}]
	}`)))
	req.Header.Set("X-Agency-Caller", "knowledge-synthesizer")
	req.Header.Set("X-Agency-Token", "token")
	rec := httptest.NewRecorder()

	h.internalLLM(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if gotPath != "/v1beta/models/gemini-2.5-flash:generateContent" {
		t.Fatalf("upstream path = %q", gotPath)
	}
	if gotKey != "gemini-secret" {
		t.Fatalf("x-goog-api-key = %q, want raw key without bearer prefix", gotKey)
	}
	if bytes.Contains(rec.Body.Bytes(), []byte("usageMetadata")) {
		t.Fatalf("response was not translated to OpenAI shape: %s", rec.Body.String())
	}
}
