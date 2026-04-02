package main

import (
	"testing"
)

func TestBudgetTrackerSetTask(t *testing.T) {
	bt := &BudgetTracker{
		taskLimit:    2.00,
		dailyLimit:   10.00,
		monthlyLimit: 200.00,
		cacheTTL:     0, // disable cache for tests
	}

	bt.SetTask("task-1")
	if bt.currentTask != "task-1" {
		t.Errorf("currentTask = %q, want task-1", bt.currentTask)
	}

	// Record some cost
	bt.mu.Lock()
	bt.taskCostUSD = 1.50
	bt.mu.Unlock()

	// Switching task should reset cost
	bt.SetTask("task-2")
	if bt.taskCostUSD != 0 {
		t.Errorf("taskCostUSD after task switch = %f, want 0", bt.taskCostUSD)
	}
}

func TestBudgetTrackerCheckBudgetTaskExhausted(t *testing.T) {
	bt := &BudgetTracker{
		taskLimit:    2.00,
		dailyLimit:   10.00,
		monthlyLimit: 200.00,
		cacheTTL:     0,
	}

	bt.SetTask("task-1")
	bt.mu.Lock()
	bt.taskCostUSD = 2.50
	bt.mu.Unlock()

	result := bt.CheckBudget()
	if result == nil {
		t.Fatal("expected budget exhausted error")
	}
	if result.Error.Level != "task" {
		t.Errorf("Error.Level = %q, want task", result.Error.Level)
	}
	if result.Error.Type != "budget_exhausted" {
		t.Errorf("Error.Type = %q, want budget_exhausted", result.Error.Type)
	}
}

func TestBudgetTrackerCheckBudgetOK(t *testing.T) {
	bt := &BudgetTracker{
		taskLimit:    2.00,
		dailyLimit:   10.00,
		monthlyLimit: 200.00,
		cacheTTL:     0,
	}

	bt.SetTask("task-1")
	bt.mu.Lock()
	bt.taskCostUSD = 0.50
	bt.mu.Unlock()

	result := bt.CheckBudget()
	if result != nil {
		t.Errorf("expected nil (budget OK), got %+v", result)
	}
}

func TestBudgetTrackerGetRemaining(t *testing.T) {
	bt := &BudgetTracker{
		taskLimit:    2.00,
		dailyLimit:   10.00,
		monthlyLimit: 200.00,
		cacheTTL:     0,
	}

	bt.SetTask("task-1")
	bt.mu.Lock()
	bt.taskCostUSD = 0.75
	bt.mu.Unlock()

	result := bt.GetRemaining()
	if result["per_task_used"] != 0.75 {
		t.Errorf("per_task_used = %v, want 0.75", result["per_task_used"])
	}
	if result["per_task_limit"] != 2.00 {
		t.Errorf("per_task_limit = %v, want 2.00", result["per_task_limit"])
	}
}

func TestEnvFloat(t *testing.T) {
	t.Setenv("TEST_FLOAT", "3.14")
	if v := envFloat("TEST_FLOAT", 0); v != 3.14 {
		t.Errorf("envFloat = %f, want 3.14", v)
	}
	if v := envFloat("NONEXISTENT", 1.0); v != 1.0 {
		t.Errorf("envFloat fallback = %f, want 1.0", v)
	}
}
