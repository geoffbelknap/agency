An optional reflection phase injected into the task completion flow. When enabled on a mission, the body runtime intercepts `complete_task()` and forces the agent to evaluate its own output against the mission's criteria before the `task_complete` signal fires. If the self-review finds deficiencies, the agent continues working. If it passes, completion proceeds normally.

Reflection is a quality-improvement mechanism, not a security control. It runs inside the agent boundary and does not replace external enforcement.


## Problem

Agents self-report task completion via `complete_task(summary=...)`. The runtime emits `task_complete`, the signal propagates through comms to the gateway WebSocket hub, and the task is done. There is no structured mechanism for the agent to pause and evaluate whether its output actually satisfies the mission's goals before declaring done. The agent grades its own homework in a single pass.

This produces two failure modes:

1. **Premature completion** — the agent declares done but missed a requirement from the mission instructions. The operator discovers the gap after the fact.
2. **Shallow output** — the agent produces a structurally complete but substantively weak response. A second pass would catch the weakness, but the runtime has no second pass.

Both failures are detectable by the agent itself if given the opportunity to review. The reflection loop provides that opportunity.


## Mission Schema Addition

Add a `reflection` block to the mission YAML schema:

```yaml
id: 8a3f2b1c-4d5e-6f7a-8b9c-0d1e2f3a4b5c
name: ticket-triage
description: Review and respond to incident response tickets
status: active
assigned_to: henrybot900
assigned_type: agent

instructions: |
  You are responsible for triaging incoming incident response tickets.
  ...

reflection:
  enabled: true              # default: false
  max_rounds: 3              # max reflection cycles before forced completion (default: 3)
  criteria:                  # optional — overrides default reflection prompt
    - "Output addresses all aspects of the triggering event"
    - "Severity assessment is justified with specific evidence"
    - "Recommended actions are concrete and actionable"

triggers:
  - source: connector
    connector: jira-ops
    event_type: issue_created
```

### Field Details

- **`reflection.enabled`** — Boolean. When `true`, the runtime intercepts `complete_task()` and runs the reflection phase. Default: `false`. Missions without this block behave exactly as they do today.
- **`reflection.max_rounds`** — Integer. Maximum number of reflection cycles before the runtime forces completion. Prevents infinite self-critique loops. Default: `3`. Minimum: `1`. Maximum: `10`.
- **`reflection.criteria`** — List of strings. Optional. When provided, these criteria replace the default reflection prompt's quality checklist. Each criterion is injected verbatim into the reflection prompt. When omitted, the runtime uses the default criteria: completeness, accuracy, and actionability.


## Runtime Behavior

When `reflection.enabled` is `true` in the active mission, the task completion flow changes:

```
Agent calls complete_task(summary=...)
  │
  ├─ reflection.enabled == false → emit task_complete (existing behavior)
  │
  └─ reflection.enabled == true
       │
       ├─ Intercept: do NOT emit task_complete
       ├─ Inject reflection prompt into conversation (system role)
       ├─ LLM responds with verdict
       │
       ├─ APPROVED → emit task_complete with reflection metadata
       │
       └─ REVISION_NEEDED
            ├─ Clear completion flag
            ├─ Inject revision feedback into conversation
            ├─ Increment round counter
            ├─ Agent continues working
            └─ Agent eventually calls complete_task() again → loop
```

### Step-by-step

1. Agent calls `complete_task(summary=...)`.
2. Runtime checks `reflection.enabled` on the active mission. If `false` or no mission is active, emit `task_complete` immediately (existing behavior, no change).
3. Runtime does NOT emit `task_complete`. It stores the summary and sets an internal `pending_reflection` flag.
4. Runtime injects a reflection prompt into the conversation as a user-role message. The prompt contains the agent's summary, the mission criteria, and instructions to evaluate honestly. User-role is used (not system-role) because mid-conversation system messages are non-standard in the OpenAI chat completions API and may not work as expected through the enforcer's proxy.
5. The LLM responds. The runtime parses the response for a structured verdict.
6. If **APPROVED** (with optional notes): emit `task_complete` with `reflection_rounds` set to the number of cycles completed. Clear `pending_reflection`.
7. If **REVISION_NEEDED** (with specific issues): clear the completion flag, emit a `reflection_cycle` signal for observability, inject the revision feedback as a user-role message, and let the agent continue working. The agent will eventually call `complete_task()` again, restarting from step 1.
8. If the round counter reaches `max_rounds`: force completion. Emit `task_complete` with `reflection_forced: true` and the round count. Log a warning.

### Completion Flag Interaction

The existing `complete_task()` handler sets a flag that the conversation loop checks after each tool call. During reflection, this flag is held in a `pending` state — the loop does not terminate. The flag only transitions to `done` when the reflection verdict is APPROVED or max rounds are reached.


## Reflection Prompt Template

The runtime injects this as a user-role message when intercepting `complete_task()`:

```
You just called complete_task with the following summary:

---
{summary}
---

Before this task is marked complete, evaluate your output honestly against these criteria:

{criteria_block}

Respond with JSON only. No text outside the JSON.

{
  "verdict": "APPROVED or REVISION_NEEDED",
  "criteria_results": [
    {
      "criterion": "<the criterion text>",
      "met": true or false,
      "justification": "<one sentence>"
    }
  ],
  "issues": ["<specific issue to address>"]
}

Set verdict to APPROVED only if all criteria are met. Set issues to an empty list if APPROVED.
Do not hedge. Do not add caveats about limitations. Evaluate the actual output you produced.
```

The `{criteria_block}` is built from a fallback chain:

1. **`reflection.criteria`** — if present in the mission, use these verbatim
2. **`success_criteria.checklist`** — if the mission defines success criteria (see Mission Success Criteria spec), format each checklist item's `description` as a criterion
3. **Default criteria** — if neither is present, use the built-in defaults below

This fallback chain avoids operators defining criteria in two places. If a mission has `success_criteria.checklist` but no `reflection.criteria`, the reflection loop evaluates against the same checklist that platform-side evaluation uses.

Default criteria (used only when no mission-specific criteria exist):

```
1. The output is complete — it addresses the full scope of the task, not just part of it.
2. The output is accurate — claims are supported, no hallucinated details.
3. The output is actionable — the recipient can act on it without needing to ask follow-up questions.
```

### Verdict Parsing

The runtime parses the LLM response as JSON. It extracts the `verdict` field (`APPROVED` or `REVISION_NEEDED`) and the `issues` list. If the response is malformed JSON or the `verdict` field is missing, the runtime treats it as `REVISION_NEEDED` with a single issue: "Reflection verdict was unparseable — treating as revision needed." Fail closed, not open.

The structured JSON format ensures reliable extraction of per-criterion results and issue lists. Freeform text parsing was considered and rejected — it produces inconsistent issue extraction across models and prompt variations.


## Signal Extension

### task_complete

The `task_complete` signal gains optional reflection fields:

```json
{
  "signal_type": "task_complete",
  "data": {
    "task_id": "tsk-a1b2c3d4",
    "result": "Triaged INC-1234 as P2, assigned to platform-team, posted to #incidents",
    "turns": 12,
    "reflection_rounds": 2,
    "reflection_forced": false
  }
}
```

- **`reflection_rounds`** — Integer. Number of reflection cycles completed. `0` when reflection is disabled. `1` means one review pass that was approved on the first try.
- **`reflection_forced`** — Boolean. `true` when the agent hit `max_rounds` without self-approving. Absent or `false` otherwise.
- **`reflection_budget_exhausted`** — Boolean. `true` when budget exhaustion forced completion mid-reflection. Absent or `false` otherwise.

When reflection is disabled, these fields are omitted from the signal. Existing consumers are unaffected.

### reflection_cycle

New signal type emitted after each REVISION_NEEDED verdict. Provides observability into the reflection process:

```json
{
  "signal_type": "reflection_cycle",
  "data": {
    "task_id": "tsk-a1b2c3d4",
    "round": 1,
    "verdict": "REVISION_NEEDED",
    "issues": ["Severity not justified with specific evidence", "Missing recommended actions"]
  }
}
```

- **`round`** — Integer. Which reflection cycle just completed (1-indexed).
- **`verdict`** — String. Always `REVISION_NEEDED` for this signal (APPROVED triggers `task_complete` instead).
- **`issues`** — List of strings. The specific deficiencies identified by the agent. Extracted from the LLM response.

This signal is appended to `agent-signals.jsonl` like all other signals and propagated through comms to the gateway WebSocket hub.


## Budget Interaction

Reflection rounds consume budget like any other LLM call. Each reflection cycle involves one injected prompt and one LLM response — roughly equivalent to one conversation turn in cost.

If budget is exhausted mid-reflection (the enforcer returns a budget-exceeded response while the agent is in the reflection loop), the runtime forces completion immediately:

- Emit `task_complete` with `reflection_forced: true` and `reflection_budget_exhausted: true`
- Include the last summary the agent provided (from the most recent `complete_task()` call)
- Log a warning: budget exhaustion interrupted reflection

The runtime does not attempt to reserve budget for reflection. Budget tracking remains in the enforcer. The reflection loop simply observes the same budget boundaries as all other LLM interactions.

Operators should account for reflection overhead when setting task budgets. Each reflection round costs approximately one additional LLM turn. A mission with `max_rounds: 3` could add up to 3 extra turns of cost per task.


## REST API

No new endpoints. The reflection configuration is part of the mission YAML and flows through existing mission CRUD endpoints:

| Method | Path | Effect on Reflection |
|--------|------|---------------------|
| `POST` | `/api/v1/missions` | Creates mission with optional `reflection` block |
| `PUT` | `/api/v1/missions/{name}` | Updates reflection config; hot-reloads via enforcer SIGHUP |
| `GET` | `/api/v1/missions/{name}` | Returns mission including `reflection` block |

Reflection config changes take effect on the next task. In-progress reflection cycles complete under the config that was active when they started.


## ASK Tenet Compliance

Reflection is self-evaluation within the agent boundary. It is the agent reviewing its own work using the same LLM that produced the work. This has important implications:

- **Tenet 1 (Constraints are external and inviolable)** — Reflection is NOT external enforcement. It runs inside the agent process, using the agent's LLM context. An agent could theoretically produce a weak review and approve itself. External enforcement (enforcer, constraints, policy engine) remains unchanged and operates independently of reflection. Operators must not treat reflection as a security control or compliance mechanism.
- **Tenet 2 (Every action leaves a trace)** — Reflection cycles are logged via `reflection_cycle` signals appended to `agent-signals.jsonl`. The final `task_complete` signal records round count and forced-completion status.
- **Tenet 3 (Mediation is complete)** — Reflection prompts flow through the same enforcer-mediated LLM path as all other conversation turns. No new unmediated paths are introduced.
- **Tenet 17 (Instructions only come from verified principals)** — The reflection prompt is generated by the runtime from operator-authored mission criteria. The agent does not receive instructions from external entities during reflection.

### Validation Warning

When a mission enables `reflection` without also enabling `success_criteria.evaluation`, the gateway emits a validation warning on `mission create` and `mission assign`:

```
Warning: Mission "ticket-triage" has reflection enabled but no platform-side
evaluation. Reflection is agent-internal self-review — the agent grades its
own homework. For external quality verification, also enable
success_criteria.evaluation.
```

This warning is informational — it does not block mission creation. It reminds operators that reflection is a quality aid, not an enforcement mechanism (ASK tenet 1).


## Task Tier Interaction

Reflection only activates at task tier `full`. At `minimal` and `standard` tiers, reflection is skipped regardless of mission config — the task isn't complex enough to justify 1-3 extra LLM rounds.

When `cost_mode: thorough` is set, the default `reflection.max_rounds` is 2 (not 3) to balance quality with cost. When `cost_mode: frugal` or `balanced`, reflection is disabled unless explicitly overridden in mission YAML.

See the Task Tier and Cost Model spec for the full tier classification logic.

Cost attribution: reflection LLM calls are tagged with `X-Agency-Cost-Source: reflection` so they appear separately in budget breakdowns.


## When NOT to Use

Reflection adds latency and cost. Each round is one additional LLM turn. Skip it for:

- **Time-sensitive triage** — where speed matters more than polish. A P1 incident response that takes 30 extra seconds to self-review is 30 seconds of downtime.
- **Simple routing and forwarding tasks** — tasks where the output is mechanical (route ticket to team, post message to channel). There is nothing meaningful to reflect on.
- **Meeseeks** — ephemeral agents with tight budgets and narrow scope. Reflection overhead is disproportionate to their lifespan. Meeseeks do not hold missions, so reflection does not apply to them by default.
- **High-volume event processing** — missions triggered by frequent events (e.g., every commit, every message). Reflection on each event compounds cost quickly.

The default is `reflection.enabled: false`. Operators opt in per mission.
