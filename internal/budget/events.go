package budget

// Budget event type constants for the event bus.
const (
	EventBudgetTaskExhausted    = "budget_task_exhausted"
	EventBudgetInputRejected    = "budget_input_rejected"
	EventBudgetDailyWarning     = "budget_daily_warning"
	EventBudgetDailyExhausted   = "budget_daily_exhausted"
	EventBudgetMonthlyWarning   = "budget_monthly_warning"
	EventBudgetMonthlyExhausted = "budget_monthly_exhausted"
)
