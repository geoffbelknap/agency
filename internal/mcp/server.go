package mcp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
)

// ---------------------------------------------------------------------------
// JSON-RPC 2.0 protocol types
// ---------------------------------------------------------------------------

// Request represents a JSON-RPC 2.0 request or notification.
type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      interface{}     `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// Response represents a JSON-RPC 2.0 response.
type Response struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      interface{} `json:"id,omitempty"`
	Result  interface{} `json:"result,omitempty"`
	Error   *RPCError   `json:"error,omitempty"`
}

// RPCError represents a JSON-RPC 2.0 error object.
type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// ToolResult is returned from tools/call and serialised into the response.
type ToolResult struct {
	Content []ContentBlock `json:"content"`
	IsError bool           `json:"isError,omitempty"`
}

// ContentBlock is a single content item inside a ToolResult.
type ContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// ---------------------------------------------------------------------------
// Server
// ---------------------------------------------------------------------------

// Server implements the MCP JSON-RPC 2.0 stdio loop, proxying all tool
// requests to the gateway via a Proxy.
type Server struct {
	proxy *Proxy
}

// NewProxyServer creates a Server that delegates tool calls to the gateway
// via the given Proxy.
func NewProxyServer(proxy *Proxy) *Server {
	return &Server{proxy: proxy}
}

// HandleMessage parses a raw JSON-RPC message and dispatches it to the
// appropriate handler. It returns nil for notifications (requests with no id).
func (s *Server) HandleMessage(raw []byte) *Response {
	var req Request
	if err := json.Unmarshal(raw, &req); err != nil {
		return &Response{
			JSONRPC: "2.0",
			Error: &RPCError{
				Code:    -32700,
				Message: "Parse error",
			},
		}
	}

	// Notifications have no id — acknowledge silently.
	if req.ID == nil {
		return nil
	}

	switch req.Method {
	case "initialize":
		return s.handleInitialize(req.ID)
	case "tools/list":
		return s.handleToolsList(req.ID)
	case "tools/call":
		return s.handleToolsCall(req.ID, req.Params)
	default:
		return &Response{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error: &RPCError{
				Code:    -32601,
				Message: "Method not found",
			},
		}
	}
}

func (s *Server) handleInitialize(id interface{}) *Response {
	return &Response{
		JSONRPC: "2.0",
		ID:      id,
		Result: map[string]interface{}{
			"protocolVersion": "2024-11-05",
			"capabilities":    map[string]interface{}{"tools": map[string]interface{}{}},
			"serverInfo": map[string]interface{}{
				"name":    "agency",
				"version": "0.1.0",
			},
		},
	}
}

// handleToolsList fetches the tool list from the gateway.
func (s *Server) handleToolsList(id interface{}) *Response {
	tools, err := s.proxy.Tools()
	if err != nil {
		return &Response{JSONRPC: "2.0", ID: id, Result: map[string]interface{}{"tools": []interface{}{}}}
	}
	return &Response{JSONRPC: "2.0", ID: id, Result: map[string]interface{}{"tools": tools}}
}

// handleToolsCall proxies the tool invocation to the gateway.
func (s *Server) handleToolsCall(id interface{}, params json.RawMessage) *Response {
	var p struct {
		Name      string                 `json:"name"`
		Arguments map[string]interface{} `json:"arguments"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return &Response{JSONRPC: "2.0", ID: id, Error: &RPCError{Code: -32602, Message: "Invalid params"}}
	}
	result, err := s.proxy.Call(p.Name, p.Arguments)
	if err != nil {
		return &Response{JSONRPC: "2.0", ID: id, Error: &RPCError{Code: -32603, Message: err.Error()}}
	}
	return &Response{JSONRPC: "2.0", ID: id, Result: result}
}

// ---------------------------------------------------------------------------
// stdio loop
// ---------------------------------------------------------------------------

// maxLineSize is the maximum size of a single JSON-RPC message (1 MB).
const maxLineSize = 1024 * 1024

// Run starts the JSON-RPC loop on os.Stdin / os.Stdout.
func (s *Server) Run() error {
	return s.RunWithIO(os.Stdin, os.Stdout)
}

// RunWithIO reads newline-delimited JSON-RPC messages from r, dispatches
// them via HandleMessage, and writes responses as newline-delimited JSON to w.
func (s *Server) RunWithIO(r io.Reader, w io.Writer) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, maxLineSize), maxLineSize)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		resp := s.HandleMessage(line)
		if resp == nil {
			continue // notification — no response
		}

		out, err := json.Marshal(resp)
		if err != nil {
			return fmt.Errorf("marshal response: %w", err)
		}
		if _, err := fmt.Fprintf(w, "%s\n", out); err != nil {
			return fmt.Errorf("write response: %w", err)
		}
	}

	return scanner.Err()
}
