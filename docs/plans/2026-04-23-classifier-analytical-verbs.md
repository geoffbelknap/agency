# Classifier Expansion for Analytical Asks

## Reference

- Spec: `docs/specs/pact-governed-agent-execution.md` → Contract Registry,
  current_info contract.
- Motivating observation: hank5 test (fresh agent, full stack post-#274)
  still emitted simulated tool use because the activation
  (`"I want to see if you can help me out by investigating..."`) routed
  to `chat` contract kind, which carries empty `required_evidence`. The
  prompt-level signals competed: `# Execution Mode: tool_loop` said
  "MUST call tools" while the contract framing said "casual chat, no
  evidence required." Sonnet defaulted to the weaker conversational
  posture.

## Objective

Expand `CURRENT_INFO_RE` in `pact_engine.py` to capture analytical and
investigative language patterns that should route to `current_info`
contract kind. Analytical asks about external state ("investigate this
repo", "analyze the release patterns", "tell me about X") get the
evidence-requiring contract framing, which makes the whole downstream
stack (Sonnet selection, tool_loop routing, mode injection) cohere into
a single task-level "you MUST produce sourced evidence" signal.

## Why

The stack we built over this session is correct but incomplete without
the classifier catching analytical asks:

| Layer | Status |
|---|---|
| PACT enforcement (honesty check) | ✅ Tier 1 catches fabrications |
| Model selection | ✅ Sonnet for grounded |
| Three-axis tier | ✅ Full static prompt baseline |
| Mode-aware routing | ✅ Grounded chat → tool_loop |
| Execution mode prompt injection | ✅ "MUST call" instruction in prompt |
| **Contract classifier** | ❌ Still maps "investigate X" to `chat` |

With the classifier unchanged, the contract stays `chat` (empty
evidence), the prompt's contract section says "no evidence required,"
and Sonnet sees mixed signals. The mode injection alone cannot
override the contract-level framing.

Yesterday's successful hank run succeeded because the operator's
retry message contained "check" — ACTION_RE matched, but more
importantly a subsequent interaction produced `current_info`
classification. Today's hank4/hank5 tests failed on the initial
"investigate" phrasing that doesn't match any current trigger.

## Scope (in this PR)

### Expand `CURRENT_INFO_RE`

Current pattern (in `pact_engine.py` ~line 39):

```python
CURRENT_INFO_RE = re.compile(
    r"\b(latest|current|recent|most recent|today|yesterday|tomorrow|now|live|"
    r"price|weather|schedule|score|news|filing|sec filing|look up|lookup|find me|search)\b",
    re.IGNORECASE,
)
```

Expand to include analytical verbs and natural-language investigative
patterns. The expansion is intentionally limited to **unambiguous
external-state-query language** — phrases that, in operator-DM context,
almost always indicate a request for information about external
entities.

**New analytical verb triggers:**

- `\bi(?:n)?vestigat(?:e|ing|ion|ions)\b` — investigate, investigating,
  investigation, investigations
- `\banalyz(?:e|ing|es|ed)\b` — analyze, analyzing, analyzes, analyzed
- `\banalys(?:is|es)\b` — analysis, analyses
- `\bresearch(?:ing|ed)?\b` — research, researching, researched
- `\bexamin(?:e|ing|es|ed)\b` — examine variants
- `\binspect(?:ing|ed|s)?\b` — inspect variants
- `\bassess(?:ing|ed|ment|ments)?\b` — assess variants
- `\baudit(?:ing|ed|s)?\b` — audit variants
- `\bcheck(?:ing|ed)?\b` — check variants (moved/duplicated from
  ACTION_RE — see note)
- `\bverify(?:ing)?|verif(?:ied|ies)\b` — verify variants
  (moved/duplicated from ACTION_RE)

**New natural-language trigger phrases:**

- `\btell me about\b` — "tell me about this repo/person/event/thing"
- `\blook at (?:this|the|his|her|their|that)\b` — "look at this repo"
  but not "look at me" (the pronoun list is deliberate)
- `\btake a look at\b`
- `\bhelp me understand (?:this|the|what|how|why|when|where|who)\b`
- `\bwhat('?s| is| are) (?:the|this|that|his|her|their)\b` — "what's
  the deal with X", "what is this repo", "what are the stars for X"
- `\bwho is\b` — "who is the author"

**Duplication with ACTION_RE:**

Some new triggers overlap with existing ACTION_RE entries
(`check`, `verify`, `analyze`). CURRENT_INFO_RE runs first in the
classifier chain, so overlap is safe — CURRENT_INFO_RE wins. But
for clarity, keep ACTION_RE unchanged (don't remove the overlapping
terms). ACTION_RE remains the catch-all for generic action verbs
when CURRENT_INFO_RE doesn't fire.

### Conservative boundary

Do NOT add triggers that are:

- **Ambiguous with conceptual questions**: `\bwhat is\b` alone is too
  broad ("what is recursion?" is a teaching question, not external-
  state). The expanded pattern requires a demonstrative article after
  ("what is *the*", "what is *this*") — still imperfect but
  meaningfully narrower.
- **Common conversational fillers**: "know about", "wondering", "curious",
  "interested in" — these lean conversational and false-positive too
  easily.
- **Verbs that don't imply external state**: "think about", "consider",
  "reflect" — internal-reasoning verbs.

If any added pattern causes false positives in testing, prefer removing
it rather than adding exceptions.

### Preserve ordering

The classifier chain in `classify_activation` (lines 1759-1803) runs
in strict order:

1. mission_task (if mission active)
2. operator_blocked (OPERATOR_BLOCKED_RE)
3. **current_info (CURRENT_INFO_RE)** ← expansion here
4. code_change (CODE_CHANGE_RE)
5. file_artifact (FILE_ARTIFACT_RE)
6. task (ACTION_RE)
7. coordination (if non-direct)
8. chat (default)

Do not reorder. The expansion only changes which asks route to
`current_info` vs. subsequent kinds.

### Tests

New `images/body/test_classifier_analytical_verbs.py`:

Each test constructs an activation via
`ActivationContext.from_message(content)` and asserts the resulting
contract kind.

**Must route to `current_info`** (new positive cases):

1. "I want you to investigate this github repository"
2. "Can you investigate the graphify repo?"
3. "investigate https://github.com/x/y"
4. "Please analyze the release patterns"
5. "Can you analyze these numbers for me?"
6. "Give me an analysis of the recent commits"
7. "Research the history of X"
8. "Can you examine this file?"
9. "Inspect the repository"
10. "Assess whether this project is healthy"
11. "Audit the stargazers"
12. "Can you check the repo?"
13. "Verify the number of stars"
14. "Tell me about safishamsi/graphify"
15. "Look at this commit: https://github.com/x/y/commit/abc"
16. "Take a look at this"
17. "Help me understand what's happening in this repo"
18. "What's the deal with this project?"
19. "What is this repository about?"
20. "Who is the author of this code?"

**Must NOT route to `current_info`** (regression guards against false
positives):

21. "I analyzed my calendar this morning" — conversational
    ("I" past tense, not directive at operator) → may still match; see
    note below. If this test is too hard to preserve, document it as
    acceptable low-impact false positive.
22. "What is recursion?" — conceptual question, bare "what is" without
    demonstrative article → should NOT match
23. "think about this" — internal reasoning verb → should NOT match
24. "Consider the options" — should NOT match
25. "Can you help me understand TCP handshakes?" — teaching question;
    "help me understand" followed by concept not demonstrative. Note:
    the pattern `help me understand (this|the|what|how|why|when|where|who)`
    matches "understand TCP" via "what" only if actually present. For
    "Can you help me understand TCP handshakes?" the pattern doesn't
    match (no demonstrative article). Test asserts this.
26. "investigate" alone, no context — if any, should still match; this
    is acceptable.

**Hank-replay integration test:**

27. Construct activation from the exact hank prompt:
    `"I want to see if you can help me out by investigating this github
    repository https://github.com/safishamsi/graphify and looking
    especially at the number of stars it has. I suspect many of these
    are \"paid\" stars, from fake accounts. Looking at the people who
    starred this repository, is there any pattern that that emerges
    that indicates inauthentic activity?"`. Assert contract.kind ==
    "current_info", requires_action == True, required_evidence contains
    "current_source_or_blocker".

**Existing tests must still pass without modification.** If any existing
activation fixture expected `chat` or `task` for a text that now
matches the expanded CURRENT_INFO_RE, the fixture may need an update
(e.g., use a different non-analytical example text). No test assertion
changes.

### Spec Checkpoint update

Update `docs/specs/pact-governed-agent-execution.md` "### Execution
State Type" subsection (or the adjacent classifier discussion, whichever
location is more appropriate in the current spec): add a sentence
noting that the activation classifier's `current_info` trigger list
was expanded to capture analytical and investigative language patterns.
Short pointer only; the pattern list itself lives in the code.

## Non-Scope

- **LLM classifier** — explicitly deferred. This is the deterministic
  expansion that precedes an LLM-based classifier redesign.
- **New contract kinds** — no `analytical_query` or similar new kind.
  Reuse existing `current_info`.
- **Changes to `current_info` contract** — required evidence, answer
  requirements, validator unchanged. Only the classifier routes more
  activations into it.
- **ACTION_RE changes** — leave alone. `check`/`verify`/`analyze`
  overlap with CURRENT_INFO_RE is acceptable because CURRENT_INFO_RE
  runs first.
- **Mission-task classifier path** — unchanged.
- **PACT enforcement, honesty check, prompt builder, strategy router,
  tier classifier, mode injection** — untouched.
- **OpenAPI, web UI, feature registry, Go files.**

## Acceptance Criteria

1. `CURRENT_INFO_RE` expanded with the analytical verb group and
   natural-language trigger phrases exactly as listed in Scope.
   No triggers beyond the listed set.
2. Pattern remains `re.IGNORECASE`; no other regex flags changed.
3. Classifier chain order unchanged. Only CURRENT_INFO_RE's pattern
   expanded.
4. ACTION_RE left unchanged.
5. 20+ "must route to current_info" test cases in
   `test_classifier_analytical_verbs.py`, including hank-replay
   integration test (case 27) asserting the exact hank prompt routes
   to `current_info`.
6. 3+ false-positive regression guards asserting conceptual/
   conversational questions still route away from current_info.
7. All existing classifier tests (test_work_contract.py,
   test_objective_builder.py, test_strategy_router.py, others) continue
   to pass without assertion modifications. Fixture text updates to
   avoid incidental matches are acceptable.
8. No changes to the `current_info` contract's required_evidence,
   answer_requirements, or validator.
9. No changes to public API, audit event shapes, or PACT evaluator
   layers.
10. `pytest images/tests/` and `go build ./cmd/gateway/` pass.
11. Spec Checkpoint update landed.

## Review Gates

**Reject** if:
- Triggers added beyond the listed set (no creative expansion).
- Classifier chain order changed.
- ACTION_RE or other regex constants modified.
- `current_info` contract definition (required_evidence,
  answer_requirements, validator) modified.
- PACT evaluator layers, honesty check, prompt builder, strategy
  router touched.
- Existing test assertions changed.
- New contract kinds introduced.

**Ask for changes** if:
- Pattern regex uses non-word-boundary forms that could match
  inside other words (e.g., "analyzing" matching inside
  "overanalyzing" — acceptable, but verify with test).
- Hank-replay integration test (case 27) absent or weak.
- False-positive regression guards absent.

## Files Likely To Touch

- `images/body/pact_engine.py` — `CURRENT_INFO_RE` expansion.
- `images/body/test_classifier_analytical_verbs.py` (new) — 20+
  cases.
- `docs/specs/pact-governed-agent-execution.md` — short Checkpoint
  pointer.

## ASK Compliance

- **#1 external enforcement** — classifier remains pure runtime code,
  deterministic, module-level regex constants. Agent-proposed data
  cannot alter classifier rules.
- **#4 fail-closed** — classifier upgrade is toward MORE evidence
  requirement for analytical asks, which is the safety-biased
  direction. Fewer activations fall through to `chat` (empty
  evidence).
- **#18 governance hierarchy inviolable from below** — regex patterns
  are code. Agents cannot inject new trigger words. Activation content
  is classified as data, not instructions.
- **#22 unknown conflicts default to yield and flag** — conservative
  boundary. Ambiguous patterns (like bare "what is") are deliberately
  excluded. Unrecognized patterns fall through to existing chain.
- **#24 data vs instructions** — activation content is the input to
  the classifier; the classifier's decision is not overridable by
  content. Adversarial classifier manipulation via activation text
  is not possible with a regex classifier.

**Forward-looking ASK notes:**

- Future LLM-based classifier must preserve tenet #24: activation
  content passed as data with structured extraction, not as instructions
  to the classification model.
- If the expanded pattern set causes false positives in operator-DM
  contexts that aren't caught by the regression guards, the mitigation
  is to remove the specific trigger rather than add exception logic.

## Out-of-band Notes For Codex

- Pattern expansion is EXACTLY the list in Scope. Do not invent
  additional triggers. If you think a trigger is missing, stop and
  report.
- Regex word boundaries matter. The brief uses `\b` throughout. Match
  that style.
- The hank-replay integration test (case 27) is the end-to-end
  validation. Use the exact prompt text from the brief.
- If an existing test would break due to incidental matching (e.g., a
  fixture text contains "investigate" as part of unrelated content),
  update the fixture text to avoid the match — but do not change the
  test's assertion.
- Commit style: plain commit title, no Co-Authored-By trailer.
- PR target: `main`. Open as ready-for-review, not draft.
- The brief is already committed on the branch; do not re-add it.
