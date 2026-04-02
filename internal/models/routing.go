// agency-gateway/internal/models/routing.go
package models

import "fmt"

// VALID_TIERS is the ordered list of valid model routing tier names.
// Referenced by preset.go (Task 16) and agent.go (Task 17).
var VALID_TIERS = []string{"frontier", "standard", "fast", "mini", "nano"}

// ProviderConfig holds connection details for a single LLM provider.
type ProviderConfig struct {
	APIBase    string `yaml:"api_base" validate:"required"`
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
	Provider      string  `yaml:"provider" validate:"required"`
	ProviderModel string  `yaml:"provider_model" validate:"required"`
	CostPerMTokIn float64 `yaml:"cost_per_mtok_in" validate:"gte=0" default:"0"`
	CostPerMTokOut float64 `yaml:"cost_per_mtok_out" validate:"gte=0" default:"0"`
	CostPerMTokCached float64 `yaml:"cost_per_mtok_cached" validate:"gte=0" default:"0"`
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
}

// RoutingSettings holds global routing behaviour settings.
type RoutingSettings struct {
	XPIAScan       bool   `yaml:"xpia_scan" default:"true"`
	DefaultTimeout int    `yaml:"default_timeout" default:"300"`
	DefaultTier    string `yaml:"default_tier" default:"standard"`
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

	for name, p := range r.Providers {
		pc := p // copy so we can take address
		if err := pc.Validate(); err != nil {
			return fmt.Errorf("providers.%s: %w", name, err)
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
