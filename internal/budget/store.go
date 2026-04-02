package budget

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/geoffbelknap/agency/internal/models"
)

// Store manages budget state persistence at ~/.agency/budget/{agent}.json.
type Store struct {
	baseDir string
	mu      sync.Mutex
}

// NewStore creates a budget store at the given directory.
func NewStore(baseDir string) *Store {
	return &Store{baseDir: baseDir}
}

// Load reads the budget state for an agent, creating defaults if not found.
func (s *Store) Load(agentName string) (*models.BudgetState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	path := s.statePath(agentName)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return s.newState(agentName), nil
		}
		return nil, fmt.Errorf("read budget state: %w", err)
	}

	var state models.BudgetState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("parse budget state: %w", err)
	}

	// Handle date rollover
	s.rollover(&state)
	return &state, nil
}

// Save writes the budget state for an agent.
func (s *Store) Save(state *models.BudgetState) error {
	if err := os.MkdirAll(s.baseDir, 0755); err != nil {
		return fmt.Errorf("create budget dir: %w", err)
	}

	state.UpdatedAt = time.Now().UTC()
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal budget state: %w", err)
	}

	target := s.statePath(state.AgentName)
	tmp := target + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, target)
}

// RecordCost adds cost to daily and monthly totals.
func (s *Store) RecordCost(agentName string, costUSD float64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	state, err := s.loadUnlocked(agentName)
	if err != nil {
		return err
	}

	s.rollover(state)
	state.DailyUsed += costUSD
	state.MonthlyUsed += costUSD
	state.UpdatedAt = time.Now().UTC()

	return s.saveUnlocked(state)
}

// RecordTaskUsage records cost for a specific task.
func (s *Store) RecordTaskUsage(agentName string, usage models.TaskUsage) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	state, err := s.loadUnlocked(agentName)
	if err != nil {
		return err
	}

	s.rollover(state)
	state.DailyUsed += usage.CostUSD
	state.MonthlyUsed += usage.CostUSD

	// Update existing task or append new
	found := false
	for i := range state.TaskUsage {
		if state.TaskUsage[i].TaskID == usage.TaskID {
			state.TaskUsage[i].CostUSD += usage.CostUSD
			state.TaskUsage[i].InputTokens += usage.InputTokens
			state.TaskUsage[i].OutputTokens += usage.OutputTokens
			state.TaskUsage[i].CachedTokens += usage.CachedTokens
			state.TaskUsage[i].LLMCalls += usage.LLMCalls
			state.TaskUsage[i].EndedAt = time.Now().UTC()
			found = true
			break
		}
	}
	if !found {
		if usage.StartedAt.IsZero() {
			usage.StartedAt = time.Now().UTC()
		}
		state.TaskUsage = append(state.TaskUsage, usage)
		// Keep only last 50 tasks
		if len(state.TaskUsage) > 50 {
			state.TaskUsage = state.TaskUsage[len(state.TaskUsage)-50:]
		}
	}

	state.UpdatedAt = time.Now().UTC()
	return s.saveUnlocked(state)
}

// Remaining calculates remaining budget given limits.
func (s *Store) Remaining(agentName string, limits models.PlatformBudgetConfig) (*models.BudgetRemaining, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	state, err := s.loadUnlocked(agentName)
	if err != nil {
		return nil, err
	}

	s.rollover(state)

	dailyRemaining := limits.AgentDaily - state.DailyUsed
	if dailyRemaining < 0 {
		dailyRemaining = 0
	}
	monthlyRemaining := limits.AgentMonthly - state.MonthlyUsed
	if monthlyRemaining < 0 {
		monthlyRemaining = 0
	}

	return &models.BudgetRemaining{
		PerTask:          limits.PerTask,
		PerTaskLimit:     limits.PerTask,
		DailyUsed:        state.DailyUsed,
		DailyLimit:       limits.AgentDaily,
		DailyRemaining:   dailyRemaining,
		MonthlyUsed:      state.MonthlyUsed,
		MonthlyLimit:     limits.AgentMonthly,
		MonthlyRemaining: monthlyRemaining,
	}, nil
}

func (s *Store) statePath(agentName string) string {
	return filepath.Join(s.baseDir, agentName+".json")
}

func (s *Store) newState(agentName string) *models.BudgetState {
	now := time.Now().UTC()
	return &models.BudgetState{
		AgentName:    agentName,
		DailyDate:    now.Format("2006-01-02"),
		MonthlyMonth: now.Format("2006-01"),
		UpdatedAt:    now,
	}
}

func (s *Store) rollover(state *models.BudgetState) {
	now := time.Now().UTC()
	today := now.Format("2006-01-02")
	month := now.Format("2006-01")

	if state.DailyDate != today {
		state.DailyUsed = 0
		state.DailyDate = today
	}
	if state.MonthlyMonth != month {
		state.MonthlyUsed = 0
		state.MonthlyMonth = month
		state.TaskUsage = nil // Clear task history on month rollover
	}
}

func (s *Store) loadUnlocked(agentName string) (*models.BudgetState, error) {
	path := s.statePath(agentName)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return s.newState(agentName), nil
		}
		return nil, fmt.Errorf("read budget state: %w", err)
	}
	var state models.BudgetState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("parse budget state: %w", err)
	}
	return &state, nil
}

func (s *Store) saveUnlocked(state *models.BudgetState) error {
	if err := os.MkdirAll(s.baseDir, 0755); err != nil {
		return fmt.Errorf("create budget dir: %w", err)
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal budget state: %w", err)
	}
	target := s.statePath(state.AgentName)
	tmp := target + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, target)
}
