package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
)

// getAgentTrajectory handles GET /api/v1/agents/{name}/trajectory
// Proxies the request to the agent's enforcer container on port 8081.
// The enforcer tracks trajectory state in-memory (sliding window of tool calls,
// active anomalies, detector config). No data is persisted — this is a live view.
func (h *handler) getAgentTrajectory(w http.ResponseWriter, r *http.Request) {
	name, ok := requireName(w, chi.URLParam(r, "name"))
	if !ok {
		return
	}

	// Verify the agent exists
	_, err := h.deps.AgentManager.Show(r.Context(), name)
	if err != nil {
		writeJSON(w, 404, map[string]string{"error": "agent not found: " + err.Error()})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	enforcerName := fmt.Sprintf("agency-%s-enforcer", name)
	if body, ok, err := trajectoryBodyFromExec(ctx, h.deps.DC, enforcerName); ok {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write(body)
		return
	} else if err != nil && h.deps.Logger != nil {
		h.deps.Logger.Warn("trajectory exec unavailable", "agent", name, "container", enforcerName, "err", err)
	}

	// Fallback to the container DNS name for deployments where the gateway is
	// itself on the mediation network.
	enforcerURL := fmt.Sprintf("http://agency-%s-enforcer:8081/trajectory", name)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, enforcerURL, nil)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "failed to create request: " + err.Error()})
		return
	}

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		if h.deps.Logger != nil {
			h.deps.Logger.Warn("trajectory network proxy failed", "agent", name, "url", enforcerURL, "err", err)
		}
		// Enforcer not reachable — agent may be stopped or starting
		writeJSON(w, 502, map[string]string{
			"error": "enforcer unavailable — agent may not be running",
			"agent": name,
		})
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		writeJSON(w, 502, map[string]string{"error": "failed to read enforcer response"})
		return
	}

	// Validate we got valid JSON before forwarding
	var check json.RawMessage
	if json.Unmarshal(body, &check) != nil {
		writeJSON(w, 502, map[string]string{"error": "invalid response from enforcer"})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	w.Write(body)
}

func trajectoryBodyFromExec(ctx context.Context, dc DockerClient, enforcerName string) ([]byte, bool, error) {
	if dc == nil {
		return nil, false, nil
	}
	body, err := dc.ExecInContainer(ctx, enforcerName, []string{"curl", "-sf", "http://127.0.0.1:8081/trajectory"})
	if err != nil {
		return nil, false, err
	}
	var check json.RawMessage
	if json.Unmarshal([]byte(body), &check) != nil {
		return nil, false, fmt.Errorf("invalid trajectory JSON from exec")
	}
	return []byte(body), true, nil
}
