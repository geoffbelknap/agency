package budget

import (
	"os"
	"testing"
	"time"

	"github.com/geoffbelknap/agency/internal/models"
)

func TestStoreLoadMissing(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)

	state, err := store.Load("test-agent")
	if err != nil {
		t.Fatal(err)
	}
	if state.AgentName != "test-agent" {
		t.Errorf("AgentName = %q, want test-agent", state.AgentName)
	}
	if state.DailyUsed != 0 {
		t.Errorf("DailyUsed = %f, want 0", state.DailyUsed)
	}
	today := time.Now().UTC().Format("2006-01-02")
	if state.DailyDate != today {
		t.Errorf("DailyDate = %q, want %q", state.DailyDate, today)
	}
}

func TestStoreRecordCost(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)

	if err := store.RecordCost("test-agent", 1.50); err != nil {
		t.Fatal(err)
	}
	if err := store.RecordCost("test-agent", 0.50); err != nil {
		t.Fatal(err)
	}

	state, err := store.Load("test-agent")
	if err != nil {
		t.Fatal(err)
	}
	if state.DailyUsed != 2.00 {
		t.Errorf("DailyUsed = %f, want 2.00", state.DailyUsed)
	}
	if state.MonthlyUsed != 2.00 {
		t.Errorf("MonthlyUsed = %f, want 2.00", state.MonthlyUsed)
	}
}

func TestStoreRemaining(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)

	_ = store.RecordCost("test-agent", 3.00)

	limits := models.DefaultPlatformBudgetConfig()
	rem, err := store.Remaining("test-agent", limits)
	if err != nil {
		t.Fatal(err)
	}
	if rem.DailyRemaining != 7.00 {
		t.Errorf("DailyRemaining = %f, want 7.00", rem.DailyRemaining)
	}
	if rem.MonthlyRemaining != 197.00 {
		t.Errorf("MonthlyRemaining = %f, want 197.00", rem.MonthlyRemaining)
	}
	if rem.PerTask != 2.00 {
		t.Errorf("PerTask = %f, want 2.00", rem.PerTask)
	}
}

func TestStoreDailyRollover(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)

	// Create state with yesterday's date
	state := store.newState("test-agent")
	state.DailyUsed = 5.00
	state.DailyDate = "2020-01-01"
	state.MonthlyUsed = 50.00
	if err := store.Save(state); err != nil {
		t.Fatal(err)
	}

	loaded, err := store.Load("test-agent")
	if err != nil {
		t.Fatal(err)
	}
	if loaded.DailyUsed != 0 {
		t.Errorf("DailyUsed after rollover = %f, want 0", loaded.DailyUsed)
	}
	if loaded.MonthlyUsed != 50.00 {
		t.Errorf("MonthlyUsed should not reset on daily rollover, got %f", loaded.MonthlyUsed)
	}
}

func TestStoreMonthlyRollover(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)

	state := store.newState("test-agent")
	state.DailyUsed = 5.00
	state.DailyDate = "2020-01-01"
	state.MonthlyUsed = 100.00
	state.MonthlyMonth = "2020-01"
	if err := store.Save(state); err != nil {
		t.Fatal(err)
	}

	loaded, err := store.Load("test-agent")
	if err != nil {
		t.Fatal(err)
	}
	if loaded.MonthlyUsed != 0 {
		t.Errorf("MonthlyUsed after rollover = %f, want 0", loaded.MonthlyUsed)
	}
}

func TestStoreRecordTaskUsage(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)

	usage := models.TaskUsage{
		TaskID:       "task-1",
		CostUSD:      0.50,
		InputTokens:  1000,
		OutputTokens: 500,
		LLMCalls:     1,
		Model:        "claude-sonnet",
	}
	if err := store.RecordTaskUsage("test-agent", usage); err != nil {
		t.Fatal(err)
	}
	// Record again for same task
	if err := store.RecordTaskUsage("test-agent", usage); err != nil {
		t.Fatal(err)
	}

	state, err := store.Load("test-agent")
	if err != nil {
		t.Fatal(err)
	}
	if len(state.TaskUsage) != 1 {
		t.Fatalf("TaskUsage len = %d, want 1", len(state.TaskUsage))
	}
	if state.TaskUsage[0].CostUSD != 1.00 {
		t.Errorf("TaskUsage CostUSD = %f, want 1.00", state.TaskUsage[0].CostUSD)
	}
	if state.DailyUsed != 1.00 {
		t.Errorf("DailyUsed = %f, want 1.00", state.DailyUsed)
	}
}

func TestStoreFileCreation(t *testing.T) {
	dir := t.TempDir()
	budgetDir := dir + "/budget"
	store := NewStore(budgetDir)

	if err := store.RecordCost("new-agent", 1.00); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(budgetDir + "/new-agent.json"); os.IsNotExist(err) {
		t.Error("budget file was not created")
	}
}
