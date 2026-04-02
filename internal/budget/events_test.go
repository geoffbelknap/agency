package budget

import "testing"

func TestBudgetEventConstants(t *testing.T) {
	events := []string{
		EventBudgetTaskExhausted,
		EventBudgetInputRejected,
		EventBudgetDailyWarning,
		EventBudgetDailyExhausted,
		EventBudgetMonthlyWarning,
		EventBudgetMonthlyExhausted,
	}
	seen := make(map[string]bool)
	for _, e := range events {
		if e == "" {
			t.Error("empty event constant")
		}
		if seen[e] {
			t.Errorf("duplicate event constant: %s", e)
		}
		seen[e] = true
	}
}
