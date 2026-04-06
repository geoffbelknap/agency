package routing

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
)

// ModelTaskStats holds aggregated performance metrics for a (model, task_type) pair.
type ModelTaskStats struct {
	Model        string  `json:"model"`
	TaskType     string  `json:"task_type"`
	TotalCalls   int     `json:"total_calls"`
	Retries      int     `json:"retries"`
	SuccessRate  float64 `json:"success_rate"`
	AvgLatencyMs float64 `json:"avg_latency_ms"`
	AvgInputTok  int     `json:"avg_input_tokens"`
	AvgOutputTok int     `json:"avg_output_tokens"`
	TotalCostUSD float64 `json:"total_cost_usd"`
	CostPer1K    float64 `json:"cost_per_1k"`
}

// RoutingSuggestion recommends switching from one model to another for a task type.
type RoutingSuggestion struct {
	ID              string         `json:"id"`
	TaskType        string         `json:"task_type"`
	CurrentModel    string         `json:"current_model"`
	SuggestedModel  string         `json:"suggested_model"`
	Reason          string         `json:"reason"`
	SavingsPercent  float64        `json:"savings_percent"`
	SavingsUSDPer1K float64        `json:"savings_usd_per_1k"`
	CurrentStats    ModelTaskStats `json:"current_stats"`
	SuggestedStats  ModelTaskStats `json:"suggested_stats"`
	CreatedAt       string         `json:"created_at"`
	Status          string         `json:"status"` // pending, approved, rejected
}

// CallRecord represents a single LLM call for routing optimization tracking.
type CallRecord struct {
	Model        string    `json:"model"`
	TaskType     string    `json:"task_type"`
	IsRetry      bool      `json:"is_retry"`
	InputTokens  int       `json:"input_tokens"`
	OutputTokens int       `json:"output_tokens"`
	CachedTokens int       `json:"cached_tokens"`
	LatencyMs    float64   `json:"latency_ms"`
	CostUSD      float64   `json:"cost_usd"`
	Timestamp    time.Time `json:"timestamp"`
}

// persistedState is the JSON structure saved to disk.
type persistedState struct {
	Calls       []CallRecord        `json:"calls"`
	Suggestions []RoutingSuggestion `json:"suggestions"`
}

// RoutingOptimizer tracks LLM call patterns and generates cost-saving suggestions.
type RoutingOptimizer struct {
	mu            sync.RWMutex
	calls         []CallRecord
	suggestions   []RoutingSuggestion
	statsPath     string
	windowDays    int
	minCalls      int
	minSuccess    float64
	minSavings    float64
	currentModels map[string]string // task_type → current preferred model
	recordCount   int               // tracks calls for lazy pruning
}

// OptimizerOption configures the RoutingOptimizer.
type OptimizerOption func(*RoutingOptimizer)

// WithWindowDays sets the sliding window size in days.
func WithWindowDays(days int) OptimizerOption {
	return func(o *RoutingOptimizer) { o.windowDays = days }
}

// WithMinCalls sets the minimum call count for a model to be considered.
func WithMinCalls(n int) OptimizerOption {
	return func(o *RoutingOptimizer) { o.minCalls = n }
}

// WithMinSuccess sets the minimum success rate for a model to be suggested.
func WithMinSuccess(rate float64) OptimizerOption {
	return func(o *RoutingOptimizer) { o.minSuccess = rate }
}

// WithMinSavings sets the minimum savings percentage to generate a suggestion.
func WithMinSavings(pct float64) OptimizerOption {
	return func(o *RoutingOptimizer) { o.minSavings = pct }
}

// NewOptimizer creates a RoutingOptimizer with the given persistence path and options.
func NewOptimizer(statsPath string, opts ...OptimizerOption) *RoutingOptimizer {
	o := &RoutingOptimizer{
		statsPath:     statsPath,
		windowDays:    7,
		minCalls:      20,
		minSuccess:    0.90,
		minSavings:    0.30,
		currentModels: make(map[string]string),
	}
	for _, opt := range opts {
		opt(o)
	}
	return o
}

// RecordCall appends a call record and lazily prunes old entries.
func (o *RoutingOptimizer) RecordCall(record CallRecord) {
	o.mu.Lock()
	defer o.mu.Unlock()

	o.calls = append(o.calls, record)
	o.recordCount++

	// Lazy pruning every 100 calls.
	if o.recordCount%100 == 0 {
		o.pruneLockedCalls()
	}
}

// pruneLockedCalls removes calls older than windowDays. Caller must hold mu.
func (o *RoutingOptimizer) pruneLockedCalls() {
	cutoff := time.Now().Add(-time.Duration(o.windowDays) * 24 * time.Hour)
	n := 0
	for _, c := range o.calls {
		if !c.Timestamp.Before(cutoff) {
			o.calls[n] = c
			n++
		}
	}
	o.calls = o.calls[:n]
}

// ComputeStats groups calls by (task_type, model) within the sliding window
// and computes aggregate statistics.
func (o *RoutingOptimizer) ComputeStats() []ModelTaskStats {
	o.mu.RLock()
	defer o.mu.RUnlock()

	cutoff := time.Now().Add(-time.Duration(o.windowDays) * 24 * time.Hour)

	type key struct {
		taskType string
		model    string
	}
	type accum struct {
		total      int
		retries    int
		latencySum float64
		inputSum   int
		outputSum  int
		costSum    float64
	}

	groups := make(map[key]*accum)
	for _, c := range o.calls {
		if c.Timestamp.Before(cutoff) {
			continue
		}
		k := key{c.TaskType, c.Model}
		a, ok := groups[k]
		if !ok {
			a = &accum{}
			groups[k] = a
		}
		a.total++
		if c.IsRetry {
			a.retries++
		}
		a.latencySum += c.LatencyMs
		a.inputSum += c.InputTokens
		a.outputSum += c.OutputTokens
		a.costSum += c.CostUSD
	}

	stats := make([]ModelTaskStats, 0, len(groups))
	for k, a := range groups {
		successRate := float64(a.total-a.retries) / float64(a.total)
		costPer1K := 0.0
		if a.total > 0 {
			costPer1K = a.costSum / float64(a.total) * 1000
		}
		avgLatency := 0.0
		if a.total > 0 {
			avgLatency = a.latencySum / float64(a.total)
		}
		stats = append(stats, ModelTaskStats{
			Model:        k.model,
			TaskType:     k.taskType,
			TotalCalls:   a.total,
			Retries:      a.retries,
			SuccessRate:  successRate,
			AvgLatencyMs: avgLatency,
			AvgInputTok:  a.inputSum / a.total,
			AvgOutputTok: a.outputSum / a.total,
			TotalCostUSD: a.costSum,
			CostPer1K:    costPer1K,
		})
	}

	sort.Slice(stats, func(i, j int) bool {
		if stats[i].TaskType != stats[j].TaskType {
			return stats[i].TaskType < stats[j].TaskType
		}
		return stats[i].Model < stats[j].Model
	})

	return stats
}

// GenerateSuggestions compares alternative models against current preferred models
// and produces cost-saving suggestions when thresholds are met.
func (o *RoutingOptimizer) GenerateSuggestions() []RoutingSuggestion {
	stats := o.ComputeStats()

	o.mu.Lock()
	defer o.mu.Unlock()

	// Index stats by task_type → model → stats.
	byTask := make(map[string]map[string]ModelTaskStats)
	for _, s := range stats {
		m, ok := byTask[s.TaskType]
		if !ok {
			m = make(map[string]ModelTaskStats)
			byTask[s.TaskType] = m
		}
		m[s.Model] = s
	}

	// Track existing pending suggestions for dedup.
	pending := make(map[string]bool) // "task_type|suggested_model"
	for _, s := range o.suggestions {
		if s.Status == "pending" {
			pending[s.TaskType+"|"+s.SuggestedModel] = true
		}
	}

	var newSuggestions []RoutingSuggestion

	for taskType, models := range byTask {
		currentModel, ok := o.currentModels[taskType]
		if !ok {
			continue
		}
		currentStats, ok := models[currentModel]
		if !ok {
			continue
		}
		if currentStats.CostPer1K == 0 {
			continue
		}

		for altModel, altStats := range models {
			if altModel == currentModel {
				continue
			}
			if altStats.TotalCalls < o.minCalls {
				continue
			}
			if altStats.SuccessRate < o.minSuccess {
				continue
			}

			savings := (currentStats.CostPer1K - altStats.CostPer1K) / currentStats.CostPer1K
			if savings < o.minSavings {
				continue
			}

			dedupKey := taskType + "|" + altModel
			if pending[dedupKey] {
				continue
			}

			savingsUSD := currentStats.CostPer1K - altStats.CostPer1K

			suggestion := RoutingSuggestion{
				ID:              uuid.New().String(),
				TaskType:        taskType,
				CurrentModel:    currentModel,
				SuggestedModel:  altModel,
				Reason:          fmt.Sprintf("%s costs %.1f%% less than %s for %s tasks with %.0f%% success rate", altModel, savings*100, currentModel, taskType, altStats.SuccessRate*100),
				SavingsPercent:  savings,
				SavingsUSDPer1K: savingsUSD,
				CurrentStats:    currentStats,
				SuggestedStats:  altStats,
				CreatedAt:       time.Now().UTC().Format(time.RFC3339),
				Status:          "pending",
			}
			newSuggestions = append(newSuggestions, suggestion)
			pending[dedupKey] = true
		}
	}

	o.suggestions = append(o.suggestions, newSuggestions...)
	return newSuggestions
}

// Stats returns the current computed statistics (read-only snapshot).
func (o *RoutingOptimizer) Stats() []ModelTaskStats {
	return o.ComputeStats()
}

// Suggestions returns all suggestions (read-only snapshot).
func (o *RoutingOptimizer) Suggestions() []RoutingSuggestion {
	o.mu.RLock()
	defer o.mu.RUnlock()
	out := make([]RoutingSuggestion, len(o.suggestions))
	copy(out, o.suggestions)
	return out
}

// SetCurrentModels sets the task_type → preferred model mapping.
func (o *RoutingOptimizer) SetCurrentModels(m map[string]string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.currentModels = make(map[string]string, len(m))
	for k, v := range m {
		o.currentModels[k] = v
	}
}

// Approve marks a suggestion as approved and returns it.
func (o *RoutingOptimizer) Approve(id string) (*RoutingSuggestion, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	for i := range o.suggestions {
		if o.suggestions[i].ID == id {
			o.suggestions[i].Status = "approved"
			s := o.suggestions[i]
			return &s, nil
		}
	}
	return nil, fmt.Errorf("suggestion %s not found", id)
}

// Reject marks a suggestion as rejected.
func (o *RoutingOptimizer) Reject(id string) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	for i := range o.suggestions {
		if o.suggestions[i].ID == id {
			o.suggestions[i].Status = "rejected"
			return nil
		}
	}
	return fmt.Errorf("suggestion %s not found", id)
}

// Save persists calls and suggestions to the stats file.
func (o *RoutingOptimizer) Save() error {
	o.mu.RLock()
	state := persistedState{
		Calls:       o.calls,
		Suggestions: o.suggestions,
	}
	o.mu.RUnlock()

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal routing stats: %w", err)
	}

	dir := filepath.Dir(o.statsPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create stats dir: %w", err)
	}

	return os.WriteFile(o.statsPath, data, 0o644)
}

// Load reads persisted calls and suggestions from the stats file.
func (o *RoutingOptimizer) Load() error {
	data, err := os.ReadFile(o.statsPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // no state yet
		}
		return fmt.Errorf("read routing stats: %w", err)
	}

	var state persistedState
	if err := json.Unmarshal(data, &state); err != nil {
		return fmt.Errorf("unmarshal routing stats: %w", err)
	}

	o.mu.Lock()
	defer o.mu.Unlock()
	o.calls = state.Calls
	o.suggestions = state.Suggestions
	return nil
}
