package main

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// Provider represents an LLM provider configuration.
type Provider struct {
	APIBase   string `yaml:"api_base"`
	APIFormat string `yaml:"api_format,omitempty"`
	Caching   *bool  `yaml:"caching,omitempty"`
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
	Provider                 string                       `yaml:"provider"`
	ProviderModel            string                       `yaml:"provider_model"`
	Capabilities             []string                     `yaml:"capabilities"`
	ProviderToolCapabilities []string                     `yaml:"provider_tool_capabilities"`
	ProviderToolCosts        map[string]float64           `yaml:"provider_tool_costs"`
	ProviderToolPricing      map[string]ProviderToolPrice `yaml:"provider_tool_pricing"`
	CostIn                   float64                      `yaml:"cost_per_mtok_in"`
	CostOut                  float64                      `yaml:"cost_per_mtok_out"`
	CostCached               float64                      `yaml:"cost_per_mtok_cached"`
}

// ProviderToolPrice describes a provider-side tool billing unit. It is
// intentionally metadata-only: enforcement still happens through explicit
// provider-tool grants.
type ProviderToolPrice struct {
	Unit        string  `yaml:"unit" json:"unit"`
	USDPerUnit  float64 `yaml:"usd_per_unit" json:"usd_per_unit"`
	Source      string  `yaml:"source" json:"source"`
	Confidence  string  `yaml:"confidence" json:"confidence"`
	Description string  `yaml:"description,omitempty" json:"description,omitempty"`
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

// HasProviderToolCapability returns true if the model explicitly declares
// support for the given provider-executed server tool capability.
func (m Model) HasProviderToolCapability(cap string) bool {
	for _, c := range m.ProviderToolCapabilities {
		if c == cap {
			return true
		}
	}
	return false
}

func (m Model) ProviderToolPriceFor(cap string) (ProviderToolPrice, bool) {
	if m.ProviderToolPricing != nil {
		if p, ok := m.ProviderToolPricing[cap]; ok {
			p = normalizeProviderToolPrice(p)
			return p, true
		}
	}
	if m.ProviderToolCosts != nil {
		if cost, ok := m.ProviderToolCosts[cap]; ok {
			return ProviderToolPrice{
				Unit:       "tool_call",
				USDPerUnit: cost,
				Source:     "legacy_provider_tool_costs",
				Confidence: "estimated",
			}, true
		}
	}
	return ProviderToolPrice{}, false
}

func normalizeProviderToolPrice(p ProviderToolPrice) ProviderToolPrice {
	if strings.TrimSpace(p.Unit) == "" {
		p.Unit = "tool_call"
	}
	if strings.TrimSpace(p.Source) == "" {
		p.Source = "provider_catalog"
	}
	if strings.TrimSpace(p.Confidence) == "" {
		p.Confidence = "unknown"
	}
	return p
}

// Settings holds enforcer operational settings.
type Settings struct {
	XPIAScan       bool `yaml:"xpia_scan"`
	DefaultTimeout int  `yaml:"default_timeout"`
}

// RoutingConfig represents the full routing.yaml configuration.
type RoutingConfig struct {
	Version   string              `yaml:"version"`
	Providers map[string]Provider `yaml:"providers"`
	Models    map[string]Model    `yaml:"models"`
	Settings  Settings            `yaml:"settings"`
}

var allowedProviderToolCapabilities = map[string]bool{
	capProviderWebSearch:       true,
	capProviderWebFetch:        true,
	capProviderURLContext:      true,
	capProviderFileSearch:      true,
	capProviderCodeExecution:   true,
	capProviderComputerUse:     true,
	capProviderShell:           true,
	capProviderTextEditor:      true,
	capProviderMemory:          true,
	capProviderMCP:             true,
	capProviderImageGeneration: true,
	capProviderGoogleMaps:      true,
	capProviderToolSearch:      true,
	capProviderApplyPatch:      true,
}

func validateProviderToolCapability(capability string) error {
	if allowedProviderToolCapabilities[capability] {
		return nil
	}
	return fmt.Errorf("unknown provider tool capability %q", capability)
}

func (rc *RoutingConfig) Validate() error {
	if rc == nil {
		return fmt.Errorf("routing config is nil")
	}
	if _, ok := rc.Providers["gemini"]; ok {
		return fmt.Errorf("providers.gemini is not supported; use provider principal google with api_format gemini")
	}
	for alias, model := range rc.Models {
		if strings.TrimSpace(model.Provider) == "" {
			return fmt.Errorf("models.%s.provider is required", alias)
		}
		if _, ok := rc.Providers[model.Provider]; !ok {
			return fmt.Errorf("models.%s references unknown provider %q", alias, model.Provider)
		}
		for _, capability := range model.ProviderToolCapabilities {
			if err := validateProviderToolCapability(capability); err != nil {
				return fmt.Errorf("models.%s.provider_tool_capabilities: %w", alias, err)
			}
		}
		for capability := range model.ProviderToolPricing {
			if err := validateProviderToolCapability(capability); err != nil {
				return fmt.Errorf("models.%s.provider_tool_pricing: %w", alias, err)
			}
		}
		for capability := range model.ProviderToolCosts {
			if err := validateProviderToolCapability(capability); err != nil {
				return fmt.Errorf("models.%s.provider_tool_costs: %w", alias, err)
			}
		}
	}
	return nil
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
	if err := rc.Validate(); err != nil {
		return nil, fmt.Errorf("validate routing config: %w", err)
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
	providerModel = model.ProviderModel
	base := strings.TrimRight(provider.APIBase, "/")
	switch provider.APIFormat {
	case "gemini":
		targetURL = fmt.Sprintf("%s/models/%s:generateContent", base, providerModel)
	case "anthropic":
		targetURL = base + "/messages"
	default:
		if model.Provider == "anthropic" {
			targetURL = base + "/messages"
		} else {
			targetURL = base + "/chat/completions"
		}
	}

	return targetURL, providerModel, model.Provider, nil
}
