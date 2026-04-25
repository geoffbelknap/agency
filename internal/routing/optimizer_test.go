package routing

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

func newTestOptimizer(t *testing.T) *RoutingOptimizer {
	t.Helper()
	dir := t.TempDir()
	return NewOptimizer(filepath.Join(dir, "stats.json"),
		WithWindowDays(7),
		WithMinCalls(5),
		WithMinSuccess(0.90),
		WithMinSavings(0.30),
	)
}

func TestRecordCallAddsToList(t *testing.T) {
	opt := newTestOptimizer(t)
	opt.RecordCall(CallRecord{
		Model:    "provider-a-standard",
		TaskType: "analysis",
		CostUSD:  0.01,
		Timestamp: time.Now(),
	})
	opt.mu.RLock()
	defer opt.mu.RUnlock()
	if len(opt.calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(opt.calls))
	}
}

func TestComputeStatsGroupsByTaskTypeAndModel(t *testing.T) {
	opt := newTestOptimizer(t)
	now := time.Now()

	for i := 0; i < 3; i++ {
		opt.RecordCall(CallRecord{Model: "provider-a-standard", TaskType: "analysis", InputTokens: 100, OutputTokens: 50, CostUSD: 0.01, LatencyMs: 200, Timestamp: now})
	}
	for i := 0; i < 2; i++ {
		opt.RecordCall(CallRecord{Model: "provider-b-fast", TaskType: "analysis", InputTokens: 80, OutputTokens: 40, CostUSD: 0.005, LatencyMs: 150, Timestamp: now})
	}
	opt.RecordCall(CallRecord{Model: "provider-a-standard", TaskType: "coding", InputTokens: 200, OutputTokens: 100, CostUSD: 0.02, LatencyMs: 300, Timestamp: now})

	stats := opt.ComputeStats()
	if len(stats) != 3 {
		t.Fatalf("expected 3 stat groups, got %d", len(stats))
	}

	// Sorted by task_type then model: analysis/provider-a-standard, analysis/provider-b-fast, coding/provider-a-standard
	if stats[0].TaskType != "analysis" || stats[0].Model != "provider-a-standard" {
		t.Errorf("unexpected first entry: %s/%s", stats[0].TaskType, stats[0].Model)
	}
	if stats[1].TaskType != "analysis" || stats[1].Model != "provider-b-fast" {
		t.Errorf("unexpected second entry: %s/%s", stats[1].TaskType, stats[1].Model)
	}
	if stats[2].TaskType != "coding" || stats[2].Model != "provider-a-standard" {
		t.Errorf("unexpected third entry: %s/%s", stats[2].TaskType, stats[2].Model)
	}
}

func TestSuccessRateComputed(t *testing.T) {
	opt := newTestOptimizer(t)
	now := time.Now()

	// 5 calls, 1 retry → success rate = (5-1)/5 = 0.80
	for i := 0; i < 4; i++ {
		opt.RecordCall(CallRecord{Model: "provider-a-standard", TaskType: "analysis", CostUSD: 0.01, InputTokens: 100, OutputTokens: 50, Timestamp: now})
	}
	opt.RecordCall(CallRecord{Model: "provider-a-standard", TaskType: "analysis", IsRetry: true, CostUSD: 0.01, InputTokens: 100, OutputTokens: 50, Timestamp: now})

	stats := opt.ComputeStats()
	if len(stats) != 1 {
		t.Fatalf("expected 1 stat group, got %d", len(stats))
	}
	if stats[0].SuccessRate != 0.80 {
		t.Errorf("expected success rate 0.80, got %f", stats[0].SuccessRate)
	}
	if stats[0].Retries != 1 {
		t.Errorf("expected 1 retry, got %d", stats[0].Retries)
	}
}

func TestCostPer1KComputed(t *testing.T) {
	opt := newTestOptimizer(t)
	now := time.Now()

	// 4 calls at $0.01 each → total $0.04, cost_per_1k = 0.04/4*1000 = $10.00
	for i := 0; i < 4; i++ {
		opt.RecordCall(CallRecord{Model: "provider-a-standard", TaskType: "analysis", CostUSD: 0.01, InputTokens: 100, OutputTokens: 50, Timestamp: now})
	}

	stats := opt.ComputeStats()
	if len(stats) != 1 {
		t.Fatalf("expected 1 stat group, got %d", len(stats))
	}
	expected := 10.0
	if stats[0].CostPer1K != expected {
		t.Errorf("expected cost_per_1k %f, got %f", expected, stats[0].CostPer1K)
	}
}

func TestGenerateSuggestionsCreatesWhenThresholdsMet(t *testing.T) {
	opt := newTestOptimizer(t)
	now := time.Now()

	// Current model: provider-a-standard for "analysis" — expensive
	opt.SetCurrentModels(map[string]string{"analysis": "provider-a-standard"})

	// provider-a-standard: 10 calls at $0.10 each
	for i := 0; i < 10; i++ {
		opt.RecordCall(CallRecord{Model: "provider-a-standard", TaskType: "analysis", CostUSD: 0.10, InputTokens: 1000, OutputTokens: 500, Timestamp: now})
	}

	// provider-b-fast: 10 calls at $0.02 each (80% cheaper, above 30% threshold)
	for i := 0; i < 10; i++ {
		opt.RecordCall(CallRecord{Model: "provider-b-fast", TaskType: "analysis", CostUSD: 0.02, InputTokens: 800, OutputTokens: 400, Timestamp: now})
	}

	suggestions := opt.GenerateSuggestions()
	if len(suggestions) != 1 {
		t.Fatalf("expected 1 suggestion, got %d", len(suggestions))
	}
	s := suggestions[0]
	if s.TaskType != "analysis" {
		t.Errorf("expected task_type analysis, got %s", s.TaskType)
	}
	if s.CurrentModel != "provider-a-standard" {
		t.Errorf("expected current model provider-a-standard, got %s", s.CurrentModel)
	}
	if s.SuggestedModel != "provider-b-fast" {
		t.Errorf("expected suggested model provider-b-fast, got %s", s.SuggestedModel)
	}
	if s.Status != "pending" {
		t.Errorf("expected status pending, got %s", s.Status)
	}
	if s.SavingsPercent < 0.30 {
		t.Errorf("expected savings >= 30%%, got %f", s.SavingsPercent)
	}
}

func TestGenerateSuggestionsSkipsInsufficientCalls(t *testing.T) {
	opt := newTestOptimizer(t)
	now := time.Now()

	opt.SetCurrentModels(map[string]string{"analysis": "provider-a-standard"})

	for i := 0; i < 10; i++ {
		opt.RecordCall(CallRecord{Model: "provider-a-standard", TaskType: "analysis", CostUSD: 0.10, InputTokens: 1000, OutputTokens: 500, Timestamp: now})
	}
	// Only 3 calls for provider-b-fast (below minCalls=5)
	for i := 0; i < 3; i++ {
		opt.RecordCall(CallRecord{Model: "provider-b-fast", TaskType: "analysis", CostUSD: 0.02, InputTokens: 800, OutputTokens: 400, Timestamp: now})
	}

	suggestions := opt.GenerateSuggestions()
	if len(suggestions) != 0 {
		t.Fatalf("expected 0 suggestions, got %d", len(suggestions))
	}
}

func TestGenerateSuggestionsSkipsLowSuccessRate(t *testing.T) {
	opt := newTestOptimizer(t)
	now := time.Now()

	opt.SetCurrentModels(map[string]string{"analysis": "provider-a-standard"})

	for i := 0; i < 10; i++ {
		opt.RecordCall(CallRecord{Model: "provider-a-standard", TaskType: "analysis", CostUSD: 0.10, InputTokens: 1000, OutputTokens: 500, Timestamp: now})
	}
	// 10 calls but 2 retries → 80% success (below minSuccess=90%)
	for i := 0; i < 8; i++ {
		opt.RecordCall(CallRecord{Model: "provider-b-fast", TaskType: "analysis", CostUSD: 0.02, InputTokens: 800, OutputTokens: 400, Timestamp: now})
	}
	for i := 0; i < 2; i++ {
		opt.RecordCall(CallRecord{Model: "provider-b-fast", TaskType: "analysis", CostUSD: 0.02, InputTokens: 800, OutputTokens: 400, IsRetry: true, Timestamp: now})
	}

	suggestions := opt.GenerateSuggestions()
	if len(suggestions) != 0 {
		t.Fatalf("expected 0 suggestions (low success rate), got %d", len(suggestions))
	}
}

func TestGenerateSuggestionsSkipsSmallSavings(t *testing.T) {
	opt := newTestOptimizer(t)
	now := time.Now()

	opt.SetCurrentModels(map[string]string{"analysis": "provider-a-standard"})

	for i := 0; i < 10; i++ {
		opt.RecordCall(CallRecord{Model: "provider-a-standard", TaskType: "analysis", CostUSD: 0.10, InputTokens: 1000, OutputTokens: 500, Timestamp: now})
	}
	// provider-b-fast at $0.08 -> only 20% savings (below minSavings=30%)
	for i := 0; i < 10; i++ {
		opt.RecordCall(CallRecord{Model: "provider-b-fast", TaskType: "analysis", CostUSD: 0.08, InputTokens: 800, OutputTokens: 400, Timestamp: now})
	}

	suggestions := opt.GenerateSuggestions()
	if len(suggestions) != 0 {
		t.Fatalf("expected 0 suggestions (small savings), got %d", len(suggestions))
	}
}

func TestApproveChangeStatus(t *testing.T) {
	opt := newTestOptimizer(t)
	now := time.Now()

	opt.SetCurrentModels(map[string]string{"analysis": "provider-a-standard"})
	for i := 0; i < 10; i++ {
		opt.RecordCall(CallRecord{Model: "provider-a-standard", TaskType: "analysis", CostUSD: 0.10, InputTokens: 1000, OutputTokens: 500, Timestamp: now})
	}
	for i := 0; i < 10; i++ {
		opt.RecordCall(CallRecord{Model: "provider-b-fast", TaskType: "analysis", CostUSD: 0.02, InputTokens: 800, OutputTokens: 400, Timestamp: now})
	}

	suggestions := opt.GenerateSuggestions()
	if len(suggestions) == 0 {
		t.Fatal("expected at least 1 suggestion")
	}

	approved, err := opt.Approve(suggestions[0].ID)
	if err != nil {
		t.Fatalf("approve failed: %v", err)
	}
	if approved.Status != "approved" {
		t.Errorf("expected status approved, got %s", approved.Status)
	}

	// Verify stored state also changed.
	all := opt.Suggestions()
	found := false
	for _, s := range all {
		if s.ID == suggestions[0].ID && s.Status == "approved" {
			found = true
		}
	}
	if !found {
		t.Error("approved suggestion not found in Suggestions()")
	}
}

func TestRejectChangeStatus(t *testing.T) {
	opt := newTestOptimizer(t)
	now := time.Now()

	opt.SetCurrentModels(map[string]string{"analysis": "provider-a-standard"})
	for i := 0; i < 10; i++ {
		opt.RecordCall(CallRecord{Model: "provider-a-standard", TaskType: "analysis", CostUSD: 0.10, InputTokens: 1000, OutputTokens: 500, Timestamp: now})
	}
	for i := 0; i < 10; i++ {
		opt.RecordCall(CallRecord{Model: "provider-b-fast", TaskType: "analysis", CostUSD: 0.02, InputTokens: 800, OutputTokens: 400, Timestamp: now})
	}

	suggestions := opt.GenerateSuggestions()
	if len(suggestions) == 0 {
		t.Fatal("expected at least 1 suggestion")
	}

	err := opt.Reject(suggestions[0].ID)
	if err != nil {
		t.Fatalf("reject failed: %v", err)
	}

	all := opt.Suggestions()
	for _, s := range all {
		if s.ID == suggestions[0].ID {
			if s.Status != "rejected" {
				t.Errorf("expected status rejected, got %s", s.Status)
			}
			return
		}
	}
	t.Error("rejected suggestion not found in Suggestions()")
}

func TestApproveNotFound(t *testing.T) {
	opt := newTestOptimizer(t)
	_, err := opt.Approve("nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent ID")
	}
}

func TestRejectNotFound(t *testing.T) {
	opt := newTestOptimizer(t)
	err := opt.Reject("nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent ID")
	}
}

func TestSaveLoadRoundtrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "stats.json")

	opt := NewOptimizer(path, WithMinCalls(5), WithMinSavings(0.30), WithMinSuccess(0.90))
	now := time.Now().UTC().Truncate(time.Second) // truncate for JSON roundtrip

	opt.RecordCall(CallRecord{
		Model: "provider-a-standard", TaskType: "analysis", CostUSD: 0.10,
		InputTokens: 1000, OutputTokens: 500, LatencyMs: 200, Timestamp: now,
	})
	opt.RecordCall(CallRecord{
		Model: "provider-b-fast", TaskType: "coding", CostUSD: 0.05,
		InputTokens: 800, OutputTokens: 400, LatencyMs: 150, Timestamp: now,
	})

	// Add a suggestion manually for roundtrip test.
	opt.mu.Lock()
	opt.suggestions = append(opt.suggestions, RoutingSuggestion{
		ID: "test-id", TaskType: "analysis", CurrentModel: "provider-a-standard",
		SuggestedModel: "provider-b-fast", Status: "pending",
	})
	opt.mu.Unlock()

	if err := opt.Save(); err != nil {
		t.Fatalf("save failed: %v", err)
	}

	// Verify file exists.
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("stats file not created: %v", err)
	}

	opt2 := NewOptimizer(path)
	if err := opt2.Load(); err != nil {
		t.Fatalf("load failed: %v", err)
	}

	opt2.mu.RLock()
	defer opt2.mu.RUnlock()

	if len(opt2.calls) != 2 {
		t.Fatalf("expected 2 calls after load, got %d", len(opt2.calls))
	}
	if opt2.calls[0].Model != "provider-a-standard" {
		t.Errorf("expected first call model provider-a-standard, got %s", opt2.calls[0].Model)
	}
	if len(opt2.suggestions) != 1 {
		t.Fatalf("expected 1 suggestion after load, got %d", len(opt2.suggestions))
	}
	if opt2.suggestions[0].ID != "test-id" {
		t.Errorf("expected suggestion ID test-id, got %s", opt2.suggestions[0].ID)
	}
}

func TestLoadNonexistentFile(t *testing.T) {
	opt := NewOptimizer("/nonexistent/path/stats.json")
	err := opt.Load()
	if err != nil {
		t.Fatalf("expected nil error for nonexistent file, got: %v", err)
	}
}

func TestSlidingWindowExcludesOldCalls(t *testing.T) {
	opt := newTestOptimizer(t) // windowDays=7
	now := time.Now()
	old := now.Add(-10 * 24 * time.Hour) // 10 days ago

	// Old calls should be excluded from stats.
	for i := 0; i < 5; i++ {
		opt.RecordCall(CallRecord{Model: "provider-a-standard", TaskType: "analysis", CostUSD: 0.10, InputTokens: 1000, OutputTokens: 500, Timestamp: old})
	}
	// Recent calls should be included.
	for i := 0; i < 3; i++ {
		opt.RecordCall(CallRecord{Model: "provider-a-standard", TaskType: "analysis", CostUSD: 0.05, InputTokens: 500, OutputTokens: 250, Timestamp: now})
	}

	stats := opt.ComputeStats()
	if len(stats) != 1 {
		t.Fatalf("expected 1 stat group, got %d", len(stats))
	}
	if stats[0].TotalCalls != 3 {
		t.Errorf("expected 3 calls (recent only), got %d", stats[0].TotalCalls)
	}
}

func TestDeduplicatePendingSuggestions(t *testing.T) {
	opt := newTestOptimizer(t)
	now := time.Now()

	opt.SetCurrentModels(map[string]string{"analysis": "provider-a-standard"})
	for i := 0; i < 10; i++ {
		opt.RecordCall(CallRecord{Model: "provider-a-standard", TaskType: "analysis", CostUSD: 0.10, InputTokens: 1000, OutputTokens: 500, Timestamp: now})
	}
	for i := 0; i < 10; i++ {
		opt.RecordCall(CallRecord{Model: "provider-b-fast", TaskType: "analysis", CostUSD: 0.02, InputTokens: 800, OutputTokens: 400, Timestamp: now})
	}

	s1 := opt.GenerateSuggestions()
	if len(s1) != 1 {
		t.Fatalf("expected 1 suggestion on first call, got %d", len(s1))
	}

	// Second call should not duplicate.
	s2 := opt.GenerateSuggestions()
	if len(s2) != 0 {
		t.Fatalf("expected 0 new suggestions on second call, got %d", len(s2))
	}

	// Total suggestions should still be 1.
	all := opt.Suggestions()
	pending := 0
	for _, s := range all {
		if s.Status == "pending" {
			pending++
		}
	}
	if pending != 1 {
		t.Errorf("expected 1 pending suggestion total, got %d", pending)
	}
}

func TestLazyPruning(t *testing.T) {
	dir := t.TempDir()
	opt := NewOptimizer(filepath.Join(dir, "stats.json"), WithWindowDays(1))

	old := time.Now().Add(-3 * 24 * time.Hour)
	recent := time.Now()

	// Add 99 old calls (no pruning yet).
	for i := 0; i < 99; i++ {
		opt.RecordCall(CallRecord{Model: "provider-a-standard", TaskType: "test", CostUSD: 0.01, InputTokens: 100, OutputTokens: 50, Timestamp: old})
	}
	opt.mu.RLock()
	beforePrune := len(opt.calls)
	opt.mu.RUnlock()
	if beforePrune != 99 {
		t.Fatalf("expected 99 calls before pruning, got %d", beforePrune)
	}

	// 100th call triggers pruning.
	opt.RecordCall(CallRecord{Model: "provider-a-standard", TaskType: "test", CostUSD: 0.01, InputTokens: 100, OutputTokens: 50, Timestamp: recent})

	opt.mu.RLock()
	afterPrune := len(opt.calls)
	opt.mu.RUnlock()

	// Only the recent call should survive.
	if afterPrune != 1 {
		t.Errorf("expected 1 call after pruning, got %d", afterPrune)
	}
}

func TestAvgLatencyComputed(t *testing.T) {
	opt := newTestOptimizer(t)
	now := time.Now()

	opt.RecordCall(CallRecord{Model: "provider-a-standard", TaskType: "analysis", CostUSD: 0.01, InputTokens: 100, OutputTokens: 50, LatencyMs: 100, Timestamp: now})
	opt.RecordCall(CallRecord{Model: "provider-a-standard", TaskType: "analysis", CostUSD: 0.01, InputTokens: 100, OutputTokens: 50, LatencyMs: 300, Timestamp: now})

	stats := opt.ComputeStats()
	if len(stats) != 1 {
		t.Fatalf("expected 1 stat group, got %d", len(stats))
	}
	if stats[0].AvgLatencyMs != 200 {
		t.Errorf("expected avg latency 200, got %f", stats[0].AvgLatencyMs)
	}
}

func TestApproveWritesLocalYAML(t *testing.T) {
	dir := t.TempDir()
	localPath := filepath.Join(dir, "routing.local.yaml")
	opt := NewOptimizer(filepath.Join(dir, "stats.json"),
		WithMinCalls(5),
		WithMinSuccess(0.90),
		WithMinSavings(0.30),
		WithLocalYAMLPath(localPath),
	)

	now := time.Now()
	opt.SetCurrentModels(map[string]string{"memory_capture": "provider-a-standard"})

	for i := 0; i < 10; i++ {
		opt.RecordCall(CallRecord{Model: "provider-a-standard", TaskType: "memory_capture", CostUSD: 0.10, InputTokens: 1000, OutputTokens: 500, Timestamp: now})
	}
	for i := 0; i < 10; i++ {
		opt.RecordCall(CallRecord{Model: "provider-b-fast", TaskType: "memory_capture", CostUSD: 0.02, InputTokens: 800, OutputTokens: 400, Timestamp: now})
	}

	suggestions := opt.GenerateSuggestions()
	if len(suggestions) == 0 {
		t.Fatal("expected at least 1 suggestion")
	}

	approved, err := opt.Approve(suggestions[0].ID)
	if err != nil {
		t.Fatalf("approve failed: %v", err)
	}
	if approved.Status != "approved" {
		t.Errorf("expected status approved, got %s", approved.Status)
	}

	// Verify routing.local.yaml was created.
	data, err := os.ReadFile(localPath)
	if err != nil {
		t.Fatalf("failed to read routing.local.yaml: %v", err)
	}

	var config localConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		t.Fatalf("failed to parse routing.local.yaml: %v", err)
	}

	override, ok := config.Overrides["memory_capture"]
	if !ok {
		t.Fatal("expected memory_capture override in routing.local.yaml")
	}
	if override.PreferredModel != "provider-b-fast" {
		t.Errorf("expected preferred_model provider-b-fast, got %s", override.PreferredModel)
	}
	if override.ApprovedFrom != suggestions[0].ID {
		t.Errorf("expected approved_from %s, got %s", suggestions[0].ID, override.ApprovedFrom)
	}
	if !strings.HasPrefix(override.ApprovedAt, "20") {
		t.Errorf("expected approved_at to be an RFC3339 timestamp, got %s", override.ApprovedAt)
	}
}

func TestApproveOverwritesExisting(t *testing.T) {
	dir := t.TempDir()
	localPath := filepath.Join(dir, "routing.local.yaml")

	// Pre-populate with an existing override.
	existing := localConfig{
		Overrides: map[string]localOverride{
			"memory_capture": {
				PreferredModel: "old-model",
				ApprovedFrom:   "sug-old",
				ApprovedAt:     "2026-01-01T00:00:00Z",
			},
		},
	}
	data, _ := yaml.Marshal(existing)
	if err := os.WriteFile(localPath, data, 0o644); err != nil {
		t.Fatalf("failed to write seed file: %v", err)
	}

	opt := NewOptimizer(filepath.Join(dir, "stats.json"),
		WithMinCalls(5),
		WithMinSuccess(0.90),
		WithMinSavings(0.30),
		WithLocalYAMLPath(localPath),
	)

	now := time.Now()
	opt.SetCurrentModels(map[string]string{"memory_capture": "provider-a-standard"})

	for i := 0; i < 10; i++ {
		opt.RecordCall(CallRecord{Model: "provider-a-standard", TaskType: "memory_capture", CostUSD: 0.10, InputTokens: 1000, OutputTokens: 500, Timestamp: now})
	}
	for i := 0; i < 10; i++ {
		opt.RecordCall(CallRecord{Model: "provider-b-fast", TaskType: "memory_capture", CostUSD: 0.02, InputTokens: 800, OutputTokens: 400, Timestamp: now})
	}

	suggestions := opt.GenerateSuggestions()
	if len(suggestions) == 0 {
		t.Fatal("expected at least 1 suggestion")
	}

	_, err := opt.Approve(suggestions[0].ID)
	if err != nil {
		t.Fatalf("approve failed: %v", err)
	}

	// Verify the override was updated.
	data, err = os.ReadFile(localPath)
	if err != nil {
		t.Fatalf("failed to read routing.local.yaml: %v", err)
	}

	var config localConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		t.Fatalf("failed to parse routing.local.yaml: %v", err)
	}

	override := config.Overrides["memory_capture"]
	if override.PreferredModel != "provider-b-fast" {
		t.Errorf("expected preferred_model provider-b-fast, got %s", override.PreferredModel)
	}
	if override.ApprovedFrom == "sug-old" {
		t.Error("expected approved_from to be updated, still has old value")
	}
	if override.ApprovedAt == "2026-01-01T00:00:00Z" {
		t.Error("expected approved_at to be updated, still has old value")
	}
}

func TestApproveNonexistentReturnsError(t *testing.T) {
	dir := t.TempDir()
	opt := NewOptimizer(filepath.Join(dir, "stats.json"),
		WithLocalYAMLPath(filepath.Join(dir, "routing.local.yaml")),
	)

	_, err := opt.Approve("sug-does-not-exist")
	if err == nil {
		t.Fatal("expected error for nonexistent suggestion ID")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' in error message, got: %s", err.Error())
	}

	// Verify no file was created.
	if _, err := os.Stat(filepath.Join(dir, "routing.local.yaml")); err == nil {
		t.Error("routing.local.yaml should not exist when approval fails")
	}
}
