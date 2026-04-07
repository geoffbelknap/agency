package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"
)

// BudgetTracker tracks per-task and aggregate budget in the enforcer.
type BudgetTracker struct {
	mu           sync.Mutex
	currentTask  string
	taskCostUSD  float64
	taskLimit    float64
	dailyLimit   float64
	monthlyLimit float64
	agentName    string
	gatewayURL   string

	// Cached gateway budget remaining (refreshed every 30s)
	cachedRemaining *budgetRemainingResponse
	cachedAt        time.Time
	cacheTTL        time.Duration
}

type budgetRemainingResponse struct {
	PerTask          float64 `json:"per_task"`
	PerTaskLimit     float64 `json:"per_task_limit"`
	DailyUsed        float64 `json:"daily_used"`
	DailyLimit       float64 `json:"daily_limit"`
	DailyRemaining   float64 `json:"daily_remaining"`
	MonthlyUsed      float64 `json:"monthly_used"`
	MonthlyLimit     float64 `json:"monthly_limit"`
	MonthlyRemaining float64 `json:"monthly_remaining"`
}

// budgetErrorResponse is the JSON error for budget exhaustion.
type budgetErrorResponse struct {
	Error struct {
		Type    string `json:"type"`
		Message string `json:"message"`
		Level   string `json:"level"`
	} `json:"error"`
}

// NewBudgetTracker creates a budget tracker from environment variables.
func NewBudgetTracker(agentName string) *BudgetTracker {
	gatewayURL := os.Getenv("GATEWAY_URL")
	if gatewayURL == "" {
		gatewayURL = "http://gateway:8200"
	}
	return &BudgetTracker{
		agentName:    agentName,
		taskLimit:    envFloat("AGENCY_BUDGET_PER_TASK", 2.00),
		dailyLimit:   envFloat("AGENCY_BUDGET_DAILY", 10.00),
		monthlyLimit: envFloat("AGENCY_BUDGET_MONTHLY", 200.00),
		gatewayURL:   gatewayURL,
		cacheTTL:     30 * time.Second,
	}
}

// SetTask sets the current task ID. Resets per-task cost on task change.
func (bt *BudgetTracker) SetTask(taskID string) {
	bt.mu.Lock()
	defer bt.mu.Unlock()
	if taskID != "" && taskID != bt.currentTask {
		bt.currentTask = taskID
		bt.taskCostUSD = 0
	}
}

// RecordUsage adds token costs to the current task and reports to gateway.
func (bt *BudgetTracker) RecordUsage(inputTokens, outputTokens, cachedTokens int64, costPerMTokIn, costPerMTokOut, costPerMTokCached float64) {
	cost := float64(inputTokens)*costPerMTokIn/1_000_000 +
		float64(outputTokens)*costPerMTokOut/1_000_000 +
		float64(cachedTokens)*costPerMTokCached/1_000_000

	bt.mu.Lock()
	bt.taskCostUSD += cost
	bt.mu.Unlock()

	// Report to gateway asynchronously
	bt.reportCostToGateway(cost, inputTokens, outputTokens, cachedTokens)
}

// CheckBudget returns an error if any budget level is exhausted.
// Should be called before each LLM request.
func (bt *BudgetTracker) CheckBudget() *budgetErrorResponse {
	bt.mu.Lock()
	taskCost := bt.taskCostUSD
	taskLimit := bt.taskLimit
	bt.mu.Unlock()

	// Check per-task budget (local tracking)
	if taskLimit > 0 && taskCost >= taskLimit {
		resp := &budgetErrorResponse{}
		resp.Error.Type = "budget_exhausted"
		resp.Error.Message = fmt.Sprintf("Per-task budget exhausted ($%.2f/$%.2f)", taskCost, taskLimit)
		resp.Error.Level = "task"
		return resp
	}

	// Check daily/monthly from cached gateway data
	remaining := bt.getCachedRemaining()
	if remaining != nil {
		if remaining.DailyLimit > 0 && remaining.DailyRemaining <= 0 {
			resp := &budgetErrorResponse{}
			resp.Error.Type = "budget_exhausted"
			resp.Error.Message = fmt.Sprintf("Daily budget exhausted ($%.2f/$%.2f)", remaining.DailyUsed, remaining.DailyLimit)
			resp.Error.Level = "daily"
			return resp
		}
		if remaining.MonthlyLimit > 0 && remaining.MonthlyRemaining <= 0 {
			resp := &budgetErrorResponse{}
			resp.Error.Type = "budget_exhausted"
			resp.Error.Message = fmt.Sprintf("Monthly budget exhausted ($%.2f/$%.2f)", remaining.MonthlyUsed, remaining.MonthlyLimit)
			resp.Error.Level = "monthly"
			return resp
		}
	}

	return nil
}

// GetRemaining returns the current budget status for the body runtime.
func (bt *BudgetTracker) GetRemaining() map[string]interface{} {
	bt.mu.Lock()
	taskCost := bt.taskCostUSD
	taskLimit := bt.taskLimit
	dailyLimit := bt.dailyLimit
	monthlyLimit := bt.monthlyLimit
	bt.mu.Unlock()

	remaining := bt.getCachedRemaining()

	result := map[string]interface{}{
		"per_task_used":     taskCost,
		"per_task_limit":    taskLimit,
		"daily_limit":       dailyLimit,
		"monthly_limit":     monthlyLimit,
		"daily_used":        0.0,
		"daily_remaining":   dailyLimit,
		"monthly_used":      0.0,
		"monthly_remaining": monthlyLimit,
	}

	if remaining != nil {
		result["daily_used"] = remaining.DailyUsed
		result["daily_remaining"] = remaining.DailyRemaining
		result["monthly_used"] = remaining.MonthlyUsed
		result["monthly_remaining"] = remaining.MonthlyRemaining
	}

	return result
}

func (bt *BudgetTracker) getCachedRemaining() *budgetRemainingResponse {
	bt.mu.Lock()
	cached := bt.cachedRemaining
	cachedAt := bt.cachedAt
	bt.mu.Unlock()

	if cached != nil && time.Since(cachedAt) < bt.cacheTTL {
		return cached
	}

	// Refresh from gateway
	remaining := bt.fetchGatewayRemaining()
	if remaining != nil {
		bt.mu.Lock()
		bt.cachedRemaining = remaining
		bt.cachedAt = time.Now()
		bt.mu.Unlock()
	}
	return remaining
}

func (bt *BudgetTracker) fetchGatewayRemaining() *budgetRemainingResponse {
	url := fmt.Sprintf("%s/api/v1/agents/%s/budget/remaining", bt.gatewayURL, bt.agentName)
	client := &http.Client{Timeout: 5 * time.Second}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		slog.Warn("budget: failed to create gateway request", "error", err)
		return nil
	}
	if token := os.Getenv("GATEWAY_TOKEN"); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := client.Do(req)
	if err != nil {
		slog.Warn("budget: gateway budget query failed", "error", err)
		return nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		io.Copy(io.Discard, resp.Body)
		slog.Warn("budget: gateway budget query failed", "status", resp.StatusCode)
		return nil
	}

	var result budgetRemainingResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		slog.Warn("budget: failed to decode gateway budget response", "error", err)
		return nil
	}
	return &result
}

func (bt *BudgetTracker) reportCostToGateway(costUSD float64, inputTokens, outputTokens, cachedTokens int64) {
	go func() {
		url := fmt.Sprintf("%s/api/v1/agents/%s/budget/record", bt.gatewayURL, bt.agentName)
		payload := map[string]interface{}{
			"cost_usd":      costUSD,
			"input_tokens":  inputTokens,
			"output_tokens": outputTokens,
			"cached_tokens": cachedTokens,
			"task_id":       bt.currentTask,
		}
		body, err := json.Marshal(payload)
		if err != nil {
			return
		}
		client := &http.Client{Timeout: 5 * time.Second}
		req, err := http.NewRequest("POST", url, bytes.NewReader(body))
		if err != nil {
			return
		}
		req.Header.Set("Content-Type", "application/json")
		if token := os.Getenv("GATEWAY_TOKEN"); token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		resp, err := client.Do(req)
		if err != nil {
			slog.Warn("budget: failed to report cost to gateway", "error", err)
			return
		}
		resp.Body.Close()
	}()
}

func envFloat(key string, fallback float64) float64 {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return fallback
	}
	return f
}
