package mcp

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// newTestServer creates a Server backed by a mock gateway that returns the
// provided tools slice and call result.
func newTestServer(tools []interface{}, callResult ToolResult) *Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/mcp/tools":
			json.NewEncoder(w).Encode(map[string]interface{}{"tools": tools})
		case "/api/v1/mcp/call":
			json.NewEncoder(w).Encode(callResult)
		default:
			http.Error(w, "not found", http.StatusNotFound)
		}
	}))
	// Note: the test server is not closed here — callers that need cleanup
	// should create the httptest.Server directly (see TestProxyServerIntegration).
	proxy := NewProxy(srv.URL, "")
	return NewProxyServer(proxy)
}

func TestHandleInitialize(t *testing.T) {
	// handleInitialize does not use the proxy — pass nil proxy.
	s := NewProxyServer(nil)
	raw := `{"jsonrpc":"2.0","id":1,"method":"initialize"}`
	resp := s.HandleMessage([]byte(raw))

	if resp == nil {
		t.Fatal("expected non-nil response")
	}
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}

	result, ok := resp.Result.(map[string]interface{})
	if !ok {
		t.Fatalf("result is not a map: %T", resp.Result)
	}

	if v := result["protocolVersion"]; v != "2024-11-05" {
		t.Errorf("protocolVersion = %v, want 2024-11-05", v)
	}

	caps, ok := result["capabilities"].(map[string]interface{})
	if !ok {
		t.Fatal("capabilities missing or wrong type")
	}
	if _, ok := caps["tools"]; !ok {
		t.Error("capabilities.tools missing")
	}

	info, ok := result["serverInfo"].(map[string]interface{})
	if !ok {
		t.Fatal("serverInfo missing or wrong type")
	}
	if info["name"] != "agency" {
		t.Errorf("serverInfo.name = %v, want agency", info["name"])
	}
	if info["version"] != "0.1.0" {
		t.Errorf("serverInfo.version = %v, want 0.1.0", info["version"])
	}
}

func TestHandleUnknownMethod(t *testing.T) {
	s := NewProxyServer(nil)
	raw := `{"jsonrpc":"2.0","id":2,"method":"bogus/method"}`
	resp := s.HandleMessage([]byte(raw))

	if resp == nil {
		t.Fatal("expected non-nil response")
	}
	if resp.Error == nil {
		t.Fatal("expected error response")
	}
	if resp.Error.Code != -32601 {
		t.Errorf("error code = %d, want -32601", resp.Error.Code)
	}
}

func TestNotificationReturnsNil(t *testing.T) {
	s := NewProxyServer(nil)
	// A notification has no "id" field.
	raw := `{"jsonrpc":"2.0","method":"notifications/initialized"}`
	resp := s.HandleMessage([]byte(raw))

	if resp != nil {
		t.Errorf("expected nil response for notification, got %+v", resp)
	}
}

func TestParseError(t *testing.T) {
	s := NewProxyServer(nil)
	raw := `{not valid json`
	resp := s.HandleMessage([]byte(raw))

	if resp == nil {
		t.Fatal("expected non-nil response for parse error")
	}
	if resp.Error == nil {
		t.Fatal("expected error response")
	}
	if resp.Error.Code != -32700 {
		t.Errorf("error code = %d, want -32700", resp.Error.Code)
	}
}

func TestRunWithIO(t *testing.T) {
	mockSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/mcp/tools":
			json.NewEncoder(w).Encode(map[string]interface{}{"tools": []interface{}{}})
		default:
			http.Error(w, "not found", http.StatusNotFound)
		}
	}))
	defer mockSrv.Close()

	proxy := NewProxy(mockSrv.URL, "")
	s := NewProxyServer(proxy)

	input := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize"}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
	}, "\n") + "\n"

	var out bytes.Buffer
	err := s.RunWithIO(strings.NewReader(input), &out)
	if err != nil {
		t.Fatalf("RunWithIO error: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	// We expect 2 responses (initialize + tools/list); the notification produces none.
	if len(lines) != 2 {
		t.Fatalf("expected 2 response lines, got %d: %v", len(lines), lines)
	}

	// Verify first response is initialize result.
	var resp1 Response
	if err := json.Unmarshal([]byte(lines[0]), &resp1); err != nil {
		t.Fatalf("unmarshal resp1: %v", err)
	}
	if resp1.Error != nil {
		t.Errorf("resp1 unexpected error: %+v", resp1.Error)
	}
}

func TestToolsCall(t *testing.T) {
	mockSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/mcp/call" {
			json.NewEncoder(w).Encode(ToolResult{
				Content: []ContentBlock{{Type: "text", Text: "result text"}},
				IsError: false,
			})
			return
		}
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer mockSrv.Close()

	proxy := NewProxy(mockSrv.URL, "")
	s := NewProxyServer(proxy)

	raw := `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"agency_list","arguments":{}}}`
	resp := s.HandleMessage([]byte(raw))

	if resp == nil {
		t.Fatal("expected non-nil response")
	}
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}

	b, _ := json.Marshal(resp.Result)
	var tr ToolResult
	if err := json.Unmarshal(b, &tr); err != nil {
		t.Fatalf("unmarshal ToolResult: %v", err)
	}
	if tr.IsError {
		t.Error("expected IsError=false")
	}
	if len(tr.Content) != 1 || tr.Content[0].Text != "result text" {
		t.Errorf("unexpected content: %+v", tr.Content)
	}
}

func TestToolsCallGatewayDown(t *testing.T) {
	// Point at a port with nothing listening — gateway unreachable scenario.
	proxy := NewProxy("http://127.0.0.1:19998", "")
	s := NewProxyServer(proxy)

	raw := `{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"agency_list","arguments":{}}}`
	resp := s.HandleMessage([]byte(raw))

	if resp == nil {
		t.Fatal("expected non-nil response")
	}
	if resp.Error != nil {
		t.Fatalf("unexpected RPC error: %+v", resp.Error)
	}

	b, _ := json.Marshal(resp.Result)
	var tr ToolResult
	if err := json.Unmarshal(b, &tr); err != nil {
		t.Fatalf("unmarshal ToolResult: %v", err)
	}
	if !tr.IsError {
		t.Error("expected IsError=true when gateway is unreachable")
	}
}
