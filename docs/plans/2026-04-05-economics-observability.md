# Economics Observability Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add granular per-call timing (TTFT, TPOT), workflow rollups (loop cost, steps to resolution, context expansion, tool hallucination rate), and a dedicated `/economics` API surface.

**Architecture:** Two workstreams. Enforcer side (Tasks 1-5): new fields on `AuditEntry`, TTFT measurement in streaming relays, step counter, tool call validation, retry header. Gateway side (Tasks 6-9): per-workflow rollups in routing metrics, `/economics` API endpoints, WebSocket signal, audit summarizer extension. Enforcer changes are a standalone Go binary at `images/enforcer/`; gateway changes are in `internal/`.

**Tech Stack:** Go (enforcer + gateway), JSONL audit logs, knowledge graph

**Spec:** `docs/specs/economics-observability.md`

---

### Task 1: Add new fields to AuditEntry

**Files:**
- Modify: `images/enforcer/audit.go:21-42`
- Test: `images/enforcer/audit_test.go`

- [ ] **Step 1: Write test for new fields**

Add to `images/enforcer/audit_test.go`:

```go
func TestAuditEntry_NewFields(t *testing.T) {
	entry := AuditEntry{
		Type:          "LLM_DIRECT_STREAM",
		Model:         "claude-sonnet",
		TTFTMs:        380,
		TPOTMs:        28.5,
		ContextTokens: 4200,
		StepIndex:     3,
	}
	valid := true
	entry.ToolCallValid = &valid
	entry.RetryOf = "corr-123"

	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatal(err)
	}

	// Verify fields are present in JSON
	var parsed map[string]interface{}
	json.Unmarshal(data, &parsed)

	if parsed["ttft_ms"] != float64(380) {
		t.Errorf("expected ttft_ms=380, got %v", parsed["ttft_ms"])
	}
	if parsed["step_index"] != float64(3) {
		t.Errorf("expected step_index=3, got %v", parsed["step_index"])
	}
	if parsed["tool_call_valid"] != true {
		t.Errorf("expected tool_call_valid=true, got %v", parsed["tool_call_valid"])
	}
	if parsed["retry_of"] != "corr-123" {
		t.Errorf("expected retry_of, got %v", parsed["retry_of"])
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd images/enforcer && go test -run TestAuditEntry_NewFields -v
```

Expected: Compilation error — new fields don't exist.

- [ ] **Step 3: Add new fields to AuditEntry struct**

In `images/enforcer/audit.go`, add before the `Extra` field (line 41):

```go
	CachedTokens  int               `json:"cached_tokens,omitempty"`
	TTFTMs        int64             `json:"ttft_ms,omitempty"`
	TPOTMs        float64           `json:"tpot_ms,omitempty"`
	ContextTokens int64             `json:"context_tokens,omitempty"`
	StepIndex     int               `json:"step_index,omitempty"`
	ToolCallValid *bool             `json:"tool_call_valid,omitempty"`
	RetryOf       string            `json:"retry_of,omitempty"`
```

- [ ] **Step 4: Run test**

```bash
cd images/enforcer && go test -run TestAuditEntry_NewFields -v
```

Expected: PASS.

- [ ] **Step 5: Run all enforcer tests**

```bash
cd images/enforcer && go test ./... -v 2>&1 | tail -10
```

Expected: All pass.

- [ ] **Step 6: Commit**

```bash
git add images/enforcer/audit.go images/enforcer/audit_test.go
git commit -m "feat(enforcer): add TTFT, TPOT, StepIndex, ToolCallValid, RetryOf to AuditEntry"
```

---

### Task 2: Implement StepIndex counter

**Files:**
- Modify: `images/enforcer/llm.go:43-53` (LLMHandler struct)
- Modify: `images/enforcer/llm.go` (ServeHTTP, around line 146)
- Test: `images/enforcer/llm_test.go`

- [ ] **Step 1: Write test**

Add to `images/enforcer/llm_test.go`:

```go
func TestStepIndexIncrement(t *testing.T) {
	lh := &LLMHandler{
		stepCounters: make(map[string]int),
	}

	// Same task ID increments
	idx1 := lh.nextStepIndex("task-1")
	idx2 := lh.nextStepIndex("task-1")
	idx3 := lh.nextStepIndex("task-1")
	if idx1 != 1 || idx2 != 2 || idx3 != 3 {
		t.Errorf("expected 1,2,3 got %d,%d,%d", idx1, idx2, idx3)
	}

	// Different task ID resets
	idx4 := lh.nextStepIndex("task-2")
	if idx4 != 1 {
		t.Errorf("expected 1 for new task, got %d", idx4)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd images/enforcer && go test -run TestStepIndex -v
```

- [ ] **Step 3: Implement step counter**

Add `stepCounters` field to `LLMHandler` struct:

```go
type LLMHandler struct {
	routing      *RoutingConfig
	proxy        string
	audit        *AuditLogger
	transport    *http.Transport
	rateLimiter  *RateLimiter
	agentName    string
	budget       *BudgetTracker
	toolTracker  *ToolTracker
	trajectory   *TrajectoryMonitor
	stepCounters map[string]int // task ID → step counter
	stepMu       sync.Mutex
}
```

Add the method:

```go
// nextStepIndex returns the next step index for a task, starting at 1.
// Resets when a new task ID is seen.
func (lh *LLMHandler) nextStepIndex(taskID string) int {
	lh.stepMu.Lock()
	defer lh.stepMu.Unlock()
	if lh.stepCounters == nil {
		lh.stepCounters = make(map[string]int)
	}
	lh.stepCounters[taskID]++
	return lh.stepCounters[taskID]
}
```

Initialize `stepCounters` where `LLMHandler` is created (find `NewLLMHandler` or the constructor).

In `ServeHTTP`, after the task ID extraction (around line 146), add:

```go
	var stepIndex int
	if taskID := r.Header.Get("X-Agency-Task-Id"); taskID != "" {
		stepIndex = lh.nextStepIndex(taskID)
	}
```

Then pass `stepIndex` to the audit log entries in all four relay functions.

- [ ] **Step 4: Run tests**

```bash
cd images/enforcer && go test ./... -v 2>&1 | tail -10
```

- [ ] **Step 5: Commit**

```bash
git add images/enforcer/llm.go images/enforcer/llm_test.go
git commit -m "feat(enforcer): add StepIndex counter per task"
```

---

### Task 3: Instrument TTFT in streaming relays

**Files:**
- Modify: `images/enforcer/llm.go` (relayStream and relayAnthropicStream)

- [ ] **Step 1: Add TTFT measurement to relayStream**

In `relayStream()` (around line 472), add a variable before the SSE read loop:

```go
	var ttftTime time.Time
```

On the first SSE data chunk received (inside the scanner loop, when a `data:` line arrives with actual content), record:

```go
	if ttftTime.IsZero() {
		ttftTime = time.Now()
	}
```

After the loop, compute TTFT and TPOT:

```go
	ttftMs := int64(0)
	if !ttftTime.IsZero() {
		ttftMs = ttftTime.Sub(start).Milliseconds()
	}
	tpotMs := float64(0)
	if outputTokens > 0 && ttftMs > 0 {
		durationMs := time.Since(start).Milliseconds()
		tpotMs = float64(durationMs-ttftMs) / float64(outputTokens)
	}
```

Add `TTFTMs: ttftMs` and `TPOTMs: tpotMs` to the audit entry.

- [ ] **Step 2: Same for relayAnthropicStream**

Apply the same pattern to `relayAnthropicStream()` (around line 592). The first SSE chunk from the Anthropic stream (`message_start` event) marks TTFT.

- [ ] **Step 3: Handle buffered responses**

For `relayBuffered()` and `relayAnthropicBuffered()`, TTFT equals DurationMs (the response arrives all at once). Set `TTFTMs: time.Since(start).Milliseconds()` in those audit entries.

- [ ] **Step 4: Run tests**

```bash
cd images/enforcer && go test ./... -v 2>&1 | tail -10
```

- [ ] **Step 5: Commit**

```bash
git add images/enforcer/llm.go
git commit -m "feat(enforcer): instrument TTFT and TPOT in streaming relays"
```

---

### Task 4: Add ToolCallValid check

**Files:**
- Modify: `images/enforcer/llm.go` (after tool call parsing in relayBuffered)

- [ ] **Step 1: Implement tool call validation**

In `relayBuffered()` (around line 397), after tool calls are parsed, add validation:

```go
	var toolCallValid *bool
	if toolCalls, ok := message["tool_calls"].([]interface{}); ok && len(toolCalls) > 0 {
		allValid := true
		for _, tc := range toolCalls {
			tcMap, ok := tc.(map[string]interface{})
			if !ok {
				allValid = false
				break
			}
			fn, ok := tcMap["function"].(map[string]interface{})
			if !ok {
				allValid = false
				break
			}
			argsStr, _ := fn["arguments"].(string)
			if argsStr != "" {
				var args json.RawMessage
				if json.Unmarshal([]byte(argsStr), &args) != nil {
					allValid = false
				}
			}
		}
		toolCallValid = &allValid
	}
```

Add `ToolCallValid: toolCallValid` to the audit entry.

- [ ] **Step 2: Run tests**

```bash
cd images/enforcer && go test ./... -v 2>&1 | tail -10
```

- [ ] **Step 3: Commit**

```bash
git add images/enforcer/llm.go
git commit -m "feat(enforcer): add ToolCallValid check for hallucination detection"
```

---

### Task 5: Add RetryOf header propagation

**Files:**
- Modify: `images/enforcer/llm.go` (ServeHTTP, around line 146)

- [ ] **Step 1: Read and propagate RetryOf header**

In `ServeHTTP`, after the task ID extraction, add:

```go
	retryOf := r.Header.Get("X-Agency-Retry-Of")
```

Pass `retryOf` to all four relay functions and add `RetryOf: retryOf` to their audit entries.

- [ ] **Step 2: Run tests**

```bash
cd images/enforcer && go test ./... -v 2>&1 | tail -10
```

- [ ] **Step 3: Commit**

```bash
git add images/enforcer/llm.go
git commit -m "feat(enforcer): propagate X-Agency-Retry-Of header for waste attribution"
```

---

### Task 6: Extend routing metrics with per-workflow rollups

**Files:**
- Modify: `internal/routing/metrics.go`
- Test: `internal/routing/metrics_test.go` (if it exists, add tests; if not, create)

- [ ] **Step 1: Add new fields to auditRecord and Totals**

In `internal/routing/metrics.go`, extend `auditRecord` struct:

```go
type auditRecord struct {
	Timestamp     string
	Type          string
	Agent         string
	Source        string
	Model         string
	ProviderModel string
	Status        int
	Error         string
	DurationMs    int64
	InputTokens   int
	OutputTokens  int
	TTFTMs        int64
	TPOTMs        float64
	StepIndex     int
	ToolCallValid *bool
	RetryOf       string
	ContextTokens int64
	TaskID        string
}
```

Extend `Totals` struct with new fields:

```go
type Totals struct {
	Requests           int     `json:"requests"`
	InputTokens        int64   `json:"input_tokens"`
	OutputTokens       int64   `json:"output_tokens"`
	TotalTokens        int64   `json:"total_tokens"`
	EstCostUSD         float64 `json:"est_cost_usd"`
	Errors             int     `json:"errors"`
	AvgLatencyMs       int64   `json:"avg_latency_ms"`
	P95LatencyMs       int64   `json:"p95_latency_ms"`
	TTFTP50Ms          int64   `json:"ttft_p50_ms,omitempty"`
	TTFTP95Ms          int64   `json:"ttft_p95_ms,omitempty"`
	TPOTP50Ms          float64 `json:"tpot_p50_ms,omitempty"`
	TPOTP95Ms          float64 `json:"tpot_p95_ms,omitempty"`
	ToolCalls          int     `json:"tool_calls,omitempty"`
	ToolHallucinations int     `json:"tool_hallucinations,omitempty"`
	RetryCostUSD       float64 `json:"retry_cost_usd,omitempty"`
	latencies          []int64   `json:"-"`
	ttfts              []int64   `json:"-"`
	tpots              []float64 `json:"-"`
	costAcc            float64   `json:"-"`
}
```

- [ ] **Step 2: Update accumWithCost to accumulate new fields**

```go
func accumWithCost(t *Totals, rec auditRecord, cost float64) {
	t.Requests++
	t.InputTokens += int64(rec.InputTokens)
	t.OutputTokens += int64(rec.OutputTokens)
	t.EstCostUSD += cost
	if rec.Status >= 400 || rec.Error != "" {
		t.Errors++
	}
	if rec.DurationMs > 0 {
		t.latencies = append(t.latencies, rec.DurationMs)
	}
	if rec.TTFTMs > 0 {
		t.ttfts = append(t.ttfts, rec.TTFTMs)
	}
	if rec.TPOTMs > 0 {
		t.tpots = append(t.tpots, rec.TPOTMs)
	}
	if rec.ToolCallValid != nil {
		t.ToolCalls++
		if !*rec.ToolCallValid {
			t.ToolHallucinations++
		}
	}
	if rec.RetryOf != "" {
		t.RetryCostUSD += cost
	}
}
```

- [ ] **Step 3: Update finalise to compute percentiles for TTFT and TPOT**

```go
func finalise(t *Totals) {
	t.TotalTokens = t.InputTokens + t.OutputTokens
	if len(t.latencies) > 0 {
		var sum int64
		for _, l := range t.latencies { sum += l }
		t.AvgLatencyMs = sum / int64(len(t.latencies))
		t.P95LatencyMs = percentile(t.latencies, 95)
	}
	if len(t.ttfts) > 0 {
		t.TTFTP50Ms = percentile(t.ttfts, 50)
		t.TTFTP95Ms = percentile(t.ttfts, 95)
	}
	if len(t.tpots) > 0 {
		sort.Float64s(t.tpots)
		idx50 := int(float64(len(t.tpots)) * 0.50)
		idx95 := int(float64(len(t.tpots)) * 0.95)
		if idx50 >= len(t.tpots) { idx50 = len(t.tpots) - 1 }
		if idx95 >= len(t.tpots) { idx95 = len(t.tpots) - 1 }
		t.TPOTP50Ms = t.tpots[idx50]
		t.TPOTP95Ms = t.tpots[idx95]
	}
	t.EstCostUSD = math.Round(t.EstCostUSD*1e6) / 1e6
	t.RetryCostUSD = math.Round(t.RetryCostUSD*1e6) / 1e6
	t.latencies = nil
	t.ttfts = nil
	t.tpots = nil
}
```

Add `"sort"` to imports if not present.

- [ ] **Step 4: Update readJSONLDir to parse new fields**

In the JSON parsing section, extract the new fields from audit log entries.

- [ ] **Step 5: Run tests**

```bash
go test ./internal/routing/ -v
```

- [ ] **Step 6: Commit**

```bash
git add internal/routing/metrics.go
git commit -m "feat(metrics): add TTFT/TPOT percentiles, tool hallucination rate, retry waste"
```

---

### Task 7: Add /economics API endpoints

**Files:**
- Create: `internal/api/handlers_economics.go`
- Modify: `internal/api/routes.go` (register new routes)

- [ ] **Step 1: Create handlers_economics.go**

```go
package api

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
)

// getAgentEconomics returns per-agent economics for the current day.
func (h *Handlers) getAgentEconomics(w http.ResponseWriter, r *http.Request) {
	agentName := chi.URLParam(r, "name")
	if agentName == "" {
		writeJSON(w, 400, map[string]string{"error": "missing agent name"})
		return
	}

	costs := loadModelCosts(h.home)
	today := time.Now().Format("2006-01-02")

	summary, err := h.metrics.CollectWithCosts(agentName, today, today, costs)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}

	agentTotals := summary.Totals
	result := map[string]interface{}{
		"agent":                    agentName,
		"period":                   today,
		"total_cost_usd":           agentTotals.EstCostUSD,
		"requests":                 agentTotals.Requests,
		"input_tokens":             agentTotals.InputTokens,
		"output_tokens":            agentTotals.OutputTokens,
		"ttft_p50_ms":              agentTotals.TTFTP50Ms,
		"ttft_p95_ms":              agentTotals.TTFTP95Ms,
		"tpot_p50_ms":              agentTotals.TPOTP50Ms,
		"tpot_p95_ms":              agentTotals.TPOTP95Ms,
		"tool_calls":               agentTotals.ToolCalls,
		"tool_hallucinations":      agentTotals.ToolHallucinations,
		"tool_hallucination_rate":  hallRate(agentTotals.ToolCalls, agentTotals.ToolHallucinations),
		"retry_waste_usd":          agentTotals.RetryCostUSD,
		"avg_latency_ms":           agentTotals.AvgLatencyMs,
		"p95_latency_ms":           agentTotals.P95LatencyMs,
		"by_model":                 summary.ByModel,
	}

	writeJSON(w, 200, result)
}

// getEconomicsSummary returns cross-agent economics rollup.
func (h *Handlers) getEconomicsSummary(w http.ResponseWriter, r *http.Request) {
	costs := loadModelCosts(h.home)
	today := time.Now().Format("2006-01-02")

	summary, err := h.metrics.CollectWithCosts("", today, today, costs)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}

	result := map[string]interface{}{
		"period":                   today,
		"total_cost_usd":           summary.Totals.EstCostUSD,
		"requests":                 summary.Totals.Requests,
		"ttft_p50_ms":              summary.Totals.TTFTP50Ms,
		"ttft_p95_ms":              summary.Totals.TTFTP95Ms,
		"tool_hallucination_rate":  hallRate(summary.Totals.ToolCalls, summary.Totals.ToolHallucinations),
		"retry_waste_usd":          summary.Totals.RetryCostUSD,
		"by_agent":                 summary.ByAgent,
		"by_model":                 summary.ByModel,
	}

	writeJSON(w, 200, result)
}

func hallRate(calls, hallucinations int) float64 {
	if calls == 0 {
		return 0
	}
	return float64(hallucinations) / float64(calls)
}
```

- [ ] **Step 2: Register routes**

In `internal/api/routes.go`, find the agent budget routes and add nearby:

```go
	r.Get("/api/v1/agents/{name}/economics", h.getAgentEconomics)
	r.Get("/api/v1/economics/summary", h.getEconomicsSummary)
```

Read routes.go first to find the exact location and confirm the `Handlers` struct has a `metrics` field and `home` field.

- [ ] **Step 3: Build**

```bash
go build ./...
```

- [ ] **Step 4: Commit**

```bash
git add internal/api/handlers_economics.go internal/api/routes.go
git commit -m "feat(api): add /economics endpoints for cost and performance analytics"
```

---

### Task 8: Add workflow_economics WebSocket signal

**Files:**
- Modify: `internal/api/handlers_budget.go` (or wherever budget/record POST is handled)

- [ ] **Step 1: Emit WebSocket signal on task completion**

Find where the gateway receives `POST /api/v1/agents/{name}/budget/record` from the enforcer. After recording the cost, check if the task has a `task_complete` signal pending or if this is the final record for a task.

Simpler approach: emit the signal from the existing task_complete signal handler. Find where `agent_signal_task_complete` is broadcast in the WebSocket hub and add a companion `agent_signal_workflow_economics` signal with the rollup data.

Read the codebase to find where task completion signals are processed. The signal likely flows: body → comms → gateway WebSocket hub. Find the handler and add the economics signal.

```go
// After task_complete is broadcast, compute and broadcast workflow economics
economicsData := map[string]interface{}{
	"task_id":               taskID,
	"loop_cost_usd":         taskCost,
	"steps":                 stepCount,
	"hallucinated_tool_calls": hallCount,
	"retry_waste_usd":       retryWaste,
	"duration_total_ms":     durationMs,
}
h.wsHub.Broadcast(ws.Event{
	Type:  "agent_signal_workflow_economics",
	Agent: agentName,
	Data:  economicsData,
})
```

The exact implementation depends on where task completion is handled. Read the code first.

- [ ] **Step 2: Build and test**

```bash
go build ./...
```

- [ ] **Step 3: Commit**

```bash
git add internal/api/
git commit -m "feat(ws): emit workflow_economics signal on task completion"
```

---

### Task 9: Extend audit summarizer

**Files:**
- Modify: `internal/audit/summarizer.go`

- [ ] **Step 1: Add new fields to MissionMetric**

Add to the `MissionMetric` struct:

```go
type MissionMetric struct {
	Mission            string   `json:"mission"`
	Date               string   `json:"date"`
	Activations        int      `json:"activations"`
	TotalInputTokens   int      `json:"total_input_tokens"`
	TotalOutputTokens  int      `json:"total_output_tokens"`
	EstimatedCostUSD   float64  `json:"estimated_cost_usd"`
	AvgTokensPerAct    float64  `json:"avg_tokens_per_activation"`
	Model              string   `json:"model"`
	TTFTP50Ms          int64    `json:"ttft_p50_ms"`
	ToolHallRate       float64  `json:"tool_hallucination_rate"`
	RetryWasteUSD      float64  `json:"retry_waste_usd"`
	EscalationCount    *int     `json:"escalation_count"`
	FindingsCount      *int     `json:"findings_count"`
}
```

- [ ] **Step 2: Update aggregation to compute new fields**

In the aggregation loop, collect TTFT values and tool call validity from audit entries (which now have the new fields). Compute P50 TTFT, hallucination rate, and retry waste.

- [ ] **Step 3: Update knowledge graph node properties**

In `upsertMetricsToKnowledge`, add the new fields to the node properties map.

- [ ] **Step 4: Build and test**

```bash
go build ./...
go test ./internal/audit/ -v
```

- [ ] **Step 5: Commit**

```bash
git add internal/audit/summarizer.go
git commit -m "feat(summarizer): add TTFT, hallucination rate, retry waste to mission metrics"
```

---

### Task 10: Update documentation and push

**Files:**
- Modify: `CLAUDE.md`

- [ ] **Step 1: Add economics documentation to CLAUDE.md**

Add a bullet in the Key Rules section:

```
- **Economics observability**: Every LLM call records TTFT (time to first token), TPOT (time per output token), StepIndex, ToolCallValid, and RetryOf in the enforcer audit log. Gateway aggregates into per-workflow rollups: loop cost, steps to resolution, context expansion rate, tool hallucination rate, retry waste. `GET /api/v1/agents/{name}/economics` and `GET /api/v1/economics/summary` expose the data. `agent_signal_workflow_economics` WebSocket signal emitted on task completion.
```

- [ ] **Step 2: Commit**

```bash
git add CLAUDE.md
git commit -m "docs: document economics observability"
```

---

### Task 11: Push and create PR

- [ ] **Step 1: Run all tests**

```bash
go test ./... 2>&1 | tail -10
cd images/enforcer && go test ./... 2>&1 | tail -10
```

- [ ] **Step 2: Create branch and push**

```bash
git checkout -b feature/economics-observability
git push -u origin feature/economics-observability
```

- [ ] **Step 3: Create PR**

```bash
gh pr create --title "feat: economics observability — TTFT, TPOT, workflow rollups, /economics API" --body "..."
```
