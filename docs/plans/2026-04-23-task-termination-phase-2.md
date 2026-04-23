# Task Termination Phase 2 — Dual Termination + Hard Turn Cap

## Reference

- Spec: `docs/specs/model-native-task-termination.md` (#276, merging)
- Spec migration phase: Phase 2 (observe-and-act plus hard cap)
- Motivating observation: the hank6 runaway loop. Six `send_message`
  calls in six minutes, no `complete_task`, no termination, burning
  budget until operator halt.

## Objective

Implement Phase 2 of the task termination migration:

1. Add a provider-agnostic **TurnOutcome** detector with an Anthropic
   adapter that reads `stop_reason` from the provider response.
2. Modify the body runtime's turn loop to exit on **either** a
   legacy `complete_task` call **or** a `TurnOutcome.is_terminal`
   signal from the detector. First signal wins.
3. When the loop exits via `is_terminal` (not via `complete_task`),
   the runtime internally invokes the same commit hook that
   `complete_task` would — so PACT evaluation, audit, and
   artifact writing all run identically.
4. Enforce a **hard turn cap** (default 8 turns per task) as a
   permanent safety net. When hit, the task terminates with
   `verdict=blocked, reasons=["runtime:turn_limit_exceeded"]`
   regardless of model behavior.
5. Capture `stop_reason` in `pact_verdict` signal payload, result
   artifact frontmatter, and the PACT run projection — additive
   fields, no schema breaks.

This is the specific PR that closes the hank6 loop class.

## Why

Per the merged spec (#276), the current `complete_task`-as-agent-tool
pattern is a counter-trained legacy design. Modern LLMs are trained to
terminate via natural stop signals; requiring them to call an explicit
completion tool produces exactly the runaway loops we observed.

Phase 2 introduces dual termination (both legacy and new paths
coexist) and the hard turn cap (independent safety net). Phases 3/4
are cleanup that follow once Phase 2 is validated in the field.

## Scope (in this PR)

### Add `images/body/completion_detector.py`

New module, sibling to `pact_engine.py`. Pure functions, no I/O, no
runtime dependencies beyond standard library + existing Agency typed
state.

```python
@dataclass(frozen=True)
class TurnOutcome:
    """Runtime's interpretation of a provider response."""
    is_terminal: bool
    has_pending_tool_use: bool
    final_text: str
    stop_reason: str

def detect_anthropic(response: dict) -> TurnOutcome:
    """Parse an Anthropic API response into a TurnOutcome."""
```

**`detect_anthropic` rules:**

Read `response.stop_reason` and `response.content` (list of content
blocks).

- `stop_reason == "tool_use"` → `has_pending_tool_use=True`,
  `is_terminal=False`, `final_text` = accumulated text block content
  from this response (may be empty)
- `stop_reason == "pause_turn"` → `has_pending_tool_use=True`
  (server-side tool still running; runtime awaits and re-invokes),
  `is_terminal=False`
- `stop_reason == "end_turn"` → `is_terminal=True`,
  `has_pending_tool_use=False`, `final_text` = accumulated text
- `stop_reason == "stop_sequence"` → `is_terminal=True`
- `stop_reason == "max_tokens"` → `is_terminal=True` (caller
  decides whether the truncated content is committable; PACT
  evaluator may flag it)
- `stop_reason == "refusal"` → `is_terminal=True`,
  `final_text` = refusal text (PACT evaluator will see it)
- Any other `stop_reason` (including missing) → `is_terminal=False`,
  `has_pending_tool_use=False`. Turn loop continues; hard turn cap is
  the eventual backstop.

**`final_text` extraction:**

Walk `response.content` blocks in order. Concatenate `text` blocks
with newlines. Ignore `tool_use` and `tool_result` blocks — those are
not user-visible content. This is the text to commit.

### Add `TURN_CAP` constant and `_turn_cap_for_task` helper

In `body.py`, add a module-level `TURN_CAP_DEFAULT = 8` and a helper:

```python
def _turn_cap_for_task(mission: dict | None) -> int:
    """Resolve the hard turn cap, respecting cost_mode upper bounds."""
    if mission is None:
        return TURN_CAP_DEFAULT
    cost_mode = mission.get("cost_mode", "balanced")
    if cost_mode == "frugal":
        return 4
    if cost_mode == "thorough":
        return 12
    return TURN_CAP_DEFAULT
```

### Modify `body.py` turn loop

In the main turn loop (around `body.py:2142+`), the loop structure
currently does:

```
while True:
    turn += 1
    self._current_task_turns = turn
    # ... LLM call ...
    # ... tool handling, including complete_task detection ...
```

Add two things:

1. **After each LLM response, compute a `TurnOutcome`** via
   `detect_anthropic(response)`. Store the `stop_reason` in
   `ExecutionState` for audit (new field; see below).

2. **Exit conditions (evaluated in order):**
   - Legacy: `complete_task` tool call → existing flow, unchanged.
   - New: `outcome.is_terminal == True` and no tool calls pending →
     invoke runtime commit hook internally with
     `content=outcome.final_text`. Go through existing
     `evaluate_pre_commit` / `map_pre_commit_verdict` flow.
   - Safety: `turn >= _turn_cap_for_task(mission)` → invoke commit
     hook with `content=""` and force the verdict shape to
     `verdict=blocked, reasons=["runtime:turn_limit_exceeded"]`.

The three paths are mutually exclusive. First match wins. They all
end the turn loop and lead to commit/post/audit/artifact writing.

### Add `ExecutionState.stop_reason` field

In `pact_engine.py` ExecutionState dataclass:

```python
stop_reason: str = ""
```

Serialized in `to_dict()`. Populated when the runtime receives a
model response; updated on each turn (final value is what committed).

### Update `map_pre_commit_verdict` to include `stop_reason`

In `body.py` (or wherever the mapper lives post-#266), add
`stop_reason` to the output dict as an additive field. Source:
`state.stop_reason`. Default: empty string for backwards compat.

### Update result artifact frontmatter

Add `stop_reason` to the `pact` block in result artifact
frontmatter. Additive; legacy artifacts without the field still
parse.

### Update PACT run projection (Go side)

In `internal/api/agents/handlers_pact.go`, surface `stop_reason` in
the run projection's verdict or execution block. Additive. Update
OpenAPI schema (both files) with a one-line `stop_reason: { type: string }`.

### Hard turn cap as runtime-forced block

When the cap is hit:

- Runtime calls the commit hook with final_text = `""` (or the last
  accumulated text if available)
- Mapper produces a verdict dict with `verdict="blocked"`,
  `reasons=["runtime:turn_limit_exceeded"]`,
  `missing_evidence=[]`
- `pact_verdict` signal fired with that payload
- `complete_task` path invoked with a synthesized terminal message:
  *"Task exceeded the runtime turn limit ({cap} turns) without
  natural termination. Commit blocked for safety review."*
- Result artifact reflects the blocked outcome with the reason

### Non-behavioral changes

- `complete_task` remains an agent-callable tool. The agent's tool
  list is not modified.
- Existing `_simulated_tool_retry_sent` and `_work_contract_retry_sent`
  flags remain in place with existing semantics.
- PACT evaluator (`evaluate_pre_commit`), honesty check, all contract
  validators — unchanged.
- Strategy router, objective builder, classifier — unchanged.
- The "How to Respond" prompt section still tells the model about
  `complete_task`. Phase 3 removes that language.

### Tests

**New `images/body/test_completion_detector.py`:**

1. `detect_anthropic` on `stop_reason=end_turn`, no tool_use blocks,
   single text block → `is_terminal=True, has_pending_tool_use=False,
   final_text=<text>, stop_reason="end_turn"`.
2. `stop_reason=tool_use` with one tool_use block →
   `is_terminal=False, has_pending_tool_use=True, final_text=""`.
3. `stop_reason=end_turn` with multiple text blocks → `final_text`
   is the concatenation (newlines between).
4. `stop_reason=end_turn` with mixed text + tool_use blocks →
   `has_pending_tool_use=True`, `is_terminal=False` (tool_use wins;
   runtime executes the tool first).
   *Actually per Anthropic semantics, when stop_reason=end_turn
   there shouldn't be pending tool_use. If it happens, treat it as
   tool_use (safer: handle the tool, then re-invoke).*
5. `stop_reason=stop_sequence` → `is_terminal=True`.
6. `stop_reason=max_tokens` → `is_terminal=True` (content may be
   truncated; PACT evaluator decides committability).
7. `stop_reason=refusal` → `is_terminal=True`, final_text carries
   the refusal text.
8. Unknown `stop_reason` → `is_terminal=False,
   has_pending_tool_use=False` (let runtime continue or hit cap).
9. Missing `stop_reason` field → `is_terminal=False` (same as above).
10. Empty `content` → `final_text=""`, other fields per stop_reason.

**New `images/body/test_turn_termination.py`:**

11. Legacy path: simulated model flow where the agent calls
    `complete_task` → commit hook invoked, `stop_reason` in audit is
    the value from the final detected outcome (or empty if the
    legacy path doesn't go through the detector). Must pass — this
    is backward compatibility.
12. New path: simulated model flow where the model emits
    `stop_reason=end_turn` with text content and no `complete_task`
    call → runtime invokes commit hook with `content=final_text`,
    PACT evaluator runs, verdict emitted. The task completes cleanly
    without any `complete_task` tool call from the agent.
13. Dual-path: both paths can coexist. A model that calls
    `complete_task` still triggers commit via the legacy path; a
    model that naturally stops triggers commit via the new path.
14. **Hank-replay integration (LOAD-BEARING)**: simulate a runtime
    scenario where the model emits `send_message` tool_use calls
    repeatedly without ever emitting `end_turn` or `complete_task`.
    Assert:
    - Turn loop exits when `turn >= TURN_CAP_DEFAULT` (8)
    - `pact_verdict` signal fired with `verdict="blocked"`,
      `reasons=["runtime:turn_limit_exceeded"]`
    - Task state cleared
    - No send_message calls beyond the 8th turn
    - Result artifact written with blocked outcome and the reason
15. Cost-mode caps: with `cost_mode="frugal"`, cap is 4 turns.
    With `cost_mode="thorough"`, cap is 12 turns. With missing or
    balanced cost_mode, default 8.
16. `ExecutionState.stop_reason` is populated after a model
    response, surfaced in `to_dict()`.

**Update `images/body/test_pact_pre_commit_rewire.py`:**

17. Existing tests continue to pass. Any test that asserted
    `pact_verdict` payload shape gains the additive `stop_reason`
    field; update assertions to allow the field's presence (value
    defaults to empty string for legacy-path tests).

### Spec Checkpoint update

Update `docs/specs/pact-governed-agent-execution.md` "Current
Implementation Checkpoint" section to note:

- `pact_verdict` signal payload now carries `stop_reason` (additive)
- `ExecutionState` has a new `stop_reason` field
- Task turn loop enforces a hard turn cap with
  `runtime:turn_limit_exceeded` as a terminal reason
- Dual-termination model active (legacy `complete_task` OR
  provider termination signal)

Update `docs/specs/model-native-task-termination.md` status note
that Phase 2 is implemented.

## Non-Scope

- **Phase 3 (remove `complete_task` from agent tool list)** —
  separate PR.
- **Phase 4 cleanup** — separate PR.
- **OpenAI and Google detectors** — stubs acceptable. Only Anthropic
  is live-detected in this PR since all current traffic is
  Anthropic.
- **Context-window-exceeded automatic compaction** — spec open
  question; treat as terminal-failed in this PR.
- **Replay with extended budget** — spec open question; operators
  can still halt + restart a task manually.
- **Meeseeks mode prompt overhaul** — keep Meeseeks' current
  prompt template; the Meeseeks flow may call `complete_task`
  explicitly and the dual-termination path preserves that.
- **PACT evaluator, honesty check, contract validators, classifier,
  strategy router, objective builder, tier classifier, mode
  injection** — all unchanged.
- **Removing simulated tool markup detection** — the
  `SIMULATED_TOOL_TAG_RE` retry path is orthogonal; keep unchanged.
- **Changes to `send_message`** — its role as a tool is unchanged;
  this PR just doesn't require it for commit signaling.

## Acceptance Criteria

1. `completion_detector.py` module exists with `TurnOutcome`
   frozen dataclass and `detect_anthropic` pure function.
2. Detector rules match the Scope section exactly. Unknown
   `stop_reason` values default to non-terminal (fail-closed, let
   the cap fire).
3. Body.py turn loop has three exit paths (complete_task, is_terminal,
   hard turn cap) evaluated in order. First match wins.
4. `_turn_cap_for_task` helper returns 4/8/12 per frugal/balanced/
   thorough (or 8 when mission is `None`).
5. `ExecutionState.stop_reason` field added, serialized, and
   populated on model responses.
6. `map_pre_commit_verdict` output includes `stop_reason` as an
   additive field.
7. Result artifact frontmatter includes `stop_reason` under the
   `pact` block (additive).
8. Go-side PACT run projection exposes `stop_reason`. OpenAPI schema
   additively updated.
9. Hard turn cap fires when exceeded. Commit hook invoked with
   `verdict="blocked"`, `reasons=["runtime:turn_limit_exceeded"]`.
10. 17 test cases passing: 10 detector, 6 turn-loop/integration,
    1 rewire compatibility.
11. **Hank6-replay test (case 14) passes.** Simulated loop of
    `send_message` without `complete_task` terminates at
    `TURN_CAP_DEFAULT` with the correct verdict.
12. All prior Wave 2 tests continue to pass without assertion
    changes. Fixture updates that add `stop_reason` where required
    are acceptable.
13. `pytest images/tests/` and `go build ./cmd/gateway/` pass.
14. Spec Checkpoint subsection updated in PACT spec and model-native
    spec marked as Phase 2 implemented.

## Review Gates

**Reject** if:
- `complete_task` removed from the agent tool list (Phase 3 scope).
- PACT evaluator, honesty check, or any contract validator modified.
- Strategy router, objective builder, classifier modified.
- `pact_verdict` existing fields renamed or removed. Only `stop_reason`
  is added.
- Hard turn cap is configurable by activation content or agent-emitted
  data. Cap must come from code + mission cost_mode only.
- Hard turn cap is bypassable under any condition short of operator-
  halt semantics.
- `TurnOutcome` fields diverge from the struct in Scope.
- Unknown `stop_reason` is treated as terminal (must be non-terminal to
  let cap be the safety authority).
- Detector performs I/O, model calls, or reads agent-emitted content
  to derive termination. It reads only provider structured metadata.

**Ask for changes** if:
- Test 14 (hank-replay loop simulation) is missing or weak.
- `runtime:turn_limit_exceeded` reason label isn't stable or
  greppable.
- `stop_reason` population in `ExecutionState` isn't visible in
  `to_dict()` output.
- Cost-mode caps deviate from 4/8/12 without justification.

## Files Likely To Touch

- `images/body/completion_detector.py` (new) — TurnOutcome +
  `detect_anthropic`.
- `images/body/body.py` — turn loop modifications, turn cap helper,
  runtime commit invocation on is_terminal path.
- `images/body/pact_engine.py` — `ExecutionState.stop_reason` field.
- `images/body/test_completion_detector.py` (new) — detector unit tests.
- `images/body/test_turn_termination.py` (new) — turn loop and cap tests.
- `images/tests/test_pact_pre_commit_rewire.py` — additive
  `stop_reason` field assertions.
- `internal/api/agents/handlers_pact.go` — run projection
  `stop_reason` field.
- `internal/api/openapi.yaml` + `internal/api/openapi-core.yaml` —
  additive schema field.
- `docs/specs/pact-governed-agent-execution.md` — Checkpoint update.
- `docs/specs/model-native-task-termination.md` — Phase 2 status
  note.

## ASK Compliance

- **#1 external enforcement** — completion detector is pure runtime
  code. Termination decision is runtime-owned. Agent cannot alter
  detection or bypass the hard cap.
- **#2 audit** — additive `stop_reason` field on `pact_verdict`
  signal, result artifact, run projection. No existing fields
  mutated. Hash scope grows uniformly.
- **#3 complete mediation** — commit hook still runs through PACT
  evaluator on every termination path. The new path (is_terminal)
  doesn't bypass evaluation; it just triggers evaluation without
  requiring an agent tool call.
- **#4 fail-closed** — unknown `stop_reason` values treated as
  non-terminal; hard cap fires as the eventual backstop.
  `runtime:turn_limit_exceeded` is a blocked (not committed) outcome.
- **#5 runtime is a known quantity** — `stop_reason` surfaced in
  audit makes termination cause operator-inspectable per task.
- **#7 least privilege** — agent loses no authority; `complete_task`
  remains available. Runtime gains the authority to commit on
  natural termination.
- **#8 bounded operations** — hard turn cap is an explicit new
  bound. Retry budgets unchanged. No unbounded operation introduced.
- **#11 halts auditable** — operator halt flow unchanged. Hard turn
  cap is a different mechanism (runtime-initiated, not
  operator-initiated) and emits its own distinct reason.
- **#18 governance hierarchy inviolable from below** — detector
  logic is module-level code. Agent-emitted content does not
  influence detection.
- **#22 unknown conflicts default to yield and flag** — unknown
  stop_reason → non-terminal → eventual cap; never implicit
  extension of the task.

**Forward-looking ASK notes:**

- Phase 3 removes `complete_task` from agent tool list. Must
  preserve the runtime's commit hook; only the agent-facing surface
  changes.
- Future OpenAI/Google detectors follow the same rule set:
  provider-metadata-only, module-level pure functions, unknown
  signals default to non-terminal.

## Out-of-band Notes For Codex

- The hard turn cap is the load-bearing safety mechanism. Do not
  make it bypassable under any condition short of operator halt.
  Do not gate it behind a feature flag.
- Test 14 (hank-replay) should simulate the exact loop shape we
  observed: model returns `stop_reason=tool_use` with
  `send_message` tool calls, never reaches `end_turn`, never calls
  `complete_task`. Assert the task terminates at turn cap with the
  correct verdict and reason.
- Legacy path backward compatibility (test 11) is critical. No
  regression in `complete_task` flow.
- Commit style: plain commit title, no Co-Authored-By trailer.
- PR target: `main`. Open as ready-for-review, not draft.
- Brief is already committed on the branch; do not re-add it.
