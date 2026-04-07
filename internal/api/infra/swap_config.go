package infra

import (
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"

	"github.com/geoffbelknap/agency/internal/hub"
)

// regenerateSwapConfig rebuilds credential-swaps.yaml from the credential
// store. If the store is nil or empty, it falls back to the legacy hub-based
// generation so existing file-based setups keep working.
func (h *handler) regenerateSwapConfig() {
	if h.deps.CredStore == nil {
		hub.WriteSwapConfig(h.deps.Config.Home)
		return
	}

	data, err := h.deps.CredStore.GenerateSwapConfig()
	if err != nil {
		h.deps.Logger.Warn("failed to generate swap config from store", "err", err)
		hub.WriteSwapConfig(h.deps.Config.Home)
		return
	}

	// If the store produced an empty swap map, fall back to legacy so that
	// service-definition / routing-based entries are still generated.
	if len(data) == 0 {
		hub.WriteSwapConfig(h.deps.Config.Home)
		return
	}

	// Merge: generate legacy config, then overlay store entries on top.
	// This ensures service-definition swaps survive while the store is
	// being gradually populated.
	legacyData, legacyErr := hub.GenerateSwapConfig(h.deps.Config.Home)

	swapPath := filepath.Join(h.deps.Config.Home, "infrastructure", "credential-swaps.yaml")
	os.MkdirAll(filepath.Dir(swapPath), 0755)

	if legacyErr == nil && len(legacyData) > 0 {
		// Parse both, merge store entries on top of legacy
		var legacy hub.SwapConfigFile
		var store hub.SwapConfigFile
		if yaml.Unmarshal(legacyData, &legacy) == nil && yaml.Unmarshal(data, &store) == nil {
			if legacy.Swaps == nil {
				legacy.Swaps = map[string]hub.SwapEntry{}
			}
			for k, v := range store.Swaps {
				legacy.Swaps[k] = v
			}
			if merged, err := yaml.Marshal(legacy); err == nil {
				os.WriteFile(swapPath, merged, 0644)
				return
			}
		}
	}

	// If merge failed, write store-only config
	os.WriteFile(swapPath, data, 0644)
}
