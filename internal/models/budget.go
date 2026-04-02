package models

import "time"

// PlatformBudgetConfig holds USD-denominated budget limits.
type PlatformBudgetConfig struct {
	AgentDaily   float64 `yaml:"agent_daily" json:"agent_daily" validate:"gte=0"`
	AgentMonthly float64 `yaml:"agent_monthly" json:"agent_monthly" validate:"gte=0"`
	PerTask      float64 `yaml:"per_task" json:"per_task" validate:"gte=0"`
	InfraDaily   float64 `yaml:"infrastructure_daily" json:"infrastructure_daily" validate:"gte=0"`
}

// DefaultPlatformBudgetConfig returns platform defaults.
func DefaultPlatformBudgetConfig() PlatformBudgetConfig {
	return PlatformBudgetConfig{
		AgentDaily:   10.00,
		AgentMonthly: 200.00,
		PerTask:      2.00,
		InfraDaily:   5.00,
	}
}

// BudgetState tracks cumulative spending for an agent.
type BudgetState struct {
	AgentName    string      `json:"agent_name"`
	DailyUsed    float64     `json:"daily_used"`
	DailyDate    string      `json:"daily_date"`    // YYYY-MM-DD
	MonthlyUsed  float64     `json:"monthly_used"`
	MonthlyMonth string      `json:"monthly_month"` // YYYY-MM
	TaskUsage    []TaskUsage `json:"task_usage,omitempty"`
	UpdatedAt    time.Time   `json:"updated_at"`
}

// TaskUsage tracks cost for a single task.
type TaskUsage struct {
	TaskID       string    `json:"task_id"`
	MissionID    string    `json:"mission_id,omitempty"`
	CostUSD      float64   `json:"cost_usd"`
	InputTokens  int64     `json:"input_tokens"`
	OutputTokens int64     `json:"output_tokens"`
	CachedTokens int64     `json:"cached_tokens"`
	LLMCalls     int       `json:"llm_calls"`
	Model        string    `json:"model"`
	StartedAt    time.Time `json:"started_at"`
	EndedAt      time.Time `json:"ended_at,omitempty"`
}

// BudgetRemaining returns remaining budget at each level.
type BudgetRemaining struct {
	PerTask        float64 `json:"per_task"`
	PerTaskLimit   float64 `json:"per_task_limit"`
	DailyUsed      float64 `json:"daily_used"`
	DailyLimit     float64 `json:"daily_limit"`
	DailyRemaining float64 `json:"daily_remaining"`
	MonthlyUsed    float64 `json:"monthly_used"`
	MonthlyLimit   float64 `json:"monthly_limit"`
	MonthlyRemaining float64 `json:"monthly_remaining"`
}
