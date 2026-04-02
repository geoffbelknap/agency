Missions define *instructions* — what the agent should do — but not *success criteria* — how to know the work is done well. When an agent calls `complete_task(summary=...)`, the platform accepts the completion unconditionally. The agent decides when it's done and whether the output is good enough. This is the "agents grade their own homework" problem.

This spec adds two things:

1. A `success_criteria` field in mission YAML that declares measurable conditions for task completion
2. An optional platform-side evaluation that runs after `task_complete` — external to the agent, executed by the gateway


## Mission Schema Addition

The `success_criteria` block is added at the top level of the mission YAML, alongside `instructions`, `triggers`, `requires`, and `health`:

```yaml
id: 8a3f2b1c-4d5e-6f7a-8b9c-0d1e2f3a4b5c
name: ticket-triage
description: Review and respond to incident response tickets
version: 1
status: active
assigned_to: henrybot900
assigned_type: agent

instructions: |
  You are responsible for triaging incoming incident response tickets.
  For each new ticket:
  1. Assess severity (P1-P4) based on impact and urgency
  2. Assign appropriate responder tags
  3. Post initial assessment to #incidents
  4. Escalate P1/P2 to operator immediately

success_criteria:
  # Checklist items — evaluated by the platform after task_complete
  checklist:
    - id: severity_assessed
      description: "Severity level (P1-P4) is assigned with justification"
      required: true
    - id: responder_tagged
      description: "At least one responder tag is assigned"
      required: true
    - id: assessment_posted
      description: "Initial assessment posted to #incidents channel"
      required: true
    - id: escalation_if_needed
      description: "P1/P2 tickets escalated to operator"
      required: false  # only applies to high-severity

  # Evaluation mode
  evaluation:
    enabled: true          # default: false
    mode: llm              # llm | checklist_only
    model: default         # which model to use for LLM eval (default = agent's model)
    on_failure: flag       # flag | retry | block
    max_retries: 1         # only used when on_failure=retry

triggers:
  - source: connector
    connector: jira-ops
    event_type: issue_created

requires:
  capabilities: [jira]
  channels: [incidents, escalations]

health:
  indicators:
    - "If tickets are in the queue but none have been triaged in 2 hours"
    - "If a P1/P2 ticket sits unacknowledged for more than 15 minutes"
  business_hours: "09:00-17:00 America/Los_Angeles"
```

### Field Details

- **`success_criteria`** — Optional top-level block. If omitted, `complete_task()` behaves as today — unconditional acceptance.
- **`success_criteria.checklist`** — List of criteria items. Each has an `id` (unique within the mission), `description` (human-readable condition), and `required` flag.
- **`success_criteria.checklist[].id`** — Unique string identifier. Used in evaluation results and audit events. Must be a valid slug (`[a-z0-9_-]+`).
- **`success_criteria.checklist[].description`** — Natural language description of the criterion. Used by the evaluator (checklist or LLM) and displayed in health output.
- **`success_criteria.checklist[].required`** — If `true`, failing this criterion triggers the `on_failure` action. If `false`, the criterion is evaluated but failure is informational only.
- **`success_criteria.evaluation`** — Controls platform-side evaluation behavior. If absent, criteria are injected into the agent's prompt as guidance but not evaluated by the platform.
- **`success_criteria.evaluation.enabled`** — Master switch. Default `false`. When `false`, criteria exist in the mission YAML (visible in `mission show`, injected into prompt) but no post-completion evaluation runs.
- **`success_criteria.evaluation.mode`** — `llm` or `checklist_only`. See Evaluation Modes below.
- **`success_criteria.evaluation.model`** — Model identifier for LLM evaluation. `default` uses the agent's configured model. Only relevant when `mode: llm`.
- **`success_criteria.evaluation.on_failure`** — Action when required criteria fail. One of `flag`, `retry`, `block`. Default `flag`.
- **`success_criteria.evaluation.max_retries`** — Maximum retry attempts when `on_failure: retry`. Default `1`. After exhausting retries, the gateway falls back to `flag` behavior.


## Evaluation Modes

### `checklist_only`

The gateway checks whether the agent's `complete_task(summary=...)` text addresses each required checklist item using keyword overlap — no LLM call, no embedding model. The gateway tokenizes both the summary and each checklist item description into lowercased word sets, removes stop words, and computes Jaccard similarity. A criterion passes if the similarity score exceeds a configurable threshold (default: 0.3).

Characteristics: fast (sub-millisecond), zero cost, coarse. Good for missions where criteria are concrete and keyword-matchable ("severity assigned", "posted to #incidents"). Poor for nuanced criteria that require judgment — use `llm` mode for those.

The threshold is tuned to minimize false negatives — better to pass a borderline case than to block a valid completion. Operators can adjust the threshold per-mission via `success_criteria.evaluation.checklist_threshold` (float, 0.0-1.0).

This mode intentionally does not use embeddings. Adding an embedding model dependency would increase infrastructure requirements for what is designed to be a lightweight check. If accuracy matters, use `llm` mode.

### `llm`

The gateway sends the task summary and checklist to a one-shot LLM call — separate from the agent's conversation session — asking the evaluator to assess each criterion. The evaluator has no tools, no memory, no side effects. It receives the checklist and the summary, returns structured JSON.

Characteristics: accurate, costs one LLM call per evaluation, latency ~2-5 seconds. Good for missions where criteria require judgment ("severity assigned *with justification*", "assessment is technically accurate").

The LLM evaluation call is a standalone request — it does not share the agent's conversation context, token budget, or session state.

**Routing:** The evaluation call does NOT route through any agent's enforcer. The enforcer is a per-agent component inside the agent boundary; routing evaluation through it would blur the "external to agent" separation. Instead, the gateway sends the evaluation request directly to the egress proxy using a platform-level credential. The egress proxy handles credential swap as usual. The gateway logs the request in its own audit trail with `source: platform_evaluation`, `mission_id`, and `task_id`. This establishes a platform-level LLM path that is fully mediated (tenet 3) without passing through the agent boundary (tenet 1).


## On-Failure Actions

### `flag` (default)

Accept the completion. The `task_complete` signal is delivered normally. The evaluation result is attached to the signal with `evaluation_result: partial` and a list of which criteria failed. The operator sees this in mission health, `mission show`, and audit logs.

Use `flag` when the agent's output is time-sensitive and blocking would be worse than accepting imperfect work. The operator reviews flagged completions asynchronously.

### `retry`

Reject the completion. The `task_complete` signal is not emitted to external subscribers. The gateway writes evaluation feedback to `session-context.json` with `action: evaluation_feedback`, including the failed criteria and evaluator reasoning. The body runtime picks up the feedback and re-enters the task loop — the agent sees which criteria weren't met and tries again.

Retries are limited by `max_retries`. After exhausting retries, the gateway falls back to `flag` — accept the completion and tag it as partial. This prevents infinite loops.

Each retry consumes budget from the agent's task allocation. If the agent exhausts its task budget during a retry, budget limits take precedence — the task stops regardless of evaluation outcome.

### `block`

Reject the completion entirely. The task stays in-progress. The gateway emits a `task_evaluation_failed` platform event (routed to operator notifications). The agent receives a message explaining which criteria weren't met but is not automatically retried — the operator must intervene.

Use `block` for high-stakes missions where incomplete work is worse than no work (compliance reporting, incident escalation).

**Blocked agent state:** When `block` fires, the task remains in-progress but the agent is not automatically retried. The agent returns to its idle state — it can still respond to @mentions and operator DMs, but the blocked task is flagged as `evaluation_blocked` in the agent's status. The agent does not re-enter the task loop for the blocked task unless the operator explicitly resumes it via `agency mission resume` or the gateway receives a new trigger event for the mission. The blocked task's summary is preserved in the agent's session context so a retry (manual or event-triggered) can reference what was already attempted. This avoids a limbo state where the agent is neither idle nor actively working.


## Evaluation Flow

When `success_criteria.evaluation.enabled` is `true`:

```
Agent calls complete_task(summary=...)
        │
        ▼
Body runtime emits task_complete signal (as normal)
        │
        ▼
Gateway receives signal, reads mission success_criteria
        │
        ▼
Gateway calls evaluateTaskCompletion(mission, taskSummary)
        │
        ├── mode=checklist_only → keyword/semantic match
        │
        └── mode=llm → one-shot LLM call via enforcer
                │
                ▼
        EvaluationResult returned
                │
                ├── all required criteria passed → accept, attach result to signal
                │
                └── required criteria failed
                        │
                        ├── on_failure=flag   → accept, tag as partial, log
                        │
                        ├── on_failure=retry  → reject, write feedback, decrement retries
                        │
                        └── on_failure=block  → reject, notify operator, task stays in-progress
```

When `success_criteria` is present but `evaluation.enabled` is `false` (or `evaluation` is absent), the checklist is injected into the agent's system prompt as guidance but no post-completion evaluation runs. The agent sees the criteria and can self-evaluate, but the platform does not enforce them.


## Gateway Implementation

### `evaluateTaskCompletion`

New function in the mission handler:

```go
func evaluateTaskCompletion(mission *Mission, taskSummary string) (*EvaluationResult, error)
```

Returns:

```go
type EvaluationResult struct {
    Passed          bool               `json:"passed"`
    CriteriaResults []CriterionResult  `json:"criteria_results"`
    EvaluationMode  string             `json:"evaluation_mode"`  // "llm" or "checklist_only"
    ModelUsed       string             `json:"model_used"`       // empty for checklist_only
    TokensUsed      int                `json:"tokens_used"`      // 0 for checklist_only
    EvaluatedAt     time.Time          `json:"evaluated_at"`
}

type CriterionResult struct {
    ID        string `json:"id"`
    Passed    bool   `json:"passed"`
    Required  bool   `json:"required"`
    Reasoning string `json:"reasoning"`
}
```

`Passed` is `true` when all required criteria pass. Optional criteria can fail without affecting the overall result.

### Retry Feedback

When `on_failure=retry` and evaluation fails, the gateway writes to `session-context.json`:

```json
{
  "action": "evaluation_feedback",
  "mission_id": "8a3f2b1c-4d5e-6f7a-8b9c-0d1e2f3a4b5c",
  "attempt": 1,
  "max_retries": 1,
  "failed_criteria": [
    {
      "id": "severity_assessed",
      "description": "Severity level (P1-P4) is assigned with justification",
      "reasoning": "Summary mentions P2 but provides no justification for the severity level"
    }
  ]
}
```

The body runtime reads this on the next loop iteration, injects the feedback into the conversation, and the agent re-attempts the task.

**Timing requirement:** Retry only works when the body runtime's task loop is still active. The gateway must write the evaluation feedback to `session-context.json` before the body runtime exits its task loop after emitting `task_complete`. To ensure this, the body runtime waits up to 10 seconds after emitting `task_complete` for an evaluation response before fully closing the task. If no evaluation feedback arrives within the window (evaluation disabled, or `on_failure: flag`), the task closes normally. This wait is only active when `success_criteria.evaluation.on_failure` is `retry` or `block`.

### LLM Evaluation Call

For `mode: llm`, the gateway sends a one-shot request through the enforcer. The evaluation call is a regular LLM request — it routes through the enforcer's LLM proxy path, subject to rate limits and audit logging. The evaluator has no tools and no conversation history.


## Evaluation Prompt Template

The LLM evaluation uses a fixed prompt template. The gateway substitutes the checklist and task summary:

```
You are an evaluation assistant. Your job is to assess whether a task
completion summary satisfies a set of success criteria.

## Success Criteria

{{range .Checklist}}
- [{{.ID}}] {{.Description}} ({{if .Required}}required{{else}}optional{{end}})
{{end}}

## Task Completion Summary

{{.TaskSummary}}

## Instructions

For each criterion, determine whether the task summary demonstrates that
the criterion has been satisfied. Assess based on evidence in the summary,
not assumptions about what the agent might have done.

Respond with JSON only. No explanation outside the JSON.

{
  "criteria_results": [
    {
      "id": "<criterion_id>",
      "passed": true|false,
      "reasoning": "<one sentence explaining your assessment>"
    }
  ]
}
```

The gateway parses the JSON response and constructs the `EvaluationResult`. If the LLM response is malformed, the evaluation is treated as inconclusive — the completion is accepted with `evaluation_result: error` in the signal metadata.


## Signal Extension

The `task_complete` signal gains an `evaluation` field when success criteria are configured:

```json
{
  "signal_type": "task_complete",
  "data": {
    "task_id": "tsk-7f8e9d0c",
    "result": "Triaged INC-1234 as P2, tagged @oncall-infra, posted assessment to #incidents",
    "evaluation": {
      "passed": true,
      "mode": "llm",
      "criteria_results": [
        {"id": "severity_assessed", "passed": true, "reasoning": "P2 assigned with latency data as justification"},
        {"id": "responder_tagged", "passed": true, "reasoning": "Tagged @oncall-infra"},
        {"id": "assessment_posted", "passed": true, "reasoning": "Summary states assessment posted to #incidents"},
        {"id": "escalation_if_needed", "passed": true, "reasoning": "P2 — not applicable, criterion is optional"}
      ]
    }
  }
}
```

When evaluation is not configured, the `evaluation` field is absent. Consumers must tolerate its absence.

### `task_evaluation_failed` Event

New platform event emitted when a required criterion fails and `on_failure` is `flag` or `block`:

```json
{
  "id": "evt-b2c3d4e5",
  "source_type": "platform",
  "source_name": "gateway",
  "event_type": "task_evaluation_failed",
  "timestamp": "2026-03-27T14:30:00Z",
  "data": {
    "agent": "henrybot900",
    "mission_id": "8a3f2b1c-4d5e-6f7a-8b9c-0d1e2f3a4b5c",
    "mission_name": "ticket-triage",
    "task_id": "tsk-7f8e9d0c",
    "evaluation_mode": "llm",
    "on_failure": "flag",
    "failed_criteria": [
      {"id": "severity_assessed", "reasoning": "No justification provided for P2 rating"}
    ]
  },
  "metadata": {
    "mission_id": "8a3f2b1c-4d5e-6f7a-8b9c-0d1e2f3a4b5c"
  }
}
```

This event routes through the event bus like any other platform event. Operator notification subscriptions can match on `event_type: task_evaluation_failed`.


## REST API Additions

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/v1/missions/{name}/evaluations` | List recent evaluation results for a mission |

### `GET /api/v1/missions/{name}/evaluations`

Returns the last N evaluation results (default 20, max 100, controlled by `?limit=` query param):

```json
{
  "mission": "ticket-triage",
  "evaluations": [
    {
      "task_id": "tsk-7f8e9d0c",
      "passed": true,
      "evaluation_mode": "llm",
      "criteria_results": [...],
      "evaluated_at": "2026-03-27T14:30:00Z"
    }
  ],
  "summary": {
    "total": 47,
    "passed": 42,
    "failed": 5,
    "pass_rate": 0.893
  }
}
```

Evaluation results are also included in the existing `GET /api/v1/missions/{name}` response under a `recent_evaluations` field — the last 5 results and the aggregate pass rate. This keeps `mission show` informative without requiring a separate API call.


## CLI Addition

`agency mission health <name>` already displays health indicators. When success criteria are configured, it extends to show evaluation results:

```
$ agency mission health ticket-triage

Health Indicators:
  ✓  No tickets unprocessed > 2 hours
  ✓  No P1/P2 unacknowledged > 15 minutes

Success Criteria Evaluation:
  Mode: llm
  On failure: flag
  Pass rate: 42/47 (89.4%)

  Recent evaluations:
    tsk-7f8e9d0c  2026-03-27 14:30  ✓ passed
    tsk-6e7d8c9b  2026-03-27 13:15  ✗ failed  [severity_assessed]
    tsk-5d6c7b8a  2026-03-27 12:00  ✓ passed
```

No new CLI commands are needed. The evaluation data surfaces through existing `mission health` and `mission show` commands.


## System Prompt Injection

When `success_criteria.checklist` is present, the criteria are injected into the agent's system prompt alongside the mission instructions — regardless of whether platform-side evaluation is enabled:

```
## Current Mission: ticket-triage (id: 8a3f2b1c)

You are responsible for triaging incoming incident response tickets...

### Success Criteria
When completing a task for this mission, ensure your work addresses:
- [required] Severity level (P1-P4) is assigned with justification
- [required] At least one responder tag is assigned
- [required] Initial assessment posted to #incidents channel
- [optional] P1/P2 tickets escalated to operator

Include evidence of meeting these criteria in your complete_task summary.
```

This ensures the agent knows what "done well" looks like, even when evaluation is disabled. When evaluation is enabled, the agent also knows its work will be checked — the prompt includes: "Your task completion will be evaluated against these criteria by the platform."


## ASK Tenet Compliance

**Tenet 1 — Constraints are external and inviolable.** Platform-side evaluation runs in the gateway, outside the agent boundary. The agent cannot influence, bypass, or observe the evaluation process. The evaluator is not a tool the agent can call — it is a gateway-internal function triggered by the `task_complete` signal.

**Tenet 2 — Every action leaves a trace.** Evaluation results are logged by the gateway, not the agent. The agent has no write access to evaluation logs. All evaluation outcomes (pass, fail, error) are written to the audit log with `mission_id`, `task_id`, and full criteria results.

**Tenet 3 — Mediation is complete.** LLM evaluation calls route through the egress proxy via a platform-level credential. The gateway logs each evaluation request in its audit trail. No direct gateway-to-model path — the egress proxy mediates. The evaluation request is subject to audit logging and rate limits at the egress layer.

**Tenet 4 — Least privilege.** The evaluation model receives only the task summary and checklist — no tools, no conversation history, no agent identity, no side effects. It cannot take actions, only assess.

**Tenet 12 — Synthesis cannot exceed individual authorization.** The evaluator's assessment does not grant or extend any capability. It only determines whether existing output meets predefined criteria. A passing evaluation does not authorize new actions.


## Interaction with Reflection Loop

The Reflection Loop spec (separate) adds optional self-evaluation inside the agent — the agent reviews its own work before calling `complete_task()`. This spec adds platform-side evaluation outside the agent — the gateway reviews the work after `complete_task()`.

The two are complementary:

- **Reflection** catches quality issues *before* the agent declares done. It runs inside the agent's conversation, uses the agent's context and tools, and can iterate.
- **Success evaluation** validates *after* the agent declares done. It runs outside the agent, has no context beyond the summary, and renders a verdict.

Both can be enabled independently. When both are active, the sequence is:

1. Agent finishes work
2. Reflection loop runs (agent-internal) — agent reviews, possibly iterates
3. Agent calls `complete_task(summary=...)`
4. Platform evaluation runs (gateway-external) — evaluator assesses summary against criteria
5. Result: accept, flag, retry, or block

If evaluation triggers a retry, the agent re-enters its task loop. If reflection is enabled, it will run again on the next attempt. This is correct — the agent should self-review each attempt.


## Task Tier Interaction

Evaluation behavior varies by task tier:

- **minimal** — No evaluation runs, regardless of mission config. The task is too lightweight to justify even keyword matching.
- **standard** — `checklist_only` evaluation runs if `success_criteria.evaluation` is configured. LLM evaluation is downgraded to `checklist_only` at this tier.
- **full** — Mission config is honored in full. LLM evaluation runs if `mode: llm` is set.

When `cost_mode: frugal`, evaluation mode is forced to `checklist_only` (free). When `cost_mode: thorough`, LLM evaluation is the default if evaluation is enabled.

Cost attribution: evaluation LLM calls are tagged with `X-Agency-Cost-Source: evaluation` and charged to the platform budget, not the agent's task budget.

See the Task Tier and Cost Model spec for the full tier classification logic.


## When Not to Use

Skip success criteria evaluation for:

- **Meeseeks** — Ephemeral, single-purpose, already budget-constrained. The spawning agent validates meeseeks output when it receives the result. Adding platform evaluation to a sub-second task adds latency without value.
- **Speed-critical missions** — When time-to-completion matters more than output quality (live incident response where "good enough now" beats "perfect later"). Use `flag` mode if you still want visibility.
- **Subjective missions** — Exploratory research, creative writing, open-ended analysis. If you can't write concrete checklist items, evaluation will produce noisy results.
- **Low-stakes missions** — Internal housekeeping, routine notifications. The overhead of evaluation (especially LLM mode) isn't justified.

When in doubt, start with `evaluation.enabled: false` — the criteria still appear in the agent's prompt as guidance. Enable evaluation after you've seen enough `complete_task` summaries to know what good looks like.


## Storage

Evaluation results are stored in `~/.agency/missions/.evaluations/{mission_id}.jsonl`. Each line is a complete evaluation result:

```json
{"task_id": "tsk-7f8e9d0c", "passed": true, "mode": "llm", "model_used": "claude-sonnet-4-20250514", "tokens_used": 847, "criteria_results": [...], "evaluated_at": "2026-03-27T14:30:00Z"}
```

The gateway reads this file to serve the `/evaluations` API endpoint and compute aggregate pass rates. The file is append-only. Rotation is handled by the same log management that handles other `.jsonl` audit files.


## Scope Boundaries

This spec covers success criteria definition and platform-side evaluation only. The following are separate:

- **Reflection Loop** — Agent-internal self-evaluation before `complete_task()`. Separate spec.
- **Mission instructions** — How to write good mission instructions. Covered in the missions spec.
- **Budget enforcement** — Task budget limits are orthogonal to evaluation. Budget exhaustion stops the task regardless of evaluation outcome.
- **Multi-step evaluation** — Evaluating intermediate steps (not just final output) is deferred. This spec evaluates only the `complete_task` summary.
