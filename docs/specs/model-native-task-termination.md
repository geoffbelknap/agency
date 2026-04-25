# Model-Native Task Termination

## Status

Phase 2 implemented; Phases 3/4 pending.

## Purpose

This spec governs how the Agency body runtime terminates a task turn —
specifically replacing the current `complete_task`-as-agent-tool pattern
with model-native termination signals. The runtime decides a task is
complete when the model's final response carries a
termination-class `stop_reason` (Anthropic `end_turn`, OpenAI `stop`,
Google `STOP`, etc.) and contains no pending tool-use blocks. The agent
no longer needs an explicit completion tool.

PACT's commit flow is preserved: the evaluator still gates commit
versus retry versus block. What changes is the trigger — from
"agent-authored tool call" to "runtime-observed provider signal."

## Non-Goals

- This spec does not replace PACT. PACT governs *what* may commit;
  this spec governs *when* the commit hook is invoked.
- This spec does not change Agency's channel/comms model. Operator
  DMs and channel posts still flow through the comms enforcer.
- This spec does not redesign task IDs, result artifacts, or audit
  JSONL shapes beyond the minimal changes needed to capture the new
  termination signal.
- This spec does not prescribe multi-agent delegation patterns. Those
  have their own spec territory.
- This spec does not replace the current `send_message` tool. Agents
  that need to post to channels other than the activation source
  still use it.

## Motivating Observation

A real operator test (hank6) exposed the design flaw this spec fixes.
The agent was correctly routed through every PACT prevention layer
— objective builder, strategy router, three-axis tier, execution mode
prompt injection, classifier expansion — and Sonnet successfully
called `provider-web-search` with real results. After the first
`complete_task` attempt returned PACT `needs_action` (missing
`source_url`/`checked_date` in the response text), the retry path
injected a platform message. Sonnet then:

- Wrote a better response
- Called `send_message` (posted to channel)
- **Did not call `complete_task`**
- Was re-invoked on the next turn
- Wrote another response
- Called `send_message` again
- Did not call `complete_task`
- Repeat indefinitely

The runtime kept cycling because the turn-loop terminator was tied to
an agent-authored tool call. Sonnet, responding to conversation context
(including a platform retry message + duplicate operator prompts), kept
"helpfully" rewording its answer without finalizing. Six responses in
six minutes, $0.20 of tokens, no termination — until operator halted
the agent manually.

This is a structural failure of the `complete_task`-as-agent-tool
pattern. It is not a PACT correctness failure (every response was
grounded and cited), and not a model failure (Sonnet was doing exactly
what its training would suggest — responding helpfully to conversation).
It is a runtime contract failure: the runtime demanded an explicit
completion tool that models are not generally trained to call, and had
no fallback when the model did not.

## Design Principle

> **Task termination is a runtime decision based on model-native
> signals, not an agent-authored tool call.**
>
> The model signals completion through provider-standard mechanisms
> (`stop_reason: end_turn`, tool-use absence, `finish_reason: stop`).
> The runtime observes those signals, invokes the PACT commit hook
> internally, and ends the turn loop. The agent is not responsible
> for remembering to call a completion tool, because modern LLMs are
> not reliably trained to do so. Every agent loop caused by missing
> or skipped completion-tool invocation is prevented structurally,
> not by prompt engineering or retry budgets.

Per ASK tenet #8 (bounded operations), the runtime additionally
enforces a hard maximum turn count as a safety net independent of
model behavior. The hard cap is a backstop, not the primary
termination mechanism.

## Related Design Principles

This spec composes with existing principles:

- **PACT Design Principle 4 (honesty invariant)** — unchanged. PACT
  still evaluates the final response content regardless of how
  termination was signaled.
- **PACT Design Principle 8 (runtime commits outcomes)** — reinforced.
  The runtime now *directly* owns the commit decision. The agent
  has no tool that triggers commit.
- **Task Tier / Prompt Composition Principle (tier gates cost, not
  identity)** — unchanged.

## Adapter Contract

Termination is keyed on provider metadata, but the body runtime must not
parse provider-native payloads directly. Provider adapters own translation into
the runtime contract.

The runtime contract is:

- chat-completions-like response envelope with `choices[0].message`
- `tool_calls` normalized into the message when the model is requesting tools
- `finish_reason` normalized for generic control flow
- additive `stop_reason` metadata when the provider exposes a more specific
  native termination class that should remain visible for audit and artifacts

In other words: provider quirks stop at the adapter boundary. The body runtime
consumes one normalized shape and derives `TurnOutcome` from that shape.

## Provider Signal Mapping

### Anthropic (Claude)

**Terminal signals:**
- `stop_reason: "end_turn"` — model finished its turn normally
- `stop_reason: "stop_sequence"` — a configured stop sequence was hit
- `stop_reason: "max_tokens"` — exhausted output budget (treat as
  terminal-with-warning; will often need retry for incomplete content)

**Non-terminal signals:**
- `stop_reason: "tool_use"` — model wants to call a tool; runtime
  executes and re-invokes the model. Turn loop continues.
- `stop_reason: "pause_turn"` — server-side tool is still processing;
  runtime awaits completion and continues.

**Other signals:**
- `stop_reason: "refusal"` — model declined; runtime treats as
  terminal-failed per existing refusal handling.
- `stop_reason: "model_context_window_exceeded"` — terminal-failed;
  runtime compacts context or fails the task.

### OpenAI (responses / chat completions)

**Terminal signals:**
- `finish_reason: "stop"` — normal completion
- `finish_reason: "length"` — token budget exhausted

**Non-terminal signals:**
- `finish_reason: "tool_calls"` — model wants to call tools
- `finish_reason: "function_call"` (legacy) — deprecated but supported

### Google (Gemini)

**Terminal signals:**
- `finishReason: "STOP"` — normal completion
- `finishReason: "MAX_TOKENS"` — budget exhausted

**Non-terminal signals:**
- `finishReason: "OTHER"` (with pending function calls) — continue

### Provider-agnostic view

The runtime's completion detector exposes two outputs per model
invocation:

```python
@dataclass(frozen=True)
class TurnOutcome:
    is_terminal: bool          # True → task turn ends
    has_pending_tool_use: bool # True → runtime executes tool, re-invokes
    final_text: str            # The response text (for commit)
    stop_reason: str           # Provider-specific, preserved for audit
```

The detector is a module-level pure function over the normalized adapter
contract, not over raw provider payloads. Adapters translate raw provider
responses into the normalized envelope first; the runtime then derives
`TurnOutcome` without branching on provider identity.

## Task Lifecycle

Under this spec, the per-task runtime flow becomes:

```
activation received
  ↓
runtime creates ExecutionState (Wave 1 #1)
  ↓
runtime constructs system prompt (Tier rebalance)
  ↓
turn loop:
  - runtime invokes LLM
  - completion detector parses response
  - if has_pending_tool_use:
      runtime executes the tool, appends result to conversation
      continue turn loop
  - if is_terminal:
      break turn loop → commit phase
  - if turn count exceeds hard_turn_cap (safety net):
      break turn loop → forced failure
  ↓
commit phase:
  - runtime invokes PACT evaluate_pre_commit on final_text
  - if committable:
      runtime posts final_text to activation source channel
      runtime writes result artifact
      runtime emits pact_verdict signal (completed/blocked)
      task state clears
  - if contract:needs_action and retry budget available:
      runtime injects platform retry message
      runtime re-enters turn loop (this re-engages the model)
  - if non-retryable non-committable:
      runtime posts blocker message to activation source channel
      runtime writes result artifact with blocked outcome
      task state clears
```

Key differences from current behavior:

1. **`complete_task` is no longer an agent-callable tool.** The agent
   does not see it in its tool list. If legacy agent configurations
   still reference it, the runtime provides a no-op compatibility shim
   that logs a deprecation warning.
2. **The turn loop terminates on provider signal, not agent tool
   call.** Sonnet's natural "done" behavior ends the turn.
3. **`send_message` remains available** for cross-channel posting,
   but does NOT trigger task commit. A turn containing a
   `send_message` tool_use call followed by model completion posts
   the sent message AND the final text response (the text goes to
   the activation source channel, consistent with today's behavior
   where `complete_task`'s `summary` is the channel response).
4. **Retry path uses the same turn loop.** The platform retry
   message is injected as a user-role message, the runtime re-enters
   the turn loop, the loop's natural termination applies on the
   next model response.

## Tool Interactions

### `send_message`

Continues to work as a side-effect tool. A `send_message` call
during a turn posts the specified content to the specified channel
immediately. The turn continues (since `send_message` returns a
tool_result).

The distinction between "respond to the operator" (final text to
activation channel) and "post to another channel" becomes cleaner:
- Final text (post-termination) → auto-routed to activation source
  channel
- Explicit `send_message` calls → go to whatever channel the agent
  specified

Legacy agent configurations that wired `send_message` to the
activation channel for the primary response are **not deprecated** —
they continue to work. The runtime simply also posts the final text
response if different from the last `send_message` content.

### Multi-tool turns

A single turn may contain multiple `tool_use` blocks (Anthropic
supports parallel tool use). The runtime executes all of them,
appends all results, and re-invokes the model. Same as today.

### Tools that previously required `complete_task` after

None. `complete_task` was always the termination signal, not a
dependency of other tools. Removing the agent-facing tool doesn't
affect tool composition.

## Hard Turn Cap (Safety Net)

Regardless of model behavior, the runtime enforces a maximum turn
count per task. Default: 8 turns. Configurable per mission or per
task via existing `cost_mode` upper bounds.

When the cap is hit:

- Runtime exits the turn loop
- Invokes PACT commit on whatever the final accumulated text is
  (if any), marking the verdict `failed` with reason
  `runtime:turn_limit_exceeded`
- Writes result artifact with the limit as the terminal reason
- Audit event captures the limit violation

The cap is ASK-aligned:

- **Tenet #8 bounded operations** — every task has a runtime-
  enforced turn budget independent of agent behavior
- **Tenet #4 fail-closed** — hitting the cap terminates the task
  in a safe state; does not extend the budget silently

The cap also serves as the mitigation for the class of loops that
motivated this spec. Even if the termination detector misclassifies
a signal as non-terminal, the cap ends the task.

## PACT Integration

### Commit hook mapping

Today: `complete_task` tool → PACT evaluator → verdict → commit or
retry.

Tomorrow: provider terminal signal → runtime commit method → PACT
evaluator → verdict → commit or retry.

The PACT evaluator (`evaluate_pre_commit`) is unchanged. The input
(ExecutionState + final text content) is unchanged. The trigger
changes from `complete_task` to `is_terminal == true`.

### Verdict-retry loop

When PACT returns `contract:needs_action`, the runtime injects a
platform message into the conversation and re-enters the turn loop.
The retry budget is tracked by the existing `_work_contract_retry_sent`
flag (or its Wave 2 #5b successor) to prevent infinite retries.

Crucially: **the retry does not require the agent to call any
specific tool.** The agent's next model invocation proceeds normally;
when it naturally terminates (stop_reason=end_turn), the runtime
re-invokes PACT and either commits or blocks.

This closes the hank6 loop: once retry budget is exhausted, the
block is terminal regardless of further model behavior.

### Honesty check (Tier 1)

Unchanged. The honesty check runs as part of `evaluate_pre_commit`
regardless of how termination was triggered.

### Plan, reflection, evaluation

Unchanged. Wave 2 primitives operate on typed ExecutionState. The
termination trigger change does not affect them.

## Audit and Observability

### `pact_verdict` signal

Payload unchanged. The existing `reasons` field may add new values:

- `runtime:turn_limit_exceeded` — hard cap hit
- `runtime:model_terminated` — normal completion (informational, not
  a block reason)
- `runtime:refusal` — model refused (terminal)
- `runtime:context_exhausted` — context window hit (terminal-failed)

No schema changes. Additive reason values.

### Result artifact frontmatter

Gains `stop_reason` field in the `pact` block, capturing the
provider-native termination signal. Additive; legacy artifacts
without the field are unaffected.

### Run projection

`/api/v1/agents/{name}/pact/runs/{taskId}` projection gains
`stop_reason` as an additive field. OpenAPI schema updated.

### Audit hash

Hash scope includes `stop_reason` after this change. Uniform schema
evolution (same run → same hash; new runs produce hashes different
from pre-migration hashes, which is expected and already tolerated
in prior schema additions).

## Migration Path

### Phase 1 — parallel operation (the body runtime learns to see the signal)

1. Add the provider-agnostic completion detector (`TurnOutcome`
   abstraction + Anthropic/OpenAI/Google adapters).
2. Instrument the turn loop to observe and audit the signal, but
   continue to require `complete_task` for commit. No behavior
   change. Signal is advisory, used for observability.

### Phase 2 — dual termination (signal becomes authoritative for commit trigger, with hard turn cap)

1. Turn loop exits on either `complete_task` call OR
   `is_terminal == true` from the detector, whichever comes first.
2. Hard turn cap added as safety net.
3. `complete_task` remains an available tool for backward
   compatibility, but the agent is not required to call it.
4. System prompt updated to remove "call complete_task when done"
   instruction. Existing instruction is counter-trained and
   unnecessary under the new flow.

### Phase 3 — agent-tool deprecation

1. `complete_task` removed from agent tool lists.
2. Runtime still defines a `complete_task` internal commit method,
   invoked by the detector on termination.
3. Meeseeks prompt (which explicitly instructs `complete_task`
   calls) updated to rely on natural termination.
4. Legacy agent configurations that call `complete_task` continue
   to work via the compatibility shim (silent pass-through with
   deprecation log).

### Phase 4 — cleanup

1. Remove the compatibility shim once telemetry shows no agents
   calling `complete_task`.
2. Remove stale prompt language referencing `complete_task`.
3. Audit event labels migrated; legacy label (`complete_task`)
   remains readable but new events use `stop_reason`.

Each phase is independently shippable. Phase 2 closes the hank6
loop. Phase 3 matches modern agent patterns. Phase 4 is cleanup.

## ASK Compliance

- **Tenet #1 external enforcement** — termination detection is pure
  runtime code. Completion detector adapters are module-level per
  provider, not agent-configurable. The agent cannot alter
  detection logic.
- **Tenet #2 audit** — additive `stop_reason` field on existing
  signals and artifacts. No audit JSONL mutations. Existing hash
  scope grows with the new field (uniform evolution).
- **Tenet #3 complete mediation** — the agent no longer needs an
  agent-facing commit tool. Commit is runtime-initiated. Removes a
  surface where agent authority could be ambiguous.
- **Tenet #4 fail-closed** — hard turn cap ensures bounded
  execution regardless of detection correctness. Unknown stop
  reasons treated as non-terminal by default; cap kicks in.
- **Tenet #5 runtime is a known quantity** — stop_reason surfaced
  in audit makes termination decisions operator-inspectable. Every
  task now carries its termination cause.
- **Tenet #7 least privilege** — agent loses a tool it could call
  to trigger commit. This narrows agent authority in a safe
  direction.
- **Tenet #8 bounded operations** — hard turn cap is an explicit
  new bound. Retry budget unchanged.
- **Tenet #11 halts auditable** — halted tasks still terminate via
  the existing halt path. Termination detector is orthogonal.
- **Tenet #18 governance hierarchy inviolable from below** — agent
  cannot override termination detection through tool calls or
  response content. Detection reads provider metadata, which
  agent-emitted text does not control.
- **Tenet #22 unknown conflicts default to yield and flag** —
  unknown `stop_reason` values treated as non-terminal. Hard cap
  eventually fires. Never extends the budget silently.

**Forward-looking ASK notes:**

- Future multi-agent delegation must preserve this principle:
  delegated sub-agents terminate via their own provider signals,
  not via the delegator injecting a completion tool.
- Future model providers without structured stop-reason metadata
  would require a detector fallback (e.g., "no tool_use blocks
  after N chars of text" heuristic). Provider-less detection
  cannot be primary; it is only acceptable as a fallback with the
  hard cap as backstop.

## Open Questions

- **Should `complete_task` retain any semantic role?** One
  possibility: `complete_task` becomes "agent requests early
  termination with a specific summary," useful when the agent
  wants to end the turn before the provider's natural
  `end_turn` signal. Unclear whether there's a real use case.
- **Default hard turn cap value** — 8 is a guess. Should be
  calibrated against observed task complexity (tool loops with
  several searches + reflection + retry = how many turns?).
- **Meeseeks termination semantics** — Meeseeks prompts today
  explicitly instruct `complete_task` calls. Under the new model,
  Meeseeks agents naturally terminate on `stop_reason: end_turn`.
  Confirm that Meeseeks short-lifecycle semantics are preserved.
- **Context-window-exceeded handling** — today's runtime has ad-hoc
  compaction. Should context exhaustion be a terminal failure
  (simpler), or trigger automatic summarization and retry
  (more complex)? Probably terminal failure with operator-visible
  reason in Phase 2, with automatic compaction as a follow-up.
- **`send_message` to activation channel vs. final text** — if an
  agent calls `send_message` with content identical to or overlapping
  with its final text response, does the runtime de-duplicate?
  Probably yes; details in implementation.
- **Provider-specific weirdness** — some Anthropic model versions
  (e.g., extended thinking) have different stop_reason semantics.
  Worth an audit before Phase 2.
- **Replay / resume semantics** — if a task hits the hard cap,
  should operator be able to "resume with extended budget"?
  Probably yes for specific recovery cases; needs a governance
  pathway per ASK tenet #11.

## Relationship to Other Specs

- **PACT spec** (`docs/specs/pact-governed-agent-execution.md`) —
  referenced throughout. PACT's commit flow is reused; only the
  trigger mechanism changes.
- **Task tier and prompt composition** (`docs/specs/task-tier-and-prompt-composition.md`)
  — independent. Tier gating operates on the same axes regardless
  of termination mechanism.
- **Wave 2 #5 recovery state machine** — the recovery primitives
  (merged) and their future body.py rewire (#5b, deferred) compose
  with this spec cleanly. Recovery state tracks retry attempt
  counts; hard turn cap is a separate bound on total per-task
  turns.
- **Wave 2 #4 pre-commit evaluator / #4b runtime gate** — the
  commit hook that this spec redirects. No changes to evaluator
  logic; only its trigger changes.

## Summary

The body runtime will terminate tasks based on provider-native model
stop signals, not on agent-authored `complete_task` tool calls. This
aligns Agency with modern agent frameworks, removes an entire class
of loops caused by models not calling an explicit completion tool,
and brings the termination mechanism under full runtime ownership
per ASK tenet #1.

Migration is phased to preserve backward compatibility during the
transition. The hard turn cap is a permanent safety net independent
of detection correctness.
