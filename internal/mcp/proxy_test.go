package mcp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// newTestProxy creates a Proxy pointed at the given test server URL with no token.
func newTestProxy(serverURL string) *Proxy {
	return NewProxy(serverURL, "")
}

func TestProxyTools(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/mcp/tools" || r.Method != http.MethodGet {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"tools": []interface{}{
				map[string]interface{}{"name": "agency_list", "description": "List agents"},
			},
		})
	}))
	defer srv.Close()

	proxy := newTestProxy(srv.URL)
	tools, err := proxy.Tools()
	if err != nil {
		t.Fatalf("Tools() error: %v", err)
	}
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(tools))
	}
	tool, ok := tools[0].(map[string]interface{})
	if !ok {
		t.Fatalf("tool is not a map: %T", tools[0])
	}
	if tool["name"] != "agency_list" {
		t.Errorf("tool name = %v, want agency_list", tool["name"])
	}
}

func TestProxyToolsEmpty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"tools": []interface{}{}})
	}))
	defer srv.Close()

	proxy := newTestProxy(srv.URL)
	tools, err := proxy.Tools()
	if err != nil {
		t.Fatalf("Tools() error: %v", err)
	}
	if len(tools) != 0 {
		t.Errorf("expected 0 tools, got %d", len(tools))
	}
}

func TestProxyCall(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/mcp/call" || r.Method != http.MethodPost {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		var req map[string]interface{}
		json.NewDecoder(r.Body).Decode(&req)
		if req["name"] != "agency_list" {
			http.Error(w, "wrong tool", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(ToolResult{
			Content: []ContentBlock{{Type: "text", Text: "agent1\nagent2"}},
			IsError: false,
		})
	}))
	defer srv.Close()

	proxy := newTestProxy(srv.URL)
	result, err := proxy.Call("agency_list", map[string]interface{}{})
	if err != nil {
		t.Fatalf("Call() error: %v", err)
	}
	if result.IsError {
		t.Error("expected IsError=false")
	}
	if len(result.Content) != 1 {
		t.Fatalf("expected 1 content block, got %d", len(result.Content))
	}
	if result.Content[0].Text != "agent1\nagent2" {
		t.Errorf("content text = %q, want %q", result.Content[0].Text, "agent1\nagent2")
	}
}

func TestProxyCallGatewayUnreachable(t *testing.T) {
	// Point at a port nothing is listening on.
	proxy := NewProxy("http://127.0.0.1:19999", "")
	proxy.retryMaxElapsed = 500 * time.Millisecond
	proxy.retryInitial = 50 * time.Millisecond

	result, err := proxy.Call("agency_list", map[string]interface{}{})

	// Should NOT return an error — it surfaces the error in the ToolResult instead.
	if err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if !result.IsError {
		t.Error("expected IsError=true when gateway is unreachable")
	}
	if len(result.Content) == 0 {
		t.Error("expected non-empty content")
	}
}

func TestProxyTokenHeader(t *testing.T) {
	var capturedToken string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedToken = r.Header.Get("X-Agency-Token")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"tools": []interface{}{}})
	}))
	defer srv.Close()

	proxy := NewProxy(srv.URL, "test-token-abc")
	proxy.Tools()

	if capturedToken != "test-token-abc" {
		t.Errorf("X-Agency-Token = %q, want %q", capturedToken, "test-token-abc")
	}
}

func TestProxyCallSetsContentType(t *testing.T) {
	var capturedContentType string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedContentType = r.Header.Get("Content-Type")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(ToolResult{
			Content: []ContentBlock{{Type: "text", Text: "ok"}},
		})
	}))
	defer srv.Close()

	proxy := newTestProxy(srv.URL)
	proxy.Call("some_tool", nil)

	if capturedContentType != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", capturedContentType)
	}
}

func TestProxyRetriesOnConnectionRefused(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"tools": []interface{}{}})
	}))
	srvURL := srv.URL
	srv.Close()

	proxy := newTestProxy(srvURL)
	proxy.retryMaxElapsed = 500 * time.Millisecond
	proxy.retryInitial = 50 * time.Millisecond

	_, err := proxy.Tools()
	if err == nil {
		t.Fatal("expected error after retries exhausted")
	}
}

func TestProxyRetriesAndSucceeds(t *testing.T) {
	attempts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts <= 2 {
			http.Error(w, "bad gateway", http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"tools": []interface{}{
			map[string]interface{}{"name": "test_tool"},
		}})
	}))
	defer srv.Close()

	proxy := newTestProxy(srv.URL)
	proxy.retryMaxElapsed = 5 * time.Second
	proxy.retryInitial = 50 * time.Millisecond

	tools, err := proxy.Tools()
	if err != nil {
		t.Fatalf("expected success after retry, got: %v", err)
	}
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(tools))
	}
	if attempts != 3 {
		t.Errorf("expected 3 attempts, got %d", attempts)
	}
}

func TestProxyNoRetryOn400(t *testing.T) {
	attempts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		http.Error(w, "bad request", http.StatusBadRequest)
	}))
	defer srv.Close()

	proxy := newTestProxy(srv.URL)
	proxy.retryMaxElapsed = 2 * time.Second
	proxy.retryInitial = 50 * time.Millisecond

	_, err := proxy.get("/test")
	if err != nil {
		t.Fatalf("expected no error for 400, got: %v", err)
	}
	if attempts != 1 {
		t.Errorf("expected 1 attempt (no retry on 400), got %d", attempts)
	}
}

func TestProxyCallRetriesAndReturnsError(t *testing.T) {
	proxy := NewProxy("http://127.0.0.1:19999", "")
	proxy.retryMaxElapsed = 500 * time.Millisecond
	proxy.retryInitial = 50 * time.Millisecond

	result, err := proxy.Call("test_tool", map[string]interface{}{})
	if err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}
	if !result.IsError {
		t.Error("expected IsError=true")
	}
}

// TestProxyServerIntegration wires a Proxy into a Server and runs the full
// JSON-RPC loop against a mock gateway.
func TestProxyServerIntegration(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/mcp/tools":
			json.NewEncoder(w).Encode(map[string]interface{}{
				"tools": []interface{}{
					map[string]interface{}{"name": "agency_status", "description": "Get status"},
				},
			})
		case "/api/v1/mcp/call":
			json.NewEncoder(w).Encode(ToolResult{
				Content: []ContentBlock{{Type: "text", Text: "running"}},
			})
		default:
			http.Error(w, "not found", http.StatusNotFound)
		}
	}))
	defer srv.Close()

	proxy := newTestProxy(srv.URL)
	server := NewProxyServer(proxy)

	// tools/list
	resp := server.HandleMessage([]byte(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	if resp == nil || resp.Error != nil {
		t.Fatalf("tools/list failed: %+v", resp)
	}
	result, ok := resp.Result.(map[string]interface{})
	if !ok {
		t.Fatal("result is not a map")
	}
	tools, ok := result["tools"].([]interface{})
	if !ok || len(tools) != 1 {
		t.Errorf("expected 1 tool, got: %+v", result["tools"])
	}

	// tools/call
	resp = server.HandleMessage([]byte(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"agency_status","arguments":{}}}`))
	if resp == nil || resp.Error != nil {
		t.Fatalf("tools/call failed: %+v", resp)
	}
}
