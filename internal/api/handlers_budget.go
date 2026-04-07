package api

import (
	"net/http"
	"path/filepath"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/geoffbelknap/agency/internal/budget"
	"github.com/geoffbelknap/agency/internal/models"
	"github.com/geoffbelknap/agency/internal/routing"
)

// budgetStore returns the legacy file-based budget store.
// Still used by infra LLM budget tracking and MCP tools.
func (d *mcpDeps) budgetStore() *budget.Store {
	return budget.NewStore(filepath.Join(d.cfg.Home, "budget"))
}

func (d *mcpDeps) budgetConfig() models.PlatformBudgetConfig {
	return models.DefaultPlatformBudgetConfig()
}

// getBudget returns full budget state for an agent.
// Computes usage from enforcer audit logs (same source as routing metrics)
// so it works even without the enforcer POSTing costs to /budget/record.
func (d *mcpDeps) getBudget(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	limits := d.budgetConfig()

	// Compute today's usage from audit logs
	now := time.Now().UTC()
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)

	costs := loadModelCosts(d.cfg.Home)

	// Today's metrics
	todayMetrics, _ := routing.CollectWithCosts(d.cfg.Home, routing.MetricsQuery{
		Agent: name,
		Since: todayStart.Format(time.RFC3339),
	}, costs)

	// Monthly metrics
	monthMetrics, _ := routing.CollectWithCosts(d.cfg.Home, routing.MetricsQuery{
		Agent: name,
		Since: monthStart.Format(time.RFC3339),
	}, costs)

	dailyUsed := 0.0
	monthlyUsed := 0.0
	var todayCalls int
	var todayInput, todayOutput int64

	if todayMetrics != nil {
		dailyUsed = todayMetrics.Totals.EstCostUSD
		todayCalls = todayMetrics.Totals.Requests
		todayInput = todayMetrics.Totals.InputTokens
		todayOutput = todayMetrics.Totals.OutputTokens
	}
	if monthMetrics != nil {
		monthlyUsed = monthMetrics.Totals.EstCostUSD
	}

	resp := map[string]interface{}{
		"agent_name":          name,
		"daily_used":          dailyUsed,
		"daily_limit":         limits.AgentDaily,
		"daily_remaining":     limits.AgentDaily - dailyUsed,
		"monthly_used":        monthlyUsed,
		"monthly_limit":       limits.AgentMonthly,
		"monthly_remaining":   limits.AgentMonthly - monthlyUsed,
		"per_task_limit":      limits.PerTask,
		"today_llm_calls":     todayCalls,
		"today_input_tokens":  todayInput,
		"today_output_tokens": todayOutput,
	}
	writeJSON(w, 200, resp)
}

// getBudgetRemaining returns lightweight remaining budget (computed from audit logs).
func (d *mcpDeps) getBudgetRemaining(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	limits := d.budgetConfig()
	costs := loadModelCosts(d.cfg.Home)

	now := time.Now().UTC()
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)

	todayMetrics, _ := routing.CollectWithCosts(d.cfg.Home, routing.MetricsQuery{
		Agent: name, Since: todayStart.Format(time.RFC3339),
	}, costs)
	monthMetrics, _ := routing.CollectWithCosts(d.cfg.Home, routing.MetricsQuery{
		Agent: name, Since: monthStart.Format(time.RFC3339),
	}, costs)

	dailyUsed := 0.0
	monthlyUsed := 0.0
	if todayMetrics != nil {
		dailyUsed = todayMetrics.Totals.EstCostUSD
	}
	if monthMetrics != nil {
		monthlyUsed = monthMetrics.Totals.EstCostUSD
	}

	writeJSON(w, 200, map[string]interface{}{
		"daily_used":        dailyUsed,
		"daily_limit":       limits.AgentDaily,
		"daily_remaining":   limits.AgentDaily - dailyUsed,
		"monthly_used":      monthlyUsed,
		"monthly_limit":     limits.AgentMonthly,
		"monthly_remaining": limits.AgentMonthly - monthlyUsed,
	})
}
