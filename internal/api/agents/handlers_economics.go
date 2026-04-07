package agents

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	apiinfra "github.com/geoffbelknap/agency/internal/api/infra"
	"github.com/geoffbelknap/agency/internal/routing"
)

// getAgentEconomics returns economics metrics for a specific agent (today).
//
//	GET /api/v1/agents/{name}/economics
func (h *handler) getAgentEconomics(w http.ResponseWriter, r *http.Request) {
	agentName := chi.URLParam(r, "name")
	if agentName == "" {
		writeJSON(w, 400, map[string]string{"error": "missing agent name"})
		return
	}

	costs := apiinfra.LoadModelCosts(h.deps.Config.Home)
	today := time.Now().UTC().Format("2006-01-02")

	summary, err := routing.CollectWithCosts(h.deps.Config.Home, routing.MetricsQuery{
		Agent: agentName,
		Since: today,
		Until: today,
	}, costs)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}

	t := summary.Totals
	hallRate := float64(0)
	if t.ToolCalls > 0 {
		hallRate = float64(t.ToolHallucinations) / float64(t.ToolCalls)
	}

	writeJSON(w, 200, map[string]interface{}{
		"agent":                   agentName,
		"period":                  today,
		"total_cost_usd":          t.EstCostUSD,
		"requests":                t.Requests,
		"input_tokens":            t.InputTokens,
		"output_tokens":           t.OutputTokens,
		"ttft_p50_ms":             t.TTFTP50Ms,
		"ttft_p95_ms":             t.TTFTP95Ms,
		"tpot_p50_ms":             t.TPOTP50Ms,
		"tpot_p95_ms":             t.TPOTP95Ms,
		"tool_calls":              t.ToolCalls,
		"tool_hallucinations":     t.ToolHallucinations,
		"tool_hallucination_rate": hallRate,
		"retry_waste_usd":         t.RetryCostUSD,
		"avg_latency_ms":          t.AvgLatencyMs,
		"p95_latency_ms":          t.P95LatencyMs,
		"by_model":                summary.ByModel,
		"cache_hits":              0,
		"cache_hit_rate":          0.0,
		"cache_saved_usd":         0.0,
	})
}

// getEconomicsSummary returns platform-wide economics metrics (today).
//
//	GET /api/v1/agents/economics/summary
func (h *handler) getEconomicsSummary(w http.ResponseWriter, r *http.Request) {
	costs := apiinfra.LoadModelCosts(h.deps.Config.Home)
	today := time.Now().UTC().Format("2006-01-02")

	summary, err := routing.CollectWithCosts(h.deps.Config.Home, routing.MetricsQuery{
		Since: today,
		Until: today,
	}, costs)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}

	t := summary.Totals
	hallRate := float64(0)
	if t.ToolCalls > 0 {
		hallRate = float64(t.ToolHallucinations) / float64(t.ToolCalls)
	}

	writeJSON(w, 200, map[string]interface{}{
		"period":                  today,
		"total_cost_usd":          t.EstCostUSD,
		"requests":                t.Requests,
		"ttft_p50_ms":             t.TTFTP50Ms,
		"ttft_p95_ms":             t.TTFTP95Ms,
		"tool_hallucination_rate": hallRate,
		"retry_waste_usd":         t.RetryCostUSD,
		"by_agent":                summary.ByAgent,
		"by_model":                summary.ByModel,
		"cache_hits":              0,
		"cache_hit_rate":          0.0,
		"cache_saved_usd":         0.0,
	})
}
