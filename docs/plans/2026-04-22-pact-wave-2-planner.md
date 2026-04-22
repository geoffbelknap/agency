# PACT Wave 2 #3 — Planner As Runtime Object

## Reference

- Spec: `docs/specs/pact-governed-agent-execution.md` → Wave 2 #3
- Wave: Wave 2 — Harness Capabilities, item #3
- Builds on: Wave 1 #1 `ExecutionState` and `Plan` placeholder; Wave 2 #1
  objective builder; Wave 2 #2 strategy router (`Strategy.needs_planner` is
  the trigger).
- Unblocks: Wave 2 #4 (general pre-commit evaluator) — evaluator checks plan
  completion against evidence.
- Defers: Wave 2 #3b (future) — runtime execution of plan steps. This PR
  produces plans; `body.py` does not yet consume them for step-by-step
  execution.

## Objective

Promote the Wave 1 #1 `Plan` placeholder into a runtime object with typed
ordered steps, expected evidence per step, required capabilities, and
approval points. Add a deterministic `build_plan()` that generates a plan
from typed objective + contract + strategy when
`strategy.needs_planner=True`. Populate `ExecutionState.plan` via the same
`from_task` / `attach_mission` hooks used by the objective builder and
strategy router.

Advisory in this PR: `body.py` does not yet execute plan steps. The plan
is visible in `ExecutionState.to_dict()` and provides the structure Wave
2 #4 consumes.

## Why

The strategy router emits `needs_planner: bool` but there's no typed plan
produced when it fires. Wave 2 #4 (general pre-commit evaluator) needs
`Plan.steps` and their `expected_evidence` to check whether execution
satisfied the plan — not just whether ad-hoc tool calls hit contract
evidence. The planner is that typed primitive.

Also: Wave 2 #2's forward ASK note is load-bearing here — plans for work
with `needs_approval=True` or `execution_mode=external_side_effect` must
have an approval step before any step that carries external-side-effect
capabilities. That invariant is enforced structurally in this PR.

## Scope (in this PR)

### Expand `Plan` into a runtime object

In `images/body/pact_engine.py` (or a sibling `images/body/planner.py` if
the new code exceeds ~150 lines — Codex's call).

Add a `PlanStep` dataclass and expand `Plan`:

```python
@dataclass(slots=True, frozen=True)
class PlanStep:
    """An ordered step in a typed PACT plan."""
    step_id: str
    phase: str               # preparation / execution / validation / approval
    summary: str
    required_capabilities: tuple[str, ...] = ()
    expected_evidence: tuple[str, ...] = ()
    requires_approval: bool = False
```

```python
@dataclass(slots=True)
class Plan:
    """Runtime-owned plan of typed ordered steps."""
    steps: tuple[PlanStep, ...] = ()
    stop_conditions: tuple[str, ...] = ()
    summary: str = ""
```

Deterministic `to_dict()` for both types. `Plan.to_dict()` serializes
`steps` as a list of step dicts preserving order.

### Add the builder

```python
def build_plan(
    objective: Objective,
    contract: WorkContract,
    strategy: Strategy,
    task: dict,
    *,
    mission: dict | None = None,
) -> Plan | None
```

Returns `None` when `strategy.needs_planner is False`. Otherwise returns a
typed `Plan`. Pure function — no I/O, no `datetime.now()`, no model calls.

### Plan templates (deterministic, by contract kind)

Plans are generated from typed inputs only. Activation content is not a
planning input. Each template is a minimal starter set; later waves can
enrich them.

**`code_change` template** (triggered by `strategy.needs_planner=True`):

```
step-01 preparation    "locate target files"
                       required_capabilities: ()
                       expected_evidence: ("target_files_identified",)
step-02 execution      "apply changes"
                       required_capabilities: ("write_file",)
                       expected_evidence: ("changed_file",)
step-03 validation     "run tests or build"
                       required_capabilities: ("execute_command",)
                       expected_evidence: ("validation_result",)
step-04 validation     "summarize changes"
                       expected_evidence: ("tool_result",)
stop_conditions: ("evidence_satisfied", "budget_exhausted",
                  "validation_failed")
summary: "Code change plan for {objective.statement[:80]}"
```

**`file_artifact` template**:

```
step-01 preparation    "gather inputs"
                       expected_evidence: ("tool_result",)
step-02 execution      "generate artifact"
                       required_capabilities: ("write_file",)
                       expected_evidence: ("artifact_path",)
step-03 validation     "validate artifact"
                       expected_evidence: ("tool_result",)
stop_conditions: ("evidence_satisfied", "budget_exhausted")
summary: "File artifact plan for {objective.statement[:80]}"
```

**`external_side_effect` template** (triggered when
`strategy.execution_mode == external_side_effect`; always has
`strategy.needs_planner=True` AND `strategy.needs_approval=True`):

```
step-01 preparation    "verify principal authority"
                       expected_evidence: ("authority_check",)
step-02 approval       "obtain operator approval"
                       requires_approval: True
                       expected_evidence: ("approval_decision",)
step-03 execution      "execute external operation"
                       required_capabilities: ("external_state",)
                       expected_evidence: ("side_effect_confirmation",)
step-04 validation     "confirm operation outcome"
                       expected_evidence: ("tool_result",)
stop_conditions: ("evidence_satisfied", "approval_denied",
                  "authority_check_failed", "budget_exhausted")
summary: "External side effect plan for {objective.statement[:80]}"
```

**`current_info` template** (triggered when the strategy router routes
high-risk `current_info` to `planned` — rare but possible):

```
step-01 preparation    "search for current source"
                       required_capabilities: ("web", "search")
                       expected_evidence: ("tool_result",)
step-02 validation     "verify source is current"
                       expected_evidence: ("current_source",)
step-03 execution      "formulate answer with citations"
                       expected_evidence: ("source_url",)
stop_conditions: ("evidence_satisfied", "budget_exhausted")
summary: "Current info plan for {objective.statement[:80]}"
```

**Unknown / unhandled contract kind** with `needs_planner=True` → return
`Plan(steps=(), stop_conditions=("evidence_satisfied",), summary="No
template for contract kind {kind}")`. Callers can treat an empty-steps
plan as "planner did not produce a real template" and route to clarify
in future waves.

### Structural ASK invariant (test-enforced)

For any plan emitted when `strategy.execution_mode == external_side_effect`
OR `strategy.needs_approval == True`:
- There MUST be at least one `PlanStep` with `requires_approval=True`.
- That approval step MUST precede every step whose
  `required_capabilities` contains `"external_state"` (or any future
  label in that class).

Test-enforced in Acceptance Criteria #7.

### Integrate into `ExecutionState.from_task` and `attach_mission`

After `build_strategy(...)` returns, call `build_plan(...)` and assign
`state.plan`. When `strategy is None` or `strategy.needs_planner is
False`, leave `state.plan = None`.

`attach_mission` rebuilds strategy AND plan when mission context is
attached.

### Spec Checkpoint update

Update the "### Execution State Type" subsection:
- Move `plan` out of the placeholder-fields list into the populated
  fields list.
- Add a short paragraph naming the planner builder, the four contract
  templates (`code_change`, `file_artifact`, `external_side_effect`,
  `current_info`), and the structural approval-before-side-effect
  invariant. Note that `body.py` does not yet execute plan steps
  (Wave 2 #3b).

### Tests

New `images/body/test_planner.py` covering:

1. `PlanStep` and `Plan` default construction and deterministic
   `to_dict()` round-trip.
2. `build_plan(..., strategy with needs_planner=False, ...)` returns
   `None`.
3. `code_change` plan with `needs_planner=True` has 4 steps in the
   expected phase / summary / evidence shape.
4. `file_artifact` plan has 3 steps with `artifact_path` expected
   evidence on the execution step.
5. `external_side_effect` plan has an approval step at position 2
   (after authority check) and an execution step AFTER the approval.
6. `current_info` planned variant has 3 steps.
7. **Structural ASK invariant:** for plans emitted with
   `execution_mode=external_side_effect`, assert
   (a) at least one step has `requires_approval=True` and
   (b) every step whose `required_capabilities` contains
   `"external_state"` is at an index AFTER the approval step index.
   Cover this test for `external_side_effect` template explicitly.
8. Unknown contract kind with `needs_planner=True` returns a `Plan`
   with empty `steps` and a summary naming the kind.
9. `build_plan` is a pure function (same inputs → equal plans).
10. `ExecutionState.from_task` populates `plan` only when
    `strategy.needs_planner=True`, leaves `None` otherwise.
11. `ExecutionState.attach_mission` rebuilds `plan`.
12. Steps have stable IDs (`step-01`, `step-02`, ...).

## Non-Scope

- **Wave 2 #3b runtime execution** — do NOT modify `body.py` to consume
  `plan.steps` for step-by-step execution. The plan is advisory in this
  PR.
- **Wave 2 #4 general pre-commit evaluator** — do not rewire existing
  evaluators to consume `plan.steps[*].expected_evidence`. Existing
  contract evaluators stay on their legacy evidence paths.
- Model-assisted plan refinement. Deterministic templates only.
- Plan revision mid-execution (replanning). That's recovery territory
  and ships in Wave 2 #5b's rewire.
- Cross-task plan caching.
- New contract kinds or plan templates beyond the four listed.
- New required_capability or evidence-classification labels. Use the
  labels already in the system from Wave 1 #2 (`tool_result`,
  `current_source`, `source_url`, `artifact_path`, `changed_file`,
  `validation_result`) plus the new ones named in the templates
  (`target_files_identified`, `authority_check`, `approval_decision`,
  `side_effect_confirmation`).
- Public API shapes (verdict signal, result frontmatter, PACT run
  projection, audit report, verify, admin audit enrichment), OpenAPI,
  web UI, feature registry, Go files.

## Acceptance Criteria

1. `PlanStep` frozen dataclass with 6 fields and deterministic
   `to_dict()`.
2. `Plan` dataclass expanded to 3 fields (`steps`, `stop_conditions`,
   `summary`) with deterministic `to_dict()`.
3. `build_plan` is pure, returns `None` when `needs_planner=False`,
   returns a typed `Plan` otherwise.
4. Four contract-kind templates (`code_change`, `file_artifact`,
   `external_side_effect`, `current_info`) implemented exactly per the
   specifications above. No extra templates.
5. Unknown contract kind with `needs_planner=True` returns a
   `Plan(steps=(), ...)` with a summary naming the kind.
6. `ExecutionState.from_task` and `ExecutionState.attach_mission`
   populate `state.plan` when `strategy.needs_planner=True`; leave
   `None` otherwise.
7. **Structural ASK invariant test**: plans emitted for
   `execution_mode=external_side_effect` or `needs_approval=True` must
   have at least one `requires_approval=True` step that precedes every
   step carrying `external_state` capabilities.
8. 12 test cases in `images/body/test_planner.py` covering the list
   above.
9. Public API shapes preserved. Audit-report hash stable. No Go files
   modified. `body.py` execution/tool-loop logic unchanged.
10. `pytest images/tests/` and `go build ./cmd/gateway/` succeed.
11. Spec "### Execution State Type" subsection updated per Scope.

## Review Gates

**Reject** if:
- Wave 2 #3b scope crosses in: `body.py` consumes `plan.steps` for
  step-by-step execution.
- Wave 2 #4 scope crosses in: existing contract evaluators rewired to
  consume `plan.steps[*].expected_evidence`.
- New plan templates beyond the four listed.
- New required_capability or evidence-classification label values
  invented. Use existing ones or those explicitly named in the
  templates.
- `build_plan` is not pure (reads clock, env, does I/O, calls models).
- Agent-proposed data (activation content, model output, tool
  observations) drives plan structure. The planner reads only objective,
  contract, strategy, and task metadata.
- External-side-effect plan lacks the approval-before-side-effect
  structural invariant.
- Public API shapes change.
- Audit-report hash becomes unstable.
- Go files modified.

**Ask for changes** if:
- Step IDs aren't the stable `step-NN` form (zero-padded).
- `Plan.summary` and `PlanStep.summary` aren't deterministic strings.
- Templates deviate from the specified step counts or phase labels.
- Test coverage misses the structural ASK invariant check.

## Files Likely To Touch

- `images/body/pact_engine.py` — expand `Plan`, add `PlanStep`,
  integrate into `ExecutionState.from_task` and `attach_mission`
- `images/body/planner.py` (new) — or inline in `pact_engine.py` if small
- `images/body/work_contract.py` — re-export `PlanStep`, `build_plan`
- `images/body/test_planner.py` (new)
- `docs/specs/pact-governed-agent-execution.md` — Checkpoint subsection
  update only

## ASK Compliance

- **#1 external enforcement / #3 complete mediation / #4 fail-closed** —
  planner is pure runtime code. Plans are advisory; enforcement stays
  in gateway/enforcer. When `strategy.needs_planner=False`,
  `state.plan=None` (fail-closed default).
- **#2 audit** — no audit event shapes change. Plans become visible in
  `ExecutionState.to_dict()` but not surfaced on any public audit
  endpoint in this PR.
- **#5 runtime is a known quantity** — typed ordered steps with
  expected evidence and required capabilities make runtime plans
  operator-inspectable. Net ASK gain.
- **#7 least privilege / #8 bounded operations** —
  `required_capabilities` on steps is advisory; no authority is
  granted. `stop_conditions` encode budget/approval/evidence limits.
- **#18 governance hierarchy inviolable from below** — plans are
  built from trusted sources (objective, strategy, contract, task
  metadata). Agent-proposed data cannot influence plan structure.
- **#20 synthesis cannot exceed individual authorization** —
  external-side-effect plans structurally require approval before any
  external-state step. Approval is a runtime-owned step, not an
  agent-self-reportable claim.
- **#22 unknown conflicts default to yield and flag** — unknown
  contract kind with `needs_planner=True` yields an empty-steps plan
  with a descriptive summary, not an invented template.

**Forward-looking ASK notes for downstream waves:**
- Wave 2 #3b (runtime execution) must enforce `stop_conditions` as
  hard halts. Budget-exhausted stop must not be bypassable by
  agent-proposed continuation.
- Wave 2 #4 (evaluator) must treat plans whose steps' expected
  evidence is not satisfied as non-committable, even if the
  contract-level `required_evidence` is otherwise present.

## Out-of-band Notes For Codex

- Keep enum-like string values consistent with existing ones. Do not
  invent new evidence classification labels outside the template
  specifications.
- `build_plan` is pure. No `datetime.now()`, no I/O, no model calls.
  Match the style of `build_objective` and `build_strategy`.
- Step IDs are zero-padded (`step-01`, `step-02`, ..., `step-10`). Use
  a simple helper or inline formatting.
- The structural ASK invariant (approval before side-effect) is
  load-bearing. Implement it once in the `external_side_effect`
  template; the test in Acceptance Criterion #7 verifies it.
- Commit style: plain commit title, no Co-Authored-By trailer.
- PR target: `main`. Single PR pattern — the brief is already committed
  on your branch. Do NOT commit the brief again. When committing
  implementation, `git add` only the implementation files.
