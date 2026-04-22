# PACT Wave 2 #4b — Body Runtime Rewire To Pre-Commit Evaluator

## Reference

- Spec: `docs/specs/pact-governed-agent-execution.md` → Wave 2 #4 / #4b
- Wave: Wave 2 — Harness Capabilities, item #4b (runtime integration)
- Builds on: Wave 2 #4 `evaluate_pre_commit` (merged; see its brief at
  `docs/plans/2026-04-22-pact-wave-2-pre-commit-evaluator.md`)
- Related follow-ups: Wave 2 #3b (plan-step execution), Wave 2 #5b (retry
  path consumes `recovery_state.next_action`). Both are separate PRs.

## Objective

Rewire the body runtime commit path to call `evaluate_pre_commit(state)` at
the existing `validate_completion` call site. Map the `PreCommitVerdict` into
the existing `pact_verdict` signal shape additively (new `reasons` field),
and translate non-committable verdicts into either a retry (for
`contract:needs_action`) or a blocked terminal (for every other reason
category — halt / recovery / strategy / ambiguity / approval /
incomplete_state).

This is the first PR in which the Wave 2 #4 evaluator actually *gates*
commits. Wave 2 #4 shipped the evaluator as a standalone primitive with
tests; #4b puts it on the hot path.

## Why

The Wave 2 #4 PR said the evaluator is advisory-only because `body.py`
still calls `validate_completion` directly. That leaves every load-bearing
ASK invariant from prior Wave 2 briefs (clarify blocks commit, ambiguities
block commit, approval required, halt terminates) as *type-level* guarantees
that the runtime does not enforce. This PR flips that — the evaluator's
verdict becomes the commit decision.

Keeping this as its own PR (rather than folding into Wave 2 #4) means the
review can focus on the body.py wiring without also re-reviewing the
evaluator logic.

## Scope (in this PR)

### Replace the `validate_completion` call at the commit site

Find the existing `validate_completion` call in `images/body/body.py` (near
the `_work_contract_retry_sent` flag — there should be a breadcrumb comment
from Wave 2 #4 referencing this PR). Replace the direct call with:

```python
verdict = evaluate_pre_commit(self._execution_state, content=result_text)
```

Where `result_text` is whatever the current flow passes to
`validate_completion` as the candidate answer content. The
`self._execution_state` is the typed state object that Wave 1 #1 already
constructs at task start.

### Map `PreCommitVerdict` to the existing verdict-dict shape

The existing `_emit_pact_verdict(task_id, verdict_dict)` consumes a dict
with fields `task_id`, `kind`, `verdict`, `required_evidence`,
`answer_requirements`, `missing_evidence`, `observed`, `source_urls`,
`tools`, `evidence_entries`. Preserve every field.

Build the dict from `PreCommitVerdict` + `contract_verdict`:

- If `verdict.committable is True`:
  - `verdict` field = `contract_verdict["verdict"]` (`"completed"` or
    `"blocked"`)
  - Fill `required_evidence`, `answer_requirements`, `missing_evidence`,
    `observed`, `source_urls`, `tools`, `evidence_entries` from
    `contract_verdict`
- If `verdict.committable is False` and `"contract:needs_action"` is in
  `verdict.reasons`:
  - `verdict` field = `"needs_action"`
  - Preserve all legacy fields from `contract_verdict` (missing_evidence
    comes from the embedded `contract_verdict`, not from
    `PreCommitVerdict.missing`)
- If `verdict.committable is False` with any other reason:
  - `verdict` field = `"blocked"` (treated as a terminal blocker)
  - `missing_evidence` = `list(verdict.missing)` (e.g., for
    `approval_required:no_approval_decision` this is `["approval_decision"]`)
  - Fill remaining fields from `contract_verdict` if present, else from
    `state.evidence.to_dict()`

Add **one new field** to the verdict dict:

- `reasons: list[str]` = `list(verdict.reasons)`

This is additive — existing consumers ignore unknown fields. Never remove
or rename existing fields in the verdict payload.

### Retry path for `contract:needs_action`

Keep the existing `_work_contract_retry_sent` flag and its one-retry
semantics. Trigger on `contract:needs_action` specifically (not on every
`committable=False`):

```python
if not verdict.committable and "contract:needs_action" in verdict.reasons:
    # existing retry path: append platform message, set _work_contract_retry_sent
    ...
```

All other non-committable reasons do **not** retry — they terminate the
task with a blocked outcome and the reasons recorded.

### Terminal blocked path for non-contract non-committable reasons

When `verdict.committable is False` and the reason is anything other than
`contract:needs_action` (i.e., `halt:*`, `recovery:*`, `strategy:*`,
`ambiguity:*`, `approval_required:*`, `incomplete_state:*`):

- Emit the `pact_verdict` signal with the mapped dict above
- Call `complete_task` (or the equivalent terminal path) with a blocked
  terminal outcome, recording the reasons in the task's final result
- Do **not** append a platform retry message to the conversation

The blocked terminal outcome here is legitimate per Wave 1 #1 semantics —
blocked is a terminal state, not a failure.

### Add `reasons` to the PACT run projection

`/api/v1/agents/{name}/pact/runs/{taskId}` currently projects verdict from
stored audit signals and artifact frontmatter. Add `reasons: list[str]`
to the projection's `verdict` block, populated from the audit signal when
present. Surface an empty list when not present (legacy runs without the
field).

### Audit-report hash

Adding `reasons` to the run projection changes the deterministic content
that feeds the SHA-256 hash. This is a **uniform change across all new
runs**. Existing stored hash values (if any external consumer persisted
them) become invalid, but there are no such external consumers today.

Call this out in the PR body explicitly. Add a brief note to the spec's
"Current Implementation Checkpoint" describing the hash-scope change.

### Do NOT change

- `_work_contract_retry_sent` flag semantics (still fires exactly once,
  still only on contract needs_action reason).
- `_validate_*_answer` contract-specific validators — Layer 7 of the
  evaluator already delegates to them via `validate_completion`.
- `ExecutionState.from_task` and `attach_mission` — already build the
  full typed state Wave 2 #4 evaluator reads.
- Wave 2 #3b scope (plan-step execution). Plans remain advisory in the
  runtime even though the evaluator Layer 6 records advisory reasons for
  missing plan evidence — those reasons surface in the payload but do not
  affect the commit decision.
- Wave 2 #5b scope (recovery state machine rewire). The evaluator reads
  `recovery_state` but `body.py` retry path does not yet write to it
  beyond what Wave 2 #5 established.

### Spec Checkpoint update

Update "### Verdict Signal" and the overall Current Implementation
Checkpoint intro:

- Add `reasons` to the payload field list.
- Describe the new verdict-mapping semantics: committable → completed /
  blocked from contract; non-committable → needs_action (retry once) or
  blocked (terminate with reasons).
- Note the audit-report hash scope now includes `reasons` as part of the
  projection content.

### Tests

Update `images/tests/test_pact_trajectories.py` (or add
`images/tests/test_pact_pre_commit_rewire.py` if the trajectory file would
grow unwieldy) with end-to-end test cases:

1. **Happy path (current_info completed)** — activation produces
   evidence, evaluator returns committable, `pact_verdict` signal has
   `verdict="completed"`, `reasons=["committable"]`, task completes with
   terminal completed.
2. **Happy path (operator_blocked terminal)** — activation describes a
   blocker, evaluator returns committable, `verdict="blocked"`,
   `reasons=["committable"]` (or including
   `plan_advisory:missing:*` advisory lines if plan is present),
   terminal blocked.
3. **Contract needs_action retry** — first turn fails contract
   validation, `verdict="needs_action"`, retry-message appended, second
   turn passes — terminal completed. `_work_contract_retry_sent` fires
   exactly once.
4. **Contract needs_action after retry exhausted** — both turns fail
   contract validation. Second time, task terminates blocked with
   `reasons=["contract:needs_action"]`.
5. **Load-bearing ambiguity blocks commit** — `code_change` activation
   without target files produces
   `objective.ambiguities=["ambiguity:target_files_missing"]`. At
   commit time, evaluator blocks; `verdict="blocked"`,
   `reasons=["ambiguity:target_files_missing"]`, no retry, no platform
   message appended.
6. **Strategy clarify blocks commit** — escalated-risk activation
   (untrusted trust level) produces `strategy.execution_mode="clarify"`
   (or `"escalate"`). Evaluator blocks; `verdict="blocked"`,
   `reasons=["strategy:clarify"]` (or `"strategy:escalate"`).
7. **Approval required without decision** — plan has an approval step;
   evidence has no `approval_decision`. Evaluator blocks;
   `verdict="blocked"`, `reasons=["approval_required:no_approval_decision"]`,
   `missing_evidence=["approval_decision"]`.
8. **Recovery halted** — state has `recovery_state.status="halted"`.
   Evaluator blocks; `verdict="blocked"`, `reasons=["halt:halted"]`, no
   retry.
9. **Advisory plan evidence surfaces in payload** — commit is committable
   but plan evidence is partially missing. `pact_verdict` payload has
   `reasons=["plan_advisory:missing:<label>", "committable"]`, task
   completes successfully, `verdict="completed"`.
10. **PACT run projection carries reasons** — after commit, GET
    `/api/v1/agents/{name}/pact/runs/{taskId}` returns a verdict block
    that includes `reasons: [...]`.
11. **Audit-report hash is stable across reads of the same run** —
    call `/audit-report` twice on the same task, hash must match.

## Non-Scope

- **Wave 2 #3b** (plan-step execution in body.py). Plans remain advisory.
- **Wave 2 #5b** (retry path rewired to `recovery_state.next_action`).
  Current retry logic stays on `_work_contract_retry_sent`.
- **New verdict values** beyond `completed` / `blocked` / `needs_action`.
- **Changes to `validate_completion` or contract-specific validators.**
  The evaluator Layer 7 delegates to them; this PR does not modify them.
- **New activation sources, new contract kinds, new tool observation
  fields.**
- **OpenAPI, web UI, feature registry changes** beyond the PACT run
  projection field addition. OpenAPI spec update for
  `/pact/runs/{taskId}` is in scope; web UI rendering of the new
  `reasons` field is out of scope for this PR.

## Acceptance Criteria

1. `body.py` commit site calls `evaluate_pre_commit(state, content=...)`
   instead of `validate_completion(...)` directly.
2. `pact_verdict` signal payload is produced via a mapper that:
   - Preserves every existing field (verdict / required_evidence /
     answer_requirements / missing_evidence / observed / source_urls /
     tools / evidence_entries).
   - Adds `reasons: list[str]` from `PreCommitVerdict.reasons`.
   - Sets `verdict="completed"` or `"blocked"` from contract_verdict
     when committable.
   - Sets `verdict="needs_action"` when non-committable due to
     `contract:needs_action`.
   - Sets `verdict="blocked"` for every other non-committable reason.
3. Retry path (`_work_contract_retry_sent`) fires exactly once and only
   on `contract:needs_action` reason. Other non-committable reasons
   terminate the task blocked without retry and without appending a
   platform retry message.
4. PACT run projection `/api/v1/agents/{name}/pact/runs/{taskId}`
   surfaces `reasons: list[str]` in its `verdict` block. Missing for
   legacy runs → empty list.
5. Audit-report hash includes `reasons` in its deterministic content
   scope. Hash stability holds across repeated reads of the same run
   (same run → same hash).
6. `_validate_*_answer` contract-specific validators are unchanged.
   `validate_completion` is unchanged.
7. 11 trajectory/integration test cases from Scope are covered. Existing
   trajectory tests (Wave 1 #1 foundational contracts) continue to pass
   unchanged or with minimal diffs reflecting the new `reasons` field.
8. `pytest images/tests/` and `go build ./cmd/gateway/` pass.
9. Spec "Current Implementation Checkpoint" updated: verdict-signal
   payload adds `reasons`, new mapping semantics described, hash scope
   note added.
10. OpenAPI spec for `/pact/runs/{taskId}` updated to add `reasons` to
    the verdict schema.

## Review Gates

**Reject** if:
- `validate_completion` signature, behavior, or contract-specific
  validator logic changes.
- Existing `pact_verdict` signal payload fields are renamed or removed.
- `_work_contract_retry_sent` fires on reasons other than
  `contract:needs_action`, or fires more than once per task.
- A non-contract non-committable reason retries instead of terminating
  blocked.
- The audit-report hash becomes non-deterministic (changes between reads
  of the same run).
- Wave 2 #3b or Wave 2 #5b scope crosses in.
- New verdict values introduced beyond the existing three.
- Any change to the evaluator (`evaluate_pre_commit`, `PreCommitVerdict`).
  This PR uses the evaluator; it does not modify it.

**Ask for changes** if:
- The mapper is implemented inline in body.py instead of a dedicated
  helper function that can be tested in isolation.
- Tests don't cover the short-circuit behavior (non-committable reasons
  skip retry for anything but `contract:needs_action`).
- OpenAPI schema update for `reasons` lacks a description string.

## Files Likely To Touch

- `images/body/body.py` — replace the `validate_completion` call site,
  add the verdict mapper (or import from a new helper), adjust the retry
  condition, adjust the terminal paths for non-retryable non-committable
  reasons.
- `internal/api/agents/handlers_pact.go` (and any helper file) — surface
  `reasons` from the stored verdict signal into the run projection.
- `internal/api/openapi.yaml` + `internal/api/openapi-core.yaml` — add
  `reasons` to the `/pact/runs/{taskId}` verdict schema.
- `images/tests/test_pact_trajectories.py` — update foundational
  trajectory tests to account for new `reasons` field; add new cases for
  non-contract blocked reasons.
- `images/tests/test_pact_pre_commit_rewire.py` (new, optional) — new
  integration cases if trajectories file grows unwieldy.
- `docs/specs/pact-governed-agent-execution.md` — Checkpoint section
  updates only.

## ASK Compliance

This PR is the first one in Wave 2 that moves behavior from advisory to
enforcing. ASK sensitivity is higher than the primitives work.

- **#1 external enforcement / #3 complete mediation** — the commit-gate
  decision moves from the contract-specific validators (Wave 0 layer)
  into the general evaluator. Both are runtime-owned; the decision is
  still external to the agent boundary. Mediation and enforcement paths
  (gateway, enforcer, policy) are untouched. Net: same enforcement
  posture, more expressive commit semantics.

- **#2 audit append-only** — `pact_verdict` signal payload adds a
  field (`reasons`). Existing stored audit JSONL is not mutated; new
  events simply carry the additional field. Run-projection hash
  includes `reasons`, so new run hashes differ from what legacy
  projections (without `reasons`) would have computed. No external
  consumer persists those hashes today, so this is a uniform schema
  evolution, not a mutation of prior attestations.

- **#4 fail-closed default** — when `evaluate_pre_commit` returns
  `committable=False`, body.py blocks the commit. Fail-closed is the
  default direction; the agent cannot "override" by retrying a
  non-retryable reason.

- **#5 runtime is a known quantity** — `reasons` in the signal payload
  and run projection makes the commit decision operator-inspectable in
  a structured form. Net ASK gain.

- **#7 least privilege / #8 bounded operations** — no new capabilities
  granted. Retry budget still bounded (one retry on contract:needs_action,
  zero on other reasons).

- **#11 halts auditable and reversible** — `halt:halted` reason blocks
  commit. The halted state is preserved in `recovery_state`; commit does
  not clear it. Operator action (via future admin path) remains the only
  way out.

- **#18 governance hierarchy inviolable from below** — the evaluator
  reads only runtime-owned typed state. Agent-proposed answer content
  passes through Layer 7 (contract validator) but cannot override
  Layers 0–5 or Layer 6 advisory signals. A non-committable verdict
  cannot be bypassed by answer text.

- **#22 unknown conflicts default to yield and flag** — incomplete state
  (Layer 0) still blocks commit with reason `"incomplete_state:*"`.
  Unknown recovery/strategy/ambiguity combinations all short-circuit
  to a blocked outcome with a reason label that surfaces the cause.

- **#25 identity mutations auditable** — no identity writes. Task state
  is per-task; blocking a commit for non-committable reasons does not
  alter any durable agent state.

**Forward-looking ASK notes (bind on later waves):**

- **Wave 2 #3b (plan execution)**: when plan steps actually execute, the
  evaluator's Layer 6 must upgrade from advisory to a hard check. At
  that point, `plan_advisory:missing:*` reasons become
  `plan:missing:*` and flip `committable=False`.
- **Wave 2 #5b (recovery rewire)**: retry budget and next-action
  decisions move from `_work_contract_retry_sent` into
  `recovery_state.attempt` and `recovery_state.next_action`. The retry
  condition in this PR (`"contract:needs_action"` in reasons) will be
  replaced by `recovery_state.next_action == NextAction.retry`.
- **Wave 4 / #4c (admin observability)**: `reasons` surfaced in the
  verdict signal and run projection enables aggregate reporting:
  blocked verdict trends, missing evidence categories, recovery-halt
  counts. That reporting should query the projection, not rebuild from
  raw audit events.

## Out-of-band Notes For Codex

- The verdict mapper is the load-bearing piece of this PR. Keep it as
  a dedicated helper function in `body.py` (or in `pact_engine.py`)
  that takes `(PreCommitVerdict, task_id, kind)` and returns the
  legacy verdict-dict shape. Make it unit-testable.
- Test cases 5–8 (non-contract blocked reasons) are the most load-bearing
  — they verify the ASK invariants become enforcement, not advisories.
- The audit-report hash stability test (case 11) is the structural
  backstop for tenet #2.
- The PACT run projection is Go code in `internal/api/agents/handlers_pact.go`.
  Adding `reasons` is additive: the projection reads the stored verdict
  signal's `reasons` field (if present) and emits it as a JSON array.
- OpenAPI update: add `reasons: { type: array, items: {type: string}, description: "Structured reason labels from the pre-commit evaluator" }` to the `/pact/runs/{taskId}` verdict schema.
- Commit style: plain commit title, no Co-Authored-By trailer.
- PR target: `main`. Open as ready-for-review, not draft.
- Do NOT re-add the brief file; it is already committed on the branch.
