package main

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// Provider represents an LLM provider configuration.
type Provider struct {
	APIBase string `yaml:"api_base"`
	Caching *bool  `yaml:"caching,omitempty"`
}

// CachingEnabled returns whether prompt caching is enabled for this provider.
// Defaults to true if not explicitly set.
func (p Provider) CachingEnabled() bool {
	if p.Caching == nil {
		return true
	}
	return *p.Caching
}

// Model represents an LLM model alias configuration.
type Model struct {
	Provider      string   `yaml:"provider"`
	ProviderModel string   `yaml:"provider_model"`
	Capabilities  []string `yaml:"capabilities"`
	CostIn        float64  `yaml:"cost_per_mtok_in"`
	CostOut       float64  `yaml:"cost_per_mtok_out"`
	CostCached    float64  `yaml:"cost_per_mtok_cached"`
}

// HasCapability returns true if the model declares support for the given capability.
func (m Model) HasCapability(cap string) bool {
	for _, c := range m.Capabilities {
		if c == cap {
			return true
		}
	}
	return false
}

// TierEntry maps a model alias to a preference weight within a tier.
type TierEntry struct {
	Model      string `yaml:"model"`
	Preference int    `yaml:"preference"`
}

// TierConfig defines model tiers for hybrid routing.
type TierConfig struct {
	Frontier []TierEntry `yaml:"frontier"`
	Standard []TierEntry `yaml:"standard"`
	Fast     []TierEntry `yaml:"fast"`
	Mini     []TierEntry `yaml:"mini"`
	Nano     []TierEntry `yaml:"nano"`
	Batch    []TierEntry `yaml:"batch"`
}

// Settings holds enforcer operational settings.
type Settings struct {
	XPIAScan       bool `yaml:"xpia_scan"`
	DefaultTimeout int  `yaml:"default_timeout"`
}

// RoutingConfig represents the full routing.yaml configuration.
type RoutingConfig struct {
	Version      string              `yaml:"version"`
	Providers    map[string]Provider `yaml:"providers"`
	Models       map[string]Model    `yaml:"models"`
	Tiers        TierConfig          `yaml:"tiers"`
	RoutingRules []RoutingRule       `yaml:"routing_rules"`
	Settings     Settings            `yaml:"settings"`
}

// ResolveTier returns the preferred model alias for the given tier name.
// It selects the entry with the lowest preference value and verifies
// the model exists in the config. Returns ("", false) if the tier is
// empty or the best model is not defined.
func (rc *RoutingConfig) ResolveTier(tier string) (string, bool) {
	var entries []TierEntry
	switch tier {
	case "frontier":
		entries = rc.Tiers.Frontier
	case "standard":
		entries = rc.Tiers.Standard
	case "fast":
		entries = rc.Tiers.Fast
	case "mini":
		entries = rc.Tiers.Mini
	case "nano":
		entries = rc.Tiers.Nano
	case "batch":
		entries = rc.Tiers.Batch
	default:
		return "", false
	}
	if len(entries) == 0 {
		return "", false
	}
	best := entries[0]
	for _, e := range entries[1:] {
		if e.Preference < best.Preference {
			best = e
		}
	}
	if _, ok := rc.Models[best.Model]; !ok {
		return "", false
	}
	return best.Model, true
}

// APIKey represents an API key entry in api_keys.yaml.
type APIKey struct {
	Key  string `yaml:"key"`
	Name string `yaml:"name"`
}

// ServerConfig represents the server-config.yaml.
type ServerConfig struct {
	Server struct {
		Listen string `yaml:"listen"`
	} `yaml:"server"`
	Auth struct {
		Type   string `yaml:"type"`
		APIKey struct {
			KeysFile string `yaml:"keys_file"`
		} `yaml:"api_key"`
	} `yaml:"auth"`
	Policy struct {
		Path string `yaml:"path"`
	} `yaml:"policy"`
	Audit struct {
		Path          string `yaml:"path"`
		Format        string `yaml:"format"`
		FlushInterval string `yaml:"flush_interval"`
	} `yaml:"audit"`
}

// LoadRoutingConfig loads routing configuration from a YAML file.
func LoadRoutingConfig(path string) (*RoutingConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read routing config: %w", err)
	}
	var rc RoutingConfig
	if err := yaml.Unmarshal(data, &rc); err != nil {
		return nil, fmt.Errorf("parse routing config: %w", err)
	}
	return &rc, nil
}

// LoadAPIKeys loads API key entries from a YAML file.
func LoadAPIKeys(path string) ([]APIKey, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read api keys: %w", err)
	}
	var keys []APIKey
	if err := yaml.Unmarshal(data, &keys); err != nil {
		return nil, fmt.Errorf("parse api keys: %w", err)
	}
	return keys, nil
}

// LoadServerConfig loads server configuration from a YAML file.
func LoadServerConfig(path string) (*ServerConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read server config: %w", err)
	}
	var sc ServerConfig
	if err := yaml.Unmarshal(data, &sc); err != nil {
		return nil, fmt.Errorf("parse server config: %w", err)
	}
	return &sc, nil
}

// ResolveModel resolves a model alias to a target URL, provider model name,
// and provider name. Credential handling is performed by the egress proxy,
// not the enforcer.
func (rc *RoutingConfig) ResolveModel(alias string) (targetURL string, providerModel string, providerName string, err error) {
	model, ok := rc.Models[alias]
	if !ok {
		return "", "", "", fmt.Errorf("unknown model alias: %s", alias)
	}
	provider, ok := rc.Providers[model.Provider]
	if !ok {
		return "", "", "", fmt.Errorf("unknown provider: %s", model.Provider)
	}
	base := strings.TrimRight(provider.APIBase, "/")
	if model.Provider == "anthropic" {
		targetURL = base + "/messages"
	} else {
		targetURL = base + "/chat/completions"
	}

	return targetURL, model.ProviderModel, model.Provider, nil
}
