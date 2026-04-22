# PACT Wave 2 #4 — General Pre-Commit Evaluator

## Reference

- Spec: `docs/specs/pact-governed-agent-execution.md` → Wave 2 #4
- Wave: Wave 2 — Harness Capabilities, item #4
- Builds on: every prior Wave 2 item (consumes `Objective`, `Strategy`,
  `Plan`, `EvidenceLedger`, `ToolObservation`, `RecoveryState`).
- Completes Wave 2 — Harness Capabilities.
- Defers: Wave 2 #4b (future) — runtime integration in `body.py` that gates
  commit on the evaluator's verdict.

## Objective

Add a deterministic general pre-commit evaluator that runs against the full
`ExecutionState` and produces a layered `PreCommitVerdict`. The evaluator
consolidates every load-bearing invariant set by prior Wave 2 items:

- clarify routes block commit (Wave 2 #2 forward note)
- load-bearing ambiguities block commit (Wave 2 #1 forward note)
- plan steps' expected evidence is checked against the ledger (Wave 2 #3
  forward note) — advisory in this PR, hard in Wave 2 #3b
- recovery halts / failures block commit (Wave 2 #5 forward note)
- approval-gated work blocks commit without a recorded approval decision

Advisory-only in this PR. `body.py`'s existing commit/verdict flow stays
unchanged. The evaluator is a standalone primitive that callers (tests,
future runtime integration) can invoke. Wave 2 #4b wires it into the body
runtime's commit gate.

## Why

Every Wave 2 item has left behind a forward ASK note saying "the evaluator
must do X." That evaluator didn't exist yet. This PR is where those invariants
become executable code, with test coverage that proves they hold. Once in
place, Wave 2 #4b rewires `body.py` to consult the verdict before emitting
`pact_verdict` / calling `complete_task`.

Keeping this PR scoped to the evaluator primitive (no body.py rewire) mirrors
the Wave 2 #5 primitives-first approach — small blast radius, review the
state-machine logic in isolation.

## Scope (in this PR)

### Add `PreCommitVerdict` dataclass

In `images/body/pact_engine.py` (or `images/body/pre_commit_evaluator.py`
sibling module if the new code exceeds ~200 lines — Codex's call):

```python
@dataclass(slots=True, frozen=True)
class PreCommitVerdict:
    """Outcome of the general pre-commit evaluation."""
    committable: bool
    reasons: tuple[str, ...] = ()
    missing: tuple[str, ...] = ()
    contract_verdict: dict = field(default_factory=dict)
    evaluated_at: datetime | None = None

    def to_dict(self) -> dict: ...
```

Deterministic `to_dict()` serializing `committable`, `reasons`, `missing`,
`contract_verdict` (pass-through dict), and `evaluated_at` (ISO8601 string or
`None`).

### Add `evaluate_pre_commit` function

```python
def evaluate_pre_commit(
    state: ExecutionState,
    *,
    content: str = "",
    now: datetime | None = None,
) -> PreCommitVerdict
```

Pure-ish function: reads `state`, does not mutate it, does not write to disk
or call models. `now` parameter is the clock (tests pass deterministic
datetimes). Default `now=_utc_now()` is the only allowed clock access.

`content` is the candidate terminal output text (the same input today's
`validate_completion` takes).

### Layered checks (strict order; short-circuit at first non-committable)

Run layers 0–7 in order. The first failing layer sets `committable=False`
and populates `reasons` / `missing`. Layer 8 (success) runs only if every
preceding layer passed.

**Layer 0 — state completeness.**
- If `state.activation is None` or `state.contract is None` →
  `committable=False`, `reasons=("incomplete_state:activation",)` or
  similar. Default zero-trust.

**Layer 1 — recovery halt/terminal.**
- If `state.recovery_state` exists and its `status` is in
  `{halted, failed, expired, superseded}` → `committable=False`,
  `reasons=(f"halt:{status}",)`.

**Layer 2 — recovery next action.**
- If `state.recovery_state.next_action` is in
  `{escalate, clarify, block, fail, halt}` → `committable=False`,
  `reasons=(f"recovery:{next_action}",)`.

**Layer 3 — strategy route.**
- If `state.strategy` exists and `state.strategy.execution_mode` is in
  `{clarify, escalate}` → `committable=False`,
  `reasons=(f"strategy:{execution_mode}",)`.

**Layer 4 — load-bearing ambiguities.**
- If `state.objective` exists and `state.objective.ambiguities` contains
  any load-bearing label (current set: `"ambiguity:target_files_missing"`,
  `"ambiguity:external_authority_scope"`) → `committable=False`,
  `reasons` lists each ambiguity with the `ambiguity:` prefix preserved.

**Layer 5 — approval required.**
- Triggered when either:
  - `state.strategy.needs_approval is True`, OR
  - `state.plan` has any step with `requires_approval=True`.
- Requires the evidence ledger to contain an entry with classification
  `approval_decision` (check `EvidenceLedger.entries` or look in
  `ExecutionState.tool_observations` for `evidence_classification`
  containing `"approval_decision"`).
- If approval is required and no `approval_decision` evidence present →
  `committable=False`,
  `reasons=("approval_required:no_approval_decision",)`,
  `missing=("approval_decision",)`.

**Layer 6 — plan evidence (ADVISORY in this PR).**
- If `state.plan` exists and has `steps`, check each step's
  `expected_evidence` labels against the ledger. For each missing label,
  append an advisory reason
  `f"plan_advisory:missing:{classification}"`.
- **This layer does NOT set `committable=False` in this PR.** The reasons
  are recorded for audit visibility. Wave 2 #3b / Wave 2 #4b will upgrade
  this to a hard check once plans are actually executed step-by-step.

**Layer 7 — contract-specific validator.**
- Call the existing `validate_completion(contract_dict, evidence_dict,
  content)` (from `pact_engine`) with `state.contract.to_dict()`,
  `state.evidence.to_dict()`, and the `content` parameter.
- Store the full verdict in `contract_verdict`.
- If the returned verdict value is `needs_action` → `committable=False`,
  `reasons=(f"contract:{verdict}",)` plus any `missing_evidence` from the
  verdict.
- If the returned verdict is `completed` or `blocked` → this layer
  passes (both are valid terminal outcomes per Wave 1 #1 / operator_blocked
  semantics).

**Layer 8 — success.**
- Every layer above passed. `committable=True`,
  `reasons=("committable",)`, `missing=()`, `contract_verdict` populated
  from Layer 7.

### Reason label shape

All reason labels follow the stable `category:subcategory[:detail]` form so
downstream consumers (audit, admin observability in later waves) can grep.

Examples:
```
halt:failed
recovery:escalate
strategy:clarify
ambiguity:target_files_missing
approval_required:no_approval_decision
plan_advisory:missing:artifact_path
contract:needs_action
incomplete_state:activation
committable
```

### Re-exports

Re-export `PreCommitVerdict` and `evaluate_pre_commit` from
`images/body/work_contract.py` for consumer imports.

### Do NOT modify `body.py`

The existing commit flow — `validate_completion`, `emit_pact_verdict`,
`complete_task`, the retry path — stays unchanged. Wave 2 #4b wires
`evaluate_pre_commit` into the body runtime in a follow-up PR.

Add ONE comment near the existing `validate_completion` call site referencing
Wave 2 #4b (similar to how Wave 2 #5 left a breadcrumb by
`_work_contract_retry_sent`).

### Spec Checkpoint update

Update the "### Execution State Type" subsection:
- Add a new paragraph naming the general pre-commit evaluator, the 8
  layered checks, and noting that it's advisory in this PR — `body.py`'s
  commit flow still uses `validate_completion` directly. Mark plan-evidence
  check as advisory until Wave 2 #3b.

### Tests

New `images/body/test_pre_commit_evaluator.py` covering:

1. `PreCommitVerdict` construction and deterministic `to_dict()`
   round-trip (including `evaluated_at` serialization).
2. Layer 0 triggers on `state.activation is None` → not committable,
   reason `"incomplete_state:activation"`.
3. Layer 1 triggers on `recovery_state.status=halted` → not committable,
   reason `"halt:halted"`.
4. Layer 2 triggers on `recovery_state.next_action=escalate` → not
   committable, reason `"recovery:escalate"`.
5. Layer 3 triggers on `strategy.execution_mode=clarify` → not
   committable, reason `"strategy:clarify"`.
6. Layer 4 triggers on
   `objective.ambiguities=["ambiguity:target_files_missing"]` → not
   committable, reason preserves the full `"ambiguity:..."` label.
7. Layer 5 triggers on `strategy.needs_approval=True` with no
   `approval_decision` evidence → not committable, reason
   `"approval_required:no_approval_decision"`,
   `missing=("approval_decision",)`.
8. Layer 5 passes when an `approval_decision` evidence entry exists in
   `state.tool_observations` (via `evidence_classification`).
9. Layer 6 (advisory) records `plan_advisory:missing:<label>` reasons for
   plan steps whose expected_evidence is absent, but **does NOT** set
   `committable=False`.
10. Layer 7 triggers on contract verdict `needs_action` → not
    committable, reason `"contract:needs_action"`.
11. Layer 7 passes when contract verdict is `completed`.
12. Layer 7 passes when contract verdict is `blocked` (valid terminal
    for `operator_blocked`).
13. All-pass happy path → `committable=True`, `reasons=("committable",)`.
14. Short-circuit ordering: when both Layer 1 and Layer 3 would fail,
    only Layer 1's reason appears (first-match wins, not cumulative).
15. `evaluate_pre_commit` does not mutate `state` (identity and field
    equality check before/after).
16. Determinism: calling `evaluate_pre_commit` twice with the same
    inputs and the same `now` produces equal verdicts.

## Non-Scope

- **Wave 2 #4b runtime integration** — `body.py` commit gate, emitting
  `PreCommitVerdict` as a signal, blocking `complete_task` on
  non-committable verdicts. All of that ships separately.
- **Plan evidence as a hard check** — advisory in this PR; Wave 2 #3b/#4b
  upgrades it when plans are actually executed.
- **Model-assisted critique** — spec mentions "bounded model-assisted
  critique where deterministic checks are insufficient." Deferred. This
  PR is deterministic only.
- **Rewriting existing contract-specific validators** — Layer 7
  delegates to the existing `validate_completion`. Do not change or
  replace `_validate_current_info_answer`,
  `_validate_file_artifact_answer`, `_validate_code_change_answer`,
  `_validate_operator_blocked_answer`.
- **Public API shapes** — verdict signal payload, result frontmatter,
  PACT run projection, audit report, verify, admin audit enrichment.
  All unchanged.
- **OpenAPI, web UI, feature registry, Go files.**
- **New load-bearing ambiguity labels, recovery statuses, execution
  modes, or verdict values.** Use only what's already defined.

## Acceptance Criteria

1. `PreCommitVerdict` frozen dataclass with 5 fields and deterministic
   `to_dict()`.
2. `evaluate_pre_commit(state, *, content="", now=None)` implemented
   with all 9 layers (0–8) in strict order, short-circuiting at the
   first failing layer.
3. Reason labels follow the stable `category:subcategory[:detail]` form.
4. Layer 6 (plan evidence) is advisory — records reasons but does not
   flip `committable` to `False`. Tests assert both parts.
5. Layer 7 delegates to existing `validate_completion` and treats
   `completed`/`blocked` as passing, `needs_action` as failing.
6. `body.py` commit flow, retry path, verdict signal, result artifact
   writer, and `complete_task` are all unchanged. A single comment near
   the existing `validate_completion` call site references Wave 2 #4b.
7. `evaluate_pre_commit` does not mutate `state` (asserted in tests).
8. 16 test cases in `images/body/test_pre_commit_evaluator.py` covering
   each layer, short-circuit ordering, advisory behavior, happy path,
   immutability, and determinism.
9. Public API shapes preserved. Audit-report hash stable. No Go files
   modified.
10. `pytest images/tests/` and `go build ./cmd/gateway/` succeed.
11. Spec "### Execution State Type" subsection updated with the new
    evaluator summary.

## Review Gates

**Reject** if:
- Wave 2 #4b scope crosses in: `body.py` rewired to call
  `evaluate_pre_commit` or emit its verdict as a signal.
- Existing contract-specific validators are modified or replaced.
- Layer 6 becomes a hard check (should be advisory in this PR).
- New load-bearing ambiguity labels, recovery statuses, execution modes,
  or reason-label categories invented beyond those already in the system.
- `evaluate_pre_commit` mutates `state` (breaks determinism and ASK
  tenet #2 — evaluation produces signals, not side effects).
- Public API shapes change.
- Audit-report hash becomes unstable.
- Go files modified.

**Ask for changes** if:
- Reason labels aren't in the stable `category:subcategory[:detail]` form.
- Layer ordering deviates from the Scope spec (0 through 8).
- Short-circuit ordering isn't strict (cumulative reasons across
  failing layers).
- Tests don't cover short-circuit ordering or state immutability.
- `contract_verdict` pass-through loses fields from the legacy verdict.

## Files Likely To Touch

- `images/body/pact_engine.py` — add `PreCommitVerdict` and
  `evaluate_pre_commit` (or sibling `images/body/pre_commit_evaluator.py`)
- `images/body/work_contract.py` — re-export new symbols
- `images/body/body.py` — add ONE comment referencing Wave 2 #4b near the
  existing `validate_completion` call site; no other changes
- `images/body/test_pre_commit_evaluator.py` (new)
- `docs/specs/pact-governed-agent-execution.md` — Checkpoint subsection
  update only

## ASK Compliance

- **#1 external enforcement / #3 complete mediation / #4 fail-closed** —
  evaluator is pure runtime code, produces a verdict, does not enforce.
  Enforcement stays in body.py commit flow (current state) or the future
  #4b rewire. Incomplete state (Layer 0) defaults to not-committable.
- **#2 audit** — evaluator does not write audit events. `PreCommitVerdict`
  is a value returned to the caller; the caller decides how to surface
  it. This PR does not surface it anywhere runtime-visible yet.
- **#5 runtime is a known quantity** — layered reason labels make commit
  decisions operator-inspectable. When the body.py rewire lands, each
  reason will be greppable in the audit trail.
- **#7 least privilege / #8 bounded operations** — evaluator does not
  grant capability or extend budget. It can only refuse commits.
- **#11 halts auditable and reversible** — Layer 1/2 respect halted and
  recovery-terminal states; the evaluator cannot unhalt.
- **#18 governance hierarchy inviolable from below** — every Layer
  consumes runtime-owned state (recovery, strategy, objective, plan
  populated by pure builders). Agent-authored data does not influence
  verdict.
- **#22 unknown conflicts default to yield and flag** — incomplete state
  yields not-committable. Unknown ambiguity labels, if ever encountered,
  fall through to the legacy contract validator (Layer 7) — which is
  itself fail-closed on unknown kinds.
- **#25 identity mutations auditable** — evaluator is read-only on
  `state`. Tests assert immutability.

**Forward-looking ASK note for Wave 2 #4b (runtime integration):**
- The body.py rewire must block `complete_task` when
  `verdict.committable is False`. Agent-proposed "override" arguments
  to `complete_task` must not bypass the verdict. The verdict's
  enforcement is runtime-owned.
- Emitting the verdict via `pact_verdict` signal must preserve the
  existing signal payload shape as a superset — i.e., additive fields
  only, no removals. Audit-report hash stability applies.

## Out-of-band Notes For Codex

- The evaluator is the most interconnected Wave 2 primitive. Read the
  entire Checkpoint section of the PACT spec first to see how prior
  Wave 2 items layered their typed state.
- Keep Layer 6 advisory. Do NOT make plan-evidence a hard check; tests
  #9 asserts advisory behavior explicitly.
- Short-circuit ordering matters. When Layer 1 would fail, do not also
  run Layer 3. Test #14 asserts first-match-only semantics.
- Layer 7 MUST use the existing `validate_completion` function —
  do not reimplement or bypass it. The pass-through dict in
  `PreCommitVerdict.contract_verdict` must carry every field of the
  legacy verdict.
- `now=_utc_now()` is the only clock access permitted. Pure behavior in
  tests requires passing an explicit `now` datetime.
- `evidence_classification` inspection for Layer 5 should walk
  `state.tool_observations` (Wave 1 #2 typed observations). Fall back
  to the legacy `EvidenceLedger` dict projection if needed for contracts
  that haven't been observed via tool_observations yet.
- Commit style: plain commit title, no Co-Authored-By trailer.
- PR target: `main`.
- **Open the PR as ready-for-review, not as draft.** (Prior runs
  defaulted to draft; this wastes one round-trip.)
