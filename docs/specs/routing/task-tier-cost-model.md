**Status:** Implemented
**Last updated:** 2026-04-01

**Implementation notes:** Task tier classification is implemented in `images/body/task_tier.py` with tests in `test_task_tier.py`. The `classify_task_tier()` function, `COST_MODE_DEFAULTS` expansion, `TIER_FEATURES` matrix, and `resolve_features()` are all working. The `cost_mode` field is part of the mission model in Go (`internal/models/mission.go`). Mission health checks reference task tiers. The body runtime (`body.py`) calls the tier classifier at task start and uses the result to control feature activation (reflection, memory, evaluation, prompt assembly).

Not all tasks deserve the same investment. A "hi" DM should get a fast response with zero overhead. A P1 incident triage should get reflection, evaluation, and memory capture. The platform needs to make this decision automatically — operators shouldn't have to think about which features fire on which tasks.

This spec defines two mechanisms:

1. **Task tiers** — runtime classification of task weight that determines which features activate. Automatic, based on observable signals at task start.
2. **cost_mode** — operator shorthand in mission YAML that sets the ceiling for feature activation. Optional, overrides defaults.

Together they ensure lightweight interactions stay fast and cheap while important mission work gets the full quality/learning treatment.


## Problem

The agentic design pattern features (reflection loop, success criteria evaluation, procedural memory, episodic memory, trajectory monitoring, fallback policies) each add value but some add cost. Without progressive activation:

- An agent with reflection enabled burns 1-3 extra LLM calls responding to "what time is it?"
- Memory capture fires a post-task LLM call for a one-word acknowledgment
- System prompt bloats with procedural/episodic memory for a task that doesn't need it
- Operators must manually disable features per-mission to avoid waste, or leave everything off and miss value on important tasks

The fix is automatic: classify task weight at start, activate features proportionally.


## Task Tiers

Three tiers, determined at task start by the body runtime:

| Tier | When | Features Active |
|---|---|---|
| `minimal` | Ad-hoc DMs, @mentions, short messages without mission context | Trajectory monitoring only |
| `standard` | Mission-triggered tasks, DMs to agents with active missions | Trajectory + fallback + memory capture |
| `full` | Tasks matching `cost_mode: thorough`, or tasks where mission config explicitly enables expensive features | Everything the mission config enables |

### Classification Logic

The body runtime classifies at task start, before entering the conversation loop. Classification uses signals available at that moment — no LLM call required.

```python
def classify_task_tier(task, mission):
    """Classify task into minimal/standard/full tier."""
    # No mission = always minimal
    if not mission:
        return "minimal"

    # Operator override via cost_mode
    cost_mode = mission.get("cost_mode", "balanced")
    if cost_mode == "frugal":
        return "minimal"
    if cost_mode == "thorough":
        return "full"

    # Classify by trigger source
    source = task.get("source", "")
    if source in ("dm", "mention", "idle_direct"):
        # Ad-hoc interaction — check message length
        content = task.get("content", "")
        if len(content) < 100:
            return "minimal"
        return "standard"

    # Mission trigger (connector, schedule, webhook, channel match)
    if source in ("connector", "schedule", "webhook", "channel_trigger"):
        return "full" if cost_mode == "thorough" else "standard"

    # Default
    return "standard"
```

### Tier → Feature Activation Matrix

| Feature | minimal | standard | full |
|---|---|---|---|
| **Trajectory monitoring** | on | on | on |
| **Fallback policies** | off | on | on |
| **Reflection loop** | off | off | on (if mission.reflection.enabled) |
| **Success criteria eval** | off | checklist_only (if configured) | mission config (llm or checklist) |
| **Procedural memory inject** | off | off | on (if configured) |
| **Procedural memory capture** | off | on (if configured) | on (if configured) |
| **Episodic memory inject** | off | off | on (if configured) |
| **Episodic memory capture** | off | on (if configured) | on (if configured) |
| **System prompt size** | minimal (identity + mission instructions only) | standard (+ comms, framework, skills) | full (+ procedural, episodic, org context) |

Key decisions:

- **Trajectory monitoring is always on.** It's free (in-memory, no LLM calls) and catches stuck agents regardless of task weight.
- **Memory capture runs at `standard` but injection only at `full`.** Capture is cheap (one shared post-task call) and builds the memory bank. Injection adds tokens to the system prompt — only worth it for complex tasks that benefit from past experience.
- **Reflection only at `full`.** It's the most expensive feature (1-3 extra LLM rounds). Only fires when the operator has explicitly opted in via mission config AND the task is complex enough to justify it.
- **Fallback off at `minimal`.** Ad-hoc DMs rarely hit tool errors that need structured recovery. If they do, the agent improvises — acceptable for a quick interaction.

### Tier Override

The mission YAML can force a minimum tier for all tasks:

```yaml
# Force all tasks on this mission to at least standard
min_task_tier: standard  # minimal | standard | full (default: none — auto-classify)
```

This is useful for missions where even quick DMs should get memory capture (e.g., a support agent that should remember every interaction).


## cost_mode Shorthand

A single field in mission YAML that configures the cost/quality tradeoff without requiring operators to understand 6 feature flags:

```yaml
cost_mode: balanced  # frugal | balanced | thorough
```

### Mode Definitions

**frugal** — minimize LLM calls. For high-volume, low-stakes missions.
```
Equivalent to:
  reflection.enabled: false
  success_criteria.evaluation.mode: checklist_only (if evaluation enabled)
  procedural_memory.capture: false
  procedural_memory.retrieve: false
  episodic_memory.capture: false
  episodic_memory.retrieve: false
  # Trajectory and fallback remain on (free)
```

**balanced** (default) — capture memory, use cheap evaluation, skip reflection.
```
Equivalent to:
  reflection.enabled: false
  success_criteria.evaluation.mode: checklist_only (if evaluation enabled)
  procedural_memory.capture: true
  procedural_memory.retrieve: true
  procedural_memory.max_retrieved: 3
  episodic_memory.capture: true
  episodic_memory.retrieve: true
  episodic_memory.max_retrieved: 3
  # Trajectory and fallback on
```

**thorough** — full quality treatment. For high-stakes, low-volume missions.
```
Equivalent to:
  reflection.enabled: true
  reflection.max_rounds: 2
  success_criteria.evaluation.mode: llm (if evaluation enabled)
  procedural_memory.capture: true
  procedural_memory.retrieve: true
  procedural_memory.max_retrieved: 5
  procedural_memory.include_failures: true
  episodic_memory.capture: true
  episodic_memory.retrieve: true
  episodic_memory.max_retrieved: 5
  # Trajectory and fallback on
```

### Precedence

Explicit feature config in mission YAML overrides cost_mode. If an operator sets `cost_mode: frugal` but also `reflection.enabled: true`, reflection wins — the operator explicitly asked for it.

Resolution order:
1. Explicit feature config in mission YAML (highest priority)
2. cost_mode expansion (fills in unset fields)
3. Platform defaults (lowest priority)

This means `cost_mode` is a convenient starting point, not a constraint. Operators can start with `cost_mode: balanced` and override individual features as needed.


## Cost Attribution

Every LLM call made by the new features is tagged with a `source` field in the budget tracking system. This lets operators see exactly where their budget goes.

### Source Tags

| Tag | Feature | Charged To |
|---|---|---|
| `agent_task` | Normal agent conversation turns | Agent task budget |
| `reflection` | Reflection loop rounds | Agent task budget |
| `evaluation` | Success criteria LLM evaluation | Platform budget |
| `memory_capture` | Post-task procedure/episode generation | Platform budget |
| `consolidation` | Procedure/episode consolidation | Platform budget |
| `context_summary` | Context window summarization | Agent task budget |

### Budget API Extension

The existing `GET /api/v1/agents/{name}/budget/remaining` response is extended with a `breakdown` field:

```json
{
  "daily_remaining": 8.45,
  "monthly_remaining": 187.20,
  "task_remaining": 1.65,
  "breakdown": {
    "agent_task": 0.28,
    "reflection": 0.05,
    "context_summary": 0.02
  }
}
```

The breakdown shows spending by source for the current task. This is tracked by the enforcer's budget tracker, which already sees every LLM call through the proxy.

### Enforcer Implementation

The enforcer's `BudgetTracker` gains a `source` parameter on `RecordUsage()`:

```go
func (bt *BudgetTracker) RecordUsage(tokens int, cost float64, source string) {
    bt.mu.Lock()
    defer bt.mu.Unlock()
    bt.taskCost += cost
    bt.dailyCost += cost
    bt.monthlyCost += cost
    bt.sourceBreakdown[source] += cost
}
```

The body runtime passes the source tag as an HTTP header on each LLM request:

```
X-Agency-Cost-Source: reflection
```

The enforcer reads this header and passes it to `RecordUsage()`. If the header is absent, the source defaults to `agent_task`.


## Mission Health Cost Visibility

`agency mission health <name>` is extended to show cost impact of features:

```
$ agency mission health ticket-triage

Health Indicators:
  ✓  No tickets unprocessed > 2 hours

Cost Profile:
  cost_mode: balanced
  Task tier distribution (last 24h):
    minimal:  12 tasks (avg $0.03/task)
    standard: 45 tasks (avg $0.08/task)
    full:      3 tasks (avg $0.22/task)
  Feature overhead (last 24h):
    reflection:     $0.00  (disabled)
    evaluation:     $0.00  (checklist_only — free)
    memory_capture: $0.94  (48 captures × ~$0.02)
    trajectory:     $0.00  (free)
    fallback:       $0.00  (free)
  Total feature overhead: $0.94 / $4.80 total = 19.6%
```

This gives operators a concrete answer to "how much are these features costing me?" and "is the overhead reasonable for this mission's value?"


## System Prompt Size Tiers

Prompt bloat is a hidden cost — more tokens in the system prompt means more input tokens on every LLM call for the entire task. The task tier controls prompt assembly:

### minimal prompt (~500 tokens)
```
identity.md
mission instructions (if mission active)
task completion guidance
```

### standard prompt (~2,000 tokens)
```
identity.md
mission instructions
mission behavioral frame
comms context
FRAMEWORK.md
AGENTS.md
skills
task completion guidance
```

### full prompt (~3,500+ tokens)
```
identity.md
mission instructions
mission behavioral frame
mission knowledge (org facts)
procedural memory (past approaches)
episodic memory (recent episodes)
memory index (agent private memory)
comms context
PLATFORM.md
FRAMEWORK.md
AGENTS.md
skills
task completion guidance
```

The difference between minimal and full is ~3,000 tokens of system prompt. Over a 10-turn task, that's ~30,000 extra input tokens — roughly $0.01-0.03 depending on model. Not huge per-task, but it compounds across high-volume missions.

The body runtime's `assemble_system_prompt()` already builds the prompt from parts. The tier simply controls which parts are included.


## Interaction with Existing Budget Model

Task tiers do not change budget limits — they change what the agent spends budget on. An agent with a $2.00/task budget responding to "hi" with tier `minimal` will use ~$0.01. The same agent handling a P1 incident with tier `full` might use $0.50.

Budget exhaustion still hard-stops the agent regardless of tier. If reflection burns through the task budget, the enforcer cuts it off. The tier system makes this less likely by ensuring expensive features only fire on tasks that justify them.

Platform budget (for evaluation and memory capture) is separate from agent budget and not affected by task tier. Platform costs are always operational overhead — the operator controls them via cost_mode and feature config.


## ASK Compliance

- **Tenet 1 (Constraints are external)** — Task tier classification runs in the body runtime but the ceiling is operator-set (cost_mode, min_task_tier). The agent cannot escalate its own tier.
- **Tenet 4 (Least privilege)** — minimal tier gives the agent fewer prompt sections and no expensive features. This is least-privilege applied to computational resources.
- **Tenet 2 (Every action leaves a trace)** — Task tier is logged in the `task_accepted` signal: `{"tier": "standard"}`. Cost attribution tags every LLM call with its source.


## When NOT to Use cost_mode

`cost_mode` is a convenience, not a requirement. Operators who want fine-grained control should configure individual features directly. `cost_mode` is for operators who want a reasonable default without reading 6 feature specs.

If an operator has specific quality requirements ("reflection must always run on compliance reports, but evaluation is unnecessary"), they should set those explicitly and leave `cost_mode` unset.
