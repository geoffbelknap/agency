package mcp

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFullProtocolFlow(t *testing.T) {
	// 1. Mock gateway that serves MCP endpoints.
	//    - GET /api/v1/mcp/tools  → returns a list with one tool
	//    - POST /api/v1/mcp/call  → returns a formatted agent list
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "GET" && r.URL.Path == "/api/v1/mcp/tools":
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"tools": []interface{}{
					map[string]interface{}{
						"name":        "agency_list",
						"description": "List all agents",
						"inputSchema": map[string]interface{}{
							"type":       "object",
							"properties": map[string]interface{}{},
						},
					},
				},
			})
		case r.Method == "POST" && r.URL.Path == "/api/v1/mcp/call":
			var req map[string]interface{}
			json.NewDecoder(r.Body).Decode(&req)
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(ToolResult{
				Content: []ContentBlock{{
					Type: "text",
					Text: "1 agents\nrunning: test-agent",
				}},
				IsError: false,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	// 2. Create proxy and server.
	proxy := NewProxy(ts.URL, "")
	srv := NewProxyServer(proxy)

	// 3. Pipe JSON-RPC messages through RunWithIO.
	input := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"agency_list","arguments":{}}}`,
	}, "\n") + "\n"

	var out bytes.Buffer
	if err := srv.RunWithIO(strings.NewReader(input), &out); err != nil {
		t.Fatalf("RunWithIO error: %v", err)
	}

	// 4. Parse response lines.
	var lines []string
	scanner := bufio.NewScanner(&out)
	for scanner.Scan() {
		line := scanner.Text()
		if line != "" {
			lines = append(lines, line)
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scanner error: %v", err)
	}

	// Should have exactly 3 response lines (notification gets no response).
	if len(lines) != 3 {
		t.Fatalf("expected 3 response lines, got %d:\n%s", len(lines), strings.Join(lines, "\n"))
	}

	// --- Response 1 (id:1): initialize ---
	var resp1 Response
	if err := json.Unmarshal([]byte(lines[0]), &resp1); err != nil {
		t.Fatalf("unmarshal resp1: %v", err)
	}
	if resp1.ID == nil || fmt.Sprintf("%v", resp1.ID) != "1" {
		t.Errorf("resp1 id: got %v, want 1", resp1.ID)
	}
	result1, ok := resp1.Result.(map[string]interface{})
	if !ok {
		t.Fatalf("resp1 result not a map: %T", resp1.Result)
	}
	if pv, _ := result1["protocolVersion"].(string); pv != "2024-11-05" {
		t.Errorf("protocolVersion: got %q, want %q", pv, "2024-11-05")
	}

	// --- Response 2 (id:2): tools/list ---
	var resp2 Response
	if err := json.Unmarshal([]byte(lines[1]), &resp2); err != nil {
		t.Fatalf("unmarshal resp2: %v", err)
	}
	if resp2.ID == nil || fmt.Sprintf("%v", resp2.ID) != "2" {
		t.Errorf("resp2 id: got %v, want 2", resp2.ID)
	}
	result2, ok := resp2.Result.(map[string]interface{})
	if !ok {
		t.Fatalf("resp2 result not a map: %T", resp2.Result)
	}
	tools, ok := result2["tools"].([]interface{})
	if !ok {
		t.Fatalf("resp2 tools not a slice: %T", result2["tools"])
	}
	if len(tools) != 1 {
		t.Errorf("tools/list: got %d tools, want 1", len(tools))
	}

	// --- Response 3 (id:3): tools/call agency_list ---
	var resp3 Response
	if err := json.Unmarshal([]byte(lines[2]), &resp3); err != nil {
		t.Fatalf("unmarshal resp3: %v", err)
	}
	if resp3.ID == nil || fmt.Sprintf("%v", resp3.ID) != "3" {
		t.Errorf("resp3 id: got %v, want 3", resp3.ID)
	}
	result3, ok := resp3.Result.(map[string]interface{})
	if !ok {
		t.Fatalf("resp3 result not a map: %T", resp3.Result)
	}
	content, ok := result3["content"].([]interface{})
	if !ok || len(content) == 0 {
		t.Fatalf("resp3 content missing or empty")
	}
	block, ok := content[0].(map[string]interface{})
	if !ok {
		t.Fatalf("resp3 content[0] not a map: %T", content[0])
	}
	text, _ := block["text"].(string)
	if !strings.Contains(text, "1 agents") {
		t.Errorf("agency_list text should contain '1 agents', got: %s", text)
	}
	if !strings.Contains(text, "running: test-agent") {
		t.Errorf("agency_list text should contain 'running: test-agent', got: %s", text)
	}
}
