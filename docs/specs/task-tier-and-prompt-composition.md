# Task Tier and Prompt Composition

## Status

Draft.

## Purpose

This spec governs how the Agency body runtime composes the input to every LLM
turn: which sections are included in the system prompt, which model is used,
and which retrievals are performed. It replaces the implicit single-knob
"tier" model (`minimal` / `standard` / `full`) that currently tangles three
distinct optimization axes into one classifier and mis-optimizes at every
axis simultaneously.

The core shift: **tier gates reasoning depth, not identity bandwidth**. The
agent's operational identity, tools, and environmental awareness are
constant; only deliberative work is tier-gated.

## Non-Goals

- This is not a PACT spec. PACT governs contract-bound execution; this spec
  governs how the body runtime composes a turn. They are complementary.
- This is not an LLM provider routing spec. Cross-provider fallback,
  provider-specific features, and multi-provider orchestration are out of
  scope.
- This is not a memory/retrieval system design. It describes when memory and
  retrieval are invoked per turn; it does not redesign the graph, cache, or
  retrieval protocols.
- This is not a per-tool cost optimization. It does not govern which tools
  the agent has available; that is the capability registry's concern.

## Motivating Observation

A real operator test exposed the design flaw this spec fixes. A researcher
agent (hank3) was given a grounded analytical ask over DM. The runtime
classified the task as `minimal` tier (short direct-source DM, no mission,
hits the default fallback in `classify_task_tier`). Under `minimal`:

- Model dropped to `claude-haiku` (weaker reasoning)
- System prompt lost `FRAMEWORK.md`, `AGENTS.md`, skills section,
  `PLATFORM.md`, comms context
- Only `identity.md` + a one-line "web_search is available" note remained

The agent then emitted confident-sounding analytical prose without calling
any tool. The failure was not that PACT enforcement missed a fabrication; it
was that the agent's baseline instructional context had been stripped as a
"cost optimization" on a task where cost was never the bottleneck.

The savings from stripping static instructional content in `minimal` tier
were **pennies per turn** against operational costs that were already
trivial at conversation volume. The cost was **the agent losing its
operational identity** on exactly the tasks where that identity matters
most.

## Design Principle

> **Tier gates reasoning depth, not identity bandwidth.**
>
> The agent's operational identity — who it is, what it knows it can do,
> what framework it operates under, what services it has, where it is
> communicating — is preserved in every prompt regardless of tier.
> Tier only gates deliberative work: reflection rounds, evaluation
> passes, planning elaboration, retrieval-based memory injection, and
> optional multi-step loops.

Analogue: when a body performs an automatic task (walking without thinking),
it uses the same brain with full perception and memory. What's absent is
deliberation, not identity. Stripping perception, memory, or motor skill to
"save cost" on autopilot tasks is a lobotomy, not an optimization.

This principle reframes cost optimization at the runtime layer:

- The *unit* we optimize is deliberation depth, not identity bandwidth.
- Cost savings at scale are real, but they come from skipping optional
  deliberation and from not invoking expensive retrievals that contribute
  nothing to the task at hand — not from withholding operational context.
- Identity, framework, tools, and communication context are cheap, static,
  and cacheable. There is no scenario where including them in the prompt
  is the wrong trade.

## The Three Axes

The single-knob `tier` currently conflates three orthogonal decisions. This
spec splits them.

### Axis 1 — Reasoning Depth

How much deliberative work the runtime invests in this turn beyond a
direct LLM call.

Levels:

- **`direct`** — no deliberation. Single LLM call with its tool loop.
  The agent answers or calls tools and responds. No reflection, no
  evaluation loop, no multi-round planning.
- **`reflective`** — bounded reflection. The runtime may invoke a
  deterministic evaluator (Wave 2 #4 pre-commit evaluator) and one
  bounded reflection round if the first response falls short.
- **`deliberative`** — full deliberation. LLM-assisted success-criteria
  evaluation, multi-round reflection, plan revision.

Driven by signals:

- `objective.risk_level` — high/escalated → deeper deliberation
- `contract.kind == external_side_effect` → deliberative
- Operator-configured mission cost_mode (`frugal`/`balanced`/`thorough`)
  — upper bound
- `objective.generation_mode in {creative, social}` — direct is
  almost always correct

Default: `direct`. Escalation requires a positive signal.

### Axis 2 — Model Capability

Which LLM is invoked for this turn.

Levels:

- **`small`** — Haiku or equivalent. Sufficient for social, creative,
  persona, low-stakes conversational asks.
- **`standard`** — Sonnet or equivalent. Default for grounded
  analytical work, tool-using turns, code changes.
- **`large`** — Opus or equivalent. High-risk, external-side-effect,
  high-stakes reasoning.

Driven by signals:

- `objective.generation_mode == grounded` → at least `standard`
- `objective.risk_level == high` → at least `standard`
- `objective.risk_level == escalated` → `large` (or route to clarify)
- `contract.kind == external_side_effect` → at least `standard`;
  `large` if side effect is high-risk
- `objective.generation_mode in {creative, social, persona}` with
  `risk_level in {low, medium}` → `small` is appropriate

Default: `standard`. Downgrade to `small` only when the typed signals
explicitly authorize it.

Critically: **model capability is not coupled to reasoning depth.** A
grounded analytical DM can be `direct` reasoning depth AND `standard`
model capability — a one-shot analytical answer from Sonnet. A social
chat can be `direct` reasoning depth AND `small` model. An
external-side-effect turn can be `deliberative` reasoning depth AND
`large` model. All nine combinations are meaningful.

### Axis 3 — Context Depth

Which dynamic retrievals are performed for this turn.

Levels:

- **`minimal`** — no dynamic retrievals beyond what the turn itself
  requires. No procedural memory injection, no episodic memory
  injection, no organizational context fetch.
- **`task-relevant`** — retrievals scoped to the task topic.
  Procedural memory for the current work type; recent episodic memory
  only if the task references a prior interaction.
- **`full`** — comprehensive retrievals. Organizational context,
  procedural memory, episodic memory all injected into the prompt.

Driven by signals:

- Is the task referencing prior work? → episodic memory `task-relevant`+
- Is the task operating under a mission? → procedural memory per mission
- Is the task analytical about an external entity with known
  organizational context? → organizational `task-relevant`+
- Is this a social/creative/persona turn? → `minimal`

Default: `task-relevant`. Memory retrievals happen when they contribute
to the task; they are not stripped as a cost proxy.

## Prompt Composition Rules

The system prompt is composed from two categories of content.

### Always Included (identity bandwidth — constant across all turns)

These are static, cacheable, and collectively small (typically 5-15 KB).
They define what the agent is and how it operates. They are never tier-
gated.

- `identity.md` — agent identity and self-model
- Mission context (if any active mission)
- `FRAMEWORK.md` — behavioral framework (ASK tenets, governance rules)
- `AGENTS.md` — constraints, services, and tools available to the agent
- Skills section — capabilities the agent can invoke
- `PLATFORM.md` — environmental awareness of the platform
- Provider tools section — declarations of provider-hosted tools
  (`web_search`, etc.)
- Comms context — the agent's primary interface; stripping it blinds
  the agent to its own communication surface
- How-to-respond instructions — response format expectations

### Gated by Context Depth Axis

These are dynamic, potentially expensive, and relevant only when the
task calls for them.

- Procedural memory injection (retrieval + tokens)
- Episodic memory injection (retrieval + tokens)
- Organizational context (knowledge service fetch + tokens)
- Persistent memory index (file reads + tokens, scales with memory size)
- Persistent memory tools (save_memory, search_memory) — included when
  the agent has nontrivial memory to surface

### Gated by Reasoning Depth Axis

These are extra LLM cycles beyond the primary turn.

- Pre-commit evaluator (the general one from Wave 2 #4) — always
  runs at every tier; this is enforcement, not optional deliberation
- Reflection loop — bounded retry after first-response shortfall
- LLM-assisted success criteria evaluation — extra LLM call
- Plan revision — extra LLM call

## Signals That Drive Routing

The typed signals populated by the PACT primitives (Wave 2 objective
builder, strategy router, etc.) are the inputs to routing decisions on all
three axes.

| Signal | Source | Used by axis |
|---|---|---|
| `objective.generation_mode` | Objective builder | Model capability, Context depth |
| `objective.risk_level` | Objective builder | Model capability, Reasoning depth |
| `objective.ambiguities` | Objective builder | Reasoning depth (load-bearing → clarify) |
| `contract.kind` | Contract registry | Model capability, Context depth |
| `strategy.execution_mode` | Strategy router | All three (coarse-grained) |
| `strategy.needs_planner` | Strategy router | Reasoning depth |
| `strategy.needs_approval` | Strategy router | Reasoning depth |
| Mission `cost_mode` (if active) | Mission config | Upper bound on all three |
| Task source (DM / webhook / schedule) | Activation | Minor; input to context depth |

Notably: **task source and content length alone no longer determine
routing.** The current `classify_task_tier` uses source + content length
as the primary signal, which is what produces hank3's misrouting. Source
is a weak proxy; the typed signals above are the strong signals.

## Cost Analysis

Cost optimization is real, but narrow. Numbers as of this writing (2026):

| Operation | Approximate per-turn cost |
|---|---|
| Haiku turn (3K in, 1K out) | ~$0.008 |
| Sonnet turn (3K in, 1K out) | ~$0.024 |
| Haiku→Sonnet delta | ~$0.016 |
| Episodic memory retrieval | ~$0.001-0.005 |
| Procedural memory retrieval | ~$0.001-0.005 |
| Organizational context fetch | ~$0.002-0.008 |
| Reflection round (one extra Sonnet call) | ~$0.02-0.04 |
| Stripping `FRAMEWORK.md` (~5 KB tokens) | ~$0.003 saved on Sonnet |
| Stripping `AGENTS.md` (~3 KB tokens) | ~$0.002 saved on Sonnet |

Observations:

- **Stripping static instructional content saves cents per turn.** The
  total savings from stripping all of `FRAMEWORK.md`, `AGENTS.md`,
  skills, `PLATFORM.md` combined is on the order of $0.01 per Sonnet
  turn, less on Haiku.
- **The model-choice delta dominates**. Haiku→Sonnet adds ~$0.016 per
  turn, which dwarfs any prompt-stripping savings.
- **Reflection loops are the largest avoidable per-turn expense.** A
  single extra LLM call costs more than the full set of identity
  bandwidth sections combined.
- **Retrievals are real but not dominant.** Memory and knowledge
  fetches cost $0.002-0.008 per turn typically; meaningful at scale
  but not life-or-death at conversation volume.

Where optimization actually matters:

- **High-volume automated agents** (thousands of turns/day) — per-turn
  cents compound into meaningful daily totals
- **Background monitors firing per-event** — may run constantly
- **Acknowledgment-only replies** — can legitimately use `small` model
  with `direct` reasoning and `minimal` context; no identity
  bandwidth should be stripped even here
- **Multi-agent swarms with heavy parallel fan-out** — many cheap
  turns in parallel

At conversation volume with a $10/day budget, a single agent can run
all Sonnet + full retrievals on every turn and still come nowhere near
the ceiling. The cost optimization that the current `minimal` tier is
buying is not load-bearing for operator-facing agents.

## Current State and Migration

The current `task_tier.py` embeds all three axes in one knob. Specifically:

```python
TIER_FEATURES = {
    "minimal": {
        "trajectory": True,
        "fallback": False,
        "reflection": False,        # Reasoning depth
        "evaluation": False,        # Reasoning depth
        "procedural_inject": False, # Context depth
        "procedural_capture": False,
        "episodic_inject": False,   # Context depth
        "episodic_capture": False,
        "recall_tool": False,
        "prompt_tier": "minimal",   # Identity bandwidth (WRONG axis)
    },
    ...
}
```

The `prompt_tier` field is doing the damage: `body.py`'s prompt builder
keys off it to decide whether to include `FRAMEWORK.md`, `AGENTS.md`,
skills, `PLATFORM.md`, comms context. Under this spec, that coupling is
removed. The prompt composition rules above are authoritative, not the
tier feature matrix.

Similarly, model selection currently couples to tier. Under this spec,
model selection reads from the typed signals directly.

### Migration to separated axes

A consistent with-spec implementation:

1. `classify_task_tier` is repurposed or split:
   - `classify_reasoning_depth(task, objective, strategy, mission)` →
     `direct` / `reflective` / `deliberative`
   - `classify_context_depth(task, objective, strategy, mission)` →
     `minimal` / `task-relevant` / `full`
   - `select_model(task, objective, strategy, mission)` → `small` /
     `standard` / `large` (or the model name directly)

2. The prompt builder in `body.py` always includes the identity
   bandwidth sections and gates only dynamic sections by the context
   depth result.

3. The LLM invocation code reads `select_model(...)` output rather
   than mapping tier → model.

4. Reflection / evaluation / plan revision loops read
   `classify_reasoning_depth(...)` and skip where `direct` is the
   answer.

5. Existing `cost_mode` (`frugal`/`balanced`/`thorough`) remains as a
   mission-level preference that sets **upper bounds** on all three
   axes, but does not force downgrades below what the typed signals
   demand. A `frugal` mission running an `external_side_effect`
   contract should still get at least `standard` model capability and
   `reflective` reasoning depth — safety bounds override frugality.

### What current users see after migration

Hank3's identical prompt under the new composition:

- Task activation: same text
- Objective builder: `generation_mode=grounded, risk_level=medium`
- Strategy router: `execution_mode=tool_loop, needs_planner=False`
- Reasoning depth: `direct` (no approval, no high risk)
- Context depth: `task-relevant` (analytical about external entity →
  organizational context included if available)
- Model capability: `standard` → Sonnet (grounded + medium risk)
- System prompt: identity + mission (none) + FRAMEWORK + AGENTS +
  skills + PLATFORM + comms + provider tools + how-to-respond —
  all static, always included

The agent now has its full operational baseline. Whether it calls
`web_search` on the first turn is then a function of prompt content
plus model behavior plus native tool-use training — not whether the
runtime stripped its context.

## Relationship to PACT

This spec is Agency-runtime-specific. PACT defines the governance and
quality contracts; this spec defines how the runtime composes the
turn-by-turn input that operates within those contracts.

Specifically:

- PACT's `Objective.generation_mode` is the authoritative signal for
  model capability and context depth routing.
- PACT's `Strategy.execution_mode` informs reasoning depth but does
  not set it alone.
- PACT's Wave 2 #4 pre-commit evaluator runs at every reasoning depth
  level; it is enforcement, not optional deliberation.
- PACT's honesty invariant (Design Principle 4) is orthogonal to this
  spec — it enforces truthfulness regardless of reasoning depth,
  model, or context depth.

When a future PACT extension adds a new typed signal (for example, a
multi-party delegation field), routing decisions on the three axes
should be updated to consume it. This spec is the place to record
those updates.

## Open Questions

- **Should `cost_mode` set hard caps or soft preferences?** Hard caps
  risk under-serving high-stakes work in frugal missions. Soft
  preferences risk cost-mode becoming ineffective. Proposed default:
  soft preferences for mission types that rarely involve external
  side effects; hard caps enforced only on explicitly-cost-bounded
  missions with operator-declared budgets.
- **How does this interact with the meeseeks mode?** Meeseeks
  currently uses a fixed minimal prompt template. Should it inherit
  from this spec's rules or remain a special case? Proposed: remain
  special-cased; meeseeks is a single-purpose-short-lifetime
  construct where the current template is deliberate.
- **Where does context depth decide organizational context
  inclusion?** The "is the task analytical about an external entity
  with known organizational context" signal is informal. Needs a
  concrete heuristic or a typed field on the objective.
- **What's the cost-optimization discipline for high-volume automated
  agents?** These are the populations where tier-gating actually saves
  meaningful money. This spec does not prescribe their routing beyond
  the three-axis model; a follow-up spec pass may refine.
- **Should model selection surface in the `Strategy` object?** Currently
  implicit; making it a typed field on `Strategy` would make routing
  decisions more inspectable. Probably yes.
