// agency-gateway/internal/models/routing.go
package models

import (
	"fmt"
	"strings"
)

// VALID_TIERS is the ordered list of valid model routing tier names.
// Referenced by preset.go (Task 16) and agent.go (Task 17).
var VALID_TIERS = []string{"frontier", "standard", "fast", "mini", "nano", "batch"}

// ProviderConfig holds connection details for a single LLM provider.
type ProviderConfig struct {
	APIBase    string `yaml:"api_base" validate:"required"`
	APIFormat  string `yaml:"api_format,omitempty"`
	AuthEnv    string `yaml:"auth_env"`
	AuthHeader string `yaml:"auth_header"`
	AuthPrefix string `yaml:"auth_prefix"`
	Caching    bool   `yaml:"caching" default:"true"`
}

// Validate performs cross-field validation for ProviderConfig.
func (p *ProviderConfig) Validate() error {
	if err := ValidateAPIBase(p.APIBase); err != nil {
		return err
	}
	if err := ValidateCredentialEnv(p.AuthEnv); err != nil {
		return err
	}
	return nil
}

// ModelConfig describes a specific LLM model and its cost information.
type ModelConfig struct {
	Provider                 string                       `yaml:"provider" validate:"required"`
	ProviderModel            string                       `yaml:"provider_model" validate:"required"`
	Capabilities             []string                     `yaml:"capabilities"`
	ProviderToolCapabilities []string                     `yaml:"provider_tool_capabilities"`
	ProviderToolCosts        map[string]float64           `yaml:"provider_tool_costs"`
	ProviderToolPricing      map[string]ProviderToolPrice `yaml:"provider_tool_pricing"`
	CostPerMTokIn            float64                      `yaml:"cost_per_mtok_in" validate:"gte=0" default:"0"`
	CostPerMTokOut           float64                      `yaml:"cost_per_mtok_out" validate:"gte=0" default:"0"`
	CostPerMTokCached        float64                      `yaml:"cost_per_mtok_cached" validate:"gte=0" default:"0"`
}

// ProviderToolPrice describes the billing metadata for a provider-side tool.
type ProviderToolPrice struct {
	Unit        string  `yaml:"unit" json:"unit"`
	USDPerUnit  float64 `yaml:"usd_per_unit" json:"usd_per_unit"`
	Source      string  `yaml:"source" json:"source"`
	Confidence  string  `yaml:"confidence" json:"confidence"`
	Description string  `yaml:"description,omitempty" json:"description,omitempty"`
}

var validProviderToolCapabilities = map[string]bool{
	"provider-web-search":       true,
	"provider-web-fetch":        true,
	"provider-url-context":      true,
	"provider-file-search":      true,
	"provider-code-execution":   true,
	"provider-computer-use":     true,
	"provider-shell":            true,
	"provider-text-editor":      true,
	"provider-memory":           true,
	"provider-mcp":              true,
	"provider-image-generation": true,
	"provider-google-maps":      true,
	"provider-tool-search":      true,
	"provider-apply-patch":      true,
}

func validateProviderToolCapability(capability string) error {
	if validProviderToolCapabilities[capability] {
		return nil
	}
	return fmt.Errorf("unknown provider tool capability %q", capability)
}

// HasCapability returns true if the model declares the given capability.
func (m ModelConfig) HasCapability(cap string) bool {
	for _, c := range m.Capabilities {
		if c == cap {
			return true
		}
	}
	return false
}

// TierEntry is a model reference within a tier, with an ordering preference.
type TierEntry struct {
	Model      string `yaml:"model" validate:"required"`
	Preference int    `yaml:"preference" validate:"gte=0" default:"0"`
}

// TierConfig holds the ranked model lists for each routing tier.
type TierConfig struct {
	Frontier []TierEntry `yaml:"frontier"`
	Standard []TierEntry `yaml:"standard"`
	Fast     []TierEntry `yaml:"fast"`
	Mini     []TierEntry `yaml:"mini"`
	Nano     []TierEntry `yaml:"nano"`
	Batch    []TierEntry `yaml:"batch"`
}

// RoutingSettings holds global routing behaviour settings.
type RoutingSettings struct {
	XPIAScan       bool   `yaml:"xpia_scan" default:"true"`
	DefaultTimeout int    `yaml:"default_timeout" default:"300"`
	DefaultTier    string `yaml:"default_tier" default:"standard"`
	TierStrategy   string `yaml:"tier_strategy" default:"best_effort"`
}

// Validate checks that DefaultTier is a recognised tier name and that
// DefaultTimeout is within [1, 3600].
//
// Note: defaults are applied by RoutingConfig.Validate() before this method
// is called, so DefaultTier and DefaultTimeout will never be zero here.
func (s *RoutingSettings) Validate() error {
	validTier := false
	for _, t := range VALID_TIERS {
		if s.DefaultTier == t {
			validTier = true
			break
		}
	}
	if !validTier {
		return fmt.Errorf(
			"default_tier must be one of %v, got %q",
			VALID_TIERS, s.DefaultTier,
		)
	}
	if s.DefaultTimeout < 1 || s.DefaultTimeout > 3600 {
		return fmt.Errorf("default_timeout must be between 1 and 3600, got %d", s.DefaultTimeout)
	}
	if s.TierStrategy == "" {
		s.TierStrategy = "best_effort"
	}
	validStrategies := []string{"strict", "best_effort", "catch_all"}
	validStrategy := false
	for _, st := range validStrategies {
		if s.TierStrategy == st {
			validStrategy = true
			break
		}
	}
	if !validStrategy {
		return fmt.Errorf("tier_strategy must be one of %v, got %q", validStrategies, s.TierStrategy)
	}
	return nil
}

// RoutingConfig is the top-level schema for routing.yaml.
type RoutingConfig struct {
	Version   string                    `yaml:"version" default:"0.1"`
	Providers map[string]ProviderConfig `yaml:"providers"`
	Models    map[string]ModelConfig    `yaml:"models"`
	Tiers     TierConfig                `yaml:"tiers"`
	Settings  RoutingSettings           `yaml:"settings"`
}

// Validate runs Validate() on each provider and on the settings.
// It also applies defaults to nested structs that applyDefaults skips.
func (r *RoutingConfig) Validate() error {
	// Apply defaults to nested RoutingSettings (applyDefaults only touches the
	// top-level struct, so nested struct fields keep their Go zero values).
	if r.Settings.DefaultTier == "" {
		r.Settings.DefaultTier = "standard"
	}
	if r.Settings.DefaultTimeout == 0 {
		r.Settings.DefaultTimeout = 300
	}
	if r.Settings.TierStrategy == "" {
		r.Settings.TierStrategy = "best_effort"
	}

	for name, p := range r.Providers {
		pc := p // copy so we can take address
		if err := pc.Validate(); err != nil {
			return fmt.Errorf("providers.%s: %w", name, err)
		}
	}
	for name, m := range r.Models {
		if strings.TrimSpace(m.Provider) == "" {
			return fmt.Errorf("models.%s.provider is required", name)
		}
		if _, ok := r.Providers[m.Provider]; !ok {
			return fmt.Errorf("models.%s references unknown provider %q", name, m.Provider)
		}
		for _, capability := range m.ProviderToolCapabilities {
			if err := validateProviderToolCapability(capability); err != nil {
				return fmt.Errorf("models.%s.provider_tool_capabilities: %w", name, err)
			}
		}
		for capability := range m.ProviderToolPricing {
			if err := validateProviderToolCapability(capability); err != nil {
				return fmt.Errorf("models.%s.provider_tool_pricing: %w", name, err)
			}
		}
		for capability := range m.ProviderToolCosts {
			if err := validateProviderToolCapability(capability); err != nil {
				return fmt.Errorf("models.%s.provider_tool_costs: %w", name, err)
			}
		}
	}
	return r.Settings.Validate()
}

// ResolveModel returns the ProviderConfig and ModelConfig for the given model
// alias key. Returns (nil, nil) if the alias is not found or its provider is
// not configured.
func (r *RoutingConfig) ResolveModel(alias string) (*ProviderConfig, *ModelConfig) {
	mc, ok := r.Models[alias]
	if !ok {
		return nil, nil
	}
	pc, ok := r.Providers[mc.Provider]
	if !ok {
		return nil, nil
	}
	return &pc, &mc
}

// ResolveTier returns the first available (ProviderConfig, ModelConfig) pair
// for the given tier, walking entries in preference order (lower value = higher
// priority). extraEnv is reserved for future use (caller-supplied env overrides)
// and is currently ignored. Returns (nil, nil) if no entry can be resolved.
func (r *RoutingConfig) ResolveTier(tier string, extraEnv map[string]string) (*ProviderConfig, *ModelConfig) {
	var entries []TierEntry
	switch tier {
	case "frontier":
		entries = r.Tiers.Frontier
	case "standard":
		entries = r.Tiers.Standard
	case "fast":
		entries = r.Tiers.Fast
	case "mini":
		entries = r.Tiers.Mini
	case "nano":
		entries = r.Tiers.Nano
	case "batch":
		entries = r.Tiers.Batch
	default:
		return nil, nil
	}

	// Walk entries sorted by preference (lowest wins). Entries are already
	// stored in declaration order; a simple linear scan is sufficient because
	// the list is short and callers control ordering via the preference field.
	var best *TierEntry
	for i := range entries {
		e := &entries[i]
		if best == nil || e.Preference < best.Preference {
			best = e
		}
	}
	if best == nil {
		return nil, nil
	}
	return r.ResolveModel(best.Model)
}

// tierOrder defines the hierarchy from most capable to least.
var tierOrder = []string{"frontier", "standard", "fast", "mini", "nano", "batch"}

// ResolveTierWithStrategy resolves a tier using the configured tier_strategy.
func (r *RoutingConfig) ResolveTierWithStrategy(tier string, extraEnv map[string]string) (*ProviderConfig, *ModelConfig) {
	strategy := r.Settings.TierStrategy
	if strategy == "" {
		strategy = "best_effort"
	}

	pc, mc := r.ResolveTier(tier, extraEnv)
	if pc != nil && mc != nil {
		return pc, mc
	}

	switch strategy {
	case "strict":
		return nil, nil

	case "best_effort":
		pos := -1
		for i, t := range tierOrder {
			if t == tier {
				pos = i
				break
			}
		}
		if pos < 0 {
			return nil, nil
		}
		for delta := 1; delta < len(tierOrder); delta++ {
			if pos+delta < len(tierOrder) {
				if pc, mc := r.ResolveTier(tierOrder[pos+delta], extraEnv); pc != nil && mc != nil {
					return pc, mc
				}
			}
			if pos-delta >= 0 {
				if pc, mc := r.ResolveTier(tierOrder[pos-delta], extraEnv); pc != nil && mc != nil {
					return pc, mc
				}
			}
		}
		return nil, nil

	case "catch_all":
		for _, t := range tierOrder {
			if pc, mc := r.ResolveTier(t, extraEnv); pc != nil && mc != nil {
				return pc, mc
			}
		}
		return nil, nil

	default:
		return nil, nil
	}
}

// TierCapabilities returns the intersection of capabilities across all models
// in the given tier. Returns nil if the tier is empty or unknown.
func (r *RoutingConfig) TierCapabilities(tier string) []string {
	var entries []TierEntry
	switch tier {
	case "frontier":
		entries = r.Tiers.Frontier
	case "standard":
		entries = r.Tiers.Standard
	case "fast":
		entries = r.Tiers.Fast
	case "mini":
		entries = r.Tiers.Mini
	case "nano":
		entries = r.Tiers.Nano
	case "batch":
		entries = r.Tiers.Batch
	default:
		return nil
	}
	if len(entries) == 0 {
		return nil
	}

	first := r.Models[entries[0].Model]
	caps := make(map[string]bool)
	for _, c := range first.Capabilities {
		caps[c] = true
	}

	for _, entry := range entries[1:] {
		mc, ok := r.Models[entry.Model]
		if !ok {
			continue
		}
		has := make(map[string]bool)
		for _, c := range mc.Capabilities {
			has[c] = true
		}
		for c := range caps {
			if !has[c] {
				delete(caps, c)
			}
		}
	}

	result := make([]string, 0, len(caps))
	for c := range caps {
		result = append(result, c)
	}
	return result
}

// ResolveTierWithCapabilities resolves a tier ensuring all required capabilities
// are satisfied. If the requested tier lacks a required capability, it searches
// adjacent tiers (closer first, preferring downward) for one that satisfies all
// requirements. Returns the resolved provider, model, and the actual tier used.
// Returns (nil, nil, "") if no tier can satisfy the requirements.
func (r *RoutingConfig) ResolveTierWithCapabilities(tier string, required []string, extraEnv map[string]string) (*ProviderConfig, *ModelConfig, string) {
	if len(required) == 0 {
		pc, mc := r.ResolveTierWithStrategy(tier, extraEnv)
		return pc, mc, tier
	}
	if r.tierSatisfies(tier, required) {
		pc, mc := r.ResolveTier(tier, extraEnv)
		if pc != nil && mc != nil {
			return pc, mc, tier
		}
	}
	pos := -1
	for i, t := range tierOrder {
		if t == tier {
			pos = i
			break
		}
	}
	if pos < 0 {
		return nil, nil, ""
	}
	for delta := 1; delta < len(tierOrder); delta++ {
		for _, d := range []int{delta, -delta} {
			idx := pos + d
			if idx < 0 || idx >= len(tierOrder) {
				continue
			}
			candidate := tierOrder[idx]
			if r.tierSatisfies(candidate, required) {
				pc, mc := r.ResolveTier(candidate, extraEnv)
				if pc != nil && mc != nil {
					return pc, mc, candidate
				}
			}
		}
	}
	return nil, nil, ""
}

// tierSatisfies returns true if every required capability is present in the
// intersection of capabilities for the given tier.
func (r *RoutingConfig) tierSatisfies(tier string, required []string) bool {
	caps := r.TierCapabilities(tier)
	capSet := make(map[string]bool, len(caps))
	for _, c := range caps {
		capSet[c] = true
	}
	for _, req := range required {
		if !capSet[req] {
			return false
		}
	}
	return true
}
