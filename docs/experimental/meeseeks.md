---
title: "Meeseeks Agents"
description: "Experimental ephemeral single-purpose agents that parent agents can spawn on-demand."
---


Meeseeks are ephemeral, single-purpose agents spawned on-demand by a parent agent. Named after the creatures from Rick and Morty, they are created to serve a singular purpose and cease to exist after fulfilling it.

> Experimental surface: Meeseeks are not part of the default `0.2.x` core
> Agency path. Keep them available for exploration, but do not treat them as a
> supported first-user workflow.

A Meeseeks has exactly one task, uses a cheap model, inherits its parent's constraints, and self-terminates on completion. No identity, no memory, no history — just the task.

## How to Enable

Meeseeks spawning is opt-in per mission. Add `meeseeks: true` to your mission config:

```yaml
name: ticket-triage
description: Triage incoming tickets
instructions: |
  ...

meeseeks: true
meeseeks_limit: 5           # max concurrent (default: 5)
meeseeks_model: haiku        # default model (default: haiku)
meeseeks_budget: 0.05        # default budget in USD per Meeseeks
```

Only agents with an active [mission](/missions) that has `meeseeks: true` can spawn Meeseeks.

## How Agents Spawn Them

Parent agents get a `spawn_meeseeks` built-in tool when their mission enables it:

```
spawn_meeseeks(
  task: "Extract severity, assignee, and due date from ticket INC-1234 and post a summary to #incidents",
  tools: ["read_file", "send_message"],   # optional — subset of parent's tools
  model: "haiku",                          # optional — defaults to mission config
  budget: 0.05,                            # optional — USD limit
  channel: "incidents"                     # optional — channel for results
)
```

The parent doesn't block waiting for completion. The Meeseeks works independently and posts results to the channel. Fire and forget.

### What Gets Validated

On spawn, the gateway checks:

1. Parent agent is running with standard+ trust level
2. Requested tools are a subset of the parent's granted tools
3. Requested model tier does not exceed the parent's model tier
4. Concurrent Meeseeks count is under the limit (default: 5 per parent)
5. The parent's active mission has `meeseeks: true`
6. Spawn rate is under the limit (default: 10 per minute per parent)
7. If `channel` is specified, the parent has access to that channel

If any check fails, the tool returns an error — no Meeseeks is created.

## What Happens on Spawn

1. Gateway creates a Meeseeks ID: `mks-\{short_uuid\}`
2. Gateway runs an abbreviated startup sequence (about 5 seconds vs 10-15 for regular agents)
3. Meeseeks posts to the designated channel: **"I'm Mr. Meeseeks! Look at me!"**
4. Gateway returns the ID and status to the parent
5. Meeseeks starts working on its task

The Meeseeks gets a minimal system prompt — just the task, the rules, and nothing else:

```
You are a Meeseeks — a single-purpose agent created to complete one task.
Your task: {task description}

Rules:
- Complete your task as quickly and directly as possible
- Post your results to #{channel} using send_message
- Call complete_task when done — you will cease to exist
- If you cannot complete your task, call escalate(reason=...) immediately
- Do not take on additional work. You exist for this one task only.
```

## Meeseeks vs Regular Agents

| | Regular Agent | Meeseeks |
|---|---|---|
| Created by | Operator | Parent agent |
| Lifecycle | Persistent | Ephemeral — terminates on completion |
| Identity | identity.md, personality | None — task-scoped prompt only |
| Knowledge | Accumulates, persists | Reads parent's mission knowledge; contributions tagged to parent's mission |
| Model | Configurable (sonnet default) | Cheap (haiku default) |
| Startup | Full 7-phase sequence | Abbreviated (~5 seconds) |
| Enforcer | Own enforcer | Own enforcer (enforcement is never shared) |

## Budget

Meeseeks budgets are denominated in USD. Set per-spawn or from mission defaults.

```
spawn_meeseeks(task: "...", budget: 0.05)   # 5 cents
```

The budget cannot exceed the parent's per-task budget.

### Distress Escalation

Meeseeks that struggle to complete their task escalate with increasing urgency:

**50% budget consumed** — Warning posted to the task channel:
```
I'm Mr. Meeseeks, I've spent $0.025 of my $0.05 budget and I can't extract
those fields! This is getting weird!
```

**80% budget consumed** — Escalation to `#operator`:
```
MEESEEKS DISTRESS: Can't complete 'Extract fields from INC-1234' —
spawned by henrybot900, $0.04/$0.05 spent. Need help or termination.
```

**100% budget consumed** — Platform terminates the Meeseeks automatically:
```
Meeseeks mks-a1b2 terminated — budget exhausted ($0.05) without
completing: 'Extract fields from INC-1234'. Parent: henrybot900.
```

## The `escalate` Tool

Every Meeseeks has a built-in `escalate` tool for when the task is impossible or needs human judgment:

```
escalate(reason: "Ticket INC-1234 is in a language I can't read")
```

This posts a distress message to `#operator` with the reason, parent name, Meeseeks ID, and task description. The Meeseeks continues working unless the operator kills it.

This is distinct from the automatic budget-based distress messages — a Meeseeks can call `escalate` at any time, even before hitting any budget threshold.

## Container Topology

Each Meeseeks gets its own isolated container stack:

```
Meeseeks container stack:
  ├── mks-a1b2-workspace    # body runtime with task
  ├── mks-a1b2-enforcer     # own enforcer sidecar
  ├── mks-a1b2-internal     # own internal network
  └── (connected to)         # mediation network (shared), egress-net (shared)
```

Every Meeseeks gets its own enforcer — enforcement never runs inside the agent boundary. The enforcer is configured with:

- **Scoped credentials** — derived from the parent's grants for the declared tools only
- **Tool allowlist** — only the declared tools
- **Model restriction** — only the declared model tier
- **Budget enforcement** — enforcer tracks cost and rejects calls beyond budget
- **Parent's constraints** — fully resolved constraint set, mounted read-only

### Abbreviated Startup

Meeseeks skip the identity and session phases of the regular 7-phase start:

| Phase | Regular Agent | Meeseeks |
|-------|--------------|----------|
| 1. Verify | Full config validation | Minimal — validate task + tools |
| 2. Enforcement | Start enforcer | Start enforcer (scoped) |
| 3. Constraints | Mount constraints | Inherit parent's constraints |
| 4. Workspace | Create workspace | Create workspace |
| 5. Identity | Load identity.md | **Skip** |
| 6. Body | Start body runtime | Start body runtime (task prompt) |
| 7. Session | Construct session | **Skip** |

Target startup: ~5 seconds.

## Orphaned Meeseeks

If the parent agent is halted or stopped while Meeseeks are running, the Meeseeks continue working. Parent and agent lifecycles are independent.

The platform:
1. Flags them as orphaned in `agency status` and `agency meeseeks list`
2. Alerts the operator: "Meeseeks \{id\} orphaned — parent \{parent\} is down"
3. Lets the operator decide: let them finish, or terminate with `agency meeseeks kill`

Orphaned Meeseeks retain their budget limits — they self-terminate when budget is exhausted even without operator intervention.

## Knowledge

Meeseeks have a limited relationship with knowledge:

- **Read**: Can query knowledge scoped to the parent's mission
- **Write**: Knowledge contributions are tagged with the parent's mission ID and persist after the Meeseeks terminates
- **No personal knowledge**: Meeseeks don't accumulate anything — there is no "between tasks" for a Meeseeks

## CLI Commands

| Command | Description |
|---------|-------------|
| `agency meeseeks list` | All active Meeseeks: id, parent, task, age, budget used/remaining |
| `agency meeseeks show <id>` | Detail: task, parent, tools, model, budget, status |
| `agency meeseeks kill <id>` | Terminate immediately |
| `agency meeseeks kill --parent <agent>` | Terminate all Meeseeks for a parent |

### Observability

Meeseeks show nested under their parent in `agency status`:

```
Agents:
  Name            Status    Enforcer    Build       Mission
  ────────────────────────────────────────────────────────────────────
  henrybot900     running   running     9b18c82 ✓   ticket-triage (active)
    ├ mks-a1b2    working   running                 "Extract fields from INC-1234"
    └ mks-c3d4    working   running                 "Post summary to #incidents"
```

## Termination

A Meeseeks can be terminated by:

- **Self** — calls `complete_task` (normal completion)
- **Platform** — budget exceeded
- **Operator** — `agency meeseeks kill <id>`
- **Parent** — parent agent calls `kill_meeseeks(id)` (can only kill its own)

On completion or termination: container removed, enforcer removed, network cleaned up. Audit logs preserved at `~/.agency/audit/\{meeseeks_id\}/`.

## What Meeseeks Cannot Do

- **Spawn other Meeseeks** — only regular agents can spawn them
- **Self-elevate trust** — trust is inherited from parent, static
- **Request exceptions** — no policy exception path for Meeseeks
- **Persist** — if you need persistence, use a regular agent

## Example: Data Extraction

A parent agent on a ticket-triage mission spawns a Meeseeks to handle extraction:

```python
# Inside the parent agent's tool use
spawn_meeseeks(
  task="Read ticket INC-1234 from Jira. Extract: severity, assignee, due date, and affected services. Post a formatted summary to #incidents.",
  tools=["read_file", "send_message"],
  budget=0.05,
  channel="incidents"
)
```

The parent continues triaging other tickets. The Meeseeks:
1. Posts "I'm Mr. Meeseeks! Look at me!" to #incidents
2. Reads the ticket data
3. Extracts the requested fields
4. Posts a formatted summary to #incidents
5. Calls `complete_task` and ceases to exist

Total cost: a few cents. Total time: seconds. The parent never blocked.

## Next Steps

- **[Missions](/missions)** — Enable Meeseeks in your mission config
- **[Events and Webhooks](/events)** — How mission triggers activate agents
- **[Teams](/teams)** — Multi-agent coordination
