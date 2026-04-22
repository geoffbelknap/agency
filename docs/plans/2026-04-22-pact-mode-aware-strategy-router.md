# PACT Mode-Aware Strategy Router

## Reference

- Spec: `docs/specs/pact-governed-agent-execution.md` → Design Principle 9
  ("Invention is authorized, not assumed") and Core Concept "Generation
  Mode" (amended in #269).
- Builds on: Wave 2 #2 strategy router (merged), PR #270
  (generation_mode populated on Objective).
- Completes the prevention-vs-backstop story: grounded-mode chat asks now
  route through `tool_loop` instead of `trivial_direct`, forcing tool use
  *before* the model can generate prose. Honesty check remains a backstop
  for anything that still slips.

## Objective

Make the strategy router consume `objective.generation_mode`. When
`contract.kind == "chat"` AND `objective.generation_mode == "grounded"`,
emit `ExecutionMode.tool_loop` instead of `trivial_direct`. Every other
generation mode on `chat` continues to route to `trivial_direct`
(invention is authorized for social/persona/creative).

This is the single highest-impact preventive change enabled by #269/#270.
Hank's failure mode — analytical asks conversationally phrased as chat,
falling to a `trivial_direct` route with no tool pressure — is closed at
the routing layer rather than only at the honesty backstop.

## Why

Wave 2 #2 routing rules currently map `chat` → `trivial_direct` with no
awareness of generation mode. That worked when `chat` was assumed to be
social. With #269's reframing, `chat` is now *just* a structural contract
(conversational input, no evidence shape required) and can carry any
generation mode. A grounded-mode chat ask is the exact shape operators
pose analytical questions in ("hey, can you check this", "investigate
X for me", "what do you think about Y"). Routing those to
`trivial_direct` tells the runtime "no tools expected" — which is the
setup for hank-class fabrication.

With #270 landed, the router has a clean signal: generation_mode
explicitly says when invention is authorized. Grounded = invention NOT
authorized = route must force grounded execution = tool_loop.

## Scope (in this PR)

### Add a new routing rule

Insert a rule at position 4 in the existing strict-order rule sequence.
The rule only fires for the specific intersection: `chat` contract +
`grounded` generation mode.

```python
4. objective is not None
   AND objective.generation_mode == "grounded"
   AND contract.kind == "chat"
   → Strategy(
       execution_mode=ExecutionMode.tool_loop,
       needs_planner=False,
       needs_approval=False,
       notes=("reason:grounded_informal_ask",),
     )
```

Rule numbers shift for everything after: the existing `chat →
trivial_direct` rule moves from position 4 to position 5. Existing
rules 5–8 shift to 6–9. Positions of rules 1–3 (escalated risk,
load-bearing ambiguities, external_side_effect) are unchanged — they
still win over the new rule via short-circuit.

The existing `chat → trivial_direct` rule now only fires for
non-grounded modes (social, persona, creative) because grounded chat is
short-circuited by the new rule 4.

### `needs_planner=False` rationale

A grounded informal ask ("hi, can you check this repo") needs tools,
not a formal plan. The tool loop is sufficient — planner scaffolding
is overkill for conversational analytical work and would slow the
response. If the ask is actually high-risk, existing rule 7 (high risk
→ planned) still catches it via `objective.risk_level`.

### Full rule sequence after this PR

1. `risk_level == "escalated"` → escalate
2. load-bearing ambiguities present → clarify
3. `contract.kind == "external_side_effect"` → external_side_effect
4. **NEW**: `generation_mode == "grounded"` AND `contract.kind == "chat"`
   → tool_loop, `needs_planner=False`, notes `reason:grounded_informal_ask`
5. `contract.kind == "chat"` (now implicitly non-grounded) →
   trivial_direct, `needs_planner=False`
6. `contract.kind == "operator_blocked"` → trivial_direct
7. `risk_level == "high"` → planned
8. `contract.kind == "code_change"` → planned
9. default → tool_loop

### Non-consumers

The router emits `Strategy`; downstream consumers (planner, honesty
check, body runtime executor) read from it. No consumer changes in
this PR. A fresh agent asking "hi, investigate X" will now carry
`Strategy(execution_mode=tool_loop, ...)` but the body runtime's
interpretation of `tool_loop` is unchanged. Wave 2 #3b (plan execution)
and future system-prompt-construction work will further operationalize
the strategy.

### Spec Checkpoint update

Update "### Execution State Type" subsection in the PACT spec:

- Note that the strategy router now consumes `objective.generation_mode`.
- Describe the new rule and its position (4) in the strict-order
  sequence.
- Note that `trivial_direct` for `chat` is now conditional on
  non-grounded mode; grounded-mode `chat` routes to `tool_loop` so
  that analytical asks conversationally phrased cannot slip past tool
  pressure.

### Tests

Extend `images/body/test_strategy_router.py` (or add
`images/body/test_strategy_router_mode_aware.py` if preferred) with:

1. `chat` + `grounded` → `ExecutionMode.tool_loop`, `needs_planner=False`,
   `needs_approval=False`, `notes == ("reason:grounded_informal_ask",)`.
2. `chat` + `social` → `ExecutionMode.trivial_direct` (existing behavior
   preserved).
3. `chat` + `creative` → `ExecutionMode.trivial_direct` (existing).
4. `chat` + `persona` → `ExecutionMode.trivial_direct` (existing).
5. `chat` + `grounded` + `risk_level == "escalated"` →
   `ExecutionMode.escalate` (rule 1 still wins).
6. `chat` + `grounded` + load-bearing ambiguity
   (`ambiguity:target_files_missing`) → `ExecutionMode.clarify` (rule 2
   still wins).
7. `current_info` + `grounded` → `ExecutionMode.tool_loop` (rule 9
   default, unchanged).
8. `code_change` + `grounded` → `ExecutionMode.planned` (rule 8,
   unchanged).
9. `operator_blocked` + `grounded` → `ExecutionMode.trivial_direct` (rule
   6, unchanged).
10. **Hank-replay integration**: construct a task dict with hank's
    activation text ("I want to see if you can help me out by
    investigating this github repository..."). Assert:
    - `build_objective` returns `objective.generation_mode == "grounded"`
      (validated in #270)
    - `build_strategy` returns `Strategy(execution_mode=tool_loop,
      needs_planner=False, ...)` — NOT `trivial_direct`
    - This is the load-bearing integration test proving the hank-class
      failure shape is now routed away from no-tool mode.
11. `build_strategy` remains a pure function (same inputs → equal
    output, unchanged).
12. `ExecutionState.from_task` populates `state.strategy.execution_mode
    == tool_loop` for a hank-replay activation (end-to-end integration).

### Existing tests

All 12 tests in the existing Wave 2 #2 test suite must continue to pass.
Scrutinize these specifically:

- `test_chat_routes_to_trivial_direct_without_planner_or_approval` —
  the chat activation fixture in this test must explicitly set
  `objective.generation_mode` to something non-grounded (or the test
  fixture should use a creative/social activation). Otherwise the test
  will fail because grounded-mode chat now routes to tool_loop.
- Any existing test that constructs a chat activation must declare its
  generation_mode expectation explicitly.

## Non-Scope

- **System prompt construction changes.** Prompts still come from the
  Agency presets; this PR doesn't tell the LLM the new mode-aware
  strategy. Follow-up work.
- **Tool-loop execution semantics in body.py.** The runtime's handling
  of `execution_mode=tool_loop` is unchanged. The loop still runs, the
  model still sees the same tool set. What changes is that grounded
  chat now enters that loop where it previously went to
  `trivial_direct`.
- **Honesty check mode-awareness.** Deferred to the next PR
  (mode-aware Tier 1 pattern narrowing). This PR keeps the Tier 1
  check as-is — it still fires uniformly across modes, which is fine
  because this PR mainly shifts upstream routing, not downstream
  enforcement.
- **New `ExecutionMode` values.** The 7 existing values are sufficient.
- **New routing rules beyond the one listed above.**
- **Changes to `generation_mode` detection patterns** (those are
  #270's concern).
- **Changes to the existing 8 strategy rules' semantics** beyond the
  insertion at position 4. No rule priority reshuffling.
- **OpenAPI, web UI, feature registry, Go files.** None.

## Acceptance Criteria

1. New routing rule inserted at position 4 in `build_strategy`. Rule
   fires only for `contract.kind == "chat"` AND
   `objective.generation_mode == "grounded"`.
2. Emits `Strategy(execution_mode=ExecutionMode.tool_loop,
   needs_planner=False, needs_approval=False,
   notes=("reason:grounded_informal_ask",))`.
3. Escalated-risk rule (1) still short-circuits first. Load-bearing
   ambiguity rule (2) still short-circuits second. Rule 3
   (external_side_effect) still wins over new rule 4.
4. Existing chat → trivial_direct rule now fires only for non-grounded
   generation modes. Social/persona/creative chat → trivial_direct
   preserved.
5. No existing routing rules are reordered, reweighted, or logic-changed
   beyond the position-shift from the rule insertion.
6. `build_strategy` remains pure.
7. At least 12 test cases covering the new and existing behavior (see
   brief's Tests section).
8. Hank-replay integration test asserts:
   - `objective.generation_mode == "grounded"` (from #270)
   - `strategy.execution_mode == tool_loop` (NEW)
   - `strategy.notes == ("reason:grounded_informal_ask",)`
9. All existing Wave 2 #2 strategy router tests pass — fixture updates
   that explicitly set non-grounded generation_mode on chat activations
   are acceptable; changes to test assertions are not.
10. `pytest images/tests/` and `go build ./cmd/gateway/` succeed.
11. Spec "### Execution State Type" subsection updated.

## Review Gates

**Reject** if:
- New `ExecutionMode` values are introduced.
- Consumers of `Strategy` (body runtime, planner, honesty check) start
  behaving differently. This PR only changes routing; consumers are
  unchanged.
- Existing routing rule priority is reordered (e.g., new rule 4 is
  placed before escalated-risk or load-bearing-ambiguity rules).
- Rule 4 fires for any contract kind other than `chat`, or for any
  generation_mode other than `grounded`.
- `needs_planner=True` for grounded-chat — planner is overkill for
  informal asks. If the ask is high-risk, existing rule 7 catches it.
- Any existing Wave 2 #2 test has its assertions changed (fixture
  updates to set non-grounded mode explicitly are OK; assertion
  changes are not).
- The router calls any non-pure function or reads from shared state.

**Ask for changes** if:
- `notes` string doesn't follow `reason:<label>` form (existing
  convention).
- Hank-replay integration test isn't present or doesn't assert both
  the generation_mode and the resulting execution_mode.
- Fixture updates in existing tests are excessive or touch assertions
  (only the activation content / generation_mode input should change).

## Files Likely To Touch

- `images/body/pact_engine.py` — add the new routing rule to
  `build_strategy`. Small insertion.
- `images/body/test_strategy_router.py` — add new test cases;
  possibly update existing fixtures to explicitly set non-grounded
  generation_mode for tests that expect `chat` → `trivial_direct`.
- `docs/specs/pact-governed-agent-execution.md` — Checkpoint
  subsection update only.

## ASK Compliance

- **#1 external enforcement** — routing is runtime-owned. The agent
  cannot promote its own strategy. The generation_mode → tool_loop
  mapping is runtime logic, not agent-configurable.
- **#2 audit** — no audit event shapes change. The new `notes` entry
  (`reason:grounded_informal_ask`) flows through existing
  `ExecutionState.to_dict()` serialization.
- **#4 fail-closed** — grounded is the default generation mode.
  Grounded-chat routing forcing `tool_loop` is the fail-closed
  direction: when the runtime doesn't know the ask is authorized for
  invention, it forces tool pressure.
- **#5 runtime is a known quantity** — `reason:grounded_informal_ask`
  in audit surfaces exactly why the routing path was chosen. Operator-
  inspectable.
- **#7 least privilege / #8 bounded operations** — no capability or
  budget changes.
- **#18 governance hierarchy inviolable from below** — the rule
  sequence is code, not config. Agent-proposed data (activation
  content routed as data per #269 Principle 4) cannot alter the
  routing decision.
- **#22 unknown conflicts default to yield and flag** — ambiguous
  contract/mode combinations route via the default rule (9 → tool_loop),
  which is the safer posture than trivial_direct.

**Forward-looking ASK notes:**

- Future mode-aware honesty-check PR can narrow the Tier 1 pattern
  list in non-grounded modes without risking coverage gaps: the
  router's `tool_loop` path for grounded chat means Tier 1 enforcement
  still has teeth *where* fabrication is most likely, while creative/
  persona/social paths carry reduced false-positive risk.
- Future system-prompt-construction work can tell the LLM the exact
  execution mode, letting provider-native tool-use primitives
  (Anthropic native tool use, citations) activate more reliably. The
  stronger the upstream signal ("this turn is tool_loop, call tools"),
  the fewer fabrications slip to the honesty backstop.

## Out-of-band Notes For Codex

- The new rule is a tiny insertion. Resist the urge to refactor
  `build_strategy` beyond the new rule + position shift. The existing
  rule flow is explicit `if` blocks; keep that style.
- Check all existing strategy-router tests. Some fixtures build an
  activation implicitly via `build_objective` which now populates
  `generation_mode=grounded` for plain-text activations. A test
  expecting `chat` → `trivial_direct` needs its fixture to use a
  creative/social/persona activation text (e.g., "tell me a joke",
  "hi there"), or else needs to use a non-chat contract kind.
- The hank-replay integration test (case 10) is the load-bearing
  backstop. Use a real-looking activation string similar to hank's
  original message.
- Commit style: plain title, no Co-Authored-By trailer.
- PR target: `main`. Open as ready-for-review, not draft.
- The brief is already committed on the branch; do not re-add it.
