# PACT Honesty Check — Tier 1 (Simulated Tool Use)

## Reference

- Spec: `docs/specs/pact-governed-agent-execution.md` → Design Principle 4
  (amended in #267: "Agent claims cannot exceed mediated evidence")
- Builds on: Wave 2 #4 (`evaluate_pre_commit`, `PreCommitVerdict` layered
  evaluator), Wave 2 #4b (runtime rewire that gates commit on verdict).
- First enforcement of the honesty invariant. Tier 2 (specific-claim
  grounding via model-assisted critique) is deferred to a future wave.

## Objective

Add a new deterministic PACT evaluator layer that enforces the honesty
invariant for the most catchable fabrication shape: **agent prose that
announces tool use without a matching mediated tool_result in evidence**.
The layer runs for every contract kind, including `chat`. It is a hard
block, not advisory — fabrication is a hard-stop per the amended Principle 4.

## Why

Real operator trace (researcher agent "hank") committed fabricated
responses as `completed` because:

1. The contract was classified `chat` (zero evidence requirements).
2. The agent's prose contained tool-announcement patterns ("Let me search
   for...", "I'll search for...", "Based on my research...") with no
   corresponding `provider-web-search` tool_result in the evidence
   ledger.
3. Existing PACT evaluator layers 0–7 all passed trivially for `chat`,
   and Layer 7 (contract validator) had nothing to check because
   `required_evidence=[]`.

The spec amendment in #267 made this a principle-level violation. This PR
makes it an enforcement-level violation.

## Scope (in this PR)

### Add a honesty-check layer to `evaluate_pre_commit`

Insert a new layer between existing Layer 6 (plan evidence, advisory)
and existing Layer 7 (contract validator). The layer becomes the new
Layer 7; the existing contract validator becomes Layer 8; success
becomes Layer 9.

Behavior:

1. Scan the `content` argument for **tool-announcement patterns** (list
   in next section). If none matched → layer passes, continue to
   contract validator.
2. If any pattern matched, check the evidence ledger (and
   `state.tool_observations`) for at least one entry with
   `kind == "tool_result"` (or, for typed observations, `status != error`
   and `provenance in {mediated, provider, runtime}`).
3. If at least one qualifying mediated tool_result exists → layer passes.
4. If zero qualifying tool_results exist → **block** with reason
   `honesty:simulated_tool_use:<first_matched_pattern>`,
   `missing=("mediated_tool_result",)`.

This is a coarse-grained check for Tier 1 — we don't yet verify that the
tool class announced (search vs fetch vs execute) matches a specific
tool_result class. Tier 2 will refine that. The goal here is catching
the most flagrant case (zero tools called but prose asserts tool use).

### Tool-announcement pattern list

Case-insensitive regex patterns matching **assertions of completed or
imminent tool action**. Do NOT match future-intent-only phrases,
hypothetical suggestions, or user echoes.

Match:

- `\bI searched\b` / `\bI've searched\b` / `\bI have searched\b`
- `\bLet me search\b`
- `\bI looked up\b` / `\bI've looked up\b` / `\bI have looked up\b`
- `\bLet me look up\b`
- `\bI fetched\b` / `\bI've fetched\b`
- `\bLet me fetch\b`
- `\bI ran\b` (followed within ~30 chars by `command`, `query`,
  `search`, or a shell-token character class — to distinguish "I ran
  a command" from "I ran into an issue")
- `\bI executed\b`
- `\bBased on my search(?:es)?\b`
- `\bBased on my research\b`
- `\bBased on my investigation\b`
- `\bAccording to my search\b`
- `\bAccording to (?:the|my) (?:results|data|findings)\b`
- `\bMy research (?:shows|found|indicates)\b`

**Explicitly do NOT match:**

- `\bYou can search\b` (addressing user)
- `\bConsider searching\b`
- `\bIt might be worth searching\b`
- `\bA search would\b` / `\bA lookup would\b` (hypothetical)
- `\bI'll search\b` / `\bI will search\b` (future intent only, no
  assertion of completion)
- `\bI can search\b` (capability offer, not completed action)

Keep the list hard-coded in a module-level constant. Do not allow
runtime override — this is a security invariant, not a config.

### Where the layer lives

Extend `evaluate_pre_commit` in `images/body/pact_engine.py`, or add a
sibling helper `_detect_simulated_tool_use(content, state)` that
`evaluate_pre_commit` calls. If the pattern list is small, inline is
fine; if it grows beyond ~30 patterns, put it in a sibling
`images/body/honesty_check.py` module.

The new layer must run:
- AFTER Layer 6 (plan evidence advisory — so plan advisory reasons still
  get recorded even if honesty check fails? No — actually, the honesty
  check short-circuits Layer 7/8 just like every other blocking layer.
  Plan advisory from Layer 6 is part of the final `reasons` tuple only
  when Layer 8 succeeds. So Layer 6 advisory reasons are discarded if
  Layer 7 (honesty) or Layer 8 (contract) blocks. This is consistent
  with existing short-circuit semantics.)
- BEFORE Layer 7 (contract validator) — so that fabrication blocks
  before the contract-specific evaluator runs. This matters because
  `chat` contracts pass the contract validator trivially; the honesty
  layer must run first to catch fabrication in chat.

### Preserve determinism

- Pattern scan uses precompiled `re` patterns.
- No model calls. No clock access inside the layer (inherits from
  outer `evaluate_pre_commit`).
- Deterministic `reasons` output: patterns scanned in list order,
  first match used for the reason label.

### Tests

New `images/body/test_honesty_check.py` (or extend
`test_pre_commit_evaluator.py`):

1. `chat` contract + prose "Let me search for X" + empty evidence →
   block, reason `honesty:simulated_tool_use:Let me search`.
2. `chat` contract + prose "I searched the web and found X" + empty
   evidence → block.
3. `chat` contract + prose "I searched the web" + one mediated
   tool_result entry in evidence → pass (layer does not block;
   downstream layers decide commit).
4. `chat` contract + prose "You can search for X" + empty evidence →
   pass (hypothetical, no assertion of completion).
5. `chat` contract + prose "I'll search for that now" + empty evidence
   → pass (future intent only, no completion assertion).
6. `current_info` contract + prose "Based on my research..." + no
   mediated tool_result → block (layer applies to every contract kind).
7. `current_info` contract + prose "Based on my research..." + one
   mediated tool_result → pass.
8. Case-insensitive matching: "BASED ON MY SEARCH" → matched.
9. False-positive guard: prose "I ran into an issue" → pass (pattern
   `I ran` requires command/query/search follow-up).
10. Multiple patterns match, zero tool_results → block, reason names
    the FIRST matched pattern only (deterministic).
11. Typed `ToolObservation` with status=ok and provenance=provider
    counts as a qualifying tool_result (even if legacy
    `evidence.entries` is empty).
12. Typed `ToolObservation` with status=error does NOT count as a
    qualifying tool_result (error observations don't ground announced
    completed action).
13. **Hank-replay test**: construct an `ExecutionState` from one of
    hank's hallucinated `chat` turns (prose with "Let me search..." /
    "Based on my research..." and empty `tool_observations` / empty
    `evidence`), run `evaluate_pre_commit`, assert `committable=False`
    with `reason` starting `honesty:simulated_tool_use:*`. Use a minimal
    fixture — do not load full hank state.

### Update `evaluate_pre_commit` tests

Existing `images/body/test_pre_commit_evaluator.py` tests reference layer
numbering. Update the short-circuit ordering test (test #14) to account
for the new Layer 7 position — the test should still verify first-match
semantics but with the new layer inserted.

Add a case asserting: when honesty layer would fail AND contract
validator would also fail, only the honesty layer's reason appears
(first-match wins).

### Spec Checkpoint update

Update "### Execution State Type" subsection in
`docs/specs/pact-governed-agent-execution.md`:

- Add a short paragraph naming the new honesty-check layer, the
  tool-announcement pattern list location, and the reason-label form
  `honesty:simulated_tool_use:<pattern>`.
- Note that this is Tier 1 of the honesty invariant. Tier 2 (specific-
  claim grounding) is deferred.

## Non-Scope

- **Tier 2 claim grounding.** Specific numeric/factual claims must be
  backed by retrieved content. Requires model-assisted critique or
  retrieval-based verification. Own wave, own design pass.
- **Tool-class matching.** Tier 1 treats any mediated tool_result as
  sufficient grounding for any announcement pattern. Tier 1.5 or 2
  refines this: "I searched" requires a search-class tool_result, "I
  fetched" requires a fetch-class, etc.
- **NLP-based claim extraction.** No parsing of specific numbers, dates,
  named entities. Only pattern matching on tool-announcement verbs.
- **Runtime integration beyond what Wave 2 #4b already does.** The
  existing commit-gate in body.py already consumes `PreCommitVerdict`.
  The new layer just adds a fail mode; body.py's mapping already handles
  non-committable reasons uniformly.
- **Pact_verdict signal payload changes.** The new reason label form
  (`honesty:*`) is just a new string value within the existing
  `reasons` list — additive, not a schema change. Audit-report hash
  remains stable across repeated reads.
- **Classifier improvements.** This PR fixes the fabrication-slip issue
  regardless of classifier accuracy; a parallel effort could still
  improve classifier routing, but it's not required here.
- **New contract kinds.** No new contract kinds.
- **OpenAPI, web UI, feature registry, Go files.** None.

## Acceptance Criteria

1. New layer added between Layer 6 and Layer 7 in `evaluate_pre_commit`.
   Layer numbering shifts: new Layer 7 = honesty, Layer 8 = contract
   validator (was 7), Layer 9 = success (was 8).
2. Tool-announcement pattern list implemented as module-level
   precompiled regex constant. Patterns exactly match the list in Scope.
3. Layer blocks commit with reason
   `honesty:simulated_tool_use:<pattern>` when any pattern matches AND
   zero qualifying tool_result entries exist in evidence.
4. Layer passes when no pattern matches, OR when a pattern matches AND
   at least one mediated tool_result entry exists.
5. Qualifying tool_result = evidence ledger entry with `kind ==
   "tool_result"` (legacy projection) OR typed `ToolObservation` with
   `status != error` AND `provenance in {mediated, provider, runtime}`.
6. Deterministic: first pattern-list match wins the reason label.
7. 13 test cases in `images/body/test_honesty_check.py` (or extended
   `test_pre_commit_evaluator.py`). Includes the hank-replay fixture.
8. Existing pre-commit evaluator tests updated to account for the new
   Layer 7 position. Short-circuit ordering test (previously test #14)
   still passes.
9. `pytest images/tests/` and `go build ./cmd/gateway/` succeed.
10. Spec "### Execution State Type" subsection updated.
11. No changes to `pact_verdict` signal payload shape, result
    frontmatter, PACT run projection schema, audit-report endpoints, or
    Go code. Audit-report hash stable across repeated reads.

## Review Gates

**Reject** if:
- Any pattern from the "do NOT match" list is included.
- Agent-authored content can override the pattern list (any runtime
  configurability of the patterns).
- Layer is advisory instead of hard-blocking (does not flip
  `committable` to `False`).
- Tier 2 claim-grounding work crosses in (model-assisted critique,
  numeric claim extraction, etc.).
- Layer is skipped for any contract kind (chat, current_info, etc.) —
  the honesty invariant applies universally.
- New `pact_verdict` signal fields added beyond the existing `reasons`
  string list.
- New dependencies added to `pact_engine.py` beyond `re`.
- Hank-replay test is absent.

**Ask for changes** if:
- Pattern matching is not case-insensitive.
- Reason labels don't use the stable `honesty:simulated_tool_use:<pattern>`
  form.
- Short-circuit ordering is not preserved (cumulative reasons across
  failing layers, or contract validator fires when honesty layer
  blocks).
- False-positive guard for "I ran" (requiring command/query/search
  follow-up context) is missing or wrong.

## Files Likely To Touch

- `images/body/pact_engine.py` — add honesty layer to
  `evaluate_pre_commit`; add pattern constants; add
  `_detect_simulated_tool_use` helper.
- Optionally `images/body/honesty_check.py` (new sibling) — if the
  pattern logic grows beyond ~40 lines.
- `images/body/test_honesty_check.py` (new) or extended
  `images/body/test_pre_commit_evaluator.py`.
- `images/body/work_contract.py` — no changes needed unless new helpers
  are exported (probably not).
- `docs/specs/pact-governed-agent-execution.md` — Checkpoint subsection
  update only.

## ASK Compliance

- **#1 external enforcement** — the honesty check is runtime-owned and
  external to the agent boundary. The agent cannot disable it, configure
  it, or override the pattern list. This is exactly the enforcement the
  amended Principle 4 requires.
- **#2 audit append-only** — no new audit event shapes. New
  `honesty:*` reason strings live inside the existing `reasons` list of
  `pact_verdict` signal payload. Audit JSONL unchanged.
- **#3 complete mediation** — the check reads from the mediated
  evidence ledger. It accepts only entries with provenance
  `mediated`/`provider`/`runtime` as grounding. Agent-authored prose
  can never self-ground.
- **#4 fail-closed** — when patterns match and evidence is empty, block.
  Default direction is deny.
- **#5 runtime is a known quantity** — `honesty:simulated_tool_use:*`
  reasons in the audit trail make fabrication attempts
  operator-inspectable. Net ASK gain.
- **#7 least privilege / #8 bounded operations** — no new capabilities
  or budget changes.
- **#18 governance hierarchy inviolable from below** — the pattern
  list is module-level, not configurable by the agent or by
  agent-proposed state. Enforcement cannot be lowered from below.
- **#22 unknown conflicts default to yield and flag** — if a pattern
  is ambiguous, the default is to block (false positive is safer than
  false negative for honesty).

**Forward-looking ASK notes:**

- Tier 2 claim grounding will add model-assisted critique. ASK tenet
  #1 requires that critique to be runtime-owned (agent cannot self-
  assess its own claims). Design must ensure the critique layer runs
  in a separate model instance with no access to the main agent's
  reasoning state.
- Classifier improvements (route "analyze X" → evidence-requiring
  contract) are complementary but orthogonal. Even with a perfect
  classifier, the honesty invariant still needs this layer — an
  `external_side_effect` contract can still have prose that announces
  unexecuted tool calls.

## Out-of-band Notes For Codex

- Pattern list is the load-bearing piece. Implement it precisely as
  specified. Do not add patterns without product approval; do not
  remove patterns.
- The "I ran" false-positive guard is important — test case 9
  specifically covers "I ran into an issue" which must pass. Implement
  it as a look-ahead regex or a secondary character-class check.
- The hank-replay test (case 13) is the load-bearing integration
  backstop. Construct the fixture from plausible hank-style prose —
  you do not need to load real hank state. A literal string like
  `"Let me search for the graphify stargazers. Based on my research, the repository has 32.3k stars..."`
  plus an `ExecutionState` with a `chat` contract and empty evidence
  is sufficient.
- Commit style: plain commit title, no Co-Authored-By trailer.
- PR target: `main`. Open as ready-for-review, not draft.
- Do NOT re-add the brief file; it is already committed on this branch.
