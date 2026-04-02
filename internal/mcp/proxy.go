package mcp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// Proxy is an HTTP client that talks to the gateway's MCP endpoints.
type Proxy struct {
	baseURL         string
	token           string
	client          *http.Client
	envVars         []string
	retryInitial    time.Duration
	retryMaxElapsed time.Duration
}

// NewProxy creates a Proxy that forwards MCP requests to the gateway at baseURL.
func NewProxy(baseURL, token string) *Proxy {
	return &Proxy{
		baseURL:         baseURL,
		token:           token,
		client:          &http.Client{Timeout: 300 * time.Second},
		envVars:         []string{"ANTHROPIC_API_KEY", "OPENAI_API_KEY", "GOOGLE_API_KEY"},
		retryInitial:    200 * time.Millisecond,
		retryMaxElapsed: 30 * time.Second,
	}
}

// Tools fetches the tool list from the gateway.
func (p *Proxy) Tools() ([]interface{}, error) {
	body, err := p.get("/api/v1/mcp/tools")
	if err != nil {
		return nil, err
	}
	var resp struct {
		Tools []interface{} `json:"tools"`
	}
	json.Unmarshal(body, &resp) //nolint:errcheck
	return resp.Tools, nil
}

// Call proxies a tool invocation to the gateway.
func (p *Proxy) Call(name string, args map[string]interface{}) (*ToolResult, error) {
	payload, _ := json.Marshal(map[string]interface{}{"name": name, "arguments": args})
	body, err := p.post("/api/v1/mcp/call", payload)
	if err != nil {
		// Gateway unreachable — return error as content so the caller can surface it.
		return &ToolResult{
			Content: []ContentBlock{{Type: "text", Text: fmt.Sprintf("Error: Gateway not running. Start it with: agency serve\n(%s)", err)}},
			IsError: true,
		}, nil
	}
	var result ToolResult
	json.Unmarshal(body, &result) //nolint:errcheck
	return &result, nil
}

// envHeader builds the X-Agency-Env header value from known env vars.
func (p *Proxy) envHeader() string {
	var parts []string
	for _, name := range p.envVars {
		if val := os.Getenv(name); val != "" {
			parts = append(parts, name+"="+val)
		}
	}
	return strings.Join(parts, ",")
}

func (p *Proxy) doWithRetry(req *http.Request, body []byte) ([]byte, error) {
	deadline := time.Now().Add(p.retryMaxElapsed)
	delay := p.retryInitial
	maxDelay := 10 * time.Second

	for {
		if body != nil {
			req.Body = io.NopCloser(bytes.NewReader(body))
		}

		resp, err := p.client.Do(req)
		if err != nil {
			if time.Now().After(deadline) {
				return nil, err
			}
			time.Sleep(delay)
			delay = delay * 2
			if delay > maxDelay {
				delay = maxDelay
			}
			continue
		}

		if resp.StatusCode != http.StatusBadGateway && resp.StatusCode != http.StatusServiceUnavailable {
			defer resp.Body.Close()
			return io.ReadAll(resp.Body)
		}

		resp.Body.Close()
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("gateway returned %d after retries exhausted", resp.StatusCode)
		}
		time.Sleep(delay)
		delay = delay * 2
		if delay > maxDelay {
			delay = maxDelay
		}
	}
}

func (p *Proxy) get(path string) ([]byte, error) {
	req, _ := http.NewRequest("GET", p.baseURL+path, nil)
	if p.token != "" {
		req.Header.Set("X-Agency-Token", p.token)
	}
	return p.doWithRetry(req, nil)
}

func (p *Proxy) post(path string, body []byte) ([]byte, error) {
	req, _ := http.NewRequest("POST", p.baseURL+path, nil)
	req.Header.Set("Content-Type", "application/json")
	if p.token != "" {
		req.Header.Set("X-Agency-Token", p.token)
	}
	if env := p.envHeader(); env != "" {
		req.Header.Set("X-Agency-Env", env)
	}
	return p.doWithRetry(req, body)
}
