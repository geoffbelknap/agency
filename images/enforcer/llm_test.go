package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testRoutingConfig() *RoutingConfig {
	return &RoutingConfig{
		Providers: map[string]Provider{
			"openai-compat": {
				APIBase: "PLACEHOLDER", // replaced per test
			},
		},
		Models: map[string]Model{
			"claude-sonnet": {
				Provider:      "openai-compat",
				ProviderModel: "claude-sonnet-4-20250514",
				CostIn:        3.0,
				CostOut:       15.0,
			},
		},
	}
}

func TestLLMModelResolved(t *testing.T) {
	// Fake LLM provider
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req map[string]interface{}
		json.Unmarshal(body, &req)

		// Verify model was rewritten
		model, _ := req["model"].(string)
		if model != "claude-sonnet-4-20250514" {
			t.Errorf("expected rewritten model, got: %s", model)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"id":"msg_test","model":"claude-sonnet-4-20250514","usage":{"input_tokens":10,"output_tokens":50}}`)
	}))
	defer provider.Close()

	rc := testRoutingConfig()
	rc.Providers["openai-compat"] = Provider{
		APIBase: provider.URL + "/v1/",
	}

	auditDir := t.TempDir()
	audit := NewAuditLogger(auditDir, "test-agent")
	defer audit.Close()

	// Use provider directly (no egress proxy in test)
	lh := NewLLMHandler(rc, provider.URL, audit)

	body := `{"model":"claude-sonnet","messages":[{"role":"user","content":"hello"}]}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer agency-scoped-test")
	rr := httptest.NewRecorder()
	lh.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp map[string]interface{}
	json.Unmarshal(rr.Body.Bytes(), &resp)
	if resp["model"] != "claude-sonnet-4-20250514" {
		t.Errorf("unexpected response model: %v", resp["model"])
	}
}

func TestLLMModelRewrittenInBody(t *testing.T) {
	var receivedModel string
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req map[string]interface{}
		json.Unmarshal(body, &req)
		receivedModel, _ = req["model"].(string)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"id":"msg_test"}`)
	}))
	defer provider.Close()

	rc := testRoutingConfig()
	rc.Providers["openai-compat"] = Provider{
		APIBase: provider.URL + "/v1/",
	}

	auditDir := t.TempDir()
	audit := NewAuditLogger(auditDir, "test-agent")
	defer audit.Close()

	lh := NewLLMHandler(rc, provider.URL, audit)

	body := `{"model":"claude-sonnet","messages":[{"role":"user","content":"test"}]}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	lh.ServeHTTP(rr, req)

	if receivedModel != "claude-sonnet-4-20250514" {
		t.Errorf("expected provider model in body, got: %s", receivedModel)
	}
}

func TestLLMUnknownModel(t *testing.T) {
	auditDir := t.TempDir()
	audit := NewAuditLogger(auditDir, "test-agent")
	defer audit.Close()

	rc := testRoutingConfig()
	lh := NewLLMHandler(rc, "http://localhost:1", audit)

	body := `{"model":"nonexistent-model","messages":[]}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	lh.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "unknown model") {
		t.Errorf("expected unknown model error, got: %s", rr.Body.String())
	}
}

func TestLLMBufferedResponse(t *testing.T) {
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Request-Id", "req-123")
		w.Header().Set("X-Ratelimit-Remaining", "99") // should be stripped
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"id":"msg_test","usage":{"input_tokens":100,"output_tokens":500}}`)
	}))
	defer provider.Close()

	rc := testRoutingConfig()
	rc.Providers["openai-compat"] = Provider{
		APIBase: provider.URL + "/v1/",
	}

	auditDir := t.TempDir()
	audit := NewAuditLogger(auditDir, "test-agent")
	defer audit.Close()

	lh := NewLLMHandler(rc, provider.URL, audit)

	body := `{"model":"claude-sonnet","messages":[]}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	lh.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
	if rr.Header().Get("X-Request-Id") != "req-123" {
		t.Errorf("safe header should be relayed")
	}
	if rr.Header().Get("X-Ratelimit-Remaining") != "" {
		t.Error("unsafe header should be stripped")
	}
}

func TestLLMStreamingResponse(t *testing.T) {
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)
		fmt.Fprint(w, "data: {\"id\":\"msg_1\",\"type\":\"content_block_delta\"}\n\n")
		flusher.Flush()
		fmt.Fprint(w, "data: {\"id\":\"msg_1\",\"type\":\"message_stop\",\"usage\":{\"input_tokens\":10,\"output_tokens\":20}}\n\n")
		flusher.Flush()
		fmt.Fprint(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	defer provider.Close()

	rc := testRoutingConfig()
	rc.Providers["openai-compat"] = Provider{
		APIBase: provider.URL + "/v1/",
	}

	auditDir := t.TempDir()
	audit := NewAuditLogger(auditDir, "test-agent")
	defer audit.Close()

	lh := NewLLMHandler(rc, provider.URL, audit)

	body := `{"model":"claude-sonnet","messages":[],"stream":true}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	lh.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
	if !strings.Contains(rr.Header().Get("Content-Type"), "text/event-stream") {
		t.Errorf("expected SSE content type, got: %s", rr.Header().Get("Content-Type"))
	}
	if !strings.Contains(rr.Body.String(), "content_block_delta") {
		t.Error("SSE data should be relayed")
	}
}

func TestLLMCorrelationIDPropagated(t *testing.T) {
	var receivedCorrelation string
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedCorrelation = r.Header.Get("X-Correlation-Id")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"id":"msg_test"}`)
	}))
	defer provider.Close()

	rc := testRoutingConfig()
	rc.Providers["openai-compat"] = Provider{
		APIBase: provider.URL + "/v1/",
	}

	auditDir := t.TempDir()
	audit := NewAuditLogger(auditDir, "test-agent")
	defer audit.Close()

	lh := NewLLMHandler(rc, provider.URL, audit)

	body := `{"model":"claude-sonnet","messages":[]}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Correlation-Id", "test-corr-456")
	rr := httptest.NewRecorder()
	lh.ServeHTTP(rr, req)

	if receivedCorrelation != "test-corr-456" {
		t.Errorf("correlation ID not propagated, got: %s", receivedCorrelation)
	}
}

func TestLLMRetryOnFailure(t *testing.T) {
	attempts := 0
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts < 3 {
			// Close connection to simulate failure
			hj, ok := w.(http.Hijacker)
			if ok {
				conn, _, _ := hj.Hijack()
				conn.Close()
				return
			}
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"id":"msg_test"}`)
	}))
	defer provider.Close()

	rc := testRoutingConfig()
	rc.Providers["openai-compat"] = Provider{
		APIBase: provider.URL + "/v1/",
	}

	auditDir := t.TempDir()
	audit := NewAuditLogger(auditDir, "test-agent")
	defer audit.Close()

	lh := NewLLMHandler(rc, provider.URL, audit)

	body := `{"model":"claude-sonnet","messages":[]}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	lh.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200 after retries, got %d", rr.Code)
	}
	if attempts < 3 {
		t.Errorf("expected at least 3 attempts, got %d", attempts)
	}
}

func TestLLMAuditLogWritten(t *testing.T) {
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"id":"msg_test","usage":{"input_tokens":100,"output_tokens":50}}`)
	}))
	defer provider.Close()

	rc := testRoutingConfig()
	rc.Providers["openai-compat"] = Provider{
		APIBase: provider.URL + "/v1/",
	}

	auditDir := t.TempDir()
	audit := NewAuditLogger(auditDir, "test-agent")

	lh := NewLLMHandler(rc, provider.URL, audit)

	body := `{"model":"claude-sonnet","messages":[]}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Correlation-Id", "corr-test")
	rr := httptest.NewRecorder()
	lh.ServeHTTP(rr, req)

	audit.Close()

	// Read audit log
	today := time.Now().UTC().Format("2006-01-02")
	data, err := os.ReadFile(filepath.Join(auditDir, "enforcer-"+today+".jsonl"))
	if err != nil {
		t.Fatalf("read audit: %v", err)
	}

	var entry AuditEntry
	json.Unmarshal([]byte(strings.TrimSpace(string(data))), &entry)

	if entry.Type != "LLM_DIRECT" {
		t.Errorf("wrong type: %s", entry.Type)
	}
	if entry.Model != "claude-sonnet" {
		t.Errorf("wrong model: %s", entry.Model)
	}
	if entry.ProviderModel != "claude-sonnet-4-20250514" {
		t.Errorf("wrong provider model: %s", entry.ProviderModel)
	}
	if entry.CorrelationID != "corr-test" {
		t.Errorf("wrong correlation_id: %s", entry.CorrelationID)
	}
	if entry.InputTokens != 100 {
		t.Errorf("wrong input_tokens: %d", entry.InputTokens)
	}
	if entry.OutputTokens != 50 {
		t.Errorf("wrong output_tokens: %d", entry.OutputTokens)
	}
}

func TestLLMMissingModel(t *testing.T) {
	auditDir := t.TempDir()
	audit := NewAuditLogger(auditDir, "test-agent")
	defer audit.Close()

	rc := testRoutingConfig()
	lh := NewLLMHandler(rc, "http://localhost:1", audit)

	body := `{"messages":[]}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	lh.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rr.Code)
	}
}

func TestExtractStreamUsage(t *testing.T) {
	chunk := "data: {\"usage\":{\"input_tokens\":100,\"output_tokens\":50}}\n\n"
	in, out := extractStreamUsage(chunk)
	if in != 100 {
		t.Errorf("expected input_tokens 100, got %d", in)
	}
	if out != 50 {
		t.Errorf("expected output_tokens 50, got %d", out)
	}
}

func TestExtractStreamUsageOpenAI(t *testing.T) {
	chunk := "data: {\"usage\":{\"prompt_tokens\":200,\"completion_tokens\":80}}\n\n"
	in, out := extractStreamUsage(chunk)
	if in != 200 {
		t.Errorf("expected prompt_tokens 200, got %d", in)
	}
	if out != 80 {
		t.Errorf("expected completion_tokens 80, got %d", out)
	}
}

func TestExtractStreamUsageDone(t *testing.T) {
	chunk := "data: [DONE]\n\n"
	in, out := extractStreamUsage(chunk)
	if in != 0 || out != 0 {
		t.Errorf("expected 0/0 for DONE, got %d/%d", in, out)
	}
}

func TestLLMAnthropicTranslation(t *testing.T) {
	var receivedPath string
	var receivedBody map[string]interface{}
	var receivedHeaders http.Header

	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedPath = r.URL.Path
		receivedHeaders = r.Header.Clone()
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &receivedBody)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"id":"msg_test","type":"message","role":"assistant","content":[{"type":"text","text":"Hi!"}],"stop_reason":"end_turn","model":"claude-sonnet-4-20250514","usage":{"input_tokens":10,"output_tokens":5}}`)
	}))
	defer provider.Close()

	rc := &RoutingConfig{
		Providers: map[string]Provider{
			"anthropic": {APIBase: provider.URL + "/v1/"},
		},
		Models: map[string]Model{
			"claude-sonnet": {Provider: "anthropic", ProviderModel: "claude-sonnet-4-20250514"},
		},
	}

	auditDir := t.TempDir()
	audit := NewAuditLogger(auditDir, "test-agent")
	defer audit.Close()
	lh := NewLLMHandler(rc, provider.URL, audit)

	body := `{"model":"claude-sonnet","messages":[{"role":"system","content":"Be helpful"},{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	lh.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	if receivedPath != "/v1/messages" {
		t.Errorf("expected /v1/messages, got %s", receivedPath)
	}
	if receivedHeaders.Get("Anthropic-Version") == "" {
		t.Error("missing anthropic-version header")
	}
	if receivedBody["system"] == nil {
		t.Error("system should be top-level in Anthropic request")
	}
	var resp map[string]interface{}
	json.Unmarshal(rr.Body.Bytes(), &resp)
	choices, ok := resp["choices"].([]interface{})
	if !ok || len(choices) == 0 {
		t.Fatal("response should have choices array")
	}
	choice := choices[0].(map[string]interface{})
	msg := choice["message"].(map[string]interface{})
	if msg["content"] != "Hi!" {
		t.Errorf("wrong content: %v", msg["content"])
	}
}

func TestLLMRateLimitAcquireBeforeRequest(t *testing.T) {
	// Track that LLM request happens (rate limiter grants immediately with default limits)
	var llmCalled bool

	// Mock LLM provider (Anthropic format since we use anthropic provider)
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		llmCalled = true
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Ratelimit-Limit-Requests", "100")
		w.Header().Set("X-Ratelimit-Remaining-Requests", "95")
		w.Header().Set("X-Ratelimit-Reset-Requests", "30")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"id":"msg_test","type":"message","role":"assistant","content":[{"type":"text","text":"Hi"}],"stop_reason":"end_turn","model":"claude-sonnet-4-20250514","usage":{"input_tokens":10,"output_tokens":5}}`)
	}))
	defer provider.Close()

	rc := &RoutingConfig{
		Providers: map[string]Provider{
			"anthropic": {APIBase: provider.URL + "/v1/"},
		},
		Models: map[string]Model{
			"claude-sonnet": {Provider: "anthropic", ProviderModel: "claude-sonnet-4-20250514"},
		},
	}

	auditDir := t.TempDir()
	audit := NewAuditLogger(auditDir, "test-agent")
	defer audit.Close()

	rl := NewRateLimiter(50, 60)
	lh := NewLLMHandler(rc, provider.URL, audit)
	lh.SetRateLimiter(rl, "test-agent")

	body := `{"model":"claude-sonnet","messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	lh.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	if !llmCalled {
		t.Fatal("expected LLM provider to be called")
	}

	// Verify rate limiter was updated from response headers
	state := rl.GetState("anthropic")
	if !state.Discovered {
		t.Error("expected rate limiter state to be discovered after response headers")
	}
	if state.Limit != 100 {
		t.Errorf("expected limit 100, got %d", state.Limit)
	}
}

func TestLLMRateLimitDeniedSendsKeepalive(t *testing.T) {
	// Mock LLM provider (Anthropic format)
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"id":"msg_test","type":"message","role":"assistant","content":[{"type":"text","text":"Hi"}],"stop_reason":"end_turn","model":"claude-sonnet-4-20250514","usage":{"input_tokens":10,"output_tokens":5}}`)
	}))
	defer provider.Close()

	rc := &RoutingConfig{
		Providers: map[string]Provider{
			"anthropic": {APIBase: provider.URL + "/v1/"},
		},
		Models: map[string]Model{
			"claude-sonnet": {Provider: "anthropic", ProviderModel: "claude-sonnet-4-20250514"},
		},
	}

	auditDir := t.TempDir()
	audit := NewAuditLogger(auditDir, "test-agent")
	defer audit.Close()

	// Create rate limiter with limit of 1 RPM, then exhaust it
	rl := NewRateLimiter(1, 1) // 1 rpm, 1s window
	rl.RecordRequest("anthropic")  // exhaust the single slot

	lh := NewLLMHandler(rc, provider.URL, audit)
	lh.SetRateLimiter(rl, "test-agent")

	body := `{"model":"claude-sonnet","messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	// Run in goroutine since it will block waiting for rate limit
	done := make(chan struct{})
	go func() {
		lh.ServeHTTP(rr, req)
		close(done)
	}()

	// Wait for it to complete (window is 1s so it should unblock quickly)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for rate-limited request to complete")
	}

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	// Response body should contain keepalive comment before actual response
	output := rr.Body.String()
	if !strings.Contains(output, ": queued") {
		t.Errorf("expected SSE keepalive comment in response, got: %s", output)
	}
}

func TestLLMAnthropicStreaming(t *testing.T) {
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)

		events := []string{
			"event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"model\":\"claude-sonnet-4-20250514\",\"usage\":{\"input_tokens\":50,\"cache_read_input_tokens\":40}}}",
			"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}",
			"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"Hello\"}}",
			"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}",
			"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":10}}",
			"event: message_stop\ndata: {\"type\":\"message_stop\"}",
		}

		for _, e := range events {
			fmt.Fprintf(w, "%s\n\n", e)
			flusher.Flush()
		}
	}))
	defer provider.Close()

	rc := &RoutingConfig{
		Providers: map[string]Provider{
			"anthropic": {APIBase: provider.URL + "/v1/"},
		},
		Models: map[string]Model{
			"claude-sonnet": {Provider: "anthropic", ProviderModel: "claude-sonnet-4-20250514"},
		},
	}

	auditDir := t.TempDir()
	audit := NewAuditLogger(auditDir, "test-agent")
	defer audit.Close()

	lh := NewLLMHandler(rc, provider.URL, audit)

	body := `{"model":"claude-sonnet","messages":[{"role":"user","content":"hi"}],"stream":true}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	lh.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	output := rr.Body.String()
	if !strings.Contains(output, "chat.completion.chunk") {
		t.Error("should contain OpenAI chunk format")
	}
	if !strings.Contains(output, "Hello") {
		t.Error("should contain streamed content")
	}
	if !strings.Contains(output, "[DONE]") {
		t.Error("should end with [DONE]")
	}
}
