# PACT Wave 2 #2 — Strategy Router

## Reference

- Spec: `docs/specs/pact-governed-agent-execution.md` → Wave 2 #2
- Wave: Wave 2 — Harness Capabilities, item #2
- Builds on: Wave 2 #1 objective builder (merged). Consumes
  `Objective.kind`, `Objective.risk_level`, `Objective.ambiguities`.
- Unblocks: Wave 2 #3 (planner), Wave 2 #4 (general pre-commit evaluator).

## Objective

Introduce a deterministic strategy router that chooses an explicit execution
mode from the typed `Objective` plus contract, task, and mission context.
Populates a new `Strategy` object on `ExecutionState` with fields: execution
mode, planner requirement, approval requirement, and advisory hints for
model tier / tool scope / memory / budget.

Routing is runtime-owned. Per the forward ASK note set by Wave 2 #1:
**an agent-proposed strategy can never lower the risk level or downgrade
the mode**. The router reads from typed objective + trusted context only;
activation content is not a routing input.

## Why

The body runtime today routes implicitly through prompt construction and
tool availability. Wave 2 #3 (planner) and Wave 2 #4 (general evaluator)
both need an explicit "what execution mode are we in" signal to decide
whether to produce plans, gate commits, or route to clarification.

## Scope (in this PR)

### Add a `Strategy` type and an `ExecutionMode` enum

In `images/body/pact_engine.py` (or a sibling `images/body/strategy_router.py`
— Codex's call based on file size; prefer sibling if new code > ~150 lines).

```python
class ExecutionMode(StrEnum):
    trivial_direct = "trivial_direct"
    tool_loop = "tool_loop"
    planned = "planned"
    clarify = "clarify"
    escalate = "escalate"
    external_side_effect = "external_side_effect"
    delegated = "delegated"
```

```python
@dataclass(slots=True)
class Strategy:
    execution_mode: ExecutionMode
    needs_planner: bool
    needs_approval: bool
    notes: tuple[str, ...] = ()  # advisory hints: tool_scope, model_tier, budget
```

Add `Strategy.to_dict()` for deterministic serialization.

### Add `ExecutionState.strategy` field

Add `strategy: Strategy | None = None` to `ExecutionState`. Populated by
the router when `objective` is populated; stays `None` otherwise
(fail-closed). Surfaces through `ExecutionState.to_dict()`.

### Add the router

```python
def build_strategy(
    objective: Objective,
    contract: WorkContract,
    task: dict,
    *,
    mission: dict | None = None,
) -> Strategy
```

Pure function: same inputs → same output, no I/O, no `datetime.now()`,
no model calls.

### Routing rules (strict order)

Evaluate top to bottom; first match wins.

1. `objective.risk_level == "escalated"` → `Strategy(execution_mode=escalate,
   needs_planner=False, needs_approval=True, notes=("reason:escalated_risk",))`
2. Load-bearing ambiguities present
   (`target_files_missing`, `external_authority_scope`) → `Strategy(execution_mode=clarify,
   needs_planner=False, needs_approval=False, notes=("reason:load_bearing_ambiguity",))`
3. `contract.kind == "external_side_effect"` → `Strategy(execution_mode=external_side_effect,
   needs_planner=True, needs_approval=True, notes=("reason:external_side_effect",))`
4. `contract.kind == "chat"` → `Strategy(execution_mode=trivial_direct,
   needs_planner=False, needs_approval=False, notes=("reason:chat",))`
5. `contract.kind == "operator_blocked"` → `Strategy(execution_mode=trivial_direct,
   needs_planner=False, needs_approval=False, notes=("reason:operator_blocked",))`
6. `objective.risk_level == "high"` (other contract kinds) →
   `Strategy(execution_mode=planned, needs_planner=True, needs_approval=False,
   notes=("reason:high_risk",))`
7. `contract.kind == "code_change"` → `Strategy(execution_mode=planned,
   needs_planner=True, needs_approval=False, notes=("reason:code_change_default",))`
8. Default → `Strategy(execution_mode=tool_loop, needs_planner=False,
   needs_approval=False, notes=("reason:default_tool_loop",))`

Do NOT add extra routing rules, execution modes, or advisory hints beyond
these. Delegated mode is defined in the enum but not emitted by any rule
in this PR; future delegation work enables it.

### Integrate into `ExecutionState.from_task` and `attach_mission`

After `build_objective(...)` populates `state.objective`, populate
`state.strategy` via `build_strategy(...)` in the same code paths. If
`state.objective` is `None`, leave `state.strategy = None`.

### Spec Checkpoint update

Update "### Execution State Type" subsection:
- Move `strategy` out of the placeholder-fields list... wait — `strategy`
  is **not** in the Wave 1 #1 placeholder list. Document that this PR
  adds `strategy` as a new `ExecutionState` field, and describe the
  router with one-line summaries of the routing rules. Explicitly note
  that advisory hints (tool scope / model tier / memory / budget) are
  surfaced but not yet enforced by any runtime gate.

### Tests

New `images/body/test_strategy_router.py` covering:

1. `chat` kind → `trivial_direct`, no planner, no approval.
2. `current_info` at medium risk → `tool_loop`.
3. `code_change` without load-bearing ambiguity at medium risk →
   `planned`, needs_planner.
4. `code_change` with `target_files_missing` → `clarify`.
5. `external_side_effect` without authority scope (escalated risk
   from untrusted trust level) → `escalate` (rule 1 wins over rule 3).
6. `external_side_effect` with authority scope, normal trust → `external_side_effect`,
   needs_approval.
7. `operator_blocked` kind → `trivial_direct`.
8. `high` risk on `file_artifact` (no ambiguity) → `planned`.
9. `untrusted` trust level escalates every kind → `escalate`.
10. `build_strategy` is a pure function (same inputs → equal output).
11. `ExecutionState.from_task` populates `strategy` when `objective` is
    populated; leaves it `None` otherwise.
12. `ExecutionState.attach_mission` rebuilds both `objective` and
    `strategy`.

## Non-Scope

- **Wave 2 #3** (planner runtime object) — do not build plans when
  `needs_planner=True`. The strategy just flags the requirement.
- **Wave 2 #4** (pre-commit evaluator) — do not rewire existing contract
  evaluators to consume `strategy.*`. Evaluators stay on their legacy
  evidence paths.
- **Wave 2 #5** (recovery state machine) — separate brief, separate PR.
- Enforcement of `notes` hints (tool scope / model tier / memory / budget)
  — advisory only in this PR. Mediation layer stays the authority.
- Changes to body.py routing, prompt construction, or tool registration.
  The runtime does not yet consume `strategy.execution_mode`.
- Delegated-mode emission — the enum value exists but no rule selects it.
  Delegation routing arrives in a future wave.
- Public API shapes (verdict signal, result frontmatter, PACT run
  projection, audit report, verify, admin audit enrichment), OpenAPI,
  web UI, feature registry, Go files.

## Acceptance Criteria

1. `ExecutionMode` StrEnum with exactly the 7 values above.
2. `Strategy` dataclass with the 4 fields, docstrings, deterministic
   `to_dict()`.
3. `ExecutionState.strategy` field added; `to_dict()` serializes it.
4. `build_strategy` is pure, no I/O, no model calls, no `datetime.now()`.
5. 8 routing rules match the Scope list exactly. No extras.
6. Integration: `ExecutionState.from_task` and `ExecutionState.attach_mission`
   populate `strategy` when `objective` is populated; leave `None`
   otherwise.
7. 12 test cases in `images/body/test_strategy_router.py`.
8. Public API shapes preserved. Audit-report hash stable. No Go files
   modified.
9. `pytest images/tests/` and `go build ./cmd/gateway/` both succeed.
10. Spec "### Execution State Type" subsection updated with the new
    `strategy` field and router summary.

## Review Gates

**Reject** if:
- Wave 2 #3 / #4 / #5 scope creeps in (plans produced, evaluator
  rewired, recovery state populated beyond what Wave 1 #1 already holds).
- Extra routing rules, `ExecutionMode` values, or `Strategy` fields are
  added beyond the list.
- Agent-proposed data enters the router — activation content, model
  output, or tool observations must not influence routing decisions in
  this PR.
- body.py routing, prompt construction, or tool registration changes.
- Public API shapes change.
- Audit-report hash becomes unstable.
- Go files modified.

**Ask for changes** if:
- Routing rules aren't in a single well-named function matching the
  Scope order 1-8.
- Test coverage misses any of the 12 cases.
- `notes` strings are freeform instead of `reason:<label>` form.

## Files Likely To Touch

- `images/body/strategy_router.py` (new) — or inline in `pact_engine.py`
  if small
- `images/body/pact_engine.py` — add `Strategy`, `ExecutionMode`, extend
  `ExecutionState` with `strategy` field, integrate into `from_task` and
  `attach_mission`
- `images/body/work_contract.py` — re-export new types
- `images/body/test_strategy_router.py` (new)
- `docs/specs/pact-governed-agent-execution.md` — Checkpoint subsection
  update only

## ASK Compliance

- **#1 external enforcement / #3 complete mediation / #4 fail-closed** —
  router is pure runtime code. Strategy is advisory state; mediation
  and capability enforcement stay in gateway/enforcer. When `objective`
  is missing, `strategy = None` (fail-closed default).
- **#2 audit** — no audit event shapes change. `strategy` becomes
  visible in `ExecutionState.to_dict()` but not surfaced on any public
  audit endpoint in this PR.
- **#5 runtime is a known quantity** — explicit `ExecutionMode` and
  planner/approval flags make runtime routing decisions operator-
  inspectable. Net ASK gain.
- **#7 least privilege** — `notes` hints for tool scope / memory /
  budget are advisory. Enforcement stays in mediation.
- **#17 trust earned and monitored** — escalated `risk_level` from
  Wave 2 #1's `trust_level` input routes to `escalate` mode with
  `needs_approval=True`. No self-elevation path.
- **#18 governance hierarchy inviolable from below** — the router
  reads only trusted sources (objective, contract, mission, task
  metadata). Activation content and model outputs cannot downgrade a
  route. The router is runtime-owned.
- **#22 unknown conflicts default to yield and flag** — load-bearing
  ambiguities route to `clarify` before any execution.

**Forward-looking ASK notes for downstream waves:**
- Wave 2 #3 planner must treat `strategy.needs_approval=True` as a
  hard gate: plans with approval steps cannot execute past the
  approval point without recorded operator action.
- Wave 2 #4 evaluator must treat `strategy.execution_mode == clarify`
  as a signal that commit-without-clarification is invalid.

## Out-of-band Notes For Codex

- Keep `ExecutionMode` values exactly as listed. Do not add new values
  for situations not covered; stop and report if you think one is
  needed.
- `notes` entries use the stable `reason:<label>` form — callers can
  grep for them.
- The router is pure. No `datetime.now()`, no I/O, no model calls.
  Wave 2 #1's `build_objective` is the precedent — match its style.
- Commit style: plain commit title, no Co-Authored-By trailer.
- PR target: `main`.
