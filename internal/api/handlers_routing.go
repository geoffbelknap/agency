package api

import (
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"

	"github.com/geoffbelknap/agency/internal/models"
	"github.com/geoffbelknap/agency/internal/routing"
)

// loadRoutingConfig reads routing.yaml (hub-managed) and routing.local.yaml
// (operator overrides), merging them. Local overlay wins on conflicts.
func loadRoutingConfig(home string) *models.RoutingConfig {
	infraDir := filepath.Join(home, "infrastructure")

	// Base: routing.yaml (managed by hub update)
	var rc models.RoutingConfig
	if data, err := os.ReadFile(filepath.Join(infraDir, "routing.yaml")); err == nil {
		yaml.Unmarshal(data, &rc)
	}

	// Overlay: routing.local.yaml (operator customizations, never hub-managed)
	if data, err := os.ReadFile(filepath.Join(infraDir, "routing.local.yaml")); err == nil {
		var local models.RoutingConfig
		if yaml.Unmarshal(data, &local) == nil {
			// Merge providers
			if rc.Providers == nil {
				rc.Providers = local.Providers
			} else {
				for k, v := range local.Providers {
					rc.Providers[k] = v
				}
			}
			// Merge models (local overrides base)
			if rc.Models == nil {
				rc.Models = local.Models
			} else {
				for k, v := range local.Models {
					rc.Models[k] = v
				}
			}
		}
	}

	return &rc
}

// loadModelCosts extracts pricing from the merged routing config.
func loadModelCosts(home string) map[string]routing.ModelCost {
	rc := loadRoutingConfig(home)
	if rc == nil || len(rc.Models) == 0 {
		return nil
	}
	costs := make(map[string]routing.ModelCost, len(rc.Models))
	for alias, m := range rc.Models {
		costs[alias] = routing.ModelCost{
			CostPerMTokIn:     m.CostPerMTokIn,
			CostPerMTokOut:    m.CostPerMTokOut,
			CostPerMTokCached: m.CostPerMTokCached,
		}
	}
	if len(costs) == 0 {
		return nil
	}
	return costs
}
