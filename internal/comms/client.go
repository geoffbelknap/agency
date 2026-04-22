package comms

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Client sends HTTP requests to the comms service.
type Client interface {
	CommsRequest(ctx context.Context, method, path string, body interface{}) ([]byte, error)
}

// HTTPClient implements Client via direct HTTP to a base URL.
type HTTPClient struct {
	baseURL    string
	httpClient *http.Client
}

// NewHTTPClient creates a comms client pointing at the given base URL.
// For the default Agency deployment, baseURL is "http://localhost:8202".
func NewHTTPClient(baseURL string) *HTTPClient {
	return &HTTPClient{
		baseURL:    baseURL,
		httpClient: &http.Client{Timeout: 15 * time.Second},
	}
}

func (c *HTTPClient) CommsRequest(ctx context.Context, method, path string, body interface{}) ([]byte, error) {
	url := c.baseURL + path

	var req *http.Request
	var err error

	if body != nil && (method == "POST" || method == "PUT" || method == "DELETE") {
		jsonBody, _ := json.Marshal(body)
		req, err = http.NewRequestWithContext(ctx, method, url, bytes.NewReader(jsonBody))
		if err != nil {
			return nil, fmt.Errorf("comms request %s %s: %w", method, path, err)
		}
		req.Header.Set("Content-Type", "application/json")
	} else {
		req, err = http.NewRequestWithContext(ctx, method, url, nil)
		if err != nil {
			return nil, fmt.Errorf("comms request %s %s: %w", method, path, err)
		}
	}

	// Platform-only endpoints need this header.
	if strings.Contains(path, "grant-access") || strings.Contains(path, "archive") || strings.Contains(path, "retire") {
		req.Header.Set("X-Agency-Platform", "true")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("comms request %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()

	out, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("comms request %s %s: read body: %w", method, path, err)
	}
	if resp.StatusCode >= 400 {
		return out, fmt.Errorf("comms returned %d: %s", resp.StatusCode, string(out))
	}
	return out, nil
}
