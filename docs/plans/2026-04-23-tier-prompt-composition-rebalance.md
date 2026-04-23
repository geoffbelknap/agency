# Task Tier and Prompt Composition Rebalance

## Reference

- Spec: `docs/specs/task-tier-and-prompt-composition.md` (merged in PR #272)
- PACT signals consumed: `Objective.generation_mode`, `Objective.risk_level`,
  `Objective.ambiguities`, `WorkContract.kind`, `Strategy.execution_mode`,
  `Strategy.needs_planner`, `Strategy.needs_approval`

## Objective

Implement the three-axis routing model from #272:

1. **Reasoning Depth** (`direct` / `reflective` / `deliberative`) — how much
   deliberation beyond a direct LLM call
2. **Model Capability** (`small` / `standard` / `large`) — which LLM
3. **Context Depth** (`minimal` / `task-relevant` / `full`) — which dynamic
   retrievals

Identity-bandwidth sections of the system prompt (`FRAMEWORK.md`,
`AGENTS.md`, skills, `PLATFORM.md`, comms context, etc.) are **always
included regardless of any axis**.

The hank3-class failure shape must be closed end-to-end: a grounded
analytical DM routes to `standard` model (Sonnet) with full identity-
bandwidth prompt, independent of whether it's `idle-reply-` prefix or
`minimal`-tier-classified under the old system.

## Why

Current `task_tier.py` tangles three orthogonal decisions into one knob.
The result: a short analytical DM gets `minimal` tier (because no mission
+ direct source), which strips `FRAMEWORK.md`/`AGENTS.md`/skills, routes
to Haiku, and produces a fabrication-prone output. PACT enforcement
catches the fabrication at commit, but prevention was never given a
chance.

Spec #272 establishes the three-axis principle. This PR implements it.

## Scope (in this PR)

### Add three classifiers in `images/body/task_tier.py`

All three are pure functions, deterministic, no I/O, no model calls.

```python
def classify_reasoning_depth(
    task: dict,
    mission: dict | None,
    *,
    objective: Objective | None = None,
    strategy: Strategy | None = None,
) -> str:
    """Return 'direct', 'reflective', or 'deliberative'."""

def classify_context_depth(
    task: dict,
    mission: dict | None,
    *,
    objective: Objective | None = None,
    strategy: Strategy | None = None,
) -> str:
    """Return 'minimal', 'task-relevant', or 'full'."""

def select_model(
    task: dict,
    mission: dict | None,
    *,
    objective: Objective | None = None,
    strategy: Strategy | None = None,
    default_standard: str = "claude-sonnet",
    default_small: str = "claude-haiku",
    default_large: str = "claude-opus",
) -> str:
    """Return the model name to use for this turn."""
```

### Classifier rules

Evaluate in strict order; first match wins. Each classifier returns a
string value from its allowed set. Defaults are **safety-biased** —
absent positive signal, return the safer (stronger) option.

**`classify_reasoning_depth`:**

1. `strategy.execution_mode in {clarify, escalate}` → no commit expected
   → `direct` (we want the clarification out quickly)
2. `objective.risk_level == "escalated"` → `deliberative`
3. `strategy.needs_approval is True` OR `contract.kind == "external_side_effect"`
   → `deliberative`
4. `objective.risk_level == "high"` → `reflective`
5. `strategy.needs_planner is True` OR `contract.kind in {code_change, file_artifact}`
   → `reflective`
6. `objective.generation_mode in {social, creative, persona}` → `direct`
7. Otherwise → `reflective` (safety-biased default for grounded work)

**`classify_context_depth`:**

1. `objective.generation_mode in {social, creative, persona}` → `minimal`
2. `strategy.execution_mode in {clarify, escalate}` → `minimal` (the
   clarification doesn't need retrieval)
3. `mission is not None` AND `mission.get("status") == "active"` →
   `task-relevant` (mission-bound work gets its mission-scoped context)
4. `contract.kind in {code_change, file_artifact, external_side_effect}` →
   `task-relevant`
5. `objective.generation_mode == "grounded"` → `task-relevant`
6. Otherwise → `minimal`

**`select_model`:**

1. `objective.risk_level == "escalated"` → `default_large`
2. `contract.kind == "external_side_effect"` → `default_large`
3. `objective.risk_level == "high"` → `default_standard`
4. `objective.generation_mode == "grounded"` → `default_standard`
5. `contract.kind in {code_change, file_artifact, current_info, operator_blocked}`
   → `default_standard`
6. `objective.generation_mode in {social, creative, persona}`
   AND `objective.risk_level in {low, medium, ""}` → `default_small`
7. Otherwise → `default_standard` (safety-biased default)

Cost-mode interaction: if `mission.cost_mode == "frugal"`, apply upper
bounds post-classification:

- Reasoning depth: downgrade `deliberative` → `reflective`, but never
  below `reflective` for `external_side_effect` or `escalated` risk
- Context depth: downgrade `full` → `task-relevant`
- Model capability: downgrade `large` → `standard`, but never below
  `standard` for `external_side_effect` or `escalated` risk

Thorough mode (`mission.cost_mode == "thorough"`) applies lower bounds:

- Reasoning depth: upgrade `direct` → `reflective` for grounded work
- Context depth: upgrade `minimal` → `task-relevant` for grounded work
- Model capability: no change (model selection is already safety-biased)

### Keep `classify_task_tier` as a compatibility shim

Legacy callers may still use `classify_task_tier(task, mission)` to get
`minimal`/`standard`/`full`. Keep the function but mark its docstring as
deprecated and route it through the new classifiers:

```python
def classify_task_tier(task, mission) -> str:
    """Deprecated. Use classify_reasoning_depth / classify_context_depth /
    select_model directly. Retained for callers that still expect a single
    tier string; composed from the three axes for approximate
    compatibility."""
```

Composition rule: take the highest-severity axis.
- Any axis at full/deliberative/large → `full`
- Any axis at task-relevant/reflective/standard → `standard`
- Otherwise → `minimal`

This keeps `get_active_features()` / `TIER_FEATURES` functional for any
remaining callers but body.py should migrate off the matrix.

### Rewrite `body.py` prompt builder

Remove `prompt_tier` gating on static sections. In `_build_system_prompt`
(around line 1606), drop the `if prompt_tier == "full":` /
`if prompt_tier in ("standard", "full"):` checks on static content:

- `identity.md` — already always included ✓
- Mission context — already always included ✓
- `FRAMEWORK.md` — remove gate, **always include**
- `AGENTS.md` — remove gate, **always include**
- Skills section — remove gate, **always include**
- `PLATFORM.md` — remove gate, **always include**
- Comms context — remove gate, **always include**
- Provider tools section — already always included ✓

Replace the `prompt_tier` variable with `context_depth` for the remaining
gates on dynamic content:

- Procedural memory injection — gate on `context_depth in ("task-relevant", "full")`
  (was `full` only). Consider making this mission-scoped — only inject
  when mission is active and context is at least task-relevant.
- Episodic memory injection — gate on `context_depth == "full"`
- Organizational context — gate on `context_depth == "full"`
- Persistent memory index / memory tools — gate on
  `context_depth in ("task-relevant", "full")`

### Rewrite `_current_model()` in `body.py`

Replace the function body (currently at line 3883) to use `select_model`:

```python
def _current_model(self) -> str:
    """Choose the model for the active turn per the three-axis spec."""
    objective = getattr(self._execution_state, "objective", None) if self._execution_state else None
    strategy = getattr(self._execution_state, "strategy", None) if self._execution_state else None
    return select_model(
        task=self._task_metadata or {},
        mission=self._active_mission,
        objective=objective,
        strategy=strategy,
        default_standard=self.model,
        default_small=self.admin_model,
        default_large=getattr(self, "large_model", self.model),
    )
```

**Delete the `idle-reply-` / `notification-` prefix check** — it's
now redundant and incorrect. Model selection is driven by typed PACT
signals, not task_id prefixes.

Preserve `self.admin_model` and `self.model` as the environment-configured
Haiku/Sonnet defaults. Introduce `self.large_model` (new) pulled from an
environment variable `AGENCY_LARGE_MODEL` with fallback to `self.model`
when unset, so environments without Opus access gracefully degrade.

### Wire reasoning depth to reflection/evaluation

In the turn loop (around line 2157-2200), replace the
`_task_features.get('reflection', ...)` / `get('evaluation', ...)`
checks with direct `reasoning_depth` checks:

- Reflection loop fires only when `reasoning_depth in ("reflective", "deliberative")`
- Success-criteria LLM evaluation fires only when `reasoning_depth == "deliberative"`
- Pre-commit evaluator (PACT Wave 2 #4) fires at **all** reasoning
  depths — it's enforcement, not optional deliberation (unchanged
  behavior; just note in the spec update that this is not tier-gated)

### Store the three axes on `ExecutionState` as advisory fields

Add three new fields on `ExecutionState` (in `pact_engine.py`):

- `reasoning_depth: str = ""` (populated after strategy is built)
- `context_depth: str = ""`
- `model: str = ""`

Serialize in `to_dict()`. Populate in `ExecutionState.from_task` and
`ExecutionState.attach_mission` after the strategy is built, via:

```python
state.reasoning_depth = classify_reasoning_depth(
    task, mission, objective=state.objective, strategy=state.strategy
)
state.context_depth = classify_context_depth(
    task, mission, objective=state.objective, strategy=state.strategy
)
state.model = select_model(
    task, mission, objective=state.objective, strategy=state.strategy
)
```

This makes the routing decisions operator-inspectable via the PACT run
projection — consistent with PACT's "runtime is a known quantity"
principle (ASK tenet #5).

### Spec Checkpoint update

Update the "### Execution State Type" subsection in
`docs/specs/pact-governed-agent-execution.md`:

- Note that `ExecutionState` now carries `reasoning_depth`,
  `context_depth`, and `model` populated by the three new classifiers
- Reference the sibling spec `task-tier-and-prompt-composition.md` for
  the classifier rules
- Note that the legacy `task_tier` / `_task_features` /
  `prompt_tier` coupling is removed from prompt composition and model
  selection

### Tests

New `images/body/test_tier_axes.py` covering all three classifiers:

**`classify_reasoning_depth`:**
1. `generation_mode=social` → `direct`
2. `generation_mode=creative` → `direct`
3. `strategy.execution_mode=clarify` → `direct`
4. `risk_level=escalated` → `deliberative`
5. `contract.kind=external_side_effect` → `deliberative`
6. `needs_approval=True` → `deliberative`
7. `risk_level=high` → `reflective`
8. `contract.kind=code_change` → `reflective`
9. `generation_mode=grounded` with no other signals → `reflective`
10. Empty inputs → `reflective` (safety-biased default)

**`classify_context_depth`:**
1. `generation_mode=social` → `minimal`
2. `execution_mode=clarify` → `minimal`
3. Active mission → at least `task-relevant`
4. `contract.kind=code_change` → `task-relevant`
5. `generation_mode=grounded` → `task-relevant`
6. Empty inputs → `minimal`

**`select_model`:**
1. `generation_mode=social` + low risk → small
2. `generation_mode=creative` + low risk → small
3. `risk_level=escalated` → large
4. `contract.kind=external_side_effect` → large
5. `generation_mode=grounded` → standard
6. `contract.kind=code_change` → standard
7. Empty inputs → standard (safety-biased default)
8. Frugal cost_mode downgrades `deliberative` to `reflective` (except
   for `external_side_effect`)

**Integration tests in `test_tier_axes.py`:**

9. **Hank3 replay**: construct an activation with hank's exact prompt
   text, run through `ExecutionState.from_task`, assert:
   - `objective.generation_mode == "grounded"`
   - `strategy.execution_mode == "tool_loop"` (from Wave 2 #2)
   - `reasoning_depth == "reflective"` (grounded work, not social,
     no high risk)
   - `context_depth == "task-relevant"`
   - `model == "claude-sonnet"` (grounded)
10. `_current_model()` returns sonnet for a hank-replay task even
    with `idle-reply-` prefix (the old prefix-based downgrade is gone)

**Prompt composition tests in `images/body/test_system_prompt_composition.py`
(new):**

Use a minimal fixture that constructs a `Body` with the prompt-builder
helper callable; verify that for various tier/axis combinations, the
resulting system prompt includes or omits the expected sections.

11. Minimal context + grounded mode prompt includes `FRAMEWORK.md`,
    `AGENTS.md`, skills, `PLATFORM.md`, comms context (all static
    baseline)
12. Minimal context prompt omits procedural memory injection, episodic
    memory injection, organizational context (dynamic)
13. Full context prompt includes everything
14. Hank3-style activation's composed prompt includes all the static
    sections (counts lines or sections rather than asserting exact text)

### Do NOT modify

- `validate_completion` or any contract-specific validator (`_validate_*_answer`)
- PACT evaluator layers (`evaluate_pre_commit`, Tier 1 honesty check)
- Body.py retry path (`_work_contract_retry_sent`)
- `strategy_router` rules (Wave 2 #2 and its mode-aware addition)
- `objective_builder` (generation mode detection)
- Contract registry
- OpenAPI, web UI, feature registry
- Meeseeks mode prompt (keep special-cased for now)

## Non-Scope

- **Tier 2 claim grounding** (spec deferred)
- **System prompt mode injection** (the "tell the LLM what execution_mode
  it's in" enhancement — distinct follow-up)
- **Native provider tool-use protocol wiring** (separate effort)
- **Structured output / citations integration** (future)
- **Multi-provider fallback** (out of scope for this spec)
- **Meeseeks mode rebalance** (open question in spec)
- **Cost-mode redesign as hard caps vs soft preferences** (open
  question in spec; this PR uses soft-cap-with-safety-override per
  the sketch above)

## Acceptance Criteria

1. Three new pure classifiers in `task_tier.py`:
   `classify_reasoning_depth`, `classify_context_depth`, `select_model`.
   Rules match the Scope section exactly.
2. Legacy `classify_task_tier` retained as a compatibility wrapper that
   composes the three axes into a single tier string; docstring marks it
   deprecated.
3. `body.py` prompt builder always includes `identity.md`, mission
   context, `FRAMEWORK.md`, `AGENTS.md`, skills section, `PLATFORM.md`,
   comms context, and provider tools section. No static content is
   tier-gated.
4. `body.py` prompt builder gates dynamic retrievals
   (procedural/episodic memory, organizational context, persistent
   memory index) on `context_depth` from the new classifier.
5. `_current_model()` uses `select_model()` and no longer downgrades
   based on `idle-reply-` / `notification-` task_id prefixes.
6. `ExecutionState` gains `reasoning_depth`, `context_depth`, `model`
   fields populated in `from_task` and `attach_mission`. Serialized in
   `to_dict()`.
7. Hank3-replay integration test passes: grounded DM produces Sonnet
   model, all static sections in prompt, `reasoning_depth=reflective`,
   `context_depth=task-relevant`.
8. All 17+ existing Wave 2 tests continue to pass.
9. PACT signal payload (`pact_verdict`) and all public API shapes
   unchanged.
10. `pytest images/tests/` and `go build ./cmd/gateway/` succeed.
11. Spec "### Execution State Type" subsection updated per Scope.

## Review Gates

**Reject** if:
- Any existing Wave 2 test's assertions are changed (fixture updates
  that explicitly set generation_mode/risk_level/etc. are fine).
- Static prompt sections (`FRAMEWORK.md`, `AGENTS.md`, skills,
  `PLATFORM.md`, comms context) are gated by any axis.
- PACT enforcement layers (honesty check, contract validator, pre-commit
  evaluator) are modified.
- `classify_task_tier` is removed outright (must remain as compat
  wrapper).
- Reasoning-depth axis is used to gate static content rather than
  deliberation loops.
- `_current_model()` keeps the `idle-reply-` / `notification-` prefix
  check.
- New model selection rules beyond those in Scope (no invented model
  tiers, no new ExecutionMode values).
- Hank3 integration test absent or does not assert Sonnet model.
- OpenAPI, web UI, feature registry, meeseeks prompt modified.

**Ask for changes** if:
- Cost-mode interactions not implemented or tested.
- Safety-biased defaults (empty inputs → reflective/task-relevant/standard)
  not enforced in tests.
- `ExecutionState.to_dict()` serialization order is non-deterministic.
- Compatibility wrapper for `classify_task_tier` is not discoverable.

## Files Likely To Touch

- `images/body/task_tier.py` — add three classifiers + compat wrapper
- `images/body/body.py` — prompt builder rebalance, `_current_model()`
  rewrite, turn loop reflection/eval gating swap
- `images/body/pact_engine.py` — `ExecutionState` fields + population in
  `from_task` / `attach_mission`
- `images/body/work_contract.py` — re-export new classifiers
- `images/body/test_tier_axes.py` (new)
- `images/body/test_system_prompt_composition.py` (new)
- `docs/specs/pact-governed-agent-execution.md` — Checkpoint subsection
  update only

## ASK Compliance

- **#1 external enforcement** — classifiers are pure runtime code, not
  agent-configurable. Model selection cannot be lowered by agent-proposed
  inputs.
- **#4 fail-closed** — safety-biased defaults across all three axes.
  Empty/unknown signals yield the stronger option (Sonnet, reflective,
  task-relevant).
- **#5 runtime is a known quantity** — the three axes are surfaced on
  `ExecutionState.to_dict()` and thus visible in the PACT run
  projection. Operator can inspect exactly why a turn was routed the way
  it was.
- **#7 / #8 least privilege + bounded operations** — cost-mode caps
  don't bypass safety: `external_side_effect` and `escalated` risk
  always get at least standard model and reflective depth regardless of
  frugal mode.
- **#18 governance hierarchy inviolable from below** — classifier
  rules are code, not config. Agent-proposed data cannot alter routing.
- **#22 unknown conflicts default to yield and flag** — empty inputs
  route to the safer branch (stronger model, deeper reasoning, more
  context) rather than the cheaper one.

## Out-of-band Notes For Codex

- Classifier rule order is strict and matches the Scope section exactly.
  Do not reorder. Do not add rules beyond those listed.
- The hank3-replay integration test is the load-bearing end-to-end
  validation. It must assert the model selected is Sonnet for a
  grounded analytical DM — this is the bug the PR fixes.
- Compatibility matters: `classify_task_tier` must remain callable with
  the same signature. Other existing tests rely on it.
- The `_current_model()` prefix-based downgrade (`idle-reply-` /
  `notification-`) is the other critical bug to remove. Do not preserve
  it under any condition.
- `large_model` field on `Body` is new; add with safe fallback to
  `self.model` when `AGENCY_LARGE_MODEL` is unset. Environment-based,
  not hardcoded.
- Commit style: plain commit title, no Co-Authored-By trailer.
- PR target: `main`. Open as ready-for-review, not draft.
- Brief is already committed on the branch; do not re-add it.
