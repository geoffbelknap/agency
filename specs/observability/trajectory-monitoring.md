A lightweight trajectory monitor inside the enforcer that tracks tool call patterns and timing, detects pathological behavior, and emits alerts through the existing event bus. This is not a quality evaluation — it is pattern detection for agents that are stuck, looping, or deviating from expected behavior.

The enforcer already sees every LLM call and tool invocation as the mediating proxy on the agent-internal network. Trajectory monitoring adds behavioral awareness to a component that currently only enforces rate limits and budget.


## Problem

The enforcer mediates all agent traffic but has no awareness of whether an agent is making progress. An agent could call the same tool 50 times in a row, loop between two tools endlessly, or spend 30 minutes without emitting a progress signal — and the platform would not notice until the budget runs out or the operator checks manually.

The workspace crash watcher handles container-level failures. Budget tracking handles cost overruns. Neither catches behavioral pathologies inside a running, budget-compliant agent.


## Design

Add a `TrajectoryMonitor` to the enforcer that maintains a sliding window of recent tool calls and runs a set of stateless detectors after each tool call response returns through the proxy. When a detector fires, the enforcer emits an anomaly signal through the event bus.

Key constraints:

- **Enforcer-side only.** The agent cannot perceive, influence, or circumvent trajectory monitoring (ASK tenet 1).
- **No new communication paths.** The enforcer already mediates all traffic. Trajectory monitoring adds in-process analysis, not new network calls (ASK tenet 3).
- **No quality judgment.** Detectors check structural patterns (repetition, cycles, stalls), not whether the agent's output is correct.
- **Configurable per-agent.** All thresholds are set via trajectory policy, with platform-level defaults.


## Tool Call Window

The trajectory monitor maintains a sliding window of the last 50 tool call entries per agent:

```go
type ToolEntry struct {
    ToolName   string    `json:"tool"`
    Timestamp  time.Time `json:"timestamp"`
    Success    bool      `json:"success"`
    TokensUsed int       `json:"tokens_used"`
}
```

Memory overhead: ~4KB per agent (50 entries at ~80 bytes each). The window is append-only with eviction — when a new entry arrives and the window is full, the oldest entry is dropped.


## Detectors

The trajectory monitor runs a set of stateless detectors, each checking a specific anomaly pattern. All detectors are pure functions operating on the tool call window:

```go
func(window []ToolEntry, config DetectorConfig) *Anomaly
```

Detectors run sequentially in the response path after each tool call. No goroutine per detector. Zero allocation in the hot path when no anomaly is detected.

| Detector | What It Catches | Default Threshold | Signal |
|---|---|---|---|
| `tool_repetition` | Same tool called N+ times consecutively | 5 consecutive calls | `trajectory_anomaly` |
| `tool_cycle` | Alternating between 2-3 tools in a repeating loop | 4+ complete cycles | `trajectory_anomaly` |
| `progress_stall` | No `progress_update` or `task_complete` signal for N minutes after `task_accepted` | 15 minutes | `trajectory_anomaly` |
| `error_cascade` | N+ tool errors within a time window | 5 errors in 2 minutes | `trajectory_anomaly` |
| `budget_velocity` | Budget consumption rate projects exhaustion before task is likely complete | >50% budget in first 20% of `expected_duration_minutes` | `trajectory_warning` |

### tool_repetition

Scans the window tail for consecutive calls to the same tool name. Fires when the count reaches the configured threshold. This catches agents stuck retrying a failing command or repeating an action that has no effect.

### tool_cycle

Detects repeating sequences of 2-3 distinct tools at the tail of the window. The algorithm:

1. Extract the last 2 tool names from the window tail as a candidate pattern of length 2
2. Scan backward through the window counting how many consecutive complete repetitions of this pattern exist
3. If the repetition count reaches the threshold, fire
4. If not, repeat with a candidate pattern of length 3 (last 3 tool names)
5. The shortest repeating pattern wins (length 2 is checked before length 3)

Example: for window tail `[read, edit, read, edit, read, edit, read, edit]`, the candidate pattern is `[read, edit]` with 4 complete repetitions.

This catches agents bouncing between "read file → edit file → read file → edit file" without making progress. Patterns longer than 3 are not checked — they are uncommon and the detection cost scales with window size × pattern length.

### progress_stall

Tracks the timestamp of the most recent `progress_update` or `task_complete` signal (received via the enforcer's signal monitoring path). If no progress signal arrives within the threshold after `task_accepted`, fires the anomaly. Resets when a progress signal arrives.

### error_cascade

Counts `Success: false` entries within the configured time window, scanning backward from the most recent entry. Fires when the error count reaches the threshold. This catches agents repeatedly failing tool calls without changing strategy.

### budget_velocity

Compares cumulative token usage against elapsed time since task start. If the agent has consumed more than `budget_pct`% of its task budget within the first `time_pct`% of `expected_duration_minutes`, fires a warning. This provides early warning before the hard budget limit triggers a stop.

`expected_duration_minutes` is a required field in the trajectory policy when `budget_velocity` is enabled. It is set per-mission based on the operator's knowledge of typical task duration for that mission type. If not configured, the `budget_velocity` detector is silently disabled for that agent (rather than guessing). Over time, procedural memory records provide actual duration data that operators can use to set this value.


## Anomaly Signal

When a detector fires, the enforcer emits an anomaly signal:

```json
{
  "signal_type": "trajectory_anomaly",
  "timestamp": "2026-03-27T14:32:00Z",
  "data": {
    "agent": "henrybot900",
    "detector": "tool_repetition",
    "detail": "execute_command called 8 times consecutively",
    "task_id": "task-a1b2c3d4",
    "severity": "warning",
    "window": [
      {"tool": "execute_command", "timestamp": "2026-03-27T14:31:42Z", "success": true},
      {"tool": "execute_command", "timestamp": "2026-03-27T14:31:48Z", "success": false},
      {"tool": "execute_command", "timestamp": "2026-03-27T14:31:55Z", "success": false}
    ]
  }
}
```

The `window` field contains the relevant subset of tool entries that triggered the detector — not the full 50-entry window. This keeps the signal payload focused and audit-friendly.

### Audit Persistence

Anomaly signals are written to the enforcer's HMAC-signed audit log (`~/.agency/audit/enforcer/{agent}.jsonl`) before being posted to the event bus. This ensures durability — the event bus is in-memory with at-most-once delivery (events are lost on gateway restart), but the audit log is persistent and tamper-evident. The audit entry uses the same HMAC signing mechanism as all other enforcer audit events (keyed by `ENFORCER_AUDIT_HMAC_KEY`).

### Severity Levels

Two severity levels:

- **`warning`** — Informational. Logged by the enforcer. Optionally routed to operator notifications. The agent continues operating.
- **`critical`** — Always logged, always notified. If `trajectory_policy.on_critical` is `halt`, the enforcer triggers a supervised halt (three-tier halt model, supervised level).

The distinction matters for operators who want visibility into minor anomalies without alert fatigue, while ensuring genuine stuck-agent scenarios always surface.


## Trajectory Policy

Configurable per-agent in agent config or hierarchical policy. Platform defaults apply when no per-agent config is set. Lower levels can only restrict (tighten thresholds, escalate severity), not loosen — consistent with the existing policy model.

```yaml
trajectory:
  enabled: true                    # default: true
  detectors:
    tool_repetition:
      threshold: 5
      severity: warning
    tool_cycle:
      threshold: 4
      severity: warning
    progress_stall:
      threshold_minutes: 15
      severity: critical
    error_cascade:
      threshold: 5
      window_minutes: 2
      severity: warning
    budget_velocity:
      budget_pct: 50
      time_pct: 20
      expected_duration_minutes: 10    # required when budget_velocity is enabled
      severity: warning
  on_critical: alert              # alert | halt
  cooldown_minutes: 5             # suppress re-fire of same detector within this window
```

### Field Details

- **`enabled`** — Master switch. When `false`, the trajectory monitor still maintains the tool call window (for on-demand inspection via the API) but does not run detectors. Default: `true`.
- **`detectors.<name>.threshold`** — Detector-specific trigger threshold. Meaning varies by detector (count for repetition/cycle/cascade, minutes for stall, percentage for velocity).
- **`detectors.<name>.severity`** — `warning` or `critical`. Controls notification routing and halt behavior.
- **`on_critical`** — Action when a critical-severity anomaly fires. `alert` (default): emit signal and notify operator; agent continues. `halt`: emit signal, notify operator, and trigger supervised halt.
- **`cooldown_minutes`** — After a detector fires, suppress the same detector for this many minutes. Prevents alert storms from persistent anomalies. Default: 5 minutes. The cooldown is per-detector, not global — different detectors can fire independently.


## Event Bus Integration

Anomaly signals are emitted as platform events through the existing event bus:

```json
{
  "id": "evt-traj-a1b2c3d4",
  "source_type": "platform",
  "source_name": "enforcer",
  "event_type": "trajectory_anomaly",
  "timestamp": "2026-03-27T14:32:00Z",
  "data": {
    "agent": "henrybot900",
    "detector": "tool_repetition",
    "detail": "execute_command called 8 times consecutively",
    "task_id": "task-a1b2c3d4",
    "severity": "warning"
  },
  "metadata": {
    "agent": "henrybot900"
  }
}
```

Two event types:

- **`trajectory_anomaly`** — Detectors at `warning` or `critical` severity.
- **`trajectory_warning`** — Advisory signals like `budget_velocity` that indicate risk but not pathology.

Both event types are routable via the standard subscription mechanism. Operators can subscribe to trajectory events using the existing `agency notification add` workflow. Mission health displays include active trajectory anomalies for assigned agents.

### Signal Delivery Path

The enforcer is a per-agent sidecar connected to the agent-internal and mediation networks. It does not write to the body runtime's `agent-signals.jsonl` (that file is the body's signal channel). Instead, the enforcer delivers trajectory anomaly signals via HTTP POST to the gateway's internal signal relay endpoint at `POST /api/v1/agents/{name}/signal`. The gateway receives the signal and routes it through the event bus.

```
Enforcer detects anomaly
  → Write to enforcer audit log (HMAC-signed, persistent)
  → POST /api/v1/agents/{name}/signal (gateway internal endpoint)
  → Gateway event bus routes to subscribers
  → Operator notifications (ntfy/webhook)
```

The gateway signal relay endpoint already exists for comms-originated signals. Trajectory signals use the same endpoint with `source: enforcer` in the signal metadata.


## REST API

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/v1/agents/{name}/trajectory` | Current trajectory state for an agent |

### GET /api/v1/agents/{name}/trajectory

Returns the current trajectory monitor state for the named agent. Reads from enforcer state via the existing enforcer status channel — no new communication path.

```json
{
  "agent": "henrybot900",
  "enabled": true,
  "window_size": 50,
  "current_entries": 23,
  "recent_tools": [
    {"tool": "execute_command", "timestamp": "2026-03-27T14:31:55Z", "success": false},
    {"tool": "read_file", "timestamp": "2026-03-27T14:31:40Z", "success": true}
  ],
  "active_anomalies": [
    {
      "detector": "tool_repetition",
      "detail": "execute_command called 8 times consecutively",
      "severity": "warning",
      "first_detected": "2026-03-27T14:32:00Z",
      "cooldown_until": "2026-03-27T14:37:00Z"
    }
  ],
  "detectors": {
    "tool_repetition": {"status": "cooldown", "last_fired": "2026-03-27T14:32:00Z"},
    "tool_cycle": {"status": "ok", "last_fired": null},
    "progress_stall": {"status": "ok", "last_fired": null},
    "error_cascade": {"status": "ok", "last_fired": null},
    "budget_velocity": {"status": "ok", "last_fired": null}
  }
}
```

The `recent_tools` field returns the last 10 entries from the window (most recent first) for quick inspection. The full window is not exposed — it is an internal data structure, not an API surface.


## CLI

No new commands. Trajectory status surfaces through existing commands:

| Command | Trajectory Integration |
|---------|----------------------|
| `agency show <agent>` | Includes active anomalies (if any) in the agent status output |
| `agency log <agent>` | Trajectory signals appear naturally in the signal log (`agent-signals.jsonl`) |
| `agency policy show <agent>` | Shows effective trajectory policy (merged from hierarchy) |

### Example: agency show output with active anomaly

```
Agent: henrybot900
Status: running
Mission: ticket-triage (active)
...
Trajectory:
  tool_repetition: WARNING — execute_command called 8 times consecutively (14:32 UTC)
  progress_stall: ok
  error_cascade: ok
```

When no anomalies are active, the trajectory section shows a single line: `Trajectory: ok`.


## Enforcer Implementation

### TrajectoryMonitor struct

```go
type TrajectoryMonitor struct {
    window     []ToolEntry         // sliding window, max 50 entries
    config     TrajectoryConfig    // from agent policy
    cooldowns  map[string]time.Time // detector name -> cooldown expiry
    lastSignal time.Time           // last progress_update or task_complete
    taskStart  time.Time           // when task_accepted was received
    mu         sync.Mutex          // protects window and cooldowns
}
```

Initialized per-agent at enforcer startup. The enforcer already maintains per-agent state for rate limiting and budget tracking — the trajectory monitor follows the same pattern.

### Hot Path

After each tool call response returns through the proxy:

1. Append `ToolEntry` to the window (evict oldest if full).
2. If `config.Enabled` is false, return.
3. Run each detector in sequence: `tool_repetition`, `tool_cycle`, `error_cascade`, `budget_velocity`.
4. For each detector that returns a non-nil `Anomaly`:
   a. Check cooldown. If in cooldown, skip.
   b. Set cooldown.
   c. Emit anomaly signal (write to signal log, post to event bus).
   d. If severity is `critical` and `on_critical` is `halt`, trigger supervised halt.

The `progress_stall` detector is not run in the hot path. It runs on a separate timer (checked every 60 seconds) since it fires on the absence of events, not the presence of a tool call.

### Performance

- Detector execution budget: <1ms per tool call. All detectors are O(n) scans of a 50-element array — in practice, sub-microsecond.
- No additional network calls. All state is in-process memory.
- No goroutine per detector. Sequential execution in the response path.
- No heap allocation when no anomaly is detected (detectors return nil).
- The `progress_stall` timer is the only background goroutine added — one per agent, sleeping between checks.


## Interaction with Other Systems

### Reflection Loop

Trajectory monitoring is orthogonal to agent-internal reflection. The body runtime's reflection loop (if enabled) is a quality mechanism — the agent evaluating its own work. Trajectory monitoring is a platform-external behavioral check — the enforcer detecting structural pathologies the agent cannot self-diagnose. Both can operate simultaneously without interference.

### Budget Model

The `budget_velocity` detector provides early warning before the hard budget limit triggers a stop. Budget tracking continues to enforce absolute limits. Trajectory monitoring adds rate-of-consumption awareness — "you're burning budget too fast" rather than "you've exceeded budget."

### Workspace Crash Watcher

Complementary. The crash watcher handles container-level failures (OOM, segfault, Docker daemon issues). Trajectory monitoring handles behavioral-level issues within a running, healthy container. No overlap.

### Signals

Trajectory anomaly signals follow the same format and delivery path as existing agent signals. They are appended to `agent-signals.jsonl` and routed through comms to the gateway WebSocket hub. Clients (agency-web, CLI) receive them through the standard signal stream.

### Meeseeks

Meeseeks agents get trajectory monitoring with the same defaults. Their shorter lifespans and tighter budgets make `budget_velocity` and `tool_repetition` particularly relevant. No special handling needed — the per-agent `TrajectoryMonitor` is created when the meeseeks enforcer starts.


## ASK Tenet Compliance

| Tenet | How Trajectory Monitoring Complies |
|-------|------------------------------------|
| 1 — Constraints are external and inviolable | Monitoring runs in the enforcer, outside the agent boundary. The agent cannot perceive, influence, or circumvent it. |
| 2 — Every action leaves a trace | All anomaly events are logged by the enforcer via HMAC-signed audit entries. The agent has no write access to trajectory logs. |
| 3 — Mediation is complete | Trajectory monitoring adds in-process analysis to the existing mediation path. No new communication paths between agent and infrastructure. |
| 4 — Least privilege | No new capabilities granted to agents. The trajectory API endpoint is operator-only. |
| 9 — Halt authority is asymmetric | Supervised halts triggered by trajectory monitoring follow the existing three-tier halt model. The agent cannot self-resume. |


## Task Tier Interaction

Trajectory monitoring is **always on** at every task tier (minimal, standard, full). It is free — pure in-memory pattern detection with no LLM calls. Even a "hi" DM gets trajectory monitoring because it catches stuck agents that would otherwise burn budget undetected.

Trajectory monitoring is the only feature that runs at `minimal` tier. This is by design — it's the platform's safety net, not a quality feature.


## Future Work

- **Custom detectors** — Allow operators to define detector patterns via policy (e.g., "flag if tool X is called more than N times in any task").
- **Trajectory history** — Persist trajectory summaries per-task for post-mortem analysis.
- **Cross-agent patterns** — Detect coordinated anomalies across agents in a team (e.g., multiple agents stalling simultaneously, suggesting a shared dependency failure).
- **Adaptive thresholds** — Learn per-agent baselines from historical data and flag deviations from the agent's own normal behavior, not just absolute thresholds.
