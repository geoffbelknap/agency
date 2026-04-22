# PACT Wave 1 #1 — Typed `ExecutionState`

## Reference

- Spec: `docs/specs/pact-governed-agent-execution.md`
- Wave: Wave 1 — Harness Foundations, item #1
- Related: Wave 1 #2 (structured tool observation protocol) is a sibling; do
  not start it in this PR.

## Objective

Introduce a runtime-owned, typed `ExecutionState` object in the body runtime.
Begin migrating the per-task state that is currently scattered across
`AgencyBody` instance fields behind this object. Establish the type and a
minimal migration path so Wave 2 capabilities (objective builder, strategy
router, planner, general evaluator, recovery state machine) and Wave 5 report
projection can build on a single typed primitive.

## Why

The current body runtime tracks per-task state across many private fields:
`self._current_task_id`, `self._current_task_tier`, `self._current_task_turns`,
`self._work_contract` (dict-shaped), `self._work_evidence_ledger` (typed),
`self._work_evidence` (dict projection), `self._work_contract_retry_sent`,
`self._task_tier`, `self._task_features`, `self._task_metadata`, and others.
This implicit state model is the reason the PACT run projection endpoint
currently reconstructs runs from artifact frontmatter and audit events — there
is no typed run primitive to source from. Wave 2+ depends on a typed state
object; Wave 5 reports depend on the same thing.

## Scope (in this PR)

### Define `ExecutionState`

Add a new type in `images/body/pact_engine.py` (or a new
`images/body/pact_state.py` module imported by `pact_engine` — pick whichever
keeps pact_engine under a reasonable line count; prefer a sibling module if
pact_engine.py grows past ~1100 lines).

Fields:

- `task_id: str`
- `agent: str`
- `activation: ActivationContext | None`
- `objective: Objective | None` — new placeholder type; see below
- `contract: WorkContract | None` — reuse existing type
- `plan: Plan | None` — new placeholder type; see below
- `step_history: list[StepRecord]` — new placeholder type; see below
- `tool_observations: list[ToolObservation]` — new placeholder type; see below
- `evidence: EvidenceLedger` — reuse existing type
- `partial_outputs: list[str]` — freeform until Wave 3 disposition lands
- `errors: list[ExecutionError]` — new placeholder type; see below
- `recovery_state: RecoveryState | None` — new placeholder type; see below
- `proposed_outcome: ProposedOutcome | None` — new placeholder type; see below
- `started_at: datetime`
- `updated_at: datetime`

For the placeholder types (`Objective`, `Plan`, `StepRecord`, `ToolObservation`,
`ExecutionError`, `RecoveryState`, `ProposedOutcome`), define them as
dataclasses with a minimal set of fields derivable from existing runtime data.
Each placeholder must carry a docstring stating which future wave populates it
and a link to the spec item. Examples:

- `Objective` — fields: `statement`, `kind`, `constraints`, `deliverables`,
  `success_criteria`, `ambiguities`, `assumptions`, `risk_level`. Populated
  by Wave 2 #1; in this PR, constructed as `None` or a minimal shell from the
  activation content.
- `StepRecord` — fields: `step_id`, `phase`, `turn`, `started_at`, `ended_at`,
  `summary`. Populated from existing turn tracking.
- `ToolObservation` — fields: `tool`, `status`, `summary`, `observed_at`.
  Full protocol (retryability, side effects, provenance) lands in Wave 1 #2,
  not this PR. Keep the struct minimal so Wave 1 #2 can extend it without
  breaking callers.

Provide:

- `ExecutionState.from_task(task: dict, *, agent: str) -> ExecutionState` —
  constructor used at task start.
- `ExecutionState.to_dict() -> dict` — serializer used for any surface that
  needs a dict view (mirrors `EvidenceLedger.to_dict()` pattern).
- `ExecutionState.record_evidence(entry: EvidenceEntry) -> None` — delegates
  to `self.evidence` and bumps `updated_at`.
- `ExecutionState.record_step(step: StepRecord) -> None`
- `ExecutionState.record_observation(obs: ToolObservation) -> None`

### Minimal migration in `AgencyBody`

In `images/body/body.py`, introduce a single `self._execution_state:
ExecutionState | None` field set at task start. Move the following existing
fields behind `ExecutionState`:

- `self._current_task_id` → `self._execution_state.task_id`
- `self._work_contract` → `self._execution_state.contract`
- `self._work_evidence_ledger` → `self._execution_state.evidence`
- `self._work_evidence` → `self._execution_state.evidence.to_dict()` (derived
  at read site)

Leave the remaining fields (`_current_task_tier`, `_current_task_turns`,
`_task_tier`, `_task_features`, `_task_metadata`, `_work_contract_retry_sent`)
in place for this PR. Add a TODO comment at each one naming the future wave
item that will migrate it. Do not attempt to migrate every field — the spec
says "incrementally."

At task end / clear, reset `self._execution_state = None`.

### Preserve existing behavior exactly

- `pact_verdict` audit signal payload shape: **unchanged**.
- Result artifact frontmatter shape: **unchanged**.
- `/api/v1/agents/{name}/pact/runs/{taskId}` response: **unchanged**.
- `/api/v1/agents/{name}/pact/runs/{taskId}/audit-report` response: **unchanged**.
- `/api/v1/agents/{name}/pact/runs/{taskId}/audit-report/verify` response:
  **unchanged**, including hash stability.
- Admin audit enriched log response: **unchanged**.
- Completion validation behavior for `current_info`, `file_artifact`,
  `code_change`, `operator_blocked`: **unchanged**.

Any change to a public API shape, audit event payload, or hash-scope is
out of scope for this PR and must be rejected in review.

## Non-Scope

- **Wave 1 #2** (structured tool observation protocol) — `ToolObservation`
  carries only a minimal shape in this PR. Do not extend it with retryability,
  side effects, or provenance classification here.
- **Wave 2** (objective builder, strategy router, planner as runtime object,
  general pre-commit evaluator, recovery state machine) — do not implement
  logic for these, only the typed placeholder fields.
- **Wave 4 #2** (durable typed evidence ledger as a standalone resource) — the
  in-memory `EvidenceLedger` inside `ExecutionState` is sufficient; do not
  promote to a standalone ledger resource.
- **Wave 4 #3** (additional contract evaluators including
  `external_side_effect`) — no new evaluators.
- **Wave 5** (compliance reports) — do not alter report generation to project
  from `ExecutionState` yet. The projection endpoint keeps its current
  artifact-frontmatter + audit-events implementation.
- **Runtime manifest / API spec changes** — no OpenAPI changes.
- **Web UI changes** — no UI changes.
- **Renaming existing fields for consistency** — only migrate fields named
  above; do not rename other fields.

## Acceptance Criteria

1. `ExecutionState` dataclass (or attrs class) exists with all fields listed
   in the Scope section, with type annotations and docstrings linking each
   placeholder field to its owning wave item in the spec.
2. All placeholder types (`Objective`, `Plan`, `StepRecord`, `ToolObservation`,
   `ExecutionError`, `RecoveryState`, `ProposedOutcome`) exist with minimal
   field sets and docstrings.
3. `AgencyBody` constructs an `ExecutionState` at task start and clears it at
   task end.
4. `self._current_task_id`, `self._work_contract`, `self._work_evidence_ledger`
   are read/written through `self._execution_state` where possible, with
   backwards-compatible accessors preserved for callers that still use the
   old field names (either as `@property` or by retaining the field and
   keeping it in sync).
5. Existing tests in `images/body/test_*.py` pass without modification except
   for test files directly exercising the migrated fields (those may be
   updated minimally to use the new field or retain existing field access
   via the compat shim).
6. At least one new test file `images/body/test_execution_state.py` covers:
   - Construction from a task dict
   - Evidence recording updates both the ledger and `updated_at`
   - Step recording appends to `step_history` and bumps `updated_at`
   - `to_dict()` round-trips the ledger correctly
   - Placeholder fields default to `None` / empty list
7. Spec Checkpoint section (`docs/specs/pact-governed-agent-execution.md` →
   "Current Implementation Checkpoint") is updated with a new subsection
   describing the new `ExecutionState` type and which fields are populated vs
   placeholder.
8. No changes to `pact_verdict` signal payload, result frontmatter,
   `/pact/runs/...` endpoints, audit-report hash, or admin audit enrichment.
9. `go build ./cmd/gateway/` and `pytest images/tests/` both succeed.

## Files Likely To Touch

- `images/body/pact_engine.py` (or new `images/body/pact_state.py`)
- `images/body/body.py` (construction/clear, field migration)
- `images/body/test_execution_state.py` (new)
- `images/body/test_post_task.py` (if it directly reads migrated fields)
- `docs/specs/pact-governed-agent-execution.md` (Checkpoint update only)

## Review Gates

Reviewer (Claude) will reject the PR if:

- Wave 1 #2, Wave 2+, Wave 4+, or Wave 5 work crosses into this PR.
- Any public API shape changes (verdict signal, result frontmatter, PACT run
  projection, audit report, verify endpoint, admin audit enrichment).
- Audit-report hash becomes unstable across repeated reads of the same run.
- Existing completion validation behavior changes.
- New evaluators or contract kinds are introduced.
- Placeholder types are over-defined (e.g., `ToolObservation` with
  retryability/side-effects/provenance — that is Wave 1 #2).
- Fields are migrated without a compatibility path or tests break on
  unrelated call sites.
- Feature registry, web UI, or OpenAPI is modified.

Reviewer will ask for changes (not reject) if:

- Placeholder types lack docstrings naming the future wave item.
- Migration leaves `_current_task_id` / `_work_contract` /
  `_work_evidence_ledger` inconsistent with `ExecutionState` at any point.
- `ExecutionState.to_dict()` does not serialize deterministically.

## Out-of-band Notes For Codex

- Do not rewrite `body.py` broadly. Migrate only the three fields named above.
- Do not change the verdict signal or result artifact writer beyond threading
  reads through `ExecutionState` where obviously correct.
- Prefer `@dataclass(slots=True)` or an attrs class for `ExecutionState` and
  its placeholder types; match the style already used by `EvidenceLedger` /
  `EvidenceEntry` / `WorkContract` in `pact_engine.py`.
- If you discover a scope question that requires a product decision (e.g.,
  whether a specific field should be migrated now), stop and report rather
  than expanding scope.
- Commit as a single feature commit. Do not include unrelated cleanups.
