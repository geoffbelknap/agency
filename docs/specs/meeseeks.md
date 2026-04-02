Meeseeks are ephemeral, single-purpose agents spawned on-demand by a parent agent to handle a specific sub-task. They use cheap models, inherit constraints from their parent, and self-terminate on completion. Named after the creatures from Rick and Morty — created to serve a singular purpose, they expire after fulfilling it.

## What is a Meeseeks

A Meeseeks is an agent that:

- Is spawned by a parent agent (not the operator) via a `spawn_meeseeks` tool
- Has exactly one task, declared at spawn time
- Uses a cheap/fast model (haiku by default)
- Inherits constraints from its parent but cannot exceed them (ASK tenet 11)
- Has no persistent identity, no memory, no knowledge history
- Self-terminates on task completion
- Escalates aggressively if stuck
- First message on spawn: "I'm Mr. Meeseeks! Look at me!"

### Meeseeks vs Regular Agents

| | Regular Agent | Meeseeks |
|---|---|---|
| Created by | Operator | Parent agent |
| Lifecycle | Persistent | Ephemeral — terminates on completion |
| Identity | identity.md, personality | None — task-scoped system prompt only |
| Knowledge | Accumulates, persists | Reads parent's mission knowledge; contributions tagged to parent's mission_id |
| Model | Configurable (sonnet default) | Cheap (haiku default) |
| Startup | Full 7-phase sequence | Abbreviated — skip identity/knowledge phases |
| Trust | Earned over time | Inherits parent's trust level, static, cannot exceed |
| Enforcer | Own enforcer | Own enforcer (ASK tenet 1 — enforcement never shared) |

## Spawn Mechanism

### The `spawn_meeseeks` Tool

Parent agents get a `spawn_meeseeks` built-in tool when their mission has Meeseeks spawning enabled:

```
spawn_meeseeks(
  task: "Extract severity, assignee, and due date from ticket INC-1234 and post a summary to #incidents",
  tools: ["read_file", "send_message"],     # optional — subset of parent's tools (defaults to all)
  model: "haiku",                            # optional — defaults to haiku
  budget: 0.05,                               # optional — USD limit (defaults to platform per-task budget)
  channel: "incidents"                       # optional — channel for results
)
```

### Gateway Validation

On spawn request, the gateway validates:

1. Parent agent is running and has standard+ trust level
2. Requested tools are a subset of parent's granted tools (ASK tenet 11)
3. Requested model tier does not exceed parent's model tier
4. Concurrent Meeseeks count doesn't exceed platform limit (default: 5 per parent)
5. Parent's active mission has `meeseeks: true` in its config
6. Spawn rate does not exceed platform limit (default: 10 per minute per parent)
7. If `channel` is specified, parent has access to that channel

If any check fails, the tool returns an error to the parent — no Meeseeks is created.

### Mission Config

Meeseeks spawning is opt-in per mission:

```yaml
# In mission.yaml
meeseeks: true              # allow this mission's agent to spawn Meeseeks
meeseeks_limit: 5           # max concurrent (optional, default from platform config)
meeseeks_model: haiku       # default model for spawned Meeseeks (optional)
meeseeks_budget: 0.05       # default budget per Meeseeks in USD (optional)
```

### Spawn Flow

1. Parent calls `spawn_meeseeks(task, ...)`
2. Gateway validates authority, tools, model, limits
3. Gateway creates a Meeseeks ID: `mks-\{short_uuid\}`
4. Gateway creates an abbreviated agent config (no identity, no mission)
5. Gateway runs abbreviated startup: enforcer → constraints → workspace → body
6. Meeseeks posts to designated channel: "I'm Mr. Meeseeks! Look at me!"
7. Gateway returns `\{"meeseeks_id": "mks-a1b2c3", "status": "spawned"\}` to parent
8. Parent continues its own work — fire and forget

The parent doesn't block waiting for completion. The Meeseeks works independently and posts results to the channel.

## Meeseeks Lifecycle

```
spawned → working → completed (auto-terminate)
                  → distressed (stuck, escalating)
                  → terminated (killed by parent, operator, or budget)
```

### System Prompt

The Meeseeks gets a minimal, focused system prompt:

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

No identity.md, no framework, no agents list, no mission context. Just the task.

### The `escalate` Tool

All Meeseeks have a built-in `escalate` tool:

```
escalate(reason: "string describing why the task can't be completed")
```

When called: posts a distress message to `#operator` with the reason, parent name, Meeseeks ID, and task description. The Meeseeks continues working unless the operator kills it.

This is proactive escalation — the Meeseeks calls it when it determines the task is impossible or needs human judgment. It is distinct from the automatic budget-based distress messages, which are triggered by the platform at 50% and 80% budget consumption. A Meeseeks can call `escalate(reason=...)` at any time, including before hitting any budget threshold.

### Completion

When the Meeseeks calls `complete_task`:
1. Results are posted to the channel (if not already)
2. Container is removed immediately
3. Enforcer sidecar is removed
4. Internal network is removed
5. Audit log entries persist at `~/.agency/audit/\{meeseeks_id\}/`
6. Any knowledge contributions persist (tagged to parent's `mission_id`)

### Distress Escalation

Meeseeks that can't complete their task escalate with increasing urgency:

**50% budget consumed:** Warning posted to the task channel:
```
I'm Mr. Meeseeks, I've spent ${used} of my ${budget} budget and I can't {task}!
This is getting weird!
```

**80% budget consumed:** Escalation to `#operator`:
```
MEESEEKS DISTRESS: Can't complete '{task}' — spawned by {parent},
${used}/${budget} spent. Need help or termination.
```

**100% budget consumed:** Platform terminates the Meeseeks automatically. Alert to operator:
```
Meeseeks {id} terminated — budget exhausted (${budget}) without
completing: '{task}'. Parent: {parent}.
```

The `budget` parameter is denominated in USD, consistent with the platform budget model defined in the brief deprecation spec.

### Termination

A Meeseeks can be terminated by:
- **Self** — calls `complete_task` (normal completion)
- **Platform** — budget exceeded
- **Operator** — `agency meeseeks kill <id>`
- **Parent** — parent agent calls `kill_meeseeks(id)` tool (if granted)

Termination is immediate: container stopped, removed, network cleaned up. Audit log preserved.

`kill_meeseeks(id)` is automatically available to any parent that has `spawn_meeseeks`. A parent can only kill its own Meeseeks.

## Container Topology

Each Meeseeks gets its own isolated container stack, consistent with ASK tenet 1 (enforcement never runs inside the agent boundary):

```
Meeseeks container stack:
  ├── mks-a1b2-workspace    # body runtime with task
  ├── mks-a1b2-enforcer     # own enforcer sidecar
  ├── mks-a1b2-internal     # own internal network
  └── (connected to)         # mediation network (shared), egress-net (shared)
```

The Meeseeks connects to the shared mediation and egress networks for comms access and LLM calls, same as regular agents.

The enforcer is configured with:
- **Scoped credentials** — derived from the parent's capability grants for the declared tools. Real API keys remain in the egress proxy; the Meeseeks' enforcer receives scoped tokens that the egress proxy maps to real credentials.
- **Tool allowlist** — only the declared tools, not the parent's full set
- **Model restriction** — only the declared model tier
- **Budget enforcement** — enforcer tracks LLM call count, rejects calls beyond budget
- **Parent's effective constraints** — the fully resolved constraint set (platform > org > department > team > agent), mounted read-only. Meeseeks cannot request exceptions.

### Abbreviated Startup

Regular agents use a 7-phase startup. Meeseeks skip phases that don't apply:

| Phase | Regular Agent | Meeseeks |
|-------|--------------|----------|
| 1. Verify | Full config validation | Minimal — validate task + tools |
| 2. Enforcement | Start enforcer | Start enforcer (scoped) |
| 3. Constraints | Mount constraints | Inherit parent's constraints |
| 4. Workspace | Create workspace | Create workspace (standard body image) |
| 5. Identity | Load identity.md | **Skip** — no identity |
| 6. Body | Start body runtime | Start body runtime (task-focused prompt) |
| 7. Session | Construct session | **Skip** — no session context to restore |

Target startup time: ~5 seconds (vs ~10-15 for regular agents).

## Knowledge Interaction

Meeseeks have a limited relationship with the knowledge graph:

- **Read:** A Meeseeks can query knowledge scoped to its parent's `mission_id` (if the parent is on a mission). This gives it context about the mission's accumulated knowledge.
- **Write:** Any `contribute_knowledge` calls are tagged with the parent's `mission_id`. The knowledge persists after the Meeseeks terminates.
- **No personal knowledge:** Meeseeks have no agent-scoped knowledge. They don't accumulate or carry anything between tasks (there is no "between" — they only have one task).

## Orphaned Meeseeks (ASK Tenet 13)

If the parent agent is halted or stopped while Meeseeks are running:

1. Meeseeks continue working (ASK tenet 13 — lifecycles independent)
2. Platform flags them as orphaned in `agency status` and `agency meeseeks list`
3. Alert sent to operator: "Meeseeks \{id\} orphaned — parent \{parent\} is down"
4. Operator decides: let them finish, or terminate with `agency meeseeks kill`

Orphaned Meeseeks retain their budget limits — they will self-terminate when budget is exhausted even without operator intervention.

If the parent's mission is paused while Meeseeks are running, existing Meeseeks continue working (they have their own task independent of mission state). New spawn requests are rejected because the mission is not active.

## ASK Tenet Compliance

| Tenet | Compliance |
|-------|-----------|
| **1. Constraints external** | Own enforcer per Meeseeks. Constraints inherited from parent, mounted read-only. |
| **2. Every action traced** | Own audit log at `~/.agency/audit/\{meeseeks_id\}/`. Written by enforcer, not the Meeseeks. |
| **3. Mediation complete** | Own internal network, own enforcer. No unmediated paths. |
| **4. Least privilege** | Only declared tools granted. Model capped. Budget enforced. No excess capabilities. |
| **5. No blind trust** | Parent→Meeseeks trust relationship explicit: parent spawned it, gateway validated authority. |
| **11. Delegation ≤ scope** | Gateway validates tools, model, capabilities are subset of parent's grants at spawn time. |
| **12. Synthesis ≤ authorization** | Concurrent Meeseeks limit is a practical resource control. Formal tenet 12 compliance derives from the fact that all Meeseeks inherit the parent's authorization ceiling — no individual Meeseeks can exceed the parent's scope, and the parent itself is already bounded by its own authorization. The concurrent limit prevents resource abuse, not synthesis violations. |
| **13. Lifecycles independent** | Parent halt doesn't auto-kill Meeseeks. Orphaned Meeseeks flagged for operator decision. |
| **15. Trust inherited** | Meeseeks gets parent's trust level at spawn. Static — cannot self-elevate. |
| **17. Instructions from verified principals** | The Meeseeks' instructions derive from the operator (via mission config that enabled spawning) and the gateway (which validated the spawn request). The parent provides the task description as data; the gateway packages it into the Meeseeks' system prompt. |
| **23. Knowledge durable** | Knowledge contributions tagged to parent's mission_id. Persist beyond Meeseeks lifetime. |
| **25. No identity to mutate** | Meeseeks have no persistent identity. Nothing to corrupt or roll back. |

## Observability

### `agency status`

Meeseeks show nested under their parent:
```
Agents:
  Name            Status    Enforcer    Build       Mission
  ────────────────────────────────────────────────────────────────────
  henrybot900     running   running     9b18c82 ✓   ticket-triage (active)
    ├ mks-a1b2    working   running                 "Extract fields from INC-1234"
    └ mks-c3d4    working   running                 "Post summary to #incidents"
```

### CLI Commands

| Command | Effect |
|---------|--------|
| `agency meeseeks list` | All active Meeseeks: id, parent, task, age, budget used/remaining |
| `agency meeseeks show <id>` | Detail: task, parent, tools, model, budget, status |
| `agency meeseeks kill <id>` | Terminate immediately |
| `agency meeseeks kill --parent <agent>` | Terminate all Meeseeks for a parent |

### REST API

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/api/v1/meeseeks` | Spawn (called by parent agent via tool) |
| `GET` | `/api/v1/meeseeks` | List all active Meeseeks |
| `GET` | `/api/v1/meeseeks/\{id\}` | Show detail |
| `DELETE` | `/api/v1/meeseeks/\{id\}` | Kill |
| `DELETE` | `/api/v1/meeseeks?parent=\{agent\}` | Kill all for parent |

### MCP Tools

| Tool | Description |
|------|-------------|
| `agency_meeseeks_list` | List active Meeseeks |
| `agency_meeseeks_show` | Show Meeseeks detail |
| `agency_meeseeks_kill` | Terminate a Meeseeks |

Spawning is done via the parent agent's `spawn_meeseeks` built-in tool, not an MCP tool.

### Audit Events

- `meeseeks_spawned` — id, parent, parent_mission_id, task, tools, model, budget
- `meeseeks_completed` — id, parent, task, turns_used, budget_remaining
- `meeseeks_distressed` — id, parent, task, turns_used, budget_total, detail
- `meeseeks_terminated` — id, parent, reason (budget_exceeded, operator_kill, parent_kill)
- `meeseeks_orphaned` — id, parent, reason (parent_halted, parent_stopped)

## Scope Boundaries

This spec covers Meeseeks agents only. It depends on:
- **Missions spec** — Meeseeks spawning is gated by mission config
- **Build versioning** — Meeseeks containers get build ID labels like all other containers

It does not cover:
- **Meeseeks-to-Meeseeks spawning** — a Meeseeks cannot spawn other Meeseeks. Only regular agents can spawn them.
- **Operator-spawned Meeseeks** — v1 requires a parent agent. Operator-direct spawning could be added later.
- **Persistent Meeseeks** — contradicts the core concept. If you need persistence, use a regular agent.

The missions spec should be updated to include Meeseeks config fields (`meeseeks`, `meeseeks_limit`, `meeseeks_model`, `meeseeks_budget`) in its YAML schema.

## What This Enables

- Mission-assigned agents can scale out to handle burst workloads without operator intervention
- Cheap, fast sub-task execution — haiku doing extraction/formatting while sonnet/opus does reasoning
- Natural cost control — budget per Meeseeks, concurrent limit per parent
- Clean resource management — containers auto-cleaned on completion
- Entertaining observability — distress messages make it obvious when something is stuck
