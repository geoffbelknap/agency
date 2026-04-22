# PACT Generation Mode Detection (Objective Builder)

## Reference

- Spec: `docs/specs/pact-governed-agent-execution.md` → Design Principle 9
  ("Invention is authorized, not assumed") and Core Concept "Generation Mode"
  (both amended in #269).
- Builds on: Wave 2 #1 objective builder (merged), which currently leaves
  `generation_mode` unpopulated.
- Enables: mode-aware strategy routing and mode-aware honesty check (future
  PRs).

## Objective

Populate `Objective.generation_mode` in `build_objective()`. Introduce
deterministic pattern-based detection of explicit invention authorizations:
`creative`, `persona`, and `social`. Everything else defaults to `grounded`
(fail-closed per ASK tenet #4).

This is the upstream piece for every downstream mode-aware behavior. No
consumers change in this PR — the field is populated and serialized, but
the strategy router, honesty check, and system prompt construction still
behave as they do today. Those rewires follow in separate PRs.

## Why

Wave 2 #1's objective builder created the typed `Objective` with most of
its fields populated. The `generation_mode` field was added in spec-space
(#269) but not yet populated in code. Everything downstream that consumes
the field (mode-aware routing, narrowed honesty checks) is blocked on this
PR landing.

## Scope (in this PR)

### Add `generation_mode` to `Objective`

In `images/body/pact_engine.py`, extend the `Objective` dataclass:

```python
@dataclass(slots=True)
class Objective:
    statement: str = ""
    kind: str = ""
    constraints: list[str] = field(default_factory=list)
    deliverables: list[str] = field(default_factory=list)
    success_criteria: list[str] = field(default_factory=list)
    ambiguities: list[str] = field(default_factory=list)
    assumptions: list[str] = field(default_factory=list)
    risk_level: str = ""
    generation_mode: str = "grounded"
```

`to_dict()` serializes `generation_mode` as a string. Default is
`"grounded"` — explicit, visible, and fail-closed per ASK tenet #4.

### Add `detect_generation_mode()` in `objective_builder.py`

Pure function:

```python
def detect_generation_mode(content: str) -> str:
    """Recognize explicit invention authorizations. Defaults to grounded."""
```

No I/O, no model calls, deterministic. Operates on the activation content
string.

### Pattern sets (minimal starter lists)

All patterns are case-insensitive precompiled regex, stored as module-level
tuples of `(label, pattern)` pairs — same style as
`TOOL_ANNOUNCEMENT_PATTERNS` in `pact_engine.py`.

**Creative mode** — explicit creative/playful authorization. Match-anywhere
in content (search, not fullmatch):

- `\btell me (a|an) joke\b`
- `\bwrite (a|an|me a|me an) poem\b`
- `\bwrite (a|an|me a|me an) haiku\b`
- `\bwrite (a|an|me a|me an) story\b`
- `\bwrite (a|an|me a|me an) song\b`
- `\bbrainstorm (with me|ideas|on|about)\b`
- `\blet'?s brainstorm\b`
- `\broleplay\b` / `\brole[- ]play\b`
- `\bpretend (to be|you('?re| are))\b`
- `\bmake up (a|an|some)\b` (playful fabrication)

**Persona mode** — explicit first-person-agent query. Match-anywhere
acceptable because these phrases are narrow enough:

- `\bwhat('?s| is) your name\b`
- `\bwho are you\b`
- `\btell me about yourself\b`
- `\bwhat do you do\b`
- `\bwhat are your (preferences|capabilities|tools|limits|restrictions)\b`
- `\bwhat('?s| is) your (role|job|purpose)\b`
- `\bwhat('?s| is) your favorite\b`

**Social mode** — low-stakes conversational. **Must fullmatch the
stripped content** (or match after stripping whitespace and trailing
punctuation). This is the critical guard: "hi, investigate X" must stay
grounded, not escape to social via the "hi" prefix.

Valid social patterns (fullmatch after strip):

- `(hi|hello|hey|yo)[!.?]*`
- `good (morning|afternoon|evening|night)[!.?]*`
- `how are you( (doing|today))?[?!.]*`
- `how('?s| is) it going[?!.]*`
- `what('?s| is) up[?!.]*`
- `thanks( so much)?[!.]*`
- `thank you( (so much|very much))?[!.]*`
- `goodbye[!.]*` / `bye[!.]*` / `see (you|ya)[!.]*`
- `ok[ay]*[!.]*` / `got it[!.]*` / `sounds good[!.]*`

**Reasoning mode** is out of scope for this PR. Reasoning is more about
how the agent structures its own response than about what the activation
asks for; detecting it from the activation string is unreliable. Defer.

### Detection order

When multiple mode families could match, priority order is:

1. `creative` (most specific, match-anywhere)
2. `persona` (specific, match-anywhere)
3. `social` (fullmatch; rarely collides with the others)
4. Default: `grounded`

First-match wins. Short-circuit returns.

### Populate in `build_objective`

Inside `build_objective(...)`, after determining `statement`, call:

```python
generation_mode = detect_generation_mode(activation.content)
```

Then pass it into the constructed `Objective`. No other behavior changes —
the function stays pure and deterministic.

### Re-export

Re-export `detect_generation_mode` from `images/body/work_contract.py`.

### Spec Checkpoint update

Update "### Execution State Type" subsection in
`docs/specs/pact-governed-agent-execution.md`:

- Note that `Objective.generation_mode` is now populated by the objective
  builder.
- Name the four mode values currently detected (grounded, creative,
  persona, social) and note that reasoning mode is deferred.
- Reaffirm the default is `grounded` and the classifier does not escalate
  invention authority on inference.

### Tests

New `images/body/test_generation_mode_detection.py` covering:

1. Empty or whitespace-only content → `grounded`.
2. Bare greeting "hi" → `social`.
3. Bare greeting "Hello!" → `social` (case-insensitive; punctuation
   tolerated).
4. "how are you" → `social`.
5. **"hi, investigate this repo" → `grounded`** (mixed greeting + work
   must NOT promote to social; this is the load-bearing test for the
   fullmatch guard).
6. **"hello, what's your favorite color?" → `persona`** is acceptable —
   the persona pattern matches explicitly. This is an edge case; if test
   shows we prefer grounded here, we can narrow persona patterns later.
   Document the outcome in the test.
7. "tell me a joke" → `creative`.
8. "write me a haiku about PACT" → `creative`.
9. "brainstorm ideas for the roadmap" → `creative`.
10. "what's your name" → `persona`.
11. "who are you" → `persona`.
12. "pretend to be a pirate" → `creative`.
13. "investigate the graphify repo" → `grounded` (no authorization
    pattern).
14. "can you analyze the recent release patterns of bun.js" → `grounded`
    (analytical ask, no authorization pattern).
15. "thanks!" → `social`.
16. Detection is case-insensitive: "TELL ME A JOKE" → `creative`.
17. Pattern priority: "tell me a joke about what's your name" → `creative`
    (first-match-wins, creative comes first in priority order).
18. Hank-replay activation text ("I want to see if you can help me out by
    investigating this github repository...") → `grounded`.
19. `build_objective(...)` populates `objective.generation_mode` from the
    activation content.
20. `build_objective(...)` leaves `generation_mode="grounded"` when no
    pattern matches.
21. `Objective.to_dict()` round-trips `generation_mode`.
22. `ExecutionState.from_task` populates `objective.generation_mode`
    through the existing objective-builder integration.

### Non-consumers

Do **not** modify the strategy router, honesty check, system prompt
construction, or any evaluator layer to consume `generation_mode` in this
PR. Subsequent PRs handle each consumer explicitly.

## Non-Scope

- **Mode-aware strategy router.** Routing rules currently don't consider
  generation_mode. Unchanged in this PR; follow-up.
- **Mode-aware honesty check.** The Tier 1 simulated-tool-use layer
  currently applies to every contract kind. Leave it as-is; mode-aware
  narrowing is a follow-up.
- **System prompt construction.** Agency's prompt builder does not yet
  see generation_mode; that's a separate surface.
- **Reasoning mode detection.** Deferred.
- **Expanded mode pattern lists.** Starter lists only. No additions or
  tuning in this PR without product review — the risk of false positives
  expanding into grounded territory is a security concern.
- **Changes to Wave 2 #4 pre-commit evaluator or Wave 2 #4b runtime
  rewire.** `PreCommitVerdict`, `evaluate_pre_commit`, body.py commit
  gate all unchanged.
- **Public API shape changes.** `Objective.to_dict()` gains a new field,
  which flows through `ExecutionState.to_dict()` and the PACT run
  projection. New field is additive — consumers that ignore unknown
  fields are unaffected. Audit-report hash scope includes it (uniform
  schema evolution; same-run repeated reads stable).
- **OpenAPI schema changes.** None in this PR. If the PACT run
  projection surfaces `generation_mode` as part of the objective block
  in the Go-side projection (it likely does automatically via
  `to_dict()` serialization), the OpenAPI schema for `/pact/runs/{taskId}`
  may need an additive field. Codex should check and update if needed —
  it's a tiny addition, description: "Generation mode authorized for
  this objective".

## Acceptance Criteria

1. `Objective` has a `generation_mode: str` field defaulting to
   `"grounded"`. `to_dict()` serializes it.
2. `detect_generation_mode(content)` exists in
   `images/body/objective_builder.py`, is pure, no I/O, and returns one
   of `{grounded, social, persona, creative}`.
3. Pattern lists for creative, persona, social are module-level
   precompiled regex constants matching the starter lists in Scope.
4. Detection order is creative → persona → social → grounded. First-
   match-wins.
5. Social patterns use fullmatch (after whitespace/punctuation strip);
   creative and persona may use match-anywhere. Test 5 asserts the
   fullmatch guard for social.
6. `build_objective(...)` calls `detect_generation_mode(activation.content)`
   and assigns the result to `objective.generation_mode`. Default is
   `"grounded"`.
7. `ExecutionState.from_task` populates `objective.generation_mode`
   through the existing integration.
8. 22 test cases in
   `images/body/test_generation_mode_detection.py` (or
   `test_objective_builder.py` if preferred, but a new file is cleaner).
9. Public API shapes preserved beyond the additive `generation_mode`
   field on the objective. No body.py behavior changes. No Go files
   modified except possibly an additive OpenAPI schema line for the
   PACT run projection.
10. `pytest images/tests/` and `go build ./cmd/gateway/` pass.
11. Spec "### Execution State Type" subsection updated.

## Review Gates

**Reject** if:
- Strategy router, honesty check, system prompt construction, or any
  existing evaluator layer starts consuming `generation_mode`. That's
  follow-up work.
- Pattern list expanded beyond the starter set without product review.
- Social mode uses match-anywhere (not fullmatch-after-strip). This is
  the critical guard against "hi, investigate X" escaping to social.
- New mode values invented beyond grounded/social/persona/creative.
- Reasoning mode detection attempted.
- `detect_generation_mode` is not pure (clock, I/O, model calls).
- Default changes from `"grounded"`.
- Go files are modified beyond the one-line OpenAPI schema addition for
  the projection's objective block (if warranted).

**Ask for changes** if:
- Pattern list doesn't follow the stable `(label, regex)` tuple style.
- Case-insensitive flag is missing on any pattern.
- Test 5 (mixed greeting + work stays grounded) is absent or weak.
- `Objective.to_dict()` serialization order changes compared to existing
  ordering.

## Files Likely To Touch

- `images/body/pact_engine.py` — add `generation_mode` field to
  `Objective`, serialize in `to_dict()`.
- `images/body/objective_builder.py` — add pattern lists and
  `detect_generation_mode` function; call from `build_objective`.
- `images/body/work_contract.py` — re-export `detect_generation_mode`.
- `images/body/test_generation_mode_detection.py` (new).
- `docs/specs/pact-governed-agent-execution.md` — Checkpoint update.
- Possibly `internal/api/openapi.yaml` and
  `internal/api/openapi-core.yaml` — one-line additive field on the
  projection's objective block. Only if the projection currently
  documents objective fields in the schema; if the objective is passed
  as an opaque object, no schema change needed.

## ASK Compliance

- **#1 external enforcement** — detection is runtime-owned. Agent cannot
  promote its own mode. Pattern lists are module-level constants, not
  agent-configurable.
- **#2 audit** — `generation_mode` becomes visible in
  `ExecutionState.to_dict()` and flows into the PACT run projection
  (additively). No audit JSONL mutation.
- **#4 fail-closed** — default is `grounded`. Ambiguous or unrecognized
  activation → grounded. Invention authority is never assumed from
  inference.
- **#5 runtime is a known quantity** — explicit mode label in the
  objective makes authorized generation posture operator-inspectable.
  Net ASK gain.
- **#18 governance hierarchy inviolable from below** — pattern lists are
  hardcoded. Agent-proposed data cannot influence detection.
- **#22 unknown conflicts default to yield and flag** — when patterns
  don't match cleanly, grounded is the safe default.

**Forward-looking ASK notes:**

- Future mode-aware honesty check must preserve the invariant that
  claims about external state always require mediated evidence,
  regardless of mode. Narrowing in non-grounded modes applies only to
  subjective/creative/persona content, never to factual assertions.
- Future strategy router mode handling must not permit escalation of
  generation mode beyond what the objective carries. A `grounded` mode
  cannot be "upgraded" to `creative` by the router or the agent.

## Out-of-band Notes For Codex

- Keep pattern lists exactly as listed. Do not add patterns; do not
  remove patterns. If a pattern seems wrong or missing, stop and report.
- The fullmatch guard for social mode is load-bearing. Test 5 asserts
  it. Do not relax.
- The field addition to `Objective` propagates through
  `ExecutionState.to_dict()`, the PACT run projection endpoint
  (handlers_pact.go), and the audit report. Check whether the projection
  surfaces objective fields explicitly (and needs an OpenAPI schema
  update) or passes objective through as a generic map (no schema
  change needed). If schema update is warranted, add the description:
  "Generation mode authorized for this objective".
- Commit style: plain title, no Co-Authored-By trailer.
- PR target: `main`. Open as ready-for-review, not draft.
- The brief is already committed on this branch; do not re-add it.
