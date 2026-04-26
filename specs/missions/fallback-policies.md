When a tool call fails, an API returns an error, or an approach stalls, agents improvise. Sometimes they retry the same failing call in a loop (caught by trajectory monitoring as `tool_repetition`). Sometimes they give up after a single failure. Sometimes they attempt creative workarounds that exceed their granted capabilities. The result is unpredictable recovery behavior that operators cannot control or anticipate.

The three-tier halt system (supervised, immediate, emergency) handles catastrophic failures, but there is no structured guidance between "everything is fine" and "halt the agent." Fallback policies fill that gap: operator-defined recovery playbooks attached to missions that tell agents what to do when specific failure patterns occur.


## Design

A `fallback` block in mission YAML defines ordered recovery strategies for specific failure categories. When the body runtime detects a failure matching a trigger condition, it injects the relevant fallback policy into the conversation as a system-role message, guiding recovery without removing agent autonomy.

Fallback policies are operator instructions (ASK tenet 17 — instructions only from verified principals). The agent follows operator-defined recovery chains rather than improvising its own. Policies cannot grant new capabilities; they can only suggest tools and approaches already within the agent's existing grants.


## Mission Schema Addition

The `fallback` block is a new top-level field in mission YAML, alongside `instructions`, `triggers`, `requires`, and `health`:

```yaml
fallback:
  policies:
    - trigger: tool_error
      tool: jira_get_issue           # specific tool, or "*" for any tool
      strategy:
        - action: retry
          max_attempts: 2
          backoff: exponential        # exponential | fixed | none
          delay_seconds: 5
        - action: alternative_tool
          tool: jira_search           # try a different tool/approach
          hint: "Search for the issue by key instead of direct lookup"
        - action: escalate
          severity: warning
          message: "Jira API unreachable after retries — ticket {task_id} cannot be triaged"

    - trigger: capability_unavailable
      capability: jira
      strategy:
        - action: degrade
          hint: "Proceed with available information only — note in output that Jira data is unavailable"
        - action: escalate
          severity: error
          message: "Jira capability unavailable — mission cannot execute fully"

    - trigger: budget_warning
      threshold: 80                   # % of task budget consumed
      strategy:
        - action: simplify
          hint: "Reduce scope — focus on severity assessment only, skip detailed analysis"
        - action: complete_partial
          hint: "Complete with partial results, noting what was skipped"

    - trigger: consecutive_errors
      count: 3
      strategy:
        - action: pause_and_assess
          hint: "Stop current approach. Review what's failing and why before trying again."
        - action: escalate
          severity: warning
          message: "Multiple consecutive errors during {mission_name}"

  default_policy:                     # fallback for triggers not explicitly covered
    strategy:
      - action: retry
        max_attempts: 1
      - action: escalate
        severity: warning
```

### Field Details

- **`fallback`** — Top-level block. Optional. Missions without a fallback block use the platform default (retry once, then escalate).
- **`fallback.policies`** — Ordered list of trigger-strategy pairs. When multiple policies match the same failure, the most specific match wins (exact tool name beats `*`, explicit trigger beats `default_policy`).
- **`fallback.policies[].trigger`** — The failure condition that activates this policy. One of the trigger types defined below.
- **`fallback.policies[].strategy`** — Ordered list of recovery actions. The runtime walks the chain top to bottom. Each action is attempted before moving to the next.
- **`fallback.default_policy`** — Catch-all policy for failure conditions not matched by any explicit policy. Optional. If omitted, unmatched failures use the platform default: retry once, then escalate with severity `warning`.


## Trigger Types

| Trigger | Fires When | Parameters |
|---|---|---|
| `tool_error` | A specific tool returns an error | `tool`: tool name or `*` for any tool |
| `capability_unavailable` | A required capability is not accessible | `capability`: capability name |
| `budget_warning` | Task budget consumption exceeds threshold | `threshold`: percentage (0-100) |
| `consecutive_errors` | N consecutive tool calls fail | `count`: number of consecutive failures |
| `timeout` | Tool call or LLM response exceeds time limit | `timeout_seconds`: seconds |
| `no_progress` | Agent has not emitted `progress_update` in N minutes | `minutes`: number of minutes |

### Trigger Matching

Triggers are evaluated after every tool call outcome. The runtime checks triggers in this priority order:

1. **Exact match** — `tool_error` with a specific `tool` name matching the failed tool
2. **Wildcard match** — `tool_error` with `tool: "*"` matching any failed tool
3. **Category match** — `capability_unavailable`, `budget_warning`, `consecutive_errors`, `timeout`, `no_progress`
4. **Default policy** — `default_policy` if no explicit trigger matched

Only one policy fires per failure event. The first match in priority order wins.


## Action Types

| Action | Behavior |
|---|---|
| `retry` | Retry the failed operation. Supports `max_attempts` and `backoff` (`exponential`, `fixed`, `none`). See Retry Timing below. |
| `alternative_tool` | Suggest a different tool via the `tool` field with a `hint` explaining the alternative approach. The suggested tool must already be in the agent's granted capabilities. |
| `degrade` | Continue with reduced functionality. The `hint` tells the agent what to skip and what to note in output. |
| `simplify` | Reduce task scope to fit remaining resources. The `hint` defines what to prioritize and what to drop. |
| `complete_partial` | Complete the task with partial results. The `hint` tells the agent what to include and what to mark as incomplete. |
| `pause_and_assess` | Inject a prompt asking the agent to stop, analyze the failure pattern, and reason about root cause before continuing. |
| `escalate` | Emit an escalation signal to the operator. The `severity` field (`warning` or `error`) and `message` field define the signal content. This action is terminal — the runtime emits it automatically when reached. |

### Action Chain Semantics

Actions in a strategy list are sequential. The runtime advances to the next action only when the current action fails or is exhausted:

- **`retry`** advances when `max_attempts` are exhausted without success.
- **`alternative_tool`** advances when the suggested tool also fails.
- **`degrade`**, **`simplify`**, **`complete_partial`**, and **`pause_and_assess`** are guidance injections. The runtime advances if the agent's next tool call also fails.
- **`escalate`** is always terminal. The runtime emits the signal without waiting for the agent. Place it last in any chain.

### Retry Timing

The `retry` action supports a `backoff` field but does NOT use synchronous sleeps. The runtime does not block the conversation loop. Instead, retry delay is implemented as a hint to the agent:

- `backoff: none` — the runtime immediately injects "Retry now" guidance. The agent retries on its next turn.
- `backoff: fixed` — the runtime injects "Wait approximately {delay_seconds}s before retrying" as part of the fallback guidance. The LLM's natural response time provides implicit delay. The runtime does not enforce the exact timing.
- `backoff: exponential` — same as `fixed` but the suggested wait doubles with each attempt (base: `delay_seconds`). Again, advisory — the runtime does not sleep.

If precise delay enforcement is needed, the enforcer's rate limiter is the correct mechanism — not the fallback policy. Fallback policies guide agent behavior; they do not control timing at the infrastructure level.


## Runtime Behavior

### Failure Detection

The body runtime tracks tool call outcomes after every invocation:

1. Was the tool call successful or did it return an error?
2. Which tool was called?
3. How many consecutive errors have occurred?
4. What percentage of the task budget has been consumed (from enforcer headers)?
5. How long since the last `progress_update` signal?

### Policy Injection

When a failure matches a trigger condition, the runtime looks up the matching policy and injects a system-role message into the conversation:

```
## Fallback Policy Activated: tool_error (jira_get_issue)

Your call to jira_get_issue failed. Your mission's fallback policy defines this recovery chain:

1. RETRY (up to 2 more attempts with exponential backoff)
   - If retry succeeds, continue normally
2. If retries exhausted → USE ALTERNATIVE: jira_search
   - Hint: "Search for the issue by key instead of direct lookup"
3. If alternative fails → ESCALATE (warning)
   - The platform will notify the operator: "Jira API unreachable after retries"

Follow this chain in order. Do not skip steps.
```

The injected message presents the full remaining chain so the agent understands what comes next. The runtime tracks progress through the chain and updates the injected guidance if the agent advances to a later step.

### Chain Tracking

The runtime does not rely on the agent to self-report which step it is on. It tracks chain progress by observing tool call outcomes:

- After injecting a `retry` action, the runtime counts subsequent calls to the same tool. If the tool succeeds, the fallback is cleared. If attempts are exhausted, the runtime advances and injects the next action.
- After injecting an `alternative_tool` action, the runtime watches for a call to the suggested tool. Success clears the fallback. Failure advances the chain.
- After injecting `degrade`, `simplify`, `complete_partial`, or `pause_and_assess`, the runtime watches the next tool call. If it also fails, the chain advances. If it succeeds, the fallback is cleared.

### Automatic Terminal Actions

When the chain reaches an `escalate` action, the runtime emits the escalation signal directly. The agent does not need to call `escalate` — the runtime handles it. This ensures escalation always fires even if the agent ignores the guidance.


## Fallback State Tracking

The runtime maintains a state machine per active fallback. Only one fallback can be active per trigger type at a time. A new failure matching the same trigger resets the state.

```json
{
  "trigger": "tool_error",
  "tool": "jira_get_issue",
  "current_step": 1,
  "attempts": [
    {"step": 0, "action": "retry", "attempt": 1, "result": "failed"},
    {"step": 0, "action": "retry", "attempt": 2, "result": "failed"}
  ],
  "started_at": "2026-03-27T14:30:00Z",
  "mission_id": "8a3f2b1c-4d5e-6f7a-8b9c-0d1e2f3a4b5c"
}
```

Fallback state is ephemeral — it lives in memory for the duration of the task. It is not persisted to disk. If the agent is restarted mid-task, active fallback chains are lost. This is acceptable because restart clears the conversation context that the fallback was guiding.


## Signal Extension

Fallback lifecycle events are emitted as signals through the standard signal protocol (body runtime to comms to gateway WebSocket hub).

### fallback_activated

Emitted when the runtime activates a fallback policy:

```json
{
  "signal_type": "fallback_activated",
  "data": {
    "task_id": "task-a1b2c3d4",
    "trigger": "tool_error",
    "tool": "jira_get_issue",
    "policy_steps": 3,
    "mission_id": "8a3f2b1c-4d5e-6f7a-8b9c-0d1e2f3a4b5c"
  }
}
```

### fallback_exhausted

Emitted when the agent exhausts all steps in a fallback chain:

```json
{
  "signal_type": "fallback_exhausted",
  "data": {
    "task_id": "task-a1b2c3d4",
    "trigger": "tool_error",
    "tool": "jira_get_issue",
    "final_action": "escalate",
    "mission_id": "8a3f2b1c-4d5e-6f7a-8b9c-0d1e2f3a4b5c"
  }
}
```

These signals are informational. They feed into operator notifications and audit logs. The `fallback_exhausted` signal is distinct from the escalation signal that the terminal `escalate` action emits — both fire, serving different purposes (operational awareness vs. operator alert).


## Interaction with Trajectory Monitoring

Trajectory monitoring and fallback policies are complementary systems that address different failure modes:

- **Trajectory monitoring** detects pathological patterns after the fact: looping, stalling, repeated identical tool calls.
- **Fallback policies** provide structured recovery guidance before the agent improvises.

The two systems interact at one point: when trajectory monitoring fires a `tool_repetition` anomaly, it checks whether a fallback policy exists for the repeated tool. If a matching policy exists, the runtime injects the fallback chain instead of firing the trajectory anomaly alert. If no policy exists, the trajectory anomaly alert fires as usual.

This means trajectory monitoring serves as an implicit trigger for fallback policies. An agent stuck in a retry loop on `jira_get_issue` will be caught by trajectory monitoring's `tool_repetition` detector, which will find the `tool_error` fallback policy for that tool and inject it. The agent gets structured guidance instead of a generic anomaly warning.

The same integration applies to `progress_stall` (trajectory) and `no_progress` (fallback). Both fire when the agent hasn't emitted progress signals. The distinction: trajectory monitoring's `progress_stall` is enforcer-side detection with operator alerting; fallback's `no_progress` is runtime-side with agent guidance injection. When both exist, trajectory `progress_stall` checks for a matching `no_progress` fallback policy before firing its own alert — same pattern as the `tool_repetition` integration. If a fallback policy exists, the runtime injects guidance and trajectory suppresses the alert. If no policy exists, the trajectory alert fires to the operator.


## Interaction with Budget Model

The `budget_warning` trigger integrates with the enforcer's existing budget tracking. The enforcer already tracks task budget consumption and passes it as the `X-Agency-Budget-Pct` header on responses. The body runtime checks this value after each tool call.

When the percentage crosses the configured `threshold`, the runtime activates the matching `budget_warning` policy. The `simplify` and `complete_partial` actions guide the agent to reduce scope and finish within remaining budget rather than hitting the hard budget stop.

Budget fallback policies do not override the enforcer's hard budget limit. If the agent exhausts its budget, the enforcer cuts it off regardless of fallback state.


## Meeseeks

Meeseeks agents are ephemeral and single-purpose. They inherit a simplified default fallback policy that cannot be customized:

```yaml
fallback:
  default_policy:
    strategy:
      - action: retry
        max_attempts: 1
      - action: escalate
        severity: warning
        message: "Meeseeks {agent_name} failed during task execution"
```

Meeseeks escalation signals route to the parent agent, not the operator. The parent decides whether to spawn a new meeseeks, try a different approach, or escalate further.

Custom `fallback` blocks in meeseeks spawn requests are ignored. Meeseeks are too short-lived for complex recovery chains — if the simple retry-then-escalate pattern is insufficient, the parent should handle recovery.


## REST API

No new endpoints. Fallback configuration is part of mission YAML and flows through the existing mission CRUD endpoints:

| Endpoint | Method | Fallback Behavior |
|---|---|---|
| `POST /api/v1/missions` | Create | Validates `fallback` block schema |
| `PUT /api/v1/missions/{name}` | Update | Validates `fallback` block, increments version, hot-reloads via enforcer SIGHUP |
| `GET /api/v1/missions/{name}` | Show | Returns full mission YAML including `fallback` |
| `DELETE /api/v1/missions/{name}` | Delete | Removes mission including fallback policies |

### Validation Rules

The gateway validates fallback policies on mission create and update:

- Every `trigger` must be a known trigger type.
- Every `action` must be a known action type.
- `alternative_tool` must reference a tool that exists in the agent's granted capabilities. Validated at **trigger time** (when the fallback fires), not at mission creation or assignment. Tool grants can change after assignment via capability hot-reload — a tool valid at assignment might be revoked later. If the alternative tool is unavailable at trigger time, the runtime logs a warning (`fallback_alternative_unavailable` signal), skips the action, and advances to the next step in the chain.
- `escalate` must include `severity` (one of `warning`, `error`) and `message`.
- `retry` must include `max_attempts` (positive integer). `backoff` defaults to `none`. `delay_seconds` defaults to `0`.
- `budget_warning.threshold` must be between 1 and 99.
- `consecutive_errors.count` must be at least 2.
- `timeout.timeout_seconds` must be positive.
- `no_progress.minutes` must be at least 1.
- Strategy lists must not be empty.
- `escalate` may only appear as the last action in a strategy chain.


## CLI

No new commands. Fallback configuration is visible through existing mission commands:

| Command | Behavior |
|---|---|
| `agency mission create -f mission.yaml` | Creates mission with fallback block |
| `agency mission show <name>` | Displays mission including fallback policies |
| `agency mission update <name> -f mission.yaml` | Updates fallback policies, triggers hot-reload |


## Task Tier Interaction

Fallback policies activate at `standard` and `full` tiers. At `minimal` tier (ad-hoc DMs, short messages), fallback is skipped — the agent improvises recovery for lightweight interactions, which is acceptable.

Fallback policies are free (no LLM calls — they inject prompt guidance only), so the tier gate is about prompt relevance, not cost. A "hi" message doesn't need a structured recovery chain for tool failures.

See the Task Tier and Cost Model spec for the full tier classification logic.


## ASK Compliance

- **Tenet 3 (Mediation is complete):** Escalation signals from fallback chains flow through the standard signal protocol — body runtime to comms bridge to gateway. No unmediated paths.
- **Tenet 4 (Least privilege):** Fallback policies cannot grant new capabilities. `alternative_tool` only suggests tools already in the agent's grants. The validation layer rejects references to ungranted tools.
- **Tenet 17 (Instructions from verified principals):** Fallback policies are operator-defined instructions stored in mission YAML. The agent follows operator recovery guidance rather than improvising its own strategies.
- **Tenet 2 (Every action leaves a trace):** Fallback activation and exhaustion emit signals logged through the standard audit pipeline. The `fallback_activated` and `fallback_exhausted` signals provide full traceability.
- **Tenet 11 (Delegation cannot exceed delegator scope):** Meeseeks inherit a fixed default policy. Parent agents cannot attach custom fallback policies that exceed their own authority.
