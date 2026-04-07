package hub

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// MergeProviderRouting parses the provider YAML and merges its routing block
// into {home}/infrastructure/routing.yaml. The file is created with defaults
// if it does not already exist.
func MergeProviderRouting(home, providerName string, providerData []byte) error {
	routingPath := filepath.Join(home, "infrastructure", "routing.yaml")
	cfg, err := loadRoutingYAML(routingPath)
	if err != nil {
		return err
	}

	if err := mergeProviderInto(cfg, providerName, providerData); err != nil {
		return err
	}

	return writeRoutingYAML(routingPath, cfg)
}

// mergeProviderInto merges a single provider's routing block into cfg.
// cfg is modified in place. If providerData has no routing block, this is a no-op.
func mergeProviderInto(cfg map[string]interface{}, providerName string, providerData []byte) error {
	var doc map[string]interface{}
	if err := yaml.Unmarshal(providerData, &doc); err != nil {
		return fmt.Errorf("parse provider YAML: %w", err)
	}

	routing, ok := doc["routing"].(map[string]interface{})
	if !ok {
		return nil // no routing block — nothing to merge
	}

	// Merge provider config (api_base, auth fields)
	providers := ensureMap(cfg, "providers")
	providerCfg := map[string]interface{}{}
	for _, key := range []string{"api_base", "auth_header", "auth_prefix", "auth_env"} {
		if v, ok := routing[key]; ok {
			providerCfg[key] = v
		}
	}
	// Fall back to credential.env_var if auth_env isn't in the routing block.
	// Hub provider definitions store the env var name under credential.env_var,
	// but the swap config generator looks for auth_env in the routing section.
	if _, hasAuthEnv := providerCfg["auth_env"]; !hasAuthEnv {
		if cred, ok := doc["credential"].(map[string]interface{}); ok {
			if envVar, ok := cred["env_var"].(string); ok && envVar != "" {
				providerCfg["auth_env"] = envVar
			}
		}
	}
	providers[providerName] = providerCfg

	// Merge models, stamping each with the provider name
	models := ensureMap(cfg, "models")
	if routingModels, ok := routing["models"].(map[string]interface{}); ok {
		for modelName, modelCfg := range routingModels {
			mc, ok := modelCfg.(map[string]interface{})
			if !ok {
				continue
			}
			mc["provider"] = providerName
			models[modelName] = mc
		}
	}

	// Merge tier entries — non-null string values become TierEntry items
	tiers := ensureMap(cfg, "tiers")
	if routingTiers, ok := routing["tiers"].(map[string]interface{}); ok {
		for tierName, modelRef := range routingTiers {
			if modelRef == nil {
				continue
			}
			modelName, ok := modelRef.(string)
			if !ok {
				continue
			}
			existing, _ := tiers[tierName].([]interface{})
			entry := map[string]interface{}{
				"model":      modelName,
				"preference": len(existing),
			}
			tiers[tierName] = append(existing, entry)
		}
	}

	return nil
}

// RemoveProviderRouting removes a provider and all of its models and tier
// references from {home}/infrastructure/routing.yaml.
func RemoveProviderRouting(home, providerName string) error {
	routingPath := filepath.Join(home, "infrastructure", "routing.yaml")
	cfg, err := loadRoutingYAML(routingPath)
	if err != nil {
		return err
	}

	// Remove provider entry
	if providers, ok := cfg["providers"].(map[string]interface{}); ok {
		delete(providers, providerName)
	}

	// Remove models belonging to this provider
	if models, ok := cfg["models"].(map[string]interface{}); ok {
		for name, mc := range models {
			if m, ok := mc.(map[string]interface{}); ok {
				if m["provider"] == providerName {
					delete(models, name)
				}
			}
		}
	}

	// Remove tier entries that reference models now gone
	if tiers, ok := cfg["tiers"].(map[string]interface{}); ok {
		models, _ := cfg["models"].(map[string]interface{})
		for tierName, entries := range tiers {
			tierEntries, ok := entries.([]interface{})
			if !ok {
				continue
			}
			var kept []interface{}
			for _, e := range tierEntries {
				entry, ok := e.(map[string]interface{})
				if !ok {
					continue
				}
				modelName, _ := entry["model"].(string)
				if _, exists := models[modelName]; exists {
					kept = append(kept, entry)
				}
			}
			tiers[tierName] = kept
		}
	}

	return writeRoutingYAML(routingPath, cfg)
}

// loadRoutingYAML reads routing.yaml from path. If the file does not exist it
// returns a skeleton document with sane defaults and does NOT write anything.
func loadRoutingYAML(path string) (map[string]interface{}, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]interface{}{
				"version":   "0.1",
				"providers": map[string]interface{}{},
				"models":    map[string]interface{}{},
				"tiers":     map[string]interface{}{},
				"settings": map[string]interface{}{
					"default_tier":  "standard",
					"tier_strategy": "best_effort",
				},
			}, nil
		}
		return nil, fmt.Errorf("read routing.yaml: %w", err)
	}
	var cfg map[string]interface{}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse routing.yaml: %w", err)
	}
	return cfg, nil
}

// writeRoutingYAML marshals cfg to YAML and writes it to path, creating parent
// directories as needed.
func writeRoutingYAML(path string, cfg map[string]interface{}) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("create routing dir: %w", err)
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal routing.yaml: %w", err)
	}
	return os.WriteFile(path, data, 0644)
}

// ensureMap returns the map[string]interface{} stored at parent[key], creating
// and inserting an empty map if the key is absent or holds a non-map value.
func ensureMap(parent map[string]interface{}, key string) map[string]interface{} {
	if m, ok := parent[key].(map[string]interface{}); ok {
		return m
	}
	m := map[string]interface{}{}
	parent[key] = m
	return m
}
