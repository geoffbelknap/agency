package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type discoveredModel struct {
	ID                       string
	Capabilities             []string
	ProviderToolCapabilities []string
}

// discoverModels calls the OpenAI-compatible /models endpoint.
func discoverModels(ctx context.Context, baseURL string, credential string) ([]discoveredModel, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	modelsURL := strings.TrimRight(baseURL, "/") + "/models"
	req, err := http.NewRequestWithContext(ctx, "GET", modelsURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	if credential != "" {
		req.Header.Set("Authorization", "Bearer "+credential)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("list models at %s: %w", modelsURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("list models: HTTP %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("parse model list: %w", err)
	}

	var models []discoveredModel
	for _, m := range result.Data {
		caps := probeCapabilities(ctx, client, baseURL, m.ID, credential)
		models = append(models, discoveredModel{ID: m.ID, Capabilities: caps})
	}
	return models, nil
}

// probeCapabilities sends a minimal tool call request to detect tool support.
func probeCapabilities(ctx context.Context, client *http.Client, baseURL, modelID, credential string) []string {
	caps := []string{"streaming"}

	probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	toolProbe := map[string]interface{}{
		"model":    modelID,
		"messages": []map[string]string{{"role": "user", "content": "test"}},
		"tools": []map[string]interface{}{
			{"type": "function", "function": map[string]interface{}{
				"name": "test_probe", "description": "probe",
				"parameters": map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
			}},
		},
		"max_tokens": 1,
	}

	body, _ := json.Marshal(toolProbe)
	url := strings.TrimRight(baseURL, "/") + "/chat/completions"
	req, err := http.NewRequestWithContext(probeCtx, "POST", url, strings.NewReader(string(body)))
	if err != nil {
		return caps
	}
	req.Header.Set("Content-Type", "application/json")
	if credential != "" {
		req.Header.Set("Authorization", "Bearer "+credential)
	}

	resp, err := client.Do(req)
	if err != nil {
		return caps
	}
	defer resp.Body.Close()
	io.ReadAll(io.LimitReader(resp.Body, 1024))

	if resp.StatusCode == 200 {
		caps = append(caps, "tools")
	}

	return caps
}

// writeProviderConfig writes discovered models to routing.local.yaml.
func writeProviderConfig(name, baseURL, credential string, models []discoveredModel) error {
	home, _ := os.UserHomeDir()
	localPath := filepath.Join(home, ".agency", "infrastructure", "routing.local.yaml")
	cfg := loadOrCreateLocalRouting(localPath)

	providers := ensureMapInCLI(cfg, "providers")
	providerCfg := map[string]interface{}{
		"api_base":    baseURL,
		"auth_header": "Authorization",
		"auth_prefix": "Bearer ",
	}
	if credential != "" {
		providerCfg["auth_env"] = credential
	}
	providers[name] = providerCfg

	modelsCfg := ensureMapInCLI(cfg, "models")
	for _, m := range models {
		modelCfg := map[string]interface{}{
			"provider":          name,
			"provider_model":    m.ID,
			"capabilities":      m.Capabilities,
			"cost_per_mtok_in":  0,
			"cost_per_mtok_out": 0,
		}
		if len(m.ProviderToolCapabilities) > 0 {
			modelCfg["provider_tool_capabilities"] = m.ProviderToolCapabilities
		}
		modelsCfg[m.ID] = modelCfg
	}

	return writeLocalRouting(localPath, cfg)
}

// writeProviderSkeleton writes a minimal config template.
func writeProviderSkeleton(name, baseURL, credential string) error {
	home, _ := os.UserHomeDir()
	localPath := filepath.Join(home, ".agency", "infrastructure", "routing.local.yaml")
	cfg := loadOrCreateLocalRouting(localPath)

	providers := ensureMapInCLI(cfg, "providers")
	providerCfg := map[string]interface{}{
		"api_base":    baseURL,
		"auth_header": "Authorization",
		"auth_prefix": "Bearer ",
	}
	if credential != "" {
		providerCfg["auth_env"] = credential
	}
	providers[name] = providerCfg

	modelsCfg := ensureMapInCLI(cfg, "models")
	modelsCfg["REPLACE-ME"] = map[string]interface{}{
		"provider":                   name,
		"provider_model":             "REPLACE-ME",
		"capabilities":               []string{"streaming"},
		"provider_tool_capabilities": []string{},
		"provider_tool_costs":        map[string]float64{},
		"provider_tool_pricing":      map[string]interface{}{},
		"cost_per_mtok_in":           0,
		"cost_per_mtok_out":          0,
	}

	if err := writeLocalRouting(localPath, cfg); err != nil {
		return err
	}
	fmt.Printf("Skeleton written to %s — edit to add your models\n", localPath)
	return nil
}

func loadOrCreateLocalRouting(path string) map[string]interface{} {
	data, err := os.ReadFile(path)
	if err != nil {
		return map[string]interface{}{}
	}
	var cfg map[string]interface{}
	if yaml.Unmarshal(data, &cfg) != nil {
		return map[string]interface{}{}
	}
	return cfg
}

func writeLocalRouting(path string, cfg map[string]interface{}) error {
	os.MkdirAll(filepath.Dir(path), 0755)
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func ensureMapInCLI(parent map[string]interface{}, key string) map[string]interface{} {
	if m, ok := parent[key].(map[string]interface{}); ok {
		return m
	}
	m := map[string]interface{}{}
	parent[key] = m
	return m
}
