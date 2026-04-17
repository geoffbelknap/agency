package orchestrate

import (
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

func hostPortFromEndpoint(endpoint string) string {
	if strings.TrimSpace(endpoint) == "" {
		return ""
	}
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return ""
	}
	return parsed.Port()
}

func readScopedAPIKey(path string) string {
	if strings.TrimSpace(path) == "" {
		return ""
	}
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return ""
	}
	var entries []struct {
		Key string `yaml:"key"`
	}
	if err := yaml.Unmarshal(data, &entries); err == nil {
		for _, entry := range entries {
			if strings.TrimSpace(entry.Key) != "" {
				return strings.TrimSpace(entry.Key)
			}
		}
	}
	return ""
}
