package hubclient

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"strings"
)

type Client struct {
	BaseURL    string
	HTTPClient *http.Client
}

func (c Client) FetchArtifactAssurance(ctx context.Context, hubName, kind, name, version string) (AssuranceSummary, error) {
	base := strings.TrimSpace(c.BaseURL)
	if base == "" {
		return AssuranceSummary{}, fmt.Errorf("hub API base URL is required")
	}
	if strings.TrimSpace(hubName) == "" || strings.TrimSpace(kind) == "" || strings.TrimSpace(name) == "" || strings.TrimSpace(version) == "" {
		return AssuranceSummary{}, fmt.Errorf("hub name, kind, name, and version are required")
	}

	u, err := url.Parse(base)
	if err != nil {
		return AssuranceSummary{}, fmt.Errorf("parse hub API base URL: %w", err)
	}
	u.Path = path.Join(strings.TrimRight(u.Path, "/"), "/v1/hubs", hubName, "artifacts", kind, name, version, "assurance")

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return AssuranceSummary{}, fmt.Errorf("build assurance request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	client := c.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return AssuranceSummary{}, fmt.Errorf("fetch assurance: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return AssuranceSummary{}, fmt.Errorf("fetch assurance: unexpected status %s", resp.Status)
	}

	var summary AssuranceSummary
	if err := json.NewDecoder(resp.Body).Decode(&summary); err != nil {
		return AssuranceSummary{}, fmt.Errorf("decode assurance response: %w", err)
	}
	return summary, nil
}
