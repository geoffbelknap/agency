# PACT Execution Mode Prompt Injection

## Reference

- Spec: `docs/specs/pact-governed-agent-execution.md` → Core Concept
  "Strategy" / `ExecutionMode`; and `docs/specs/task-tier-and-prompt-composition.md`.
- Builds on: Wave 2 #2 strategy router (merged — emits `ExecutionMode`);
  mode-aware router (merged — routes grounded chat to `tool_loop`); tier
  rebalance #273 (merged — prompt now has full static baseline).
- Fixes the remaining gap observed in the hank4 test: the LLM does not see
  `strategy.execution_mode`, so routing to `tool_loop` has no effect on
  model behavior.

## Objective

Surface `strategy.execution_mode` to the LLM via the system prompt. When the
runtime routes a turn to `tool_loop`, `clarify`, `escalate`, or
`external_side_effect`, inject an explicit instructional section into the
prompt that tells the model what the runtime expects.

This is the last missing rung in the prevention stack. Without it,
everything upstream (objective builder, strategy router, tier rebalance,
Sonnet selection, full static prompt) is invisible to the LLM, so
grounded-chat asks route to `tool_loop` without the model knowing it's
expected to actually call tools.

## Why

Hank4 end-to-end test exposed the gap:

- Objective: `generation_mode=grounded` ✓
- Strategy: `execution_mode=tool_loop`, `needs_planner=False` ✓
- Three axes: `model=claude-sonnet`, `context_depth=task-relevant`,
  `reasoning_depth=reflective` ✓
- Prompt: full static baseline (FRAMEWORK/AGENTS/skills/PLATFORM/comms) ✓
- **LLM behavior: Sonnet responded conversationally with simulated tool
  markup, did not call web_search.**

The classifier kept the contract as `kind=chat` (no trigger word like
"check" or "search" in the activation), so the contract's evidence
requirements were empty. Sonnet read "casual chat, tools available" and
produced the chat-mode default: conversational prose with some tool-shaped
text that the body runtime caught. The routing layer above said "this
should enter tool_loop," but the LLM never saw that signal.

Making `execution_mode` visible at the prompt layer closes the loop.

## Scope (in this PR)

### Add an execution-mode-aware prompt section

In `body.py`'s `_build_system_prompt` (around line 1606), add a new prompt
section injected when `self._execution_state.strategy.execution_mode` is
one of the mode values below. Placement: after the existing provider
tools section, before the existing "How to Respond" section.

### Mode-specific content

Use stable, prescriptive language. The model reads these as runtime-issued
instructions; phrasing should be unambiguous.

**`tool_loop`:**

```text
# Execution Mode: tool_loop

The runtime has routed this turn to tool_loop mode because the
operator's request requires external information that only tools can
produce. Before responding with any factual claim:

1. You MUST call one of the available tools (e.g., web_search) to gather evidence.
2. Use the tool's real output to inform your response.

Do not emit tool-shaped text such as <search>...</search> or
search(query=...). Do not announce tool use ("Let me search",
"I searched", "Based on my research") without actually calling the
tool. If a required tool is unavailable or fails, say so directly and
do not guess.
```

**`clarify`:**

```text
# Execution Mode: clarify

The runtime has routed this turn to clarification mode because the
request contains a load-bearing ambiguity (missing target, unknown
authority scope, or similar) that must be resolved before the work can
proceed safely.

Respond with a specific, scoped clarification question addressed to
the operator. Do not attempt to answer the original request. Do not
guess or speculate to fill the ambiguity.
```

**`escalate`:**

```text
# Execution Mode: escalate

The runtime has routed this turn to escalation mode because the request
exceeds current authority: an untrusted principal, escalated risk, or
out-of-scope ask.

Explain specifically what cannot be done and what operator action is
needed to unblock (additional capabilities, authority verification,
or principal review). Do not attempt to perform the requested work.
```

**`external_side_effect`:**

```text
# Execution Mode: external_side_effect

The runtime has routed this turn to external_side_effect mode because
the request will mutate external state (write to a service, call an
API, change records).

Before performing any side-effecting operation:

1. Verify principal authority is in scope for this action.
2. Request explicit operator approval. Do not act on assumed
   permission.

Once approval is recorded, perform the operation and report the
outcome. Do not claim a side effect occurred without observed
confirmation from the runtime.
```

**No injection for:**

- `trivial_direct` — existing behavior (conversational, no forcing)
- `planned` — has its own planner scaffolding; separate future rewire
  work can tailor the prompt for plan execution
- `delegated` — not used today; no prompt injection until delegation
  is wired

### Where `strategy` is read

The prompt builder already has access to `self._execution_state` (from
the Wave 1 #1 work). In the new section of `_build_system_prompt`:

```python
mode_section = _execution_mode_prompt_section(self._execution_state)
if mode_section:
    parts.append(mode_section)
```

Helper function in body.py (or a sibling module):

```python
def _execution_mode_prompt_section(state: ExecutionState | None) -> str:
    if state is None or state.strategy is None:
        return ""
    mode = _enum_value(state.strategy.execution_mode)
    if mode not in _MODE_PROMPT_SECTIONS:
        return ""
    return _MODE_PROMPT_SECTIONS[mode]
```

Where `_MODE_PROMPT_SECTIONS` is a module-level dict mapping mode
strings to the content blocks above. Mode strings are the StrEnum
`.value` form (e.g., `"tool_loop"`, `"clarify"`).

### Interaction with other prompt sections

The existing `_provider_tool_prompt_section` already describes available
tools. Keep it unchanged — this PR adds a new section *about what the
runtime expects the model to do with those tools this turn*, not what
the tools are.

Ordering in the composed prompt:

```text
identity.md
(mission context, if any)
FRAMEWORK.md
AGENTS.md
skills section
PLATFORM.md
(persistent memory / org context, if context_depth allows)
comms context
Provider Tools (existing — describes available tools)
Execution Mode: <mode> (NEW — describes what to do)
How to Respond (existing)
```

Execution Mode goes right before How to Respond so it's the last
instruction the model reads before composing its reply.

### No changes to

- `Strategy` dataclass (already carries `execution_mode`)
- `ExecutionState` (already carries `strategy`)
- `build_strategy` (routing rules unchanged)
- Pre-commit evaluator or honesty check
- Contract classifier (the "investigate → chat" gap is separate; fix
  is parallel but not required to make this PR work)
- Tier rebalance (PR #273) — the three axes stay as they are
- OpenAPI, web UI, Go files

### Spec Checkpoint update

Update `docs/specs/pact-governed-agent-execution.md` "### Execution State
Type" subsection to note that `strategy.execution_mode` now surfaces in
the system prompt as an explicit runtime instruction when the mode is
non-default (`tool_loop`/`clarify`/`escalate`/`external_side_effect`).
No changes to other spec sections.

Also add a one-line pointer in
`docs/specs/task-tier-and-prompt-composition.md` under "Always Included
(identity bandwidth)" or a new "Execution Instructions" subsection
noting that mode-specific instructional content is included when
applicable.

### Tests

New `images/body/test_execution_mode_prompt_injection.py` covering:

1. `strategy.execution_mode == tool_loop` → prompt contains
   `# Execution Mode: tool_loop` AND explicit "MUST call ... tools
   to gather evidence" instruction.
2. `strategy.execution_mode == clarify` → prompt contains
   `# Execution Mode: clarify` AND "Respond with a ... clarification
   question" instruction.
3. `strategy.execution_mode == escalate` → prompt contains
   `# Execution Mode: escalate` instruction.
4. `strategy.execution_mode == external_side_effect` → prompt
   contains `# Execution Mode: external_side_effect` AND "Request
   explicit operator approval" instruction.
5. `strategy.execution_mode == trivial_direct` → NO
   `# Execution Mode:` section in prompt.
6. `strategy.execution_mode == planned` → NO `# Execution Mode:`
   section (planner has its own prompt content).
7. `strategy is None` OR `execution_state is None` → NO mode section.
8. **Hank4-replay integration**: construct task dict with hank's
   activation text (the "investigate this github repository..."
   message), run through `ExecutionState.from_task`, then build the
   system prompt. Assert:
   - `objective.generation_mode == "grounded"` (from #270)
   - `strategy.execution_mode == "tool_loop"` (from #271)
   - Composed prompt contains `# Execution Mode: tool_loop`
   - Composed prompt contains the verbatim "MUST call" language
   - This is the load-bearing end-to-end test proving the signal
     reaches the LLM.
9. Prompt ordering: the execution-mode section appears AFTER
   the provider tools section and BEFORE the "How to Respond" section
   (positional assertion against the composed output).
10. Unknown/future mode strings (defensive): if
    `execution_mode` is some value not in `_MODE_PROMPT_SECTIONS`
    (e.g., a hypothetical `delegated`), no section is injected. No
    crash.

## Non-Scope

- **Classifier improvements.** The `classify_activation` function's
  weakness on analytical verbs ("investigate", "analyze") is a
  separate, parallel fix. This PR works regardless of classifier
  accuracy because `strategy.execution_mode` is set by the router,
  not by the contract kind.
- **Contract kind changes.** No new contract kinds. No changes to
  existing validators.
- **Planner prompt content.** `planned` mode still has no prompt
  injection; the planner itself produces plan content if/when its
  execution is wired (Wave 2 #3b is a separate follow-up).
- **Delegation mode.** Defined in the enum but not used; no
  injection.
- **Tier 2 claim grounding.** Deferred as always.
- **Anthropic native tool use config changes.** The tool declaration
  format (`{"type": "web_search"}`) is unchanged; yesterday's hank
  run proved it works when the LLM is prompted to use it.
- **OpenAPI, web UI, feature registry, Go files.**

## Acceptance Criteria

1. `_MODE_PROMPT_SECTIONS` (or equivalent) constant defined with
   exactly four entries: `tool_loop`, `clarify`, `escalate`,
   `external_side_effect`. Content matches the Scope section
   verbatim — same headings, same instructions, same guard language.
2. `_execution_mode_prompt_section` helper returns the appropriate
   section string when `state.strategy.execution_mode` matches an
   entry; returns empty string otherwise (including `None` state,
   `None` strategy, and unknown modes).
3. `_build_system_prompt` includes the mode section after provider
   tools and before "How to Respond".
4. `trivial_direct` and `planned` get no injection (current behavior
   preserved).
5. 10 test cases in `images/body/test_execution_mode_prompt_injection.py`.
   Test 8 (hank4 replay integration) must assert both generation_mode
   and the presence of the tool_loop instruction in the composed
   prompt.
6. Existing prompt composition tests continue to pass without
   modification. The new section is additive.
7. No public API shapes change. `pact_verdict` payload unchanged.
   Audit-report hash stable.
8. `pytest images/tests/` and `go build ./cmd/gateway/` pass.
9. Spec Checkpoint subsection in
   `pact-governed-agent-execution.md` updated.
10. `task-tier-and-prompt-composition.md` gains a one-line pointer.

## Review Gates

**Reject** if:
- Mode content deviates from the verbatim text in Scope. The wording
  is load-bearing; it's what the LLM reads and responds to.
- New mode values injected beyond the four listed.
- Mode section is placed anywhere other than between provider tools
  and "How to Respond".
- Agent-authored data influences which mode content is selected.
  Only runtime-typed state (`strategy.execution_mode`) drives the
  selection.
- `Strategy`, `ExecutionMode`, or the router rules are modified.
- Pre-commit evaluator, honesty check, or contract classifier is
  modified.
- Any existing test assertion is changed. Fixture adjustments
  that explicitly set `strategy.execution_mode` on non-tool_loop
  paths (to preserve current trivial_direct behavior in tests) are
  acceptable; assertion changes are not.

**Ask for changes** if:
- `_MODE_PROMPT_SECTIONS` is a class constant instead of a module
  constant (should be module-level for clarity and to match
  `TOOL_ANNOUNCEMENT_PATTERNS` style).
- Helper function signature deviates from `state: ExecutionState |
  None`.
- Tests don't cover the positional ordering assertion (test #9) or
  the hank4 replay (test #8).

## Files Likely To Touch

- `images/body/body.py` — add `_MODE_PROMPT_SECTIONS` constant,
  `_execution_mode_prompt_section` helper, and one call site in
  `_build_system_prompt`.
- `images/body/test_execution_mode_prompt_injection.py` (new).
- `docs/specs/pact-governed-agent-execution.md` — Checkpoint
  subsection update.
- `docs/specs/task-tier-and-prompt-composition.md` — one-line
  pointer.

## ASK Compliance

- **#1 external enforcement** — mode selection and prompt content
  are module-level constants in runtime code. Agent cannot alter.
- **#4 fail-closed** — unknown/unset mode → no injection → default
  behavior (current, safe). Adding a mode block only tightens
  instruction; absence never introduces permission.
- **#5 runtime is a known quantity** — the prompt now makes the
  runtime's routing decision visible. Both the LLM and any operator
  reading the audit prompt log can see why the turn was shaped the
  way it was.
- **#18 governance hierarchy inviolable from below** — the mode
  block tells the LLM what the runtime expects. The agent's
  response to that instruction is then evaluated by the existing
  PACT enforcement layers (pre-commit evaluator, honesty check).
  The injection does not replace enforcement; it strengthens the
  upstream signal so enforcement fires less often.
- **#22 unknown conflicts default to yield and flag** — unknown
  mode values → no injection, no crash, silent pass-through.
  Consistent with the safety-biased default across the stack.

**Forward-looking ASK notes:**

- The `tool_loop` injection explicitly tells the model "do not
  announce tool use without calling the tool." This is upstream
  reinforcement of the Tier 1 honesty check pattern from #268.
  Fewer attempts should reach the backstop.
- The `external_side_effect` injection requires operator approval
  before acting. Wave 2 #3b runtime-executes-plan work must
  preserve this — plans for side effects cannot bypass approval.
- The `clarify` injection forces a clarification question instead
  of an answer. Works hand-in-hand with the load-bearing ambiguity
  detection from #270.

## Out-of-band Notes For Codex

- The verbatim mode content in Scope is authoritative. Do not
  paraphrase, trim, or "improve" the language. Tests 1-4 assert
  specific phrases; if the wording drifts, tests fail.
- Module-level constants, not class constants. The pattern style is
  the same as `TOOL_ANNOUNCEMENT_PATTERNS` in `pact_engine.py`.
- Tests must verify the positional ordering of sections (test #9).
  Simple way: build the full prompt, check that the `#
  Execution Mode:` header appears at a string index greater than
  the `# Provider Tools` header index and less than the `# How to
  Respond` header index. The existing body runtime emits those
  headers verbatim.
- The hank4 replay test (#8) should construct a minimal fixture:
  `ExecutionState.from_task({"task_id": "t1", "metadata":
  {"pact_activation": {"content": "<hank4 prompt text>", ...},
  "work_contract": {"kind": "chat", ...}}}, agent="hank4")`.
  Then call the prompt builder. Assert the mode section is present.
- Commit style: plain commit title, no Co-Authored-By trailer.
- PR target: `main`. Open as ready-for-review, not draft.
- Brief is already committed on the branch; do not re-add it.
