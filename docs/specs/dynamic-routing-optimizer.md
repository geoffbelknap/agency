---
description: "Background optimizer that tracks model success rates per task type and suggests cheaper routing when quality thresholds are met."
status: "Draft"
---

# Dynamic Routing Optimizer

*Learn which models work for which tasks, suggest cheaper routing when quality holds.*

**Date:** 2026-04-06
**Status:** Draft
**Parent:** [Compounding Agent Organizations](compounding-agent-organizations.md) — cost architecture

---

## Overview

The enforcer logs every LLM call with model, task type, tokens, latency, and retry status. The budget system has per-model pricing (cost per million tokens for input, output, cached). Today, this data exists but nobody acts on it — routing is static in `routing.yaml`.

This spec adds a background optimizer in the gateway that aggregates call data, computes per-model success rates and costs per task type, and generates routing suggestions when a cheaper model meets quality thresholds. Operators approve or reject suggestions via CLI. Approved suggestions write to `routing.local.yaml`.

No automatic model switching. No bandit algorithms. Simple statistics + thresholds + human approval.

---

## Design Decisions

| Decision | Choice | Rationale |
|---|---|---|
| Complexity | Simple statistics + thresholds | Delivers value without ML infrastructure. Bandit can be added later if needed. |
| Success signal | Absence of retry | Fast, per-call, already tracked via X-Agency-Retry-Of header. |
| Where it runs | Background goroutine in gateway | Lightweight, data already in memory, no new container. |
| Approval model | Operator-only | ASK Tenet 21 — routing config not agent-modifiable. |
| Cost data | Real USD from model pricing | routing.yaml has per-model cost_per_mtok_in/out/cached. CalculateCost() already exists. |
| Output | routing.local.yaml | Hub-managed routing.yaml untouched. Local overrides for operator customizations. |

---

## Data Collection

The enforcer already logs every LLM call. The gateway's economics observability aggregates into per-workflow rollups. The optimizer reads from this existing data.

**Per-call fields used:**

| Field | Source | Purpose |
|---|---|---|
| `model` | Enforcer audit log | Which model handled the call |
| `cost_source` | `X-Agency-Cost-Source` header | Task type: agent_task, reflection, evaluation, memory_capture, consolidation, context_summary |
| `retry_of` | `X-Agency-Retry-Of` header | If set, this call is a retry (previous model failed) |
| `input_tokens` | Enforcer audit log | Token count for cost calculation |
| `output_tokens` | Enforcer audit log | Token count for cost calculation |
| `cached_tokens` | Enforcer audit log | Token count for cost calculation |
| `latency_ms` | TTFT + TPOT | Response time |

**No new data collection.** The optimizer consumes what the enforcer already produces.

---

## Stats Aggregation

```go
type ModelTaskStats struct {
    Model        string  `json:"model"`
    TaskType     string  `json:"task_type"`
    TotalCalls   int     `json:"total_calls"`
    Retries      int     `json:"retries"`
    SuccessRate  float64 `json:"success_rate"`   // (total - retries) / total
    AvgLatencyMs float64 `json:"avg_latency_ms"`
    AvgInputTok  int     `json:"avg_input_tokens"`
    AvgOutputTok int     `json:"avg_output_tokens"`
    TotalCostUSD float64 `json:"total_cost_usd"` // actual USD from CalculateCost()
    CostPer1K    float64 `json:"cost_per_1k"`    // USD per 1000 calls
}
```

**Sliding window:** Last 7 days (configurable via `ROUTING_OPTIMIZER_WINDOW_DAYS`).

**Update interval:** Every hour (configurable via `ROUTING_OPTIMIZER_INTERVAL_MINUTES`).

**Storage:** In-memory map keyed by `(task_type, model)`. Persisted to `~/.agency/routing-stats.json` on each update for survival across gateway restarts.

---

## Suggestion Generation

On each update cycle, the optimizer checks every task type:

1. Find the current preferred model for this task type (from routing.yaml tier config)
2. Find all other models that have been used for this task type
3. For each alternative model:
   - Skip if fewer than 20 calls (insufficient data)
   - Skip if success rate < 90%
   - Compute cost savings: `(current.CostPer1K - alternative.CostPer1K) / current.CostPer1K`
   - Skip if savings < 30% (not worth the switch)
4. If a viable alternative exists, generate a suggestion

**Suggestion:**

```go
type RoutingSuggestion struct {
    ID             string         `json:"id"`
    TaskType       string         `json:"task_type"`
    CurrentModel   string         `json:"current_model"`
    SuggestedModel string         `json:"suggested_model"`
    Reason         string         `json:"reason"`
    SavingsPercent float64        `json:"savings_percent"`
    SavingsUSDPer1K float64       `json:"savings_usd_per_1k"`
    CurrentStats   ModelTaskStats `json:"current_stats"`
    SuggestedStats ModelTaskStats `json:"suggested_stats"`
    CreatedAt      string         `json:"created_at"`
    Status         string         `json:"status"` // pending, approved, rejected
}
```

**Reason format:** "haiku-4-5 has 95% success rate for memory_capture (47 calls), saves $0.12/1K calls (62% cheaper)"

**Deduplication:** Only one pending suggestion per (task_type, suggested_model) pair. If a suggestion already exists, update its stats.

---

## Approval Flow

### CLI

```bash
agency routing suggestions                  # list pending suggestions
agency routing approve <suggestion-id>      # apply to routing.local.yaml
agency routing reject <suggestion-id>       # dismiss
agency routing stats                        # show per-model per-task-type stats
agency routing stats --task-type agent_task  # filter by task type
```

### API

| Method | Path | Description |
|---|---|---|
| `GET` | `/api/v1/infra/routing/suggestions` | List suggestions (optional `?status=pending`) |
| `POST` | `/api/v1/infra/routing/suggestions/{id}/approve` | Approve and apply |
| `POST` | `/api/v1/infra/routing/suggestions/{id}/reject` | Reject |
| `GET` | `/api/v1/infra/routing/stats` | Per-model per-task-type statistics |

### What Approval Does

When a suggestion is approved:
1. Read `routing.local.yaml` (or create if doesn't exist)
2. Add/update the task type's preferred model override
3. Write `routing.local.yaml`
4. SIGHUP the affected enforcers so they pick up the new routing
5. Log as authority-level event

`routing.local.yaml` format:
```yaml
overrides:
  memory_capture:
    preferred_model: "fast"
    approved_from: "sug-a1b2c3"
    approved_at: "2026-04-06T..."
  context_summary:
    preferred_model: "fast"
    approved_from: "sug-d4e5f6"
    approved_at: "2026-04-06T..."
```

---

## Background Goroutine

```go
type RoutingOptimizer struct {
    infra      *Infra
    stats      map[string]*ModelTaskStats  // key: "task_type:model"
    suggestions []RoutingSuggestion
    mu         sync.RWMutex
    interval   time.Duration
    windowDays int
}

func (o *RoutingOptimizer) Start(ctx context.Context)
func (o *RoutingOptimizer) Stop()
func (o *RoutingOptimizer) runCycle()          // aggregate + suggest
func (o *RoutingOptimizer) Stats() []ModelTaskStats
func (o *RoutingOptimizer) Suggestions() []RoutingSuggestion
func (o *RoutingOptimizer) Approve(id string) error
func (o *RoutingOptimizer) Reject(id string) error
```

Started in the gateway's main startup sequence alongside other background goroutines. Stopped on shutdown.

---

## Configuration

| Variable | Default | Description |
|---|---|---|
| `ROUTING_OPTIMIZER_ENABLED` | `true` | Enable/disable the optimizer |
| `ROUTING_OPTIMIZER_INTERVAL_MINUTES` | `60` | Minutes between optimization cycles |
| `ROUTING_OPTIMIZER_WINDOW_DAYS` | `7` | Days of history to consider |
| `ROUTING_OPTIMIZER_MIN_CALLS` | `20` | Minimum calls before suggesting |
| `ROUTING_OPTIMIZER_MIN_SUCCESS` | `0.90` | Minimum success rate for suggestion |
| `ROUTING_OPTIMIZER_MIN_SAVINGS` | `0.30` | Minimum cost savings (30%) for suggestion |

---

## ASK Compliance

| Tenet | How this spec complies |
|---|---|
| Tenet 1 (Enforcement external) | Optimizer is infrastructure in the gateway, not an agent. |
| Tenet 13 (Authority monitored) | Approval/rejection logged as authority-level events. |
| Tenet 21 (Config not agent-modifiable) | Routing changes require operator approval. No auto-switching. |

---

## Implementation Phases

### Phase 1: Stats Aggregation

- `internal/routing/optimizer.go` — RoutingOptimizer struct, stats aggregation from audit data
- Read from enforcer audit log / economics data
- Compute ModelTaskStats with real USD costs via CalculateCost()
- Persist to `~/.agency/routing-stats.json`
- Background goroutine with configurable interval

### Phase 2: Suggestion Generation

- Suggestion generation logic (thresholds, deduplication)
- Suggestion storage (in-memory + persisted)
- Stats and suggestions API endpoints

### Phase 3: Approval Flow + CLI

- Approve/reject handlers that write `routing.local.yaml`
- SIGHUP enforcement reload
- CLI: `agency routing suggestions/approve/reject/stats`

### Phase 4: Validation

- Tests for stats aggregation
- Tests for suggestion generation (threshold logic)
- Tests for approval flow
- Full regression

---

## Testing

### Phase 1 Tests
- Stats aggregate correctly from call data
- Success rate computed as (total - retries) / total
- Cost computed via CalculateCost() with model pricing
- Sliding window excludes old data
- Stats persist and reload across restarts

### Phase 2 Tests
- Suggestion generated when cheaper model meets thresholds
- No suggestion when insufficient calls (<20)
- No suggestion when success rate too low (<90%)
- No suggestion when savings too small (<30%)
- Duplicate suggestions updated, not duplicated

### Phase 3 Tests
- Approve writes routing.local.yaml correctly
- Reject marks suggestion as rejected
- CLI displays suggestions and stats
- API endpoints return correct data

### Phase 4 Tests
- Full Go test suite
- Gateway builds clean
