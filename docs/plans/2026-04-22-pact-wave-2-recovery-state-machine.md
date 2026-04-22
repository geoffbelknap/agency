# PACT Wave 2 #5 — Recovery State Machine (primitives)

## Reference

- Spec: `docs/specs/pact-governed-agent-execution.md` → Wave 2 #5
- Wave: Wave 2 — Harness Capabilities, item #5
- Builds on: Wave 1 #1 `ExecutionState` (merged), Wave 1 #2 structured
  tool observation protocol (merged — consumes `Retryability`).
- Unblocks: Wave 2 #5b (future) — rewiring `body.py` retry path to consume
  the state machine. Rewiring is **out of scope** in this PR.

## Objective

Promote the Wave 1 #1 `RecoveryState` placeholder into a real state
machine: explicit `RecoveryStatus` enum, `NextAction` enum, expanded
`RecoveryState` fields, pure transition methods driven by typed tool
observations and contract verdicts. Populate `ExecutionState.recovery_state`
from observation failures and contract evaluation gaps.

**Advisory-only in this PR.** The existing `body.py` retry path
(`_work_contract_retry_sent`) stays unchanged. A follow-up PR rewires
body.py to consume the state machine once these primitives are proven.

## Why

Wave 1 #2's brief set the ASK invariant that retryability decisions are
runtime-owned, not agent-directed. The recovery state machine is where
that decision lives. It consumes the typed `Retryability` classification
from tool observations, enforces a retry budget, and emits an explicit
next action (retry / replan / fallback / clarify / escalate / block /
fail / halt / none).

Staging this as primitives-first keeps the blast radius small: this PR
ships the state machine as pure code with tests, independent of the
body.py retry rewire. Parallel with Wave 2 #2 (strategy router), same
merge-conflict profile as Wave 2 #1's parallel with Wave 1 #2.

## Scope (in this PR)

### Expand `RecoveryStatus` enum

Replace the minimal Wave 1 #1 `RecoveryState` with a typed machine.

```python
class RecoveryStatus(StrEnum):
    idle = "idle"            # no recovery in progress (default)
    retrying = "retrying"
    replanning = "replanning"
    fallback = "fallback"
    clarifying = "clarifying"
    escalated = "escalated"
    blocked = "blocked"
    failed = "failed"
    halted = "halted"
    expired = "expired"
    superseded = "superseded"
```

11 values total — the 10 recovery states the spec names, plus `idle`
for the default no-recovery state.

### Add `NextAction` enum

```python
class NextAction(StrEnum):
    none = "none"            # no recovery action required
    retry = "retry"
    replan = "replan"
    fallback = "fallback"
    clarify = "clarify"
    escalate = "escalate"
    block = "block"
    fail = "fail"
    halt = "halt"
```

9 values. `none` is the default when the machine is `idle`.

### Expand `RecoveryState` dataclass

```python
@dataclass(slots=True)
class RecoveryState:
    status: RecoveryStatus = RecoveryStatus.idle
    reason: str = ""
    attempt: int = 0
    max_attempts: int = 3
    last_error: ExecutionError | None = None
    next_action: NextAction = NextAction.none
    updated_at: datetime = field(default_factory=_utc_now)
```

Deterministic `to_dict()` that serializes every field. `last_error` uses
`ExecutionError.to_dict()` when present.

### Transition methods on `RecoveryState`

All methods are pure (mutate `self` only, no I/O, no clock calls — pass
`now: datetime | None = None` to bump `updated_at`; default to a new
`datetime` provided by the caller, never from `datetime.now()`).

```python
def record_tool_failure(self, obs: ToolObservation, *, now: datetime) -> None
def record_evidence_gap(self, missing: list[str], *, now: datetime) -> None
def record_load_bearing_ambiguity(self, ambiguity: str, *, now: datetime) -> None
def record_operator_halt(self, reason: str, *, now: datetime) -> None
def record_success(self, *, now: datetime) -> None
def record_expiration(self, reason: str, *, now: datetime) -> None
def record_superseded(self, reason: str, *, now: datetime) -> None
```

Transition rules (see next section).

### Transition rules (strict per-method)

**`record_tool_failure(obs, now)`:**

1. If `obs.status != ToolStatus.error`: no-op.
2. Increment `self.attempt` by 1.
3. Set `self.last_error` from `obs.error` (or synthesize from `obs.summary`
   if `obs.error is None`).
4. Decide status/action from `obs.retryability`:
   - `retry_safe` + attempt < max_attempts → `retrying`, `NextAction.retry`
   - `retry_with_backoff` + attempt < max_attempts → `retrying`,
     `NextAction.retry` (callers can consult `obs.error.retry_after_ms`)
   - Budget exhausted OR `not_retryable` → `failed`, `NextAction.fail`
   - `unknown` + attempt < max_attempts → `fallback`, `NextAction.fallback`
   - `unknown` + budget exhausted → `escalated`, `NextAction.escalate`
5. Set `self.reason = f"tool_failure:{obs.tool}:{obs.retryability}"`.

**`record_evidence_gap(missing, now)`:**

1. If `missing` is empty: no-op.
2. Set `self.status = blocked`, `self.next_action = NextAction.block`.
3. Set `self.reason = f"evidence_gap:{','.join(sorted(missing))}"`.

**`record_load_bearing_ambiguity(ambiguity, now)`:**

1. Set `self.status = clarifying`, `self.next_action = NextAction.clarify`.
2. Set `self.reason = f"ambiguity:{ambiguity}"`.

**`record_operator_halt(reason, now)`:**

1. Set `self.status = halted`, `self.next_action = NextAction.halt`.
2. Set `self.reason = f"halt:{reason}"`.

**`record_success(now)`:**

1. Reset `attempt = 0`, `last_error = None`.
2. Set `self.status = idle`, `self.next_action = NextAction.none`.
3. Clear `self.reason = ""`.

**`record_expiration(reason, now)`:**

1. Set `self.status = expired`, `self.next_action = NextAction.none`.
2. Set `self.reason = f"expired:{reason}"`.

**`record_superseded(reason, now)`:**

1. Set `self.status = superseded`, `self.next_action = NextAction.none`.
2. Set `self.reason = f"superseded:{reason}"`.

Every transition method sets `self.updated_at = now`.

### Integrate into `ExecutionState` (advisory only)

Add helper methods to `ExecutionState` that delegate to its
`recovery_state` and ensure it exists:

```python
def _ensure_recovery(self) -> RecoveryState
def note_tool_failure(self, obs: ToolObservation, *, now: datetime | None = None) -> None
def note_evidence_gap(self, missing: list[str], *, now: datetime | None = None) -> None
def note_load_bearing_ambiguity(self, ambiguity: str, *, now: datetime | None = None) -> None
```

These are runtime helpers, not yet wired into body.py. They populate
`self.recovery_state` so the state is **visible** in
`ExecutionState.to_dict()` but does not yet drive any runtime decision
in this PR.

For `now`, use `_utc_now()` (the existing module-level helper) as
default when caller passes `None`. This is the **only** allowed clock
access in the integration layer — the transition methods themselves
remain pure.

### Do NOT modify body.py retry path

`_work_contract_retry_sent` stays as-is in this PR. The rewire to
consume `ExecutionState.recovery_state.next_action` ships in Wave 2 #5b.
Add a single-line comment near the existing retry flag referencing Wave
2 #5b so the future integration is discoverable.

### Spec Checkpoint update

Update "### Execution State Type" subsection:
- Move `recovery_state` out of the placeholder-fields list, keeping
  `plan`, `step_history`, `errors`, `proposed_outcome` etc. still listed
  as placeholder until their own waves land.
- Add a short paragraph naming the `RecoveryStatus` / `NextAction`
  enums and noting that the state is populated advisory-only in this
  PR; `body.py` retry behavior is unchanged and will be rewired in
  Wave 2 #5b.

### Tests

New `images/body/test_recovery_state_machine.py` covering:

1. Default `RecoveryState` has `status=idle`, `next_action=none`,
   `attempt=0`.
2. `record_tool_failure` with `retry_safe` and budget available →
   `retrying`, `NextAction.retry`, `attempt=1`.
3. `record_tool_failure` with `retry_with_backoff` → `retrying`,
   `NextAction.retry`.
4. `record_tool_failure` with `not_retryable` → `failed`,
   `NextAction.fail`, regardless of budget.
5. `record_tool_failure` after budget exhausted (attempt ≥ max) →
   `failed` (or `escalated` if retryability was `unknown`).
6. `record_tool_failure` with `unknown` retryability + budget
   available → `fallback`, `NextAction.fallback`.
7. `record_tool_failure` with `obs.status != error` → no-op.
8. `record_evidence_gap` with non-empty list → `blocked`,
   `NextAction.block`, reason names missing evidence.
9. `record_load_bearing_ambiguity` → `clarifying`, `NextAction.clarify`.
10. `record_operator_halt` → `halted`, `NextAction.halt`.
11. `record_success` resets attempt, clears last_error, sets
    `status=idle`.
12. `record_expiration` and `record_superseded` set correct statuses
    with `NextAction.none`.
13. `RecoveryState.to_dict()` round-trips every field including
    `last_error` (when set) and enum string values.
14. Transition methods are pure: calling the same method twice with
    same inputs on a fresh `RecoveryState` produces equal state.
15. `ExecutionState.note_tool_failure` populates `recovery_state` via
    the transition method.

## Non-Scope

- **Wave 2 #5b** — rewiring `body.py` retry path to consume
  `recovery_state.next_action`. Do not remove `_work_contract_retry_sent`
  or change retry behavior. The state machine is advisory in this PR.
- **Wave 2 #1 objective builder / Wave 2 #2 strategy router** — do not
  change either. The router's `execution_mode` is not consumed by the
  recovery state machine in this PR.
- **Wave 2 #3 planner / #4 evaluator** — not touched.
- **Durable recovery state** — lives in memory. No new storage resource.
- Public API shapes (verdict signal, result frontmatter, PACT run
  projection, audit report, verify, admin audit enrichment), OpenAPI,
  web UI, feature registry, Go files.
- New retryability values. Wave 1 #2's enum is canonical.

## Acceptance Criteria

1. `RecoveryStatus` StrEnum with exactly 11 values as listed.
2. `NextAction` StrEnum with exactly 9 values as listed.
3. `RecoveryState` dataclass expanded with the 7 fields listed,
   deterministic `to_dict()` serializing enum string values and
   `last_error` nested dict.
4. Seven transition methods (`record_tool_failure`, `record_evidence_gap`,
   `record_load_bearing_ambiguity`, `record_operator_halt`,
   `record_success`, `record_expiration`, `record_superseded`)
   implemented with the transition rules listed above.
5. Transition methods are pure (no `datetime.now()` inside them; clock
   passed via `now` kwarg).
6. Three `ExecutionState` helper methods (`note_tool_failure`,
   `note_evidence_gap`, `note_load_bearing_ambiguity`) delegate to
   `recovery_state`.
7. `body.py` retry path is unchanged. `_work_contract_retry_sent`
   remains. Single comment added near it referencing Wave 2 #5b.
8. 15 test cases in `images/body/test_recovery_state_machine.py`.
9. Public API shapes preserved. Audit-report hash stable. No Go files
   modified.
10. `pytest images/tests/` and `go build ./cmd/gateway/` both succeed.
11. Spec "### Execution State Type" subsection updated per Scope.

## Review Gates

**Reject** if:
- Wave 2 #5b scope crosses in: `body.py` retry flag removed, retry
  path rewired, `_work_contract_retry_sent` replaced.
- New `RecoveryStatus` or `NextAction` values beyond the listed sets.
- Transition methods call `datetime.now()` or do I/O.
- Agent-proposed data drives transitions. Transitions must consume only
  runtime-typed inputs: `ToolObservation` (with runtime-classified
  `retryability`), evidence missing lists (from contract evaluators),
  ambiguity labels (from objective builder), operator halt signals
  (from runtime).
- Public API shapes change.
- Audit-report hash becomes unstable.
- Go files or objective-builder / strategy-router code modified.

**Ask for changes** if:
- Reason strings aren't in stable `reason:<label>` form.
- Transition rule order in `record_tool_failure` deviates from the
  Scope list.
- Test coverage misses any of the 15 cases.

## Files Likely To Touch

- `images/body/pact_engine.py` — expand `RecoveryStatus`, add
  `NextAction`, expand `RecoveryState`, add `ExecutionState` helper
  methods
- `images/body/work_contract.py` — re-export new enums
- `images/body/body.py` — add ONE comment near the
  `_work_contract_retry_sent` flag; no other changes
- `images/body/test_recovery_state_machine.py` (new)
- `docs/specs/pact-governed-agent-execution.md` — Checkpoint
  subsection update only

## ASK Compliance

- **#1 external enforcement / #3 complete mediation / #4 fail-closed** —
  state machine is pure runtime code. Advisory-only in this PR; existing
  mediation, enforcement, and retry budgets stay in place.
- **#2 audit** — no audit event shapes change. `recovery_state` becomes
  visible in `ExecutionState.to_dict()` but not surfaced on any public
  audit endpoint in this PR.
- **#5 runtime is a known quantity** — typed `RecoveryStatus` and
  `NextAction` make recovery decisions operator-inspectable instead of
  implicit in a single `_work_contract_retry_sent` flag. Net ASK gain.
- **#7 least privilege / #8 bounded operations** — retry budget
  (`max_attempts=3` default) is a bounded operation. When budget is
  exhausted, the machine transitions to `failed`/`escalated`, not
  unbounded retry.
- **#11 halts auditable and reversible** — `record_operator_halt`
  transitions to `halted` with `NextAction.halt`. Halted state is
  preserved in `recovery_state`. Resume is a future concern (not this
  PR) and will come from operator action, not agent self-action.
- **#12 halt authority asymmetric** — agents cannot self-resume from
  `halted`. The state machine has no `unhalt()` method. Only operator
  action (via a future admin path) can move out of `halted`.
- **#17 trust earned and monitored** — the retry budget is a bound,
  not a grant.
- **#22 unknown conflicts default to yield and flag** — `unknown`
  retryability routes to `fallback`/`escalated`, never to open-ended
  retry. Ambiguity routes to `clarifying`.
- **#25 identity mutations auditable** — recovery state is per-task,
  ephemeral. No durable memory writes, no graph writes.

**Forward-looking ASK notes for downstream waves (Wave 2 #5b rewire):**
- The body.py rewire must preserve tenet #12: no agent-initiated
  `unhalt`. If the machine is `halted`, the task continues only on
  operator action routed through the runtime's authority path.
- Retry budgets (`max_attempts`) must come from runtime/policy config,
  not from agent-authored metadata. Agent-authored `max_attempts`
  would violate least-privilege and bounded-operations.

## Out-of-band Notes For Codex

- Keep enum value sets exactly as listed. Do not add new states or
  actions; stop and report if you think one is needed.
- Transition methods are pure. Pass `now` as a kwarg; do not call
  `datetime.now()` inside them. The integration helper methods on
  `ExecutionState` may default `now` to `_utc_now()` as their only
  clock access.
- **Do not rewire `body.py` retry path.** The state machine is
  advisory in this PR. A future Wave 2 #5b PR consumes it.
- Reason strings use the stable `reason:<label>:<detail>` form so
  callers can grep for them.
- Commit style: plain commit title, no Co-Authored-By trailer.
- PR target: `main`.
