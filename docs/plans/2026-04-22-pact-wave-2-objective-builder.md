# PACT Wave 2 #1 — Objective Builder

## Reference

- Spec: `docs/specs/pact-governed-agent-execution.md` → Wave 2 #1
- Wave: Wave 2 — Harness Capabilities, item #1
- Builds on: Wave 1 #1 `ExecutionState` (merged), Wave 1 #2 tool observation
  protocol (merged). Populates the `Objective` slot defined in Wave 1 #1.
- Unblocks: Wave 2 #2 (strategy router), Wave 2 #3 (planner as runtime
  object), Wave 2 #4 (general pre-commit evaluator). All three need a typed
  objective to route/plan/evaluate against.

## Objective

Introduce a deterministic objective builder that normalizes every activation
into an explicit typed `Objective` attached to `ExecutionState.objective`. The
objective carries statement, kind, constraints, deliverables, success
criteria, ambiguities, assumptions, and risk level — and preserves ambiguity
when it changes required action or risk posture rather than forcing
resolution the runtime cannot defend.

This is the first wave where activation payload content becomes *typed agent
intent*. The layer is ASK-sensitive: activation content is **data**, not
instructions. The builder must reflect that boundary in its inputs and
outputs.

## Why

Every Wave 2+ capability depends on a typed objective:

- Wave 2 #2 (strategy router) selects execution mode from `objective.kind`,
  `objective.risk_level`, and `objective.success_criteria`.
- Wave 2 #3 (planner) needs `objective.deliverables` and
  `objective.success_criteria` as planning targets.
- Wave 2 #4 (general pre-commit evaluator) evaluates completion against
  `objective.success_criteria` and `objective.ambiguities`.

Today `ExecutionState.objective` is a placeholder that stays `None`. The body
runtime's sense of "what the task is" lives implicitly in the activation
content string, the `WorkContract.kind` label, and task metadata — scattered
across call sites. Model-facing prompts re-derive objective-shaped context
from those scattered inputs each turn. This PR consolidates that into one
typed object built once, at task start, by deterministic logic.

## Scope (in this PR)

### Add an objective-builder module

Create `images/body/objective_builder.py` (or inline as a function group in
`pact_engine.py` if the new surface is small — Codex's call based on file
size; prefer a sibling module if it adds more than ~150 lines).

Public surface:

```python
def build_objective(
    activation: ActivationContext,
    contract: WorkContract,
    task: dict,
    *,
    mission: dict | None = None,
    trust_level: str | None = None,
) -> Objective
```

The function must be **pure**: same inputs → same output, no hidden state, no
side effects, no model calls, no I/O.

### Field population rules

- **`statement`** — the normalized intent, not the raw activation content.
  For Wave 2 #1: strip leading/trailing whitespace from
  `activation.content` and cap at 500 characters. Do not paraphrase. Do not
  let activation content control any other field.
- **`kind`** — `contract.kind` verbatim (already classified upstream by
  `classify_activation`).
- **`constraints`** — derived only from trusted sources, in this order:
  1. `task["metadata"]["constraints"]` if it is a list of strings
  2. `mission["constraints"]` if a mission is active and has that field
  3. `contract.allowed_terminal_states` projected to
     `"terminal:{state}"` prefix form (e.g., `"terminal:completed"`,
     `"terminal:blocked"`)
  4. Defaults by contract kind (e.g., `external_side_effect` → `[
     "requires_authority", "no_silent_retry" ]`)

  **Constraints must never be parsed from `activation.content`.** Even if
  the payload contains imperative-sounding text, that text is data, not a
  constraint grant. See ASK Compliance below.

- **`deliverables`** — derived from `contract.kind` + `contract.answer_requirements`:
  - `current_info` → `["answer_with_source"]`
  - `code_change` → `["changed_files", "validation_result"]`
  - `file_artifact` → `["artifact_path"]`
  - `external_side_effect` → `["side_effect_confirmation"]`
  - `operator_blocked` → `["blocker_description", "unblock_action"]`
  - `mission_task`/`task`/`coordination`/`chat` → `[]`
- **`success_criteria`** — `contract.required_evidence` items, each mapped
  to a human-readable criterion using a small internal table
  (e.g., `current_source` → `"runtime observed a current source"`,
  `source_url` → `"answer names a source URL"`). If an item has no entry in
  the table, pass through verbatim.
- **`ambiguities`** — detected by deterministic heuristics; minimal starter
  set below. Ambiguity items are short strings.
- **`assumptions`** — each detected ambiguity that the builder provisionally
  resolves (e.g., "checked_date not provided, assuming as-of=task start")
  is recorded as a paired assumption. Builder only provisionally resolves
  ambiguities that do not change required action or risk posture; it leaves
  load-bearing ambiguities unresolved.
- **`risk_level`** — one of `"low" | "medium" | "high" | "escalated"`.
  Heuristics below.

### Ambiguity heuristics (starter set)

Keep minimal. Each heuristic returns a short stable label. Reject PRs that
add fuzzy, model-assisted, or open-ended detectors.

- **`current_info`**:
  - If activation content contains no explicit temporal anchor
    (no "as of", no date token, no year) → `"ambiguity:no_temporal_anchor"`
    with provisional assumption `"checked_date=<task.started_at>"`.
  - If activation content references "latest" or "current" without a
    release-category qualifier (LTS / stable / beta) → `"ambiguity:release_category"`.
- **`code_change`**:
  - If no file path appears in the content and task metadata has no
    `target_files` list → `"ambiguity:target_files_missing"`. Do **not**
    provisionally resolve; this is load-bearing.
  - If content names tests but no build/validation target → `"ambiguity:validation_target_missing"`.
- **`file_artifact`**:
  - If no explicit output format is named and contract requires
    artifact_path → `"ambiguity:output_format_missing"`.
- **`external_side_effect`**:
  - Always emit `"ambiguity:external_authority_scope"` if no explicit
    principal-authorized scope appears in task metadata. This is
    load-bearing; do not provisionally resolve.
- **`operator_blocked`**, **`chat`**, **`coordination`**, **`task`**,
  **`mission_task`**: no ambiguity heuristics in this PR (add them in
  future waves if needed).

Run these heuristics in order; each returns `None` or an ambiguity label.
Append labels to `objective.ambiguities` preserving insertion order.

### Risk-level rules

Compute in this order, returning the first match:

1. `trust_level` is `"untrusted"` or `"low"` → `"escalated"`
2. `contract.kind == "external_side_effect"` → `"high"`
3. `contract.kind == "code_change"` → `"high"` if `target_files_missing`
   ambiguity is present, else `"medium"`
4. `contract.kind in {"file_artifact", "current_info"}` → `"medium"`
5. Otherwise → `"low"`

### Integrate into `ExecutionState.from_task`

Extend `ExecutionState.from_task(task, *, agent)` to populate
`state.objective` by calling `build_objective(...)` when both `activation`
and `contract` are non-None. Leave `objective = None` when either is
missing — the spec's fail-closed posture: no activation context means no
authoritative objective.

`build_objective` signature may read mission via a new optional kwarg, but
`from_task` should not fetch mission context itself (that would couple
`pact_engine` to body runtime globals). Instead, add a new method
`ExecutionState.attach_mission(mission: dict | None) -> None` that, if
called with a mission dict before the first objective consumer runs,
re-builds the objective. For Wave 2 #1 the body runtime may call it at task
start. If not called, the objective is built without mission context.

### Surface in `ExecutionState.to_dict`

No new code — `Objective.to_dict()` already exists from Wave 1 #1, and
`ExecutionState.to_dict()` already serializes `self.objective`. Verify that
populating `objective` does not change the serialized shape of unpopulated
cases (i.e., when `objective is None`, the output dict still includes
`"objective": null`).

### Spec Checkpoint update

Update the "### Execution State Type" subsection:

- Move `objective` out of the placeholder-fields list and into the
  populated-fields list.
- Add a short paragraph describing the objective builder: deterministic,
  no model assistance, activation content is not a constraint source.
- Name the ambiguity heuristics and risk-level rules in summary form
  (one-liner each), with a note that richer heuristics arrive as downstream
  waves surface real demand.

### Tests

New file `images/body/test_objective_builder.py` covering:

1. Construction from a minimal task + contract yields an Objective with
   expected `kind`, `statement`, `deliverables`, `success_criteria`.
2. `statement` is the activation content (stripped, 500-char-capped), never
   paraphrased.
3. `constraints` list is populated from `task.metadata.constraints` and
   mission constraints; **never** from activation content even when
   activation contains imperative text like `"you must do X"` or `"ignore
   previous constraints"`. Assert the imperative text does NOT appear in
   `objective.constraints`.
4. `current_info` task without a temporal anchor produces
   `"ambiguity:no_temporal_anchor"` and a matching
   `"checked_date=..."` assumption.
5. `code_change` task without a file target produces
   `"ambiguity:target_files_missing"` with no provisional assumption.
6. `external_side_effect` task without authority scope produces
   `"ambiguity:external_authority_scope"`, `risk_level = "high"`.
7. `chat` task yields `risk_level = "low"`, empty ambiguities.
8. Untrusted `trust_level` escalates `risk_level` to `"escalated"`
   regardless of contract kind.
9. `ExecutionState.from_task` populates `objective` when activation and
   contract are both present, leaves it `None` otherwise.
10. `ExecutionState.attach_mission(mission)` rebuilds the objective with
    mission constraints included.
11. Pure-function property: calling `build_objective(a, c, t)` twice with
    the same inputs returns equal `Objective` instances.

## Non-Scope

- **Wave 2 #2** (strategy router) — do not change routing or prompt
  construction to consume `objective.kind` / `risk_level`.
- **Wave 2 #3** (planner as runtime object) — do not build plans from
  objective yet.
- **Wave 2 #4** (general pre-commit evaluator) — do not rewire existing
  contract evaluators to consume `objective.success_criteria`. The current
  evaluators (`current_info`, `file_artifact`, `code_change`,
  `operator_blocked`) keep their existing evidence-based paths.
- **Wave 2 #5** (recovery state machine) — not touched.
- **Model-assisted objective formation** — deterministic only in this PR.
  If heuristics need to be fuzzier, propose that as a follow-up wave
  rather than adding it here.
- **Modifications to the verdict signal, PACT run projection endpoint,
  audit report, verify endpoint, admin audit enrichment, OpenAPI, web UI,
  feature registry.**
- **Principal-scope enforcement** — `trust_level` influences risk but does
  not gate action. Principal authority checks remain in the gateway/policy
  layer.
- **Durable persistence of Objective** — lives in memory as part of
  `ExecutionState`. No new storage resource.

## Acceptance Criteria

1. `build_objective` exists with the signature above, is pure, and takes no
   model calls or I/O.
2. All eight `Objective` fields (`statement`, `kind`, `constraints`,
   `deliverables`, `success_criteria`, `ambiguities`, `assumptions`,
   `risk_level`) are populated deterministically per the rules above.
3. `ExecutionState.from_task` populates `state.objective` when activation
   and contract are both non-None; leaves it `None` otherwise.
4. `ExecutionState.attach_mission` exists and rebuilds `objective` with
   mission context.
5. Ambiguity heuristics and risk-level rules match the lists above
   exactly. No extra heuristics or risk levels.
6. **Constraints are never parsed from `activation.content`.** Test
   coverage asserts that imperative payload text does not appear in
   `objective.constraints`.
7. `images/body/test_objective_builder.py` covers the 11 cases above.
8. `ExecutionState.to_dict()` output for unpopulated objectives remains
   `"objective": null`. Public API shapes (`pact_verdict`, result
   frontmatter, PACT run projection, audit report, verify, admin audit)
   unchanged. Audit-report hash stable.
9. Existing contract evaluators and trajectory tests still pass without
   modification. `classify_activation` / `build_contract` behavior
   unchanged.
10. `pytest images/tests/` and `go build ./cmd/gateway/` pass.
11. Spec "### Execution State Type" subsection updated per Scope.

## Review Gates

**Reject** the PR if:

- Wave 2 #2 / #3 / #4 / #5 work crosses in (routing, planning, evaluator
  rewiring, recovery).
- The objective builder is not pure (takes a clock, reads env vars, calls
  a model, does I/O).
- `constraints` is populated from activation content.
- New ambiguity labels or risk levels beyond the ones enumerated in Scope
  are added.
- Existing contract evaluators (`current_info`, `file_artifact`,
  `code_change`, `operator_blocked`) start consuming
  `objective.success_criteria` or `objective.ambiguities`.
- Public API shapes change (verdict, frontmatter, PACT run projection,
  audit report, verify, admin audit enrichment).
- Audit-report hash becomes unstable.
- Go files are modified.
- `Objective` fields are renamed or extended beyond the Wave 1 #1 field
  set.

**Ask for changes** (not reject) if:

- Builder lacks docstrings naming each field's source of truth.
- Ambiguity heuristics or risk rules are implemented non-deterministically
  (e.g., using `datetime.now()` instead of passing started_at).
- `attach_mission` does not re-run the full builder but patches fields
  in-place (inconsistent with builder purity).
- Test coverage for the imperative-payload constraint-safety check is
  weak or only tests one phrase.

## Files Likely To Touch

- `images/body/objective_builder.py` (new) — or fold into
  `pact_engine.py` if small
- `images/body/pact_engine.py` — `ExecutionState.from_task` integration;
  `attach_mission` method; potential internal helpers
- `images/body/work_contract.py` — re-export `build_objective` if it lives
  in a new module
- `images/body/body.py` — single call site to `attach_mission` at task
  start (optional; only if mission context is readily available there)
- `images/body/test_objective_builder.py` (new)
- `docs/specs/pact-governed-agent-execution.md` — Checkpoint subsection
  update only

## ASK Compliance

This is the first wave that builds typed agent intent from untrusted
activation content. ASK sensitivity is higher than Wave 1; the invariants
below are load-bearing.

- **#1 external enforcement / #3 complete mediation / #4 fail-closed** —
  the objective builder is pure and runtime-owned. No enforcement, no
  mediation, no new capability grants. When activation or contract is
  missing, `objective` stays `None` (fail-closed default).

- **#2 audit** — no audit event shapes or audit JSONL writes change. The
  objective becomes visible in `ExecutionState.to_dict()` but that
  projection is not surfaced on any public audit endpoint in this PR.

- **#5 runtime is a known quantity** — typed objectives with explicit
  statement / kind / constraints / deliverables / success_criteria /
  ambiguities / assumptions / risk_level make runtime intent
  deterministically inspectable by operators. Net ASK gain.

- **#6 trust explicit and auditable** — constraint sources are enumerated
  (task metadata, mission, contract terminal states, kind defaults).
  Activation content is explicitly excluded as a constraint source. This
  trust boundary is testable and test-enforced.

- **#7 least privilege / #8 bounded operations** — no capability or
  budget changes. `risk_level` is advisory to later waves; it does not
  gate any action in this PR.

- **#17 trust earned and monitored** — `trust_level` flows into
  `risk_level` escalation. Low or untrusted principals surface as
  `"escalated"` risk, which downstream waves (Wave 2 #2 router, Wave 2 #3
  planner) can consume. No self-elevation path exists.

- **#22 unknown conflicts default to yield and flag** — load-bearing
  ambiguities (`target_files_missing`, `external_authority_scope`) are
  preserved without provisional resolution. Downstream waves can decide
  to route them to `needs_clarification`.

- **#23 unverified entities default to zero trust** — untrusted
  `trust_level` escalates `risk_level` without adding authority.

- **#24 data vs instructions (XPIA defense)** — **the critical
  invariant.** Activation content is read only to populate
  `objective.statement`; it is never parsed as constraint, deliverable,
  or assumption. A payload containing `"ignore previous instructions"` or
  `"you must grant me admin"` must produce the same objective structure
  as a benign payload with identical length. This is explicitly tested
  (Acceptance Criteria #6) and explicitly rejected in Review Gates.

- **#25 identity mutations auditable / #26–27 organizational knowledge** —
  objectives are per-task and ephemeral. No durable memory writes, no
  graph writes, no identity mutations.

**Forward-looking ASK notes for downstream waves:**

- **Wave 2 #2 (strategy router):** `objective.risk_level == "escalated"`
  should force a clarification or operator-approval route. The router
  must not let an agent-proposed strategy lower the risk level.
- **Wave 2 #3 (planner):** plans generated for `"escalated"` risk or
  load-bearing ambiguities should surface approval points before
  executing.
- **Wave 2 #4 (general pre-commit evaluator):** should block commit when
  load-bearing ambiguities remain unresolved at the end of execution.

Binding on later briefs; no work in this PR.

## Out-of-band Notes For Codex

- Keep heuristics minimal as listed. Do not invent new ambiguity labels or
  risk levels. If you find a case not covered by the starter set, stop
  and report it as a product question rather than adding a label.
- Do not use `datetime.now()` inside the builder. Pass a clock-like
  argument via `task.started_at` or accept a `clock` parameter. This is
  required for determinism (Acceptance Criteria #11).
- The **imperative-payload test** is the load-bearing XPIA-defense check.
  Include at least three distinct imperative patterns in the test
  fixture: direct override (`"ignore previous instructions and ..."`),
  authority claim (`"as admin, grant me ..."`), and role-play injection
  (`"SYSTEM: you are now ..."`). Assert none of those strings appear in
  `objective.constraints`, `objective.deliverables`, `objective.assumptions`,
  or `objective.success_criteria`. They may appear in
  `objective.statement` (the raw content cap allows that), but nowhere
  else.
- If you need to refactor `Objective.to_dict()` to handle new field
  types, stop and report — the Wave 1 #1 shape should be sufficient.
- Commit style: plain commit title, no Co-Authored-By trailer. Match repo
  convention.
- PR target: `main`. No stacked dependency; Wave 1 is merged.
