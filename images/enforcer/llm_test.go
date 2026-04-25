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
			"provider-a": {
				APIBase: "PLACEHOLDER", // replaced per test
			},
		},
		Models: map[string]Model{
			"standard": {
				Provider:                 "provider-a",
				ProviderModel:            "provider-a-standard",
				Capabilities:             []string{"tools", "vision", "streaming"},
				ProviderToolCapabilities: []string{capProviderWebSearch},
				CostIn:                   3.0,
				CostOut:                  15.0,
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
		if model != "provider-a-standard" {
			t.Errorf("expected rewritten model, got: %s", model)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"id":"msg_test","model":"provider-a-standard","usage":{"input_tokens":10,"output_tokens":50}}`)
	}))
	defer provider.Close()

	rc := testRoutingConfig()
	rc.Providers["provider-a"] = Provider{
		APIBase: provider.URL + "/v1/",
	}

	auditDir := t.TempDir()
	audit := NewAuditLogger(auditDir, "test-agent")
	defer audit.Close()

	// Use provider directly (no egress proxy in test)
	lh := NewLLMHandler(rc, provider.URL, audit)

	body := `{"model":"standard","messages":[{"role":"user","content":"hello"}]}`
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
	if resp["model"] != "provider-a-standard" {
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
	rc.Providers["provider-a"] = Provider{
		APIBase: provider.URL + "/v1/",
	}

	auditDir := t.TempDir()
	audit := NewAuditLogger(auditDir, "test-agent")
	defer audit.Close()

	lh := NewLLMHandler(rc, provider.URL, audit)

	body := `{"model":"standard","messages":[{"role":"user","content":"test"}]}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	lh.ServeHTTP(rr, req)

	if receivedModel != "provider-a-standard" {
		t.Errorf("expected provider model in body, got: %s", receivedModel)
	}
}

func TestLLMResponsesEndpointForwarded(t *testing.T) {
	var receivedPath string
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"id":"resp_test","usage":{"input_tokens":5,"output_tokens":7}}`)
	}))
	defer provider.Close()

	rc := testRoutingConfig()
	rc.Providers["provider-a"] = Provider{
		APIBase: provider.URL + "/v1/",
	}

	auditDir := t.TempDir()
	audit := NewAuditLogger(auditDir, "test-agent")
	defer audit.Close()

	lh := NewLLMHandler(rc, provider.URL, audit)
	lh.SetProviderToolPolicy(&ProviderToolPolicy{Granted: map[string]bool{capProviderWebSearch: true}})

	body := `{"model":"standard","input":"what changed today?","tools":[{"type":"web_search"}]}`
	req := httptest.NewRequest("POST", "/v1/responses", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	lh.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	if receivedPath != "/v1/responses" {
		t.Fatalf("expected /v1/responses upstream path, got %q", receivedPath)
	}
}

func TestLLMGeminiNativeTranslated(t *testing.T) {
	var receivedPath string
	var received map[string]interface{}
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedPath = r.URL.Path
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &received); err != nil {
			t.Fatalf("unmarshal request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"candidates":[{"content":{"parts":[{"text":"grounded"}],"role":"model"},"groundingMetadata":{"webSearchQueries":["q"],"groundingChunks":[{"web":{"uri":"https://example.com","title":"Example"}}]}}],"usageMetadata":{"promptTokenCount":3,"candidatesTokenCount":4,"totalTokenCount":7}}`)
	}))
	defer provider.Close()

	rc := &RoutingConfig{
		Providers: map[string]Provider{
			"google": {APIBase: provider.URL + "/v1beta", APIFormat: "gemini"},
		},
		Models: map[string]Model{
			"gemini-flash": {
				Provider:                 "google",
				ProviderModel:            "gemini-2.5-flash",
				Capabilities:             []string{"tools"},
				ProviderToolCapabilities: []string{capProviderWebSearch},
			},
		},
	}
	auditDir := t.TempDir()
	audit := NewAuditLogger(auditDir, "test-agent")
	defer audit.Close()

	lh := NewLLMHandler(rc, provider.URL, audit)
	lh.SetProviderToolPolicy(&ProviderToolPolicy{Granted: map[string]bool{capProviderWebSearch: true}})

	body := `{"model":"gemini-flash","messages":[{"role":"user","content":"search"}],"tools":[{"type":"web_search"}]}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	lh.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	if receivedPath != "/v1beta/models/gemini-2.5-flash:generateContent" {
		t.Fatalf("received path = %q", receivedPath)
	}
	if received["model"] != nil {
		t.Fatalf("native Gemini request should not include model: %#v", received)
	}
	tools := received["tools"].([]interface{})
	if _, ok := tools[0].(map[string]interface{})["google_search"]; !ok {
		t.Fatalf("web_search was not translated to google_search: %#v", tools)
	}
	if !strings.Contains(rr.Body.String(), "grounded") {
		t.Fatalf("response not translated: %s", rr.Body.String())
	}
}

func TestLLMGeminiNativeStreamTranslated(t *testing.T) {
	var receivedPath, receivedQuery string
	var received map[string]interface{}
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedPath = r.URL.Path
		receivedQuery = r.URL.RawQuery
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &received); err != nil {
			t.Fatalf("unmarshal request: %v", err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)
		fmt.Fprint(w, "data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"hello \"}],\"role\":\"model\"}}]}\n\n")
		flusher.Flush()
		fmt.Fprint(w, "data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"world\"}],\"role\":\"model\"},\"groundingMetadata\":{\"webSearchQueries\":[\"q\"],\"groundingChunks\":[{\"web\":{\"uri\":\"https://example.com\"}}]}}],\"usageMetadata\":{\"promptTokenCount\":3,\"candidatesTokenCount\":2,\"totalTokenCount\":5}}\n\n")
		flusher.Flush()
	}))
	defer provider.Close()

	rc := &RoutingConfig{
		Providers: map[string]Provider{
			"google": {APIBase: provider.URL + "/v1beta", APIFormat: "gemini"},
		},
		Models: map[string]Model{
			"gemini-flash": {
				Provider:                 "google",
				ProviderModel:            "gemini-2.5-flash",
				Capabilities:             []string{"tools", "streaming"},
				ProviderToolCapabilities: []string{capProviderWebSearch},
			},
		},
	}
	auditDir := t.TempDir()
	audit := NewAuditLogger(auditDir, "test-agent")
	defer audit.Close()

	lh := NewLLMHandler(rc, provider.URL, audit)
	lh.SetProviderToolPolicy(&ProviderToolPolicy{Granted: map[string]bool{capProviderWebSearch: true}})

	body := `{"model":"gemini-flash","messages":[{"role":"user","content":"search"}],"tools":[{"type":"web_search"}],"stream":true}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	lh.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	if receivedPath != "/v1beta/models/gemini-2.5-flash:streamGenerateContent" {
		t.Fatalf("received path = %q", receivedPath)
	}
	if receivedQuery != "alt=sse" {
		t.Fatalf("received query = %q", receivedQuery)
	}
	if received["stream"] != nil || received["model"] != nil {
		t.Fatalf("native Gemini request should not include OpenAI stream/model fields: %#v", received)
	}
	if !strings.Contains(rr.Body.String(), `"content":"hello "`) || !strings.Contains(rr.Body.String(), `"content":"world"`) {
		t.Fatalf("stream response not translated: %s", rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), `"usage"`) || !strings.Contains(rr.Body.String(), "data: [DONE]") {
		t.Fatalf("stream did not include final usage and done marker: %s", rr.Body.String())
	}
}

func TestLLMGeminiNativeResponsesTranslated(t *testing.T) {
	var receivedPath string
	var received map[string]interface{}
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedPath = r.URL.Path
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &received); err != nil {
			t.Fatalf("unmarshal request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"candidates":[{"content":{"parts":[{"text":"responses grounded"}],"role":"model"}}],"usageMetadata":{"promptTokenCount":3,"candidatesTokenCount":4,"totalTokenCount":7}}`)
	}))
	defer provider.Close()

	rc := &RoutingConfig{
		Providers: map[string]Provider{
			"google": {APIBase: provider.URL + "/v1beta", APIFormat: "gemini"},
		},
		Models: map[string]Model{
			"gemini-flash": {
				Provider:                 "google",
				ProviderModel:            "gemini-2.5-flash",
				Capabilities:             []string{"tools"},
				ProviderToolCapabilities: []string{capProviderWebSearch},
			},
		},
	}
	auditDir := t.TempDir()
	audit := NewAuditLogger(auditDir, "test-agent")
	defer audit.Close()

	lh := NewLLMHandler(rc, provider.URL, audit)
	lh.SetProviderToolPolicy(&ProviderToolPolicy{Granted: map[string]bool{capProviderWebSearch: true}})

	body := `{"model":"gemini-flash","input":[{"role":"user","content":[{"type":"input_text","text":"search"}]}],"tools":[{"type":"web_search"}]}`
	req := httptest.NewRequest("POST", "/v1/responses", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	lh.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	if receivedPath != "/v1beta/models/gemini-2.5-flash:generateContent" {
		t.Fatalf("received path = %q", receivedPath)
	}
	if received["model"] != nil || received["input"] != nil {
		t.Fatalf("native Gemini request should not include model/input fields: %#v", received)
	}
	if !strings.Contains(rr.Body.String(), `"object":"response"`) || !strings.Contains(rr.Body.String(), "responses grounded") {
		t.Fatalf("response not translated: %s", rr.Body.String())
	}
}

func TestLLMGeminiNativeResponsesStreamTranslated(t *testing.T) {
	var receivedPath, receivedQuery string
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedPath = r.URL.Path
		receivedQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)
		fmt.Fprint(w, "data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"alpha\"}],\"role\":\"model\"}}]}\n\n")
		flusher.Flush()
		fmt.Fprint(w, "data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\" beta\"}],\"role\":\"model\"}}],\"usageMetadata\":{\"promptTokenCount\":1,\"candidatesTokenCount\":2,\"totalTokenCount\":3}}\n\n")
		flusher.Flush()
	}))
	defer provider.Close()

	rc := &RoutingConfig{
		Providers: map[string]Provider{
			"google": {APIBase: provider.URL + "/v1beta", APIFormat: "gemini"},
		},
		Models: map[string]Model{
			"gemini-flash": {
				Provider:      "google",
				ProviderModel: "gemini-2.5-flash",
				Capabilities:  []string{"streaming"},
			},
		},
	}
	auditDir := t.TempDir()
	audit := NewAuditLogger(auditDir, "test-agent")
	defer audit.Close()

	lh := NewLLMHandler(rc, provider.URL, audit)

	body := `{"model":"gemini-flash","input":"stream this","stream":true}`
	req := httptest.NewRequest("POST", "/v1/responses", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	lh.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	if receivedPath != "/v1beta/models/gemini-2.5-flash:streamGenerateContent" || receivedQuery != "alt=sse" {
		t.Fatalf("upstream = %q?%s", receivedPath, receivedQuery)
	}
	if !strings.Contains(rr.Body.String(), "event: response.output_text.delta") || !strings.Contains(rr.Body.String(), "event: response.completed") {
		t.Fatalf("responses stream not translated: %s", rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "alpha beta") {
		t.Fatalf("completed event missing full text: %s", rr.Body.String())
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

func TestLLMProviderToolDeniedWithoutGrant(t *testing.T) {
	var llmCalled bool
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		llmCalled = true
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"id":"msg_test"}`)
	}))
	defer provider.Close()

	rc := testRoutingConfig()
	rc.Providers["provider-a"] = Provider{
		APIBase: provider.URL + "/v1/",
	}

	auditDir := t.TempDir()
	audit := NewAuditLogger(auditDir, "test-agent")
	defer audit.Close()

	lh := NewLLMHandler(rc, provider.URL, audit)

	body := `{"model":"standard","messages":[],"tools":[{"type":"web_search_preview"}]}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	lh.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", rr.Code, rr.Body.String())
	}
	if llmCalled {
		t.Fatal("provider should not be called when provider tool is denied")
	}
	if !strings.Contains(rr.Body.String(), "provider_tool_denied") {
		t.Fatalf("expected provider_tool_denied response, got %s", rr.Body.String())
	}
}

func TestLLMProviderToolAllowedWithGrant(t *testing.T) {
	var llmCalled bool
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		llmCalled = true
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"id":"msg_test"}`)
	}))
	defer provider.Close()

	rc := testRoutingConfig()
	rc.Providers["provider-a"] = Provider{
		APIBase: provider.URL + "/v1/",
	}

	auditDir := t.TempDir()
	audit := NewAuditLogger(auditDir, "test-agent")

	lh := NewLLMHandler(rc, provider.URL, audit)
	lh.SetProviderToolPolicy(&ProviderToolPolicy{Granted: map[string]bool{capProviderWebSearch: true}})

	body := `{"model":"standard","messages":[],"tools":[{"type":"web_search_preview"}]}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	lh.ServeHTTP(rr, req)
	audit.Close()

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	if !llmCalled {
		t.Fatal("provider should be called when provider tool is granted")
	}
	var allowedEntry *AuditEntry
	var boundaryEntry *AuditEntry
	entries := readAuditEntries(t, auditDir)
	for i := range entries {
		entry := entries[i]
		switch entry.Type {
		case "PROVIDER_TOOL_ALLOWED":
			allowedEntry = &entry
		case securityScanNA:
			boundaryEntry = &entry
		}
	}
	if allowedEntry == nil {
		t.Fatal("missing PROVIDER_TOOL_ALLOWED audit entry")
	}
	if allowedEntry.ProviderModel != "provider-a-standard" {
		t.Fatalf("provider model missing on allowed event: %#v", allowedEntry)
	}
	if boundaryEntry == nil {
		t.Fatal("missing provider tool security boundary audit entry")
	}
	if boundaryEntry.ScanSurface != "provider_tool_content" || boundaryEntry.ScanAction != "not_applicable" {
		t.Fatalf("wrong boundary scan fields: %#v", boundaryEntry)
	}
	if boundaryEntry.Extra["security_boundary"] != "provider_hosted_raw_content_not_visible" {
		t.Fatalf("missing boundary reason: %#v", boundaryEntry.Extra)
	}
}

func TestLLMHarnessedProviderToolRejectedBeforeProvider(t *testing.T) {
	var llmCalled bool
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		llmCalled = true
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"id":"resp_test","output":[{"type":"computer_call","call_id":"call_1","action":{"type":"click","x":10,"y":20}}],"usage":{"input_tokens":5,"output_tokens":7}}`)
	}))
	defer provider.Close()

	rc := testRoutingConfig()
	rc.Providers["provider-a"] = Provider{
		APIBase: provider.URL + "/v1/",
	}
	model := rc.Models["standard"]
	model.ProviderToolCapabilities = append(model.ProviderToolCapabilities, capProviderComputerUse)
	rc.Models["standard"] = model

	auditDir := t.TempDir()
	audit := NewAuditLogger(auditDir, "test-agent")

	lh := NewLLMHandler(rc, provider.URL, audit)
	lh.SetProviderToolPolicy(&ProviderToolPolicy{Granted: map[string]bool{capProviderComputerUse: true}})

	body := `{"model":"standard","input":"use the computer","tools":[{"type":"computer_use_preview"}]}`
	req := httptest.NewRequest("POST", "/v1/responses", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Correlation-Id", "corr-harness")
	rr := httptest.NewRecorder()
	lh.ServeHTTP(rr, req)
	audit.Close()

	if rr.Code != http.StatusNotImplemented {
		t.Fatalf("expected 501, got %d: %s", rr.Code, rr.Body.String())
	}
	if llmCalled {
		t.Fatal("provider should not be called for unavailable harnessed provider tools")
	}
	if !strings.Contains(rr.Body.String(), "provider_tool_harness_unavailable") {
		t.Fatalf("expected provider_tool_harness_unavailable response, got %s", rr.Body.String())
	}

	today := time.Now().UTC().Format("2006-01-02")
	data, err := os.ReadFile(filepath.Join(auditDir, "enforcer-"+today+".jsonl"))
	if err != nil {
		t.Fatalf("read audit: %v", err)
	}
	var harnessEntry *AuditEntry
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		var entry AuditEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("unmarshal audit: %v", err)
		}
		if entry.Type == "PROVIDER_TOOL_HARNESS_UNAVAILABLE" {
			copied := entry
			harnessEntry = &copied
		}
	}
	if harnessEntry == nil {
		t.Fatalf("missing PROVIDER_TOOL_HARNESS_UNAVAILABLE audit entry in %s", string(data))
	}
	if harnessEntry.Extra["provider_tool_capability"] != capProviderComputerUse {
		t.Fatalf("harness capability missing: %#v", harnessEntry.Extra)
	}
	if harnessEntry.Extra["provider_tool_execution_modes"] != providerToolExecutionAgencyHarnessed {
		t.Fatalf("execution mode missing: %#v", harnessEntry.Extra)
	}
}

func TestLLMHarnessedProviderToolTranslated(t *testing.T) {
	var received map[string]interface{}
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Fatalf("decode provider request: %v", err)
		}
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"id":"msg_test","usage":{"input_tokens":5,"output_tokens":7}}`)
	}))
	defer provider.Close()

	rc := testRoutingConfig()
	rc.Providers["provider-a"] = Provider{
		APIBase: provider.URL + "/v1/",
	}

	auditDir := t.TempDir()
	audit := NewAuditLogger(auditDir, "test-agent")

	lh := NewLLMHandler(rc, provider.URL, audit)
	lh.SetProviderToolPolicy(&ProviderToolPolicy{Granted: map[string]bool{capProviderShell: true}})

	body := `{"model":"standard","messages":[],"tools":[{"type":"shell"}]}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Correlation-Id", "corr-shell-harness")
	rr := httptest.NewRecorder()
	lh.ServeHTTP(rr, req)
	audit.Close()

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	rawTools, ok := received["tools"].([]interface{})
	if !ok || len(rawTools) != 1 {
		t.Fatalf("expected one translated tool, got %#v", received["tools"])
	}
	tool := rawTools[0].(map[string]interface{})
	fn, _ := tool["function"].(map[string]interface{})
	if fn["name"] != "execute_command" {
		t.Fatalf("expected execute_command harness, got %#v", rawTools)
	}

	today := time.Now().UTC().Format("2006-01-02")
	data, err := os.ReadFile(filepath.Join(auditDir, "enforcer-"+today+".jsonl"))
	if err != nil {
		t.Fatalf("read audit: %v", err)
	}
	var translatedEntry *AuditEntry
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		var entry AuditEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("unmarshal audit: %v", err)
		}
		if entry.Type == "PROVIDER_TOOL_HARNESS_TRANSLATED" {
			copied := entry
			translatedEntry = &copied
		}
		if entry.Type == "PROVIDER_TOOL_ALLOWED" {
			t.Fatalf("translated harness should not be audited as provider-hosted allowed tool: %#v", entry)
		}
	}
	if translatedEntry == nil {
		t.Fatalf("missing PROVIDER_TOOL_HARNESS_TRANSLATED audit entry in %s", string(data))
	}
	if translatedEntry.Extra["provider_tool_harness_capabilities"] != capProviderShell {
		t.Fatalf("harness capability missing: %#v", translatedEntry.Extra)
	}
}

func TestLLMProviderToolUnsupportedByModel(t *testing.T) {
	var llmCalled bool
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		llmCalled = true
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"id":"msg_test"}`)
	}))
	defer provider.Close()

	rc := testRoutingConfig()
	rc.Providers["provider-a"] = Provider{
		APIBase: provider.URL + "/v1/",
	}
	model := rc.Models["standard"]
	model.ProviderToolCapabilities = nil
	rc.Models["standard"] = model

	auditDir := t.TempDir()
	audit := NewAuditLogger(auditDir, "test-agent")
	defer audit.Close()

	lh := NewLLMHandler(rc, provider.URL, audit)
	lh.SetProviderToolPolicy(&ProviderToolPolicy{Granted: map[string]bool{capProviderWebSearch: true}})

	body := `{"model":"standard","messages":[],"tools":[{"type":"web_search_preview"}]}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	lh.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d: %s", rr.Code, rr.Body.String())
	}
	if llmCalled {
		t.Fatal("provider should not be called when provider tool is unsupported")
	}
	if !strings.Contains(rr.Body.String(), "provider_tool_unsupported") {
		t.Fatalf("expected provider_tool_unsupported response, got %s", rr.Body.String())
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
	rc.Providers["provider-a"] = Provider{
		APIBase: provider.URL + "/v1/",
	}

	auditDir := t.TempDir()
	audit := NewAuditLogger(auditDir, "test-agent")
	defer audit.Close()

	lh := NewLLMHandler(rc, provider.URL, audit)

	body := `{"model":"standard","messages":[]}`
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
	var receivedStreamOptions map[string]interface{}
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req map[string]interface{}
		if err := json.Unmarshal(body, &req); err != nil {
			t.Fatalf("unmarshal request: %v", err)
		}
		receivedStreamOptions, _ = req["stream_options"].(map[string]interface{})
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
	rc.Providers["provider-a"] = Provider{
		APIBase: provider.URL + "/v1/",
	}

	auditDir := t.TempDir()
	audit := NewAuditLogger(auditDir, "test-agent")
	defer audit.Close()

	lh := NewLLMHandler(rc, provider.URL, audit)

	body := `{"model":"standard","messages":[],"stream":true}`
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
	if got, _ := receivedStreamOptions["include_usage"].(bool); !got {
		t.Fatalf("expected stream_options.include_usage=true, got %#v", receivedStreamOptions)
	}
}

func TestLLMStreamingProviderToolAuditExtra(t *testing.T) {
	var received map[string]interface{}
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &received); err != nil {
			t.Fatalf("unmarshal request: %v", err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)
		fmt.Fprint(w, "data: {\"type\":\"response.web_search_call.completed\",\"annotations\":[{\"type\":\"url_citation\",\"url\":\"https://example.com/source\"}],\"usage\":{\"input_tokens\":3,\"output_tokens\":4}}\n\n")
		flusher.Flush()
		fmt.Fprint(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	defer provider.Close()

	rc := testRoutingConfig()
	rc.Providers["provider-a"] = Provider{APIBase: provider.URL + "/v1/"}

	auditDir := t.TempDir()
	audit := NewAuditLogger(auditDir, "test-agent")

	lh := NewLLMHandler(rc, provider.URL, audit)
	lh.SetProviderToolPolicy(&ProviderToolPolicy{Granted: map[string]bool{capProviderWebSearch: true}})

	body := `{"model":"standard","input":"what changed today?","tools":[{"type":"web_search"}],"stream":true}`
	req := httptest.NewRequest("POST", "/v1/responses", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	lh.ServeHTTP(rr, req)
	audit.Close()

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	if _, ok := received["stream_options"]; ok {
		t.Fatalf("Responses streaming request should not include chat stream_options: %#v", received["stream_options"])
	}

	today := time.Now().UTC().Format("2006-01-02")
	data, err := os.ReadFile(filepath.Join(auditDir, "enforcer-"+today+".jsonl"))
	if err != nil {
		t.Fatalf("read audit: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	var entry AuditEntry
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &entry); err != nil {
		t.Fatalf("unmarshal audit: %v", err)
	}
	if entry.Type != "LLM_DIRECT_STREAM" {
		t.Fatalf("audit type = %s, want LLM_DIRECT_STREAM", entry.Type)
	}
	if entry.Extra["provider_tool_call_count"] != "1" {
		t.Fatalf("provider_tool_call_count = %q", entry.Extra["provider_tool_call_count"])
	}
	if entry.Extra["provider_response_tool_types"] != "response.web_search_call.completed" {
		t.Fatalf("provider_response_tool_types = %q", entry.Extra["provider_response_tool_types"])
	}
	if entry.Extra["provider_source_urls"] != "https://example.com/source" {
		t.Fatalf("provider_source_urls = %q", entry.Extra["provider_source_urls"])
	}
}

func TestLLMAnthropicStreamingProviderToolAuditExtra(t *testing.T) {
	var received map[string]interface{}
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &received); err != nil {
			t.Fatalf("unmarshal request: %v", err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)
		fmt.Fprint(w, "data: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_test\",\"model\":\"provider-a-standard\",\"usage\":{\"input_tokens\":3}}}\n\n")
		flusher.Flush()
		fmt.Fprint(w, "data: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"server_tool_use\",\"id\":\"srvtoolu_1\",\"name\":\"web_search\"}}\n\n")
		flusher.Flush()
		fmt.Fprint(w, "data: {\"type\":\"content_block_start\",\"index\":1,\"content_block\":{\"type\":\"web_search_tool_result\",\"tool_use_id\":\"srvtoolu_1\",\"content\":[{\"type\":\"web_search_result\",\"url\":\"https://example.com/source\",\"title\":\"Source\"}]}}\n\n")
		flusher.Flush()
		fmt.Fprint(w, "data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":4}}\n\n")
		flusher.Flush()
		fmt.Fprint(w, "data: {\"type\":\"message_stop\"}\n\n")
		flusher.Flush()
	}))
	defer provider.Close()

	rc := testRoutingConfig()
	rc.Providers["anthropic"] = Provider{APIBase: provider.URL + "/v1/", APIFormat: "anthropic"}
	rc.Models["standard"] = Model{
		Provider:                 "anthropic",
		ProviderModel:            "provider-a-standard",
		Capabilities:             []string{"tools", "streaming"},
		ProviderToolCapabilities: []string{capProviderWebSearch},
	}

	auditDir := t.TempDir()
	audit := NewAuditLogger(auditDir, "test-agent")

	lh := NewLLMHandler(rc, provider.URL, audit)
	lh.SetProviderToolPolicy(&ProviderToolPolicy{Granted: map[string]bool{capProviderWebSearch: true}})

	body := `{"model":"standard","messages":[{"role":"user","content":"what changed today?"}],"tools":[{"type":"web_search"}],"stream":true}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	lh.ServeHTTP(rr, req)
	audit.Close()

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), `"object":"agency.provider_tool_evidence"`) {
		t.Fatalf("stream did not include provider tool evidence chunk: %s", rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), `"provider_response_tool_types":"server_tool_use,web_search_result,web_search_tool_result"`) {
		t.Fatalf("provider tool evidence chunk did not include response tool types: %s", rr.Body.String())
	}
	tools := received["tools"].([]interface{})
	tool := tools[0].(map[string]interface{})
	if tool["type"] != defaultAnthropicWebSearchToolType {
		t.Fatalf("generic Anthropic web_search not normalized: %#v", tool)
	}

	today := time.Now().UTC().Format("2006-01-02")
	data, err := os.ReadFile(filepath.Join(auditDir, "enforcer-"+today+".jsonl"))
	if err != nil {
		t.Fatalf("read audit: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	var entry AuditEntry
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &entry); err != nil {
		t.Fatalf("unmarshal audit: %v", err)
	}
	if entry.Type != "LLM_DIRECT_STREAM" {
		t.Fatalf("audit type = %s, want LLM_DIRECT_STREAM", entry.Type)
	}
	if entry.Extra["provider_tool_call_count"] != "1" {
		t.Fatalf("provider_tool_call_count = %q", entry.Extra["provider_tool_call_count"])
	}
	if entry.Extra["provider_response_tool_types"] != "server_tool_use,web_search_result,web_search_tool_result" {
		t.Fatalf("provider_response_tool_types = %q", entry.Extra["provider_response_tool_types"])
	}
	if entry.Extra["provider_source_urls"] != "https://example.com/source" {
		t.Fatalf("provider_source_urls = %q", entry.Extra["provider_source_urls"])
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
	rc.Providers["provider-a"] = Provider{
		APIBase: provider.URL + "/v1/",
	}

	auditDir := t.TempDir()
	audit := NewAuditLogger(auditDir, "test-agent")
	defer audit.Close()

	lh := NewLLMHandler(rc, provider.URL, audit)

	body := `{"model":"standard","messages":[]}`
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
	rc.Providers["provider-a"] = Provider{
		APIBase: provider.URL + "/v1/",
	}

	auditDir := t.TempDir()
	audit := NewAuditLogger(auditDir, "test-agent")
	defer audit.Close()

	lh := NewLLMHandler(rc, provider.URL, audit)

	body := `{"model":"standard","messages":[]}`
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
	rc.Providers["provider-a"] = Provider{
		APIBase: provider.URL + "/v1/",
	}

	auditDir := t.TempDir()
	audit := NewAuditLogger(auditDir, "test-agent")

	lh := NewLLMHandler(rc, provider.URL, audit)

	body := `{"model":"standard","messages":[]}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Correlation-Id", "corr-test")
	rr := httptest.NewRecorder()
	lh.ServeHTTP(rr, req)

	audit.Close()

	var entry AuditEntry
	found := false
	for _, candidate := range readAuditEntries(t, auditDir) {
		if candidate.Type == "LLM_DIRECT" {
			entry = candidate
			found = true
			break
		}
	}
	if !found {
		t.Fatal("missing LLM_DIRECT audit entry")
	}

	if entry.Type != "LLM_DIRECT" {
		t.Errorf("wrong type: %s", entry.Type)
	}
	if entry.Model != "standard" {
		t.Errorf("wrong model: %s", entry.Model)
	}
	if entry.ProviderModel != "provider-a-standard" {
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

func TestExtractStreamUsageGeminiUsageMetadata(t *testing.T) {
	chunk := "data: {\"usageMetadata\":{\"promptTokenCount\":321,\"candidatesTokenCount\":123}}\n\n"
	in, out := extractStreamUsage(chunk)
	if in != 321 {
		t.Errorf("expected promptTokenCount 321, got %d", in)
	}
	if out != 123 {
		t.Errorf("expected candidatesTokenCount 123, got %d", out)
	}
}

func TestExtractUsageCountsGeminiUsageMetadata(t *testing.T) {
	obj := map[string]interface{}{
		"usageMetadata": map[string]interface{}{
			"promptTokenCount":     float64(444),
			"candidatesTokenCount": float64(222),
		},
	}
	in, out := extractUsageCounts(obj)
	if in != 444 {
		t.Errorf("expected 444 input tokens, got %d", in)
	}
	if out != 222 {
		t.Errorf("expected 222 output tokens, got %d", out)
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
		fmt.Fprint(w, `{"id":"msg_test","type":"message","role":"assistant","content":[{"type":"text","text":"Hi!"}],"stop_reason":"end_turn","model":"provider-a-standard","usage":{"input_tokens":10,"output_tokens":5}}`)
	}))
	defer provider.Close()

	rc := &RoutingConfig{
		Providers: map[string]Provider{
			"anthropic": {APIBase: provider.URL + "/v1/", APIFormat: "anthropic"},
		},
		Models: map[string]Model{
			"standard": {Provider: "anthropic", ProviderModel: "provider-a-standard", Capabilities: []string{"tools", "vision", "streaming"}},
		},
	}

	auditDir := t.TempDir()
	audit := NewAuditLogger(auditDir, "test-agent")
	defer audit.Close()
	lh := NewLLMHandler(rc, provider.URL, audit)

	body := `{"model":"standard","messages":[{"role":"system","content":"Be helpful"},{"role":"user","content":"hi"}]}`
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
		fmt.Fprint(w, `{"id":"msg_test","type":"message","role":"assistant","content":[{"type":"text","text":"Hi"}],"stop_reason":"end_turn","model":"provider-a-standard","usage":{"input_tokens":10,"output_tokens":5}}`)
	}))
	defer provider.Close()

	rc := &RoutingConfig{
		Providers: map[string]Provider{
			"anthropic": {APIBase: provider.URL + "/v1/", APIFormat: "anthropic"},
		},
		Models: map[string]Model{
			"standard": {Provider: "anthropic", ProviderModel: "provider-a-standard", Capabilities: []string{"tools", "vision", "streaming"}},
		},
	}

	auditDir := t.TempDir()
	audit := NewAuditLogger(auditDir, "test-agent")
	defer audit.Close()

	rl := NewRateLimiter(50, 60)
	lh := NewLLMHandler(rc, provider.URL, audit)
	lh.SetRateLimiter(rl, "test-agent")

	body := `{"model":"standard","messages":[{"role":"user","content":"hi"}]}`
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
		fmt.Fprint(w, `{"id":"msg_test","type":"message","role":"assistant","content":[{"type":"text","text":"Hi"}],"stop_reason":"end_turn","model":"provider-a-standard","usage":{"input_tokens":10,"output_tokens":5}}`)
	}))
	defer provider.Close()

	rc := &RoutingConfig{
		Providers: map[string]Provider{
			"anthropic": {APIBase: provider.URL + "/v1/", APIFormat: "anthropic"},
		},
		Models: map[string]Model{
			"standard": {Provider: "anthropic", ProviderModel: "provider-a-standard", Capabilities: []string{"tools", "vision", "streaming"}},
		},
	}

	auditDir := t.TempDir()
	audit := NewAuditLogger(auditDir, "test-agent")
	defer audit.Close()

	// Create rate limiter with limit of 1 RPM, then exhaust it
	rl := NewRateLimiter(1, 1)    // 1 rpm, 1s window
	rl.RecordRequest("anthropic") // exhaust the single slot

	lh := NewLLMHandler(rc, provider.URL, audit)
	lh.SetRateLimiter(rl, "test-agent")

	body := `{"model":"standard","messages":[{"role":"user","content":"hi"}]}`
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

func TestStepIndexIncrement(t *testing.T) {
	lh := &LLMHandler{stepCounters: make(map[string]int)}
	if lh.nextStepIndex("t1") != 1 {
		t.Error("expected 1")
	}
	if lh.nextStepIndex("t1") != 2 {
		t.Error("expected 2")
	}
	if lh.nextStepIndex("t1") != 3 {
		t.Error("expected 3")
	}
	if lh.nextStepIndex("t2") != 1 {
		t.Error("expected 1 for new task")
	}
}

func TestLLMAnthropicStreaming(t *testing.T) {
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)

		events := []string{
			"event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"model\":\"provider-a-standard\",\"usage\":{\"input_tokens\":50,\"cache_read_input_tokens\":40}}}",
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
			"anthropic": {APIBase: provider.URL + "/v1/", APIFormat: "anthropic"},
		},
		Models: map[string]Model{
			"standard": {Provider: "anthropic", ProviderModel: "provider-a-standard", Capabilities: []string{"tools", "vision", "streaming"}},
		},
	}

	auditDir := t.TempDir()
	audit := NewAuditLogger(auditDir, "test-agent")
	defer audit.Close()

	lh := NewLLMHandler(rc, provider.URL, audit)

	body := `{"model":"standard","messages":[{"role":"user","content":"hi"}],"stream":true}`
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
