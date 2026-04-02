package api

import (
	"net/http"
)

// ── Infrastructure Reload ───────────────────────────────────────────────────

func (h *handler) infraReload(w http.ResponseWriter, r *http.Request) {
	if h.infra == nil {
		writeJSON(w, 500, map[string]string{"error": "infrastructure manager not initialized"})
		return
	}
	// Regenerate credential-swaps.yaml before reloading — uses credential
	// store as source of truth, falling back to legacy file-based generation.
	h.regenerateSwapConfig()

	// Reload restarts all infra components to pick up config changes
	components := []string{"egress", "comms", "knowledge", "intake"}
	var reloaded []string
	for _, comp := range components {
		if err := h.infra.RestartComponent(r.Context(), comp); err != nil {
			h.log.Warn("reload skip", "component", comp, "err", err)
			continue
		}
		reloaded = append(reloaded, comp)
	}
	writeJSON(w, 200, map[string]interface{}{"status": "reloaded", "components": reloaded})
}
