# Mission Management

> Status: Experimental operator runbook. Missions are not part of the default
> supported `0.2.x` first-user path.

## Trigger

Creating, configuring, assigning, or troubleshooting agent missions. Missions are standing instructions that define what an agent does, how it operates, and what success looks like.

## Concepts

- **Mission**: A YAML document with instructions, cost mode, success criteria, reflection config, and fallback policies
- **Cost mode**: Controls task tier classification — `frugal` (minimal features), `balanced` (standard), `thorough` (full features including reflection)
- **Reflection**: Optional self-evaluation loop before task completion (only at `thorough` tier)
- **Success criteria**: Measurable checklist items with optional LLM evaluation
- **Fallback policies**: Operator-defined recovery chains for error conditions

## Creating a Mission

### 1. Write the mission YAML

```yaml
name: alert-triage
description: Triage and investigate security alerts
instructions: |
  You are responsible for triaging incoming security alerts.
  Investigate each alert, determine severity, and escalate if needed.

cost_mode: balanced  # frugal | balanced | thorough

success_criteria:
  checklist:
    - "Alert severity classified"
    - "Initial investigation documented"
    - "Escalation decision made"
  mode: checklist_only  # checklist_only (free) | llm (one LLM call)
  on_failure: flag      # flag (accept+tag) | retry (reject+feedback) | block (reject+notify)

reflection:
  enabled: false  # only activates at thorough tier
  max_rounds: 3

fallback:
  - trigger: tool_error
    actions: [retry, alternative_tool, escalate]
  - trigger: budget_warning
    actions: [simplify, complete_partial]
  - trigger: consecutive_errors
    threshold: 3
    actions: [pause_and_assess, escalate]
  - trigger: no_progress
    actions: [degrade, complete_partial]

procedural_memory:
  enabled: true   # capture task-execution patterns post-task

episodic_memory:
  enabled: true   # record task episodes
  retention_days: 90
```

Save to a file (e.g., `/tmp/alert-triage.yaml`).

### 2. Create the mission

```bash
agency mission create /tmp/alert-triage.yaml
```

### 3. Verify creation

```bash
agency mission list
agency mission show alert-triage
```

## Assigning a Mission

```bash
agency mission assign alert-triage my-agent
```

The mission is hot-reloaded into the agent's system prompt via enforcer SIGHUP. The agent does not need to restart.

Verify assignment:

```bash
agency mission show alert-triage
```

Expected: shows the agent name under assignment.

## Cost Modes

| Mode | Task Tier | Features Activated | When to Use |
|------|-----------|-------------------|-------------|
| `frugal` | minimal | Trajectory monitoring only, tiny prompt | Simple DM responses, status checks |
| `balanced` | standard | + fallback policies, memory capture | Most operational missions |
| `thorough` | full | + reflection loop, evaluation, memory injection | Complex investigations, critical work |

Cost modes control which features activate at runtime. Higher tiers consume more tokens but produce better results for complex work.

## Reflection Loop

When enabled (requires `thorough` cost mode):

1. Agent calls `complete_task()`
2. Body runtime intercepts, injects a reflection prompt
3. Agent produces structured JSON verdict: `APPROVED` or `REVISION_NEEDED`
4. If `REVISION_NEEDED`, agent revises (up to `max_rounds`, default 3)
5. If `APPROVED` or max rounds reached, task completes

### Troubleshooting stuck reflection

If an agent is stuck in a revision loop:

```bash
# Check trajectory for repetitive patterns
agency show <agent-name>
agency log <agent-name>

# If stuck, halt and restart with lower cost mode
agency halt <agent-name> --tier supervised --reason "reflection loop stuck"
agency stop <agent-name>
```

Edit the mission YAML to reduce `max_rounds` or set `reflection.enabled: false`, then re-create:

```bash
agency mission delete <mission-name>
agency mission create /path/to/updated-mission.yaml
agency mission assign <mission-name> <agent-name>
agency start <agent-name>
```

## Success Criteria

Two evaluation modes:

- **`checklist_only`** — Free. Keyword matching against the checklist items. Good for structured tasks.
- **`llm`** — One LLM call via the gateway internal LLM endpoint. Better for nuanced evaluation. Costs tokens.

On-failure actions:

| Action | Behavior |
|--------|----------|
| `flag` | Accept the result, tag it for review |
| `retry` | Reject the result, provide feedback, agent retries |
| `block` | Reject the result, notify operator, agent stops |

## Fallback Policies

Fallback triggers and their action chains:

| Trigger | Fires When |
|---------|-----------|
| `tool_error` | A tool call returns an error |
| `capability_unavailable` | Agent tries to use a capability it doesn't have |
| `budget_warning` | Budget threshold crossed |
| `consecutive_errors` | N consecutive errors (configurable `threshold`) |
| `timeout` | Task exceeds time limit |
| `no_progress` | Trajectory monitoring detects progress stall |

Actions execute in order. If an action fails, the next one in the chain runs:

`retry` → `alternative_tool` → `degrade` → `simplify` → `complete_partial` → `pause_and_assess` → `escalate`

Runtime injects fallback guidance as user-role messages. No additional LLM cost for the injection itself.

## Mission Lifecycle

```
create → assign → [active] → pause → resume → [active] → complete
                                                        → delete
```

```bash
agency mission pause <name>       # Pause — agent stops receiving events
agency mission resume <name>      # Resume — hot-reloaded
agency mission complete <name>    # Mark complete
agency mission delete <name>      # Remove entirely
```

## Mission Memory

Missions accumulate memory across task executions:

```bash
# View procedures learned from this mission
agency mission show <name>  # includes procedure/episode counts

# Procedures are consolidated after 50+ entries
# Episodes have 90-day retention with monthly narrative consolidation
```

Mission memory is injected into the agent's system prompt at session start, providing learned patterns from previous executions.

## Mission Health

```bash
agency mission health              # All missions
agency mission health <name>       # Specific mission
```

## Autonomous Agent Missions

For agents that should never ask clarifying questions (e.g., alert triage bots), the mission alone is not enough. The agent preset must also enforce autonomous behavior:

```yaml
# In the preset (agent.yaml or preset YAML)
hard_limits:
  - "Never ask clarifying questions. Act on available information."
identity:
  body: "You are an autonomous alert triage agent. You never ask questions — you investigate and report."
```

## Verification

- [ ] `agency mission list` shows the mission
- [ ] `agency mission show <name>` shows correct config, assigned agent
- [ ] Agent responds to events/messages using mission instructions
- [ ] `agency log <agent>` shows mission-related task completions
- [ ] Success criteria evaluation works (check task results)

## See Also

- [Budget & Cost](budget-and-cost.md) — cost mode economics
- [Monitoring & Observability](monitoring-and-observability.md) — trajectory monitoring, reflection debugging
- [Agent Recovery](agent-recovery.md) — stuck agents, budget exhaustion
