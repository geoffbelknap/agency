# Economics Observability

**Status:** Draft

## Problem

Agency tracks token usage and cost for budget enforcement, but can't answer:

- "Why did this workflow cost $2 when a similar one cost $0.30?"
- "Is context bloat or model choice driving the cost?"
- "How many tokens are wasted on tool hallucinations and retries?"
- "What's the actual latency breakdown — LLM thinking vs. tool execution vs. network?"

The enforcer captures per-call telemetry (tokens, cost, duration) and the routing metrics package aggregates it, but several dimensions are missing. This spec adds granular per-call timing, workflow-level rollups, and a dedicated API surface for economics data.

## What Already Exists

**Enforcer `AuditEntry`** (`images/enforcer/audit.go`):
- `InputTokens`, `OutputTokens`, `CachedTokens`
- `CostUSD` (from model cost config in `config.go`)
- `DurationMs` (wall-clock start to finish)
- `Model`, `ProviderModel`, `Agent`, `LifecycleID`

**Routing metrics** (`internal/routing/metrics.go`):
- Per-agent, per-model, per-provider breakdowns
- Avg/P95 latency from audit logs

**Budget enforcement** (`images/enforcer/budget.go`, `internal/budget/store.go`):
- Task/daily/monthly spend with hard stops
- Cost reporting to gateway via `/api/v1/agents/{name}/budget/record`

**Audit summarizer** (`internal/audit/summarizer.go`):
- 15-minute aggregation → `MissionMetrics` knowledge graph nodes

## New Per-Call Fields

Add to `AuditEntry` in `images/enforcer/audit.go`:

| Field | Type | Source | Notes |
|---|---|---|---|
| `TTFTMs` | int64 | Time from request sent to first SSE chunk received | Streaming only. For buffered calls, equals `DurationMs`. |
| `TPOTMs` | float64 | `(DurationMs - TTFTMs) / OutputTokens` | Computed. Meaningful only for streaming with >0 output tokens. |
| `ContextTokens` | int64 | Total input tokens including accumulated context | Distinct from `InputTokens` — tracks the full context window size to measure growth across steps. |
| `StepIndex` | int | Sequential position within the current task (1, 2, 3...) | Tracked by enforcer per-task counter. Resets on new task. |
| `ToolCallValid` | *bool | Whether the LLM's tool call parsed as valid JSON with correct schema | nil for non-tool-call responses. false = hallucinated tool call. |
| `RetryOf` | string | CorrelationID of the call this is retrying | Links retries to originals for waste attribution. |

## Enforcer Implementation

**TTFT measurement** — In `relayStream()` (`llm.go`), record `time.Now()` when the first SSE data chunk arrives from the upstream provider. Compute `TTFTMs = ttftTime.Sub(start).Milliseconds()`.

**TPOT computation** — After stream completes: `TPOTMs = float64(DurationMs - TTFTMs) / float64(OutputTokens)`. Only meaningful when `OutputTokens > 0`.

**StepIndex** — Add `stepCounter map[string]int` to `LLMHandler`, keyed by task ID (from `X-Agency-Task-ID` header). Increment on each LLM call. Reset when task ID changes.

**ToolCallValid** — After parsing the LLM response for tool calls, set to `true` if tool call JSON is valid and matches a known tool schema, `false` if it fails parsing or schema validation, `nil` if the response contains no tool calls.

**RetryOf** — Body runtime passes the original correlation ID in `X-Agency-Retry-Of` header when retrying. Enforcer records in audit entry.

## Per-Workflow Rollups

Extend `internal/routing/metrics.go` with per-workflow aggregation:

| Metric | Computation | What It Shows |
|---|---|---|
| **Loop Cost** | Sum of `CostUSD` for all calls in a single task | Total cost of one workflow |
| **Steps to Resolution** | Max `StepIndex` for a completed task | How many LLM calls a task required |
| **Context Expansion Rate** | `ContextTokens[step N] / ContextTokens[step 1]` | How much the context window grew during the workflow |
| **Tool Hallucination Rate** | Count of `ToolCallValid=false` / total tool calls | Percentage of tool calls with invalid args |
| **Retry Waste** | Sum of `CostUSD` where `RetryOf` is non-empty | Cost burned on retries |
| **TTFT P50/P95** | Percentiles of `TTFTMs` across calls | Provider response latency distribution |
| **TPOT P50/P95** | Percentiles of `TPOTMs` across calls | Token generation speed distribution |

## API

**`GET /api/v1/agents/{name}/economics`** — Per-agent economics for the current period:

```yaml
agent: security-analyst
period: "2026-04-04"
workflows_completed: 12
total_cost_usd: 4.87
avg_loop_cost_usd: 0.41
avg_steps_to_resolution: 6.2
avg_context_expansion_rate: 4.8
tool_hallucination_rate: 0.03
retry_waste_usd: 0.18
ttft_p50_ms: 380
ttft_p95_ms: 1200
tpot_p50_ms: 28
tpot_p95_ms: 65
by_model:
  - model: standard
    calls: 48
    cost_usd: 3.92
    avg_ttft_ms: 420
  - model: fast
    calls: 24
    cost_usd: 0.95
    avg_ttft_ms: 180
```

**`GET /api/v1/agents/economics/summary`** — Cross-agent rollup (same shape, aggregated).

## WebSocket Signal

New signal type `agent_signal_workflow_economics` emitted on task completion:

```json
{
  "type": "agent_signal_workflow_economics",
  "agent": "security-analyst",
  "data": {
    "task_id": "...",
    "loop_cost_usd": 0.37,
    "steps": 5,
    "context_expansion_rate": 3.2,
    "hallucinated_tool_calls": 0,
    "retry_waste_usd": 0.00,
    "ttft_avg_ms": 350,
    "duration_total_ms": 12400
  }
}
```

Powers real-time display alongside the existing trajectory and signal views.

## Audit Summarizer Extension

Extend `internal/audit/summarizer.go` to include the new rollup metrics in the 15-minute aggregation cycle. Enriched `MissionMetrics` nodes in the knowledge graph carry the new fields.

## Sequencing

1. Add new fields to `AuditEntry` struct and populate in enforcer `LLMHandler`
2. Add `StepIndex` counter to enforcer
3. Instrument TTFT in `relayStream()`
4. Add `ToolCallValid` check after tool call parsing
5. Add `RetryOf` header propagation
6. Extend routing metrics with per-workflow rollups
7. Add `/economics` API endpoints
8. Add `workflow_economics` WebSocket signal
9. Extend audit summarizer

Steps 1-5 are enforcer changes (single package). Steps 6-9 are gateway changes. Can be worked in parallel after step 1.
