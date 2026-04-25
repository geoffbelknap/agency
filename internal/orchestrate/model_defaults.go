package orchestrate

import (
	"os"
	"path/filepath"
	"sort"

	"gopkg.in/yaml.v3"

	"github.com/geoffbelknap/agency/internal/models"
)

func resolveConfiguredModelAlias(home, tier string) string {
	data, err := os.ReadFile(filepath.Join(home, "infrastructure", "routing.yaml"))
	if err != nil {
		return ""
	}

	var rc models.RoutingConfig
	if err := yaml.Unmarshal(data, &rc); err != nil {
		return ""
	}

	entries := tierEntries(rc.Tiers, tier)
	if len(entries) == 0 {
		entries = inferredTierEntries(rc.Models, tier)
	}
	if len(entries) > 0 && entries[0].Model != "" {
		return entries[0].Model
	}

	if len(rc.Models) == 0 {
		return ""
	}
	modelsByAlias := make([]string, 0, len(rc.Models))
	for alias := range rc.Models {
		modelsByAlias = append(modelsByAlias, alias)
	}
	sort.Strings(modelsByAlias)
	return modelsByAlias[0]
}
