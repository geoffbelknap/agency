Briefs are removed as a task delivery mechanism. All task delivery happens through channel messages and DMs, routed via the event framework. Turn limits are replaced by a cost-based budget model that controls spending at task, daily, and monthly granularity.

## Brief Removal

### What Gets Removed

| Component | Action |
|-----------|--------|
| `agency brief <agent> <task>` CLI command | Remove |
| `POST /api/v1/agents/\{name\}/brief` endpoint | Remove |
| `agency_brief` MCP tool | Remove |
| `AgentManager.Brief()` in orchestrate/agent.go | Remove |
| Brief-specific comms path (`POST /tasks/\{agent\}`) | Remove |
| Body runtime brief handler | Remove |

### Additional Code Paths to Remove

| Component | Action |
|-----------|--------|
| `ConnectorRoute.Brief` field in Go (`internal/models/connector.go`) and Python (`agency_core/models/connector.py`) | Remove field, update validation |
| Intake service brief delivery path (`images/intake/`) | Replace with event bus delivery |
| `agency_core/core/brief.py` module | Remove |
| Connector route `target.task: brief` pattern | Replace with event bus subscriptions |
| Brief references in coordination code (`agency_core/coordination.py`) | Update to use channel messages |
| Brief references in presets (`agency_core/presets/`) | Update to use DM/channel patterns |
| Test fixtures using brief routes (~8 test files) | Update to new patterns |

Coordinator-to-member delegation (previously via briefs) becomes channel @mentions. The coordinator posts "@analyst review ticket INC-1234" to the team channel — the event bus routes the mention to the team member.

### What Replaces It

All task delivery becomes messages through channels and DMs:

| Old Pattern | New Pattern |
|---|---|
| `agency brief henrybot900 "review this PR"` | `agency send henrybot900 "review this PR"` (DM) |
| Connector route → brief agent | Connector event → event bus → subscription → agent |
| Programmatic task delivery | `POST /channels/\{dm_channel\}/messages` via comms API |

### DM Channels

Each agent gets an auto-created DM channel: `#op→\{agent_name\}`. When an operator runs `agency send <agent> <message>`, the gateway:

1. Creates `#op→\{agent_name\}` channel if it doesn't exist
2. Posts the message to that channel
3. The event bus routes it to the agent (DMs always wake agents)

The `agency send` command currently sends to named channels. The change: when the target matches an agent name (not a channel name), it sends to the DM channel instead.

Lookup order for `agency send <target> <message>`: the gateway checks agent names first, then channel names. If both an agent and channel share a name, the agent DM takes precedence (use `agency comms send <channel> <message>` for the channel). DM channels are named `dm-\{agent_name\}` (not visible in `agency comms list` by default — use `--include-dm` to show them).

### Migration

No deprecation period — briefs are removed immediately. The `agency send` command is the direct replacement. Connector routes that used `target.task: brief` are updated to use the event bus subscription model from the event framework spec.

## Budget Model

### Budget Currency

Budgets are denominated in **USD**. Token counts and LLM calls are tracked for observability, but limits are in dollars. This is model-agnostic — switching from sonnet to haiku mid-mission doesn't break the budget. Cost is calculated from model pricing in the routing config (`~/.agency/infrastructure/routing.yaml`), which already maps model aliases to providers.

### Budget Levels

Budgets are hierarchical — lower levels restrict, never loosen.

**Platform default** (in `~/.agency/config.yaml`):
```yaml
budget:
  agent_daily: 10.00       # USD per agent per calendar day
  agent_monthly: 200.00    # USD per agent per calendar month
  per_task: 2.00           # USD per individual task
```

**Mission override** (in `mission.yaml`):
```yaml
budget:
  daily: 5.00       # USD per calendar day (cannot exceed platform agent_daily)
  monthly: 100.00   # USD per calendar month (cannot exceed platform agent_monthly)
  per_task: 1.00    # USD per task (cannot exceed platform per_task)
```

**Meeseeks** — per-spawn budget set by parent:
```
spawn_meeseeks(task: "...", budget: 0.05)   # 5 cents
```

Meeseeks budget cannot exceed the parent's per-task budget.

A task is any unit of work that enters the body runtime's conversation loop: a mission trigger event, a DM message, an @mention response. Each gets its own task_id and a fresh per-task budget. Multiple LLM calls within the same task share the per-task budget.

Agents without a mission use the platform defaults.

### What Gets Tracked

The enforcer already proxies every LLM call and sees token counts in responses. It maintains running totals:

| Metric | Scope | Source |
|--------|-------|--------|
| Input tokens | Per-task, per-day, per-month | From LLM response headers |
| Output tokens | Per-task, per-day, per-month | From LLM response headers |
| LLM API calls | Per-task, per-day, per-month | Count of proxied requests |
| Estimated cost (USD) | Per-task, per-day, per-month | Calculated: tokens × model pricing |

Cost calculation uses the pricing table in routing config:

```yaml
# In routing.yaml
models:
  claude-sonnet:
    provider: anthropic
    provider_model: claude-sonnet-4-20250514
    pricing:
      cost_per_mtok_in: 3.00       # USD per million input tokens
      cost_per_mtok_out: 15.00     # USD per million output tokens
      cost_per_mtok_cached: 0.30   # USD per million cached input tokens
  claude-haiku:
    provider: anthropic
    provider_model: claude-haiku-4-5-20251001
    pricing:
      cost_per_mtok_in: 0.80
      cost_per_mtok_out: 4.00
      cost_per_mtok_cached: 0.08
```

Cached input tokens (from Anthropic prompt caching) are tracked at the `cost_per_mtok_cached` rate. The enforcer reads `cache_read_input_tokens` from LLM response usage data.

### Pre-Task Budget Check

Before starting any task (mission trigger, DM, @mention), the body runtime checks:

1. **Daily budget remaining** — if exhausted, reject the task. Post to `#operator`: "Agent \{name\} daily budget exhausted ($\{used\}/$\{limit\}). \{N\} pending events queued."

2. **Input size estimate** — estimate token count of the event data before sending to the LLM. If the estimated input cost exceeds 50% of the per-task budget, reject the task. Alert to `#operator`: "Task rejected — input too large (est. \{N\} tokens, ~$\{X\}). Task: '\{summary\}'."

3. **Monthly budget remaining** — if less than one per-task budget remains in the monthly allocation, pause the mission and alert operator.

This catches oversized inputs (someone pastes a giant blob into a ticket) before any LLM call is made. The token estimate uses the existing `_estimate_tokens()` method in the body runtime (character count / chars-per-token ratio).

### Budget Enforcement

The enforcer tracks cumulative cost per task and per day. When a limit is reached:

**Per-task limit hit:**
1. Enforcer rejects the next LLM call with a budget error
2. Body runtime posts work-so-far summary to the channel
3. Task ends — next event gets a fresh per-task budget

**Daily limit hit:**
1. Enforcer rejects all LLM calls for this agent
2. Current in-progress task is allowed to complete (up to per-task limit)
3. New tasks are rejected until midnight (the timezone from the agent's active mission `health.business_hours` field, or UTC if no mission or no business_hours configured)
4. Agent posts status to `#operator`

**Monthly limit hit:**
1. Mission auto-pauses
2. Agent posts status to `#operator`
3. Operator must resume (with optional budget increase via `agency mission update`)

### Budget State Persistence

Budget state (cumulative daily and monthly cost per agent) is tracked by the gateway, not the enforcer. The gateway maintains a `~/.agency/budget/\{agent_name\}.json` file with rolling daily and monthly totals. The enforcer queries the gateway for remaining budget before each LLM call via a lightweight HTTP check (`GET /api/v1/agents/\{name\}/budget/remaining`). This ensures budget state survives enforcer container recreation (which happens on capability hot-reload and agent restart). Per-task budget is tracked by the enforcer in-memory since it resets on each task.

### Task Boundary Signaling

The body runtime signals task boundaries to the enforcer via a custom HTTP header on each LLM call: `X-Agency-Task-Id: \{task_id\}`. When the enforcer sees a new task ID, it resets the per-task budget counter. This requires no new signaling path — the header rides on existing LLM proxy requests.

### Budget Config Delivery

The body runtime receives budget configuration through the enforcer. On task start, the body runtime calls `GET /budget` on the enforcer, which returns the applicable limits (per-task, daily remaining, monthly remaining). The enforcer derives these from the gateway's budget state and the mission/platform config. This avoids mounting host-side config files into the agent container.

### Budget Event Severity

Not every budget event is an alert. Routine per-task exhaustion is normal. Monthly exhaustion is urgent.

| Event | Log | Alert #operator | External notification |
|---|---|---|---|
| Per-task budget hit | Yes | No | No |
| Per-task input rejected (too large) | Yes | Yes | Configurable |
| Daily budget at 80% | Yes | Yes (warning) | No |
| Daily budget exhausted | Yes | Yes | Configurable |
| Monthly budget at 80% | Yes | Yes (warning) | Yes (always) |
| Monthly budget exhausted + mission paused | Yes | Yes (critical) | Yes (always) |
| Meeseeks budget exhausted | Yes | No (covered by Meeseeks distress) | No |

The 80% warnings give operators time to react before hard stops.

Budget events are platform events in the event framework. Operators configure external notifications via the standard `notifications` config:

```yaml
notifications:
  - name: ops-alerts
    type: ntfy
    url: https://ntfy.sh/my-alerts
    events: [budget_daily_exhausted, budget_monthly_exhausted, budget_input_rejected]
```

### Budget Event Types

These platform events are emitted into the event bus:

- `budget_task_exhausted` — agent, task_id, mission_id, cost_used, cost_limit
- `budget_input_rejected` — agent, task_id, mission_id, estimated_tokens, estimated_cost, cost_limit
- `budget_daily_warning` — agent, cost_used, cost_limit (at 80%)
- `budget_daily_exhausted` — agent, cost_used, cost_limit, pending_events
- `budget_monthly_warning` — agent, cost_used, cost_limit (at 80%)
- `budget_monthly_exhausted` — agent, mission_id, cost_used, cost_limit

## Turn Limit Removal

With the budget model in place, turn limits are removed from the body runtime:

| Old Mechanism | Replacement |
|---|---|
| `max_turns: 5` on idle-reply tasks | Per-task budget (USD) |
| `MAX_TURNS` constant in body.py | Removed |
| `MAX_CONTINUATIONS` auto-continue logic | Removed — budget exhaustion is a hard stop, no auto-continue |
| Checkpoint injection at N turns before limit | Removed — agent decides when to checkpoint based on task complexity |

The `max_turns` field in task dicts is ignored if present (backward compatibility during transition). The body runtime's conversation loop runs until:
1. Agent calls `complete_task` — normal completion
2. Enforcer rejects an LLM call (budget exhausted) — hard stop
3. Agent calls `escalate` — requests help

No more counting turns. No more auto-continuation. No more looping.

## Observability

### `agency show <agent>` (Extended)

Agent detail includes budget usage:

```
Budget:
  Today:     $2.34 / $10.00 (23%)
  This month: $47.12 / $200.00 (24%)
  Current task: $0.12 / $2.00

Usage (today):
  LLM calls: 47
  Input tokens: 234,567
  Output tokens: 45,678
```

### CLI Commands

No new commands — budget info is surfaced through existing commands:

| Command | Budget Info |
|---------|-------------|
| `agency status` | Per-agent daily spend column (if over 50%) |
| `agency show <agent>` | Full budget breakdown |
| `agency mission show <mission>` | Mission budget config and current usage |

### REST API

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/v1/agents/\{name\}/budget` | Current budget usage for agent |

### MCP Tools

| Tool | Description |
|------|-------------|
| `agency_budget_show` | Show agent budget usage |

### Audit Events

All budget events are logged in the gateway audit with `build_id` and `mission_id`:

- `budget_task_exhausted`
- `budget_input_rejected`
- `budget_daily_warning`
- `budget_daily_exhausted`
- `budget_monthly_warning`
- `budget_monthly_exhausted`

## ASK Tenet Compliance

| Tenet | Compliance |
|-------|-----------|
| **2. Every action traced** | All budget events logged in audit. Cost tracked per-task, per-day, per-month. |
| **4. Least privilege** | Budget limits scope what an agent can spend. Lower levels restrict, never loosen. |
| **7. Constraint history** | Budget config changes (mission updates) are versioned in mission history. Platform defaults are in config.yaml under version control. |

## Scope Boundaries

This spec depends on:
- **Event framework** — budget events are platform events routed through the bus
- **Missions** — mission-level budget overrides
- **Meeseeks** — per-spawn budget

This spec does not cover:
- **Per-capability cost tracking** — tracking spend per external service (Jira API, Brave search). Future work.
- **Budget approval workflows** — operator pre-approves budget increases. Future work.
- **Chargeback / multi-tenant billing** — allocating costs to teams or departments. Future work.

## What This Enables

- Clean task delivery model — messages, not briefs. One path for all task delivery.
- Cost-based limits replace arbitrary turn counts — agents work until done or budget-exhausted
- No more looping — budget exhaustion is a hard stop, no auto-continuation
- Pre-task input validation catches oversized payloads before any LLM call
- Graduated alerting — operators get warnings at 80%, hard stops at 100%
- Observable spending — operators can see per-agent, per-mission cost at a glance
