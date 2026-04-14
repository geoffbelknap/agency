# Monitoring & Observability

> Status: Mixed operator runbook. Core signal, audit, and trajectory-monitoring
> workflows are part of the supported `0.2.x` path. Meeseeks, notification
> integrations, and some broader observability surfaces remain experimental.

## Trigger

Understanding agent behavior, investigating anomalies, tuning trajectory monitoring, interpreting signals, managing meeseeks, or debugging semantic cache behavior.

## Signals

Agents emit explicit signals — no polling, no heartbeats. Signal flow:

```
Body → Comms → Gateway WebSocket Hub → Clients
```

| Signal | Meaning |
|--------|---------|
| `processing` | Agent is working on a task |
| `task_complete` | Task finished |
| `error` | Something went wrong |
| `reflection_cycle` | Reflection loop in progress |
| `fallback_activated` | Fallback policy triggered |

Enforcer-originated signals:

| Signal | Meaning |
|--------|---------|
| `trajectory_anomaly` | Stuck/looping pattern detected |

Signals are written to `~/.agency/audit/<agent>/agent-signals.jsonl` (audit source of truth) and broadcast via WebSocket.

### Viewing signals

```bash
agency log <agent-name>   # audit log includes signals
agency show <agent-name>  # current state includes last signal
```

### Economics signal

On task completion, `agent_signal_workflow_economics` is emitted via WebSocket with per-task cost rollup.

## Trajectory Monitoring

The enforcer runs pattern detection on a sliding window of 50 tool calls. Always on (free, in-memory).

### Detectors

| Detector | What It Catches |
|----------|----------------|
| `tool_repetition` | Same tool called repeatedly with same/similar args |
| `tool_cycle` | A→B→C→A looping pattern |
| `error_cascade` | Multiple consecutive errors |
| `budget_velocity` | Spending budget too fast |
| `progress_stall` | No forward progress (output similarity) |

### Anomaly output

Anomalies are:
1. Written to the HMAC-signed audit log
2. Emitted to the gateway event bus
3. Routed to notification destinations (if subscribed to `trajectory_anomaly`)

### Viewing trajectory

```bash
agency show <agent-name>   # includes trajectory summary
```

Via API: `GET /api/v1/agents/{name}/trajectory`

### Tuning per-agent

Trajectory policy is configurable per-agent via the agent config or mission YAML:

```yaml
trajectory:
  window_size: 50           # default 50
  repetition_threshold: 5   # consecutive repeats before flagging
  cycle_min_length: 3       # minimum loop length
  error_cascade_threshold: 3
```

### False positives

Some legitimate workflows involve repetitive tool calls (e.g., polling a queue). If trajectory anomalies fire for expected patterns:

1. Increase the `repetition_threshold` for that agent
2. Or add the tool to a trajectory exclusion list in the agent config

## Meeseeks

Ephemeral single-purpose agents spawned by parent agents via `spawn_meeseeks` tool.

### Listing meeseeks

```bash
agency meeseeks list
```

Via API: `GET /api/v1/agents/meeseeks`

### Killing a meeseeks

```bash
agency meeseeks kill <meeseeks-id>
```

Via API: `DELETE /api/v1/agents/meeseeks/{id}`

### Kill all meeseeks for a parent

Via API: `DELETE /api/v1/agents/meeseeks?parent=<agent-name>`

### Meeseeks lifecycle

- Own enforcer, abbreviated startup sequence
- USD budget inherited from parent (with cap)
- Auto-terminate on completion or budget exhaustion
- Capacity-limited: `max_concurrent_meesks` in `~/.agency/capacity.yaml`

### Troubleshooting orphaned meeseeks

If a parent agent dies, meeseeks may be orphaned:

```bash
agency meeseeks list   # check for meeseeks with dead parents
agency meeseeks kill <id>
```

The gateway reconciler cleans up orphaned containers on startup, but manual cleanup may be needed between restarts.

## Semantic Cache

Completed task results are cached as `cached_result` nodes in the knowledge graph.

### Cache behavior

| Similarity | Action | Cost |
|-----------|--------|------|
| >= 0.92 | Full hit — skip LLM | Zero tokens |
| 0.80-0.92 | Partial hit — inject context | Reduced tokens |
| < 0.80 | Miss — normal execution | Full tokens |

### Clearing cache

```bash
agency cache clear --agent <agent-name>
```

Via API: `DELETE /api/v1/agents/{name}/cache`

### When to clear cache

- After updating mission instructions (cached results reflect old instructions)
- After a security incident (cached results may contain compromised data)
- When investigation results seem stale or incorrect
- Cache entries are XPIA-scanned before use, but clearing is safer post-incident

### Cache configuration

In mission YAML:

```yaml
cache:
  enabled: true
  ttl: 24h      # default 24h
```

Or via cost_mode defaults:
- `frugal`: cache enabled, shorter TTL
- `balanced`: cache enabled, default TTL
- `thorough`: cache enabled, default TTL

## Workspace Crash Watcher

Background watcher detects workspace container crashes and emits operator alerts automatically. No configuration needed — always on.

Check for crash events:

```bash
agency log <agent-name>   # look for crash/restart events
```

## Audit Log

HMAC-signed audit logs at `~/.agency/audit/<agent>/`. Written by the enforcer (not agents — ASK Tenet 2).

```bash
agency log <agent-name>
agency admin audit
```

### Audit summarization

Via API: `POST /api/v1/admin/audit/summarize`

### Verifying HMAC integrity

Audit entries are signed with `ENFORCER_AUDIT_HMAC_KEY`. Tampered entries will fail HMAC verification.

## Rate Limiting

The enforcer enforces per-agent rate limits (600 req/min). If an agent hits the rate limit:

```bash
agency log <agent-name>   # look for rate limit events
```

Rate-limited requests return HTTP 429 to the body runtime.

## Docker Socket Audit

Gateway startup runs `AuditDockerSocket()` — scans all `agency.managed` containers for Docker socket mounts. Violations are logged as security errors.

```bash
agency admin doctor   # includes docker socket audit check
```

## LLM Usage

```bash
agency admin usage
agency admin usage --since <timestamp>
```

Shows: call counts, token usage, errors, per-model breakdown.

## Troubleshooting

### Agent appears stuck but no trajectory anomaly

The trajectory window is 50 calls. If the agent is stuck in fewer calls, the detectors may not fire yet. Check manually:

```bash
agency show <agent-name>
agency log <agent-name>
```

### Notifications not firing for anomalies

1. Check notification destinations subscribe to `trajectory_anomaly`:
   ```bash
   agency notifications list
   ```

2. Check the event bus is running:
   ```bash
   agency infra status
   ```

### High retry waste in economics

Check for:
- Tool hallucinations (agent calling non-existent tools)
- Misconfigured capabilities (agent trying to use unavailable tools)
- Provider errors (API key issues, rate limits)

```bash
agency log <agent-name>
agency infra routing stats
```

## Verification

- [ ] `agency show <agent-name>` shows current signal state
- [ ] `agency log <agent-name>` shows audit entries
- [ ] `agency admin usage` shows LLM usage data
- [ ] Trajectory anomalies fire for looping agents (test with intentional loop)
- [ ] Notifications deliver for anomaly events
- [ ] `agency meeseeks list` shows expected meeseeks

## See Also

- [Mission Management](mission-management.md) — trajectory policy in mission config
- [Budget & Cost](budget-and-cost.md) — economics observability
- [Notifications & Webhooks](notifications-and-webhooks.md) — alert delivery
- [Agent Recovery](agent-recovery.md) — responding to anomalies
