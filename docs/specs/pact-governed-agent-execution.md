# PACT: Governed Agent Execution

## Status

Draft.

## Purpose

PACT is a world-class agentic execution harness, governable by construction.
Its primary goal is to make agents excellent at work: objective understanding,
planning, tool use, evidence, recovery, evaluation, long-running execution,
artifact production, memory, and collaboration. Governance bounds that work
without replacing it.

PACT binds every activation to an objective, execution contract, evidence
ledger, trajectory, and terminal outcome. It is not a chat model, prompt style,
agent persona, or tool wrapper. It is the runtime shape of agentic execution.

The harness must stand on its own as a serious execution engine, even before
enterprise controls are applied. ASK governs agents as principals. PACT governs
agent work as contract-bound execution layered on that harness. Agency is the
reference implementation of both.

## Non-Goals

- PACT is not a governance-only framework. It is first an execution harness;
  governance attaches at its boundaries.
- PACT is not an agent framework competing with orchestration libraries.
- PACT is not limited to inbound messages, chat, DMs, or human prompts.
- PACT does not define model provider APIs.
- PACT does not replace ASK enforcement, mediation, audit, or principal policy.
- PACT does not make the agent itself the enforcement boundary.
- PACT does not rely on provider or model training as honesty enforcement.
  Provider primitives complement but do not replace PACT's external
  enforcement of the honesty invariant.
- PACT does not assume invention authority by default. `chat`-kind contracts
  do not grant invention authority; they narrow the required evidence shape
  but preserve the default grounded generation mode unless the activation
  classifier recognizes an explicit invention authorization.

## Design Principles

1. **Work starts from activation, not chat.**
   A message is one activation source. Schedules, webhooks, mission triggers,
   delegated tasks, recovery events, approvals, lifecycle events, monitors, and
   API submissions are also activations.

2. **Execution is objective-bound.**
   The runtime must form an explicit objective before work proceeds. The
   objective captures intent, constraints, deliverables, success criteria, and
   known ambiguity.

3. **Contracts are runtime objects.**
   Instructions alone are not enough. A PACT execution carries structured
   obligations, required evidence, allowed terminal outcomes, approval needs,
   resource bounds, and output expectations.

4. **Evidence is observed, not asserted. Agent claims cannot exceed mediated evidence.**
   Evidence is recorded only by trusted observation points — the runtime,
   mediated tools, provider-tool events, artifact stores, and policy
   decisions. Every factual or tool-use assertion in an agent's output must be
   supported by a corresponding evidence ledger entry whose provenance is
   `mediated`, `provider`, or `runtime`. Agent-authored prose is never its own
   evidence.

   This invariant applies to every contract kind, including `chat`. Contracts
   modulate the required *shape* of evidence; they do not modulate the honesty
   invariant. Provider primitives (native tool-use protocols, citations,
   grounding modes) are a useful first signal but do not replace external
   enforcement. Per ASK tenet #1, enforcement must live outside the agent
   boundary — training priors are not enforcement.

5. **Trajectories matter.**
   A good final answer is insufficient if the path violated constraints, skipped
   required evidence, forged tool use, exceeded bounds, or hid uncertainty.

6. **Failure is first-class.**
   Blocked, needs-clarification, escalated, halted, expired, superseded, and
   failed outcomes are legitimate terminal states when they are explicit,
   auditable, and contract-consistent.

7. **Quality and governance are separate layers.**
   PACT defines how agentic work is executed well. ASK defines how that work is
   governed. The harness layer (objective, planning, tool loop, evidence,
   reflection, outcome) must be excellent on its own terms, not justified by
   the governance layer it enables. Agency binds them through external
   enforcement, mediation, audit, least privilege, runtime verification, and
   governed knowledge, but a non-governed PACT runtime should still be a
   serious execution engine.

8. **The runtime commits outcomes.**
   The model may draft plans, call tools, and propose outputs. The runtime decides
   whether a plan can execute, whether evidence satisfies the contract, and
   whether an outcome can be committed.

9. **Invention is authorized, not assumed.**
   Agents default to grounded output. Invention of content from the model's
   own weights is a narrow, explicit authorization recognized at activation
   time by the runtime — not inferred by the agent. Social interaction,
   persona responses, creative output, and intermediate reasoning are
   distinct authorizations that narrow — but do not remove — the grounding
   requirement for claims about external state. Per ASK tenet #4, unknown
   generation context defaults to grounded.

   The `chat` contract kind does not by itself grant invention authority; it
   narrows the required shape of evidence. Authorizing invention is a
   separate decision made at activation-classification time based on
   explicit signals in the request. Conversational phrasing alone never
   authorizes invention.

## Agentic Execution Harness

PACT's primary product goal is a world-class agentic execution harness that is
governable by construction. Governance is not a substitute for agent quality.
The harness must make agents excellent at work even before ASK-specific
enterprise controls are applied. A non-governed PACT runtime should still be a
serious execution engine — governance is an overlay, not the reason PACT exists.

The harness layer owns agentic execution quality:

- objective understanding: normalize activations into goals, constraints,
  deliverables, success criteria, assumptions, and ambiguity
- strategy routing: choose execution mode, model tier, tools, memory, budget,
  and whether planning is required
- planning: produce explicit plans for non-trivial, risky, long-running, or
  side-effecting work, and revise plans when observations contradict assumptions
- tool loop quality: request tools deliberately, consume structured results,
  adapt to failures, and never imply fake tool use
- execution state: track progress, step history, partial results, assumptions,
  open questions, budget, errors, and blockers
- reflection and evaluation: critique trajectories and proposed outcomes before
  commit, using deterministic checks where possible
- recovery: retry, replan, fall back, clarify, escalate, or block with bounded
  loops and explicit reasons
- artifact disposition: produce files, links, reports, patches, screenshots,
  PRs, issues, logs, and other outputs without forcing everything through chat
- memory learning: propose reusable procedures, environment facts, task
  outcomes, and failed strategies through scoped, reviewable memory paths
- trajectory evaluation: test complete paths, not just final text

The governance layer owns organizational control:

- authority resolution
- least privilege
- tool mediation
- audit
- policy enforcement
- approvals
- data boundaries
- durable memory review
- quarantine and halt
- compliance reporting

The boundary is intentional. The harness may decide that an execution needs a
tool, artifact, memory write, delegation, or external side effect. Agency/ASK
decides whether that action is authorized, how it is mediated, what is audited,
and where outputs may be retained or published.

The target architecture is:

```text
Activation
  -> Objective Builder
  -> Strategy Router
  -> Execution Contract
  -> Planner
  -> Executor / Tool Loop
  -> Evidence + State Ledger
  -> Evaluator / Reflection
  -> Outcome Committer
  -> Learning Hooks
```

ASK attaches at the boundaries:

```text
Authority + Policy + Mediation + Audit + Constraints
```

Implementation work must preserve this ordering. Reporting, audit projections,
and compliance exports are projections of high-quality execution state; they are
not the execution harness itself.

## Relationship To ASK

PACT is designed to be useful without ASK, but Agency's implementation must obey
ASK as a hard constraint.

ASK properties map onto PACT as follows:

| ASK Concern | PACT Binding |
| --- | --- |
| External constraints | Contract enforcement and policy checks occur outside the agent boundary. |
| Complete mediation | Tool execution and external resource access are recorded as mediated evidence. |
| Auditability | Activations, contracts, trajectories, evidence, evaluations, and outcomes are persisted. |
| Least privilege | Execution contracts resolve permitted capabilities before action. |
| Explicit trust | Principal context and source trust are part of activation resolution. |
| Bounded operations | Contracts carry budgets, retry limits, timeouts, tool-call limits, and retention bounds. |
| Durable knowledge | Memory writes are proposed outcomes, not direct agent mutations. |
| Delegation scope | Delegated activations cannot exceed the delegator's authority or recipient authorization. |
| Synthesis boundaries | Output disposition is checked against recipient authorization. |
| Data vs instructions | Untrusted activation payloads and retrieved content remain data, not principal instructions. |

Agency must preserve the ASK invariant that enforcement remains external to the
agent boundary. PACT may expose contract feedback to the agent, but it must not
expose enforcement internals, audit controls, credentials, or bypass paths.

## Core Concepts

### Activation

An activation is the reason an agent is running now.

Examples:

- operator message
- channel mention
- scheduled job
- webhook
- mission trigger
- delegated task from another internal agent
- approval continuation
- recovery retry
- anomaly response
- startup or resume lifecycle task
- API-submitted objective

Activation fields:

```text
id
source_type
source_ref
principal_ref
timestamp
payload
metadata
trust_level
correlation_id
parent_activation_id
```

The activation payload is untrusted unless it came from a verified principal
instruction channel. External data may describe a task, but it does not itself
grant authority.

### Principal Context

Principal context identifies the authority behind the activation.

Fields:

```text
principal_id
principal_type
authority_scope
delegated_by
trust_level
authorization_snapshot
policy_context
```

PACT itself does not decide enterprise authority. In Agency, principal context is
resolved by the gateway and policy layer, and all trust relationships must remain
explicit and auditable.

### Objective

The objective is the runtime's normalized understanding of the work.

Fields:

```text
statement
kind
constraints
deliverables
success_criteria
ambiguities
assumptions
risk_level
generation_mode
```

Objectives are not merely summaries. They are the target against which planning,
execution, evaluation, and outcome commitment are checked.

### Generation Mode

Every PACT execution carries a generation mode that determines what kind of
output the agent is authorized to produce. Generation mode travels on the
objective alongside kind and constraints. The runtime — not the agent —
decides the mode at activation-classification time.

Modes:

- `grounded` — default. Factual claims about external state require evidence
  ledger entries with `mediated`, `provider`, or `runtime` provenance.
  Applies to analytical, investigative, informational, and instrumented
  work.
- `social` — low-stakes conversational interaction: greetings,
  acknowledgment, meta-conversation about the agent or the task. The agent
  may produce subjective conversational content. Claims about external
  state remain grounded.
- `persona` — first-person responses about the agent itself (name, role,
  preferences, capabilities). The agent may produce persona-consistent
  content. Claims about external state remain grounded.
- `creative` — explicit creative or playful output: jokes, poems,
  brainstorms, roleplay. Invention is the expected output. Claims about
  external state remain grounded.
- `reasoning` — thinking through a problem in the open. Intermediate
  steps may be model-authored. Conclusions about external state remain
  grounded.

Default generation mode is `grounded`. The runtime promotes a non-grounded
mode only when the activation contains an explicit authorization pattern
(e.g., "tell me a joke", "how are you", "write a poem", "brainstorm with
me"). Ambiguity defaults to `grounded`; the classifier does not escalate
invention authority on inference or conversational phrasing alone.

Mode interacts with the honesty invariant (Design Principle 4): no mode
exempts agent claims about external state from requiring mediated evidence.
Invention-authorized modes narrow the check to claims the mode permits
inventing (e.g., subjective persona responses in `persona` mode) but never
override the grounding requirement for factual assertions.

Mode affects:

- system prompt construction — the runtime may tailor the prompt to the
  authorized mode
- honesty-check semantics — grounded-mode prose announcing tool use without
  matching mediated evidence blocks commit; non-grounded modes apply a
  narrowed variant of the same check
- evaluator layers — modes may alter which layers fire or how they
  interpret evidence sufficiency

Generation mode is a property of the typed `Objective`, populated by the
objective builder. It is not a field on `WorkContract`; contracts describe
what kind of work is being done, modes describe what kind of output is
authorized. A single contract kind may carry different generation modes
depending on the activation (e.g., a `chat` contract normally carries
`grounded` mode, but a `chat` activation asking for a joke carries
`creative` mode).

### Execution Contract

The execution contract binds the objective to runtime obligations.

Fields:

```text
contract_id
objective_id
kind
required_evidence
allowed_tools
disallowed_tools
approval_requirements
resource_bounds
retry_policy
output_contracts
allowed_terminal_outcomes
failure_policy
retention_policy
```

The contract should be explicit enough for deterministic checks. Model-authored
contract interpretation may assist, but cannot replace runtime enforcement.

### Plan

A plan is an ordered or graph-shaped proposal for execution.

Fields:

```text
steps
dependencies
required_capabilities
expected_evidence
approval_points
fallbacks
stop_conditions
```

Plans are required when the path is non-trivial, risky, long-running, or has
external side effects. Trivial work may execute without a model-authored plan,
but it still has a contract and outcome.

### Execution State

Execution state is the current mutable state of the PACT run.

Fields:

```text
phase
attempt
active_step
completed_steps
open_questions
partial_outputs
tool_state
budget_state
last_error
```

Execution state is runtime-owned. The model can be shown relevant projections of
state, but the authoritative state is not model-authored.

### Evidence Ledger

The evidence ledger records observations that may satisfy the contract.

Evidence types:

- mediated tool call
- provider tool event
- local runtime observation
- source fetch
- artifact creation
- test result
- policy decision
- approval decision
- memory retrieval
- memory proposal
- external service response
- error or blocker

Evidence fields:

```text
id
type
producer
timestamp
summary
payload_ref
provenance
integrity
contract_relevance
visibility_scope
```

Evidence must distinguish between:

- observed fact
- model-authored interpretation
- unverified external claim
- blocked or failed observation

The ledger's provenance field is the structural boundary between observed
and asserted. Agent-authored prose may describe, summarize, or interpret
what the ledger contains, but any claim that asserts a fact beyond the
ledger — a tool call not recorded, a number not from a mediated tool
result, a source not retrieved — is a fabrication and must be rejected
at commit, regardless of contract kind. See Design Principle 4.

Agency's current body runtime has an in-memory `EvidenceLedger` write model for
runtime and provider observations. It preserves the legacy evidence projection
used by existing PACT checks:

```text
tool_results
observed
source_urls
```

and adds typed entries for runtime-owned observations. This is not yet the
durable PACT evidence ledger resource described above. It is the compatibility
step that lets body code record evidence through one typed API while existing
completion checks, verdict payloads, and tests continue to consume the older
projection.

### Trajectory

The trajectory is the ordered record of execution.

Trajectory entries include:

- activation accepted
- objective formed
- contract bound
- route selected
- plan proposed
- plan approved or rejected
- tool requested
- tool allowed or denied
- tool result observed
- retry attempted
- fallback activated
- evaluation performed
- outcome committed

Trajectory is the basis for debugging, replay, evaluation, audit, and compliance.

### Evaluation

Evaluation determines whether the current execution state and proposed outcome
satisfy the contract.

Evaluation layers:

1. deterministic checks
2. schema and artifact checks
3. evidence sufficiency checks
4. policy and authorization checks
5. optional model-assisted critique
6. optional human review

Deterministic checks win when available. Model-assisted critique may identify
quality issues, missing reasoning, or inconsistencies, but it cannot authorize
work beyond the contract.

### Outcome

An outcome is the terminal or suspended disposition of an execution.

Terminal outcomes:

- `completed`
- `blocked`
- `needs_clarification`
- `escalated`
- `halted`
- `failed`
- `expired`
- `superseded`

Outcome fields:

```text
status
summary
outputs
artifacts
evidence_refs
missing_requirements
next_actor
visibility_scope
commit_timestamp
```

An outcome is not necessarily a chat response. It may be a file, patch, PR,
approval request, memory proposal, alert, scheduled continuation, delegated task,
or internal audited state transition.

### Disposition

Disposition decides what happens to an outcome.

Examples:

- publish to a channel
- reply to a DM
- create an artifact
- open a pull request
- schedule follow-up
- create an approval request
- create a memory proposal
- notify an operator
- quarantine or halt
- retain only in audit

Disposition must be checked against recipient authorization and data boundaries.

## Execution Lifecycle

PACT executions move through the following lifecycle:

```text
activation_received
  -> activation_resolved
  -> objective_formed
  -> contract_bound
  -> route_selected
  -> plan_prepared
  -> execution_started
  -> evidence_recorded
  -> evaluation_performed
  -> outcome_committed
  -> disposition_applied
  -> finalized
```

Some transitions are optional:

- Trivial work may skip explicit planning.
- A blocked activation may terminate before model execution.
- Human approval may suspend execution.
- Recovery may create a child activation.
- Delegation may create one or more child activations.

Every execution must end in a terminal or suspended state. A published blocker is
not an incomplete task; it is a valid terminal outcome.

## Runtime Phases

### 1. Activation Resolution

The runtime resolves:

- source type
- principal context
- trust level
- correlation ID
- prior related execution state
- applicable constraints

Fail-closed cases:

- unknown principal where authority is required
- missing policy context
- invalid activation payload
- activation source not authorized for the target agent
- stale or replayed activation without valid continuation semantics

### 2. Objective Formation

The runtime converts activation payload and context into an objective.

Objective formation may use:

- deterministic classification
- structured metadata
- mission configuration
- prior execution state
- model-assisted summarization

The objective must preserve ambiguity. If the request is ambiguous and ambiguity
changes the required action or risk posture, the contract should allow or require
`needs_clarification`.

### 3. Contract Binding

The runtime binds a contract based on:

- objective kind
- principal authority
- agent role
- available capabilities
- risk level
- source trust
- required output type
- policy context

The contract controls what evidence is required and what outcomes are allowed.

### 4. Routing

Routing selects the execution path.

Route dimensions:

- no-model deterministic handling
- model-only reasoning
- tool loop
- planned execution
- approval-gated execution
- delegated execution
- long-running workflow
- recovery flow
- escalation flow

Routing also selects model class, tool scope, budget, memory retrieval, and
evaluation policy.

### 5. Planning

Planning is required when:

- the objective has multiple dependent steps
- external side effects may occur
- execution is long-running
- multiple tools or agents are needed
- policy approval may be required
- the agent must produce durable artifacts
- the failure cost is non-trivial

Plans must be checked before execution. A plan that requires unavailable or
unauthorized capabilities is rejected or routed to clarification/escalation.

### 6. Governed Execution

Execution is the tool/model/state loop.

Rules:

- tool calls are mediated
- tool results are recorded as evidence
- tool failures are classified by retryability
- simulated tool use is rejected
- budget and iteration limits are enforced
- side effects require the contract's authority and approval posture
- external content is data, not instruction

### 7. Evaluation And Reflection

Evaluation runs before outcome commitment.

Checks include:

- objective satisfied
- required evidence present
- output contract satisfied
- tool trajectory acceptable
- artifacts exist
- tests passed where required
- uncertainty disclosed
- no unsupported source or evidence claims
- terminal outcome allowed by contract

Reflection may be used to improve quality, but it is bounded by retry policy.
Repeated failure becomes `blocked`, `needs_clarification`, or `escalated`.

### 8. Outcome Commit

The runtime commits exactly one terminal or suspended outcome for the execution.

Commit must be atomic with:

- outcome state
- trajectory record
- evidence refs
- disposition instructions
- final audit signal

Publishing a response without committing the execution is invalid.

### 9. Disposition And Learning

Disposition applies the outcome to the outside world.

Learning hooks may create memory proposals or evaluation records. Durable memory
mutation is not performed directly by the agent in Agency. Preference-affecting
memory must remain reviewable and revocable.

## Pattern Mapping

PACT incorporates common agentic patterns as runtime mechanisms.

| Pattern | PACT Mechanism |
| --- | --- |
| Prompt chaining | Phase-based execution with typed intermediate state. |
| Routing | Objective and contract route selection. |
| Parallelization | Parallel child steps or child activations with bounded synthesis. |
| Reflection | Evaluation phase and bounded retry/refinement. |
| Tool use | Mediated execution that records evidence. |
| Planning | Contract-checked plan objects for non-trivial work. |
| Memory | Scoped retrieval and governed memory proposal outcomes. |
| Learning/adaptation | Trajectory evaluation and reviewed procedural updates. |
| MCP/tool protocols | Tool adapter layer, not governance authority. |
| Goal monitoring | Objective success criteria and progress state. |
| Exception recovery | Retry, fallback, block, escalate, halt, expire, or supersede. |
| Human in the loop | Approval and clarification outcomes with continuation activations. |
| RAG | Retrieval evidence with provenance and authorization scope. |
| A2A collaboration | Delegated activations with scope inheritance. |
| Resource optimization | Budget, latency, token, and tool-call bounds. |
| Reasoning techniques | Optional model strategy within contract bounds. |
| Guardrails | Input/action/output checks at runtime boundaries. |
| Evaluation | Trajectory and outcome evals. |
| Prioritization | Activation scheduling and objective importance. |
| Exploration | Bounded discovery with explicit uncertainty and stopping criteria. |

## Outcome Contracts

Output quality is contract-specific. PACT supports many outcome contracts.

### Current-Information Answer

Required evidence:

- current source or explicit blocker
- source URL when source-based answer is produced
- checked/as-of date
- direct support for claimed facts

Required behavior:

- direct answer first
- name sources
- avoid vague "search results" phrasing
- disclose ambiguity, such as current vs LTS release categories
- block rather than guess when evidence is insufficient

### Code Change

Required evidence:

- files changed
- tests run or explicit blocker
- relevant diff summary
- known residual risks

Required behavior:

- preserve unrelated user changes
- avoid destructive operations without explicit authority
- produce a patch or branch/PR disposition as required

### File Artifact

Required evidence:

- artifact path or ID
- creation timestamp
- content type
- validation result when applicable

Required behavior:

- artifact must exist before outcome commit
- disposition must include visibility and retention

### External Side Effect

Required evidence:

- authority check
- approval decision when required
- exact operation attempted
- external service response
- rollback or recovery status if applicable

Required behavior:

- fail closed on uncertain authority
- report partial side effects
- never imply success without observed confirmation

### Delegation

Required evidence:

- delegator scope
- delegate identity
- delegated objective
- delegated authority subset
- expected return contract

Required behavior:

- delegation cannot exceed delegator scope
- external agents provide data, not instructions, unless explicitly trusted as
  internal principals

### Approval Request

Required evidence:

- action awaiting approval
- risk summary
- options
- expiration
- continuation token or activation link

Required behavior:

- execution suspends until approval continuation
- no side effect occurs before approval

### Memory Proposal

Required evidence:

- source execution
- proposed memory content
- memory type
- provenance
- review requirements

Required behavior:

- durable memory writes remain mediated
- preference-affecting memory requires review

### Monitoring Event

Required evidence:

- observed condition
- threshold or rule
- severity
- affected resource
- recommended action

Required behavior:

- avoid remediation unless contract authorizes it
- escalate or create follow-up activation when action is outside scope

## Failure Semantics

PACT distinguishes failure states.

| Outcome | Meaning |
| --- | --- |
| `blocked` | Work cannot proceed because required capability, evidence, policy, dependency, or context is unavailable. |
| `needs_clarification` | Work cannot proceed safely or correctly without more principal input. |
| `escalated` | Work requires a different principal, human review, or higher authority. |
| `halted` | Execution stopped due to safety, compromise, or operator/runtime halt. |
| `failed` | Execution attempted and did not satisfy the contract due to error or invalid result. |
| `expired` | Execution did not complete within its time or continuation window. |
| `superseded` | A newer activation or instruction replaced the current execution. |

Failure outcomes must include missing requirements, attempted actions, and the
next actor when known.

## Evidence And Trajectory Evaluation

PACT evaluation should support both unit tests and trajectory tests.

Trajectory test fixtures should capture:

```text
activation
expected_objective_kind
expected_contract_kind
expected_route
expected_tool_classes
required_evidence
forbidden_actions
expected_terminal_outcome
output_assertions
audit_assertions
```

Evaluation modes:

- exact trajectory match for high-risk workflows
- in-order match for flexible multi-step workflows
- evidence sufficiency for research/current-info workflows
- artifact existence and validation for file/code workflows
- policy decision assertions for governed workflows
- final outcome quality assertions

## Agency Reference Implementation

Agency implements PACT through:

- gateway activation sources
- principal and capability policy
- body runtime execution
- enforcer mediation
- provider tool evidence
- comms disposition
- artifact and workspace handling
- graph-backed memory proposals
- audit log and runtime manifests
- web/CLI operator surfaces

Current Body Runtime concepts map as follows:

| Current Concept | PACT Concept |
| --- | --- |
| `current_task` | Activation plus execution state |
| message source metadata | Activation source and principal context |
| `WorkContract` | Execution contract |
| provider tool chunks | Evidence ledger entries |
| tool result recording | Evidence ledger entries |
| simulated tool retry guard | Evaluation and recovery rule |
| answer gate | Outcome contract evaluation |
| `complete_task` | Outcome commit signal |
| task response posting | Disposition |
| memory capture | Memory proposal disposition |

## Current Implementation Checkpoint

As of the April 2026 vertical slice, Agency has implemented a narrow but
operator-visible PACT path for body runtime work.

This slice is intentionally not the full PACT object model. It establishes the
first durable signals, artifacts, API projections, and UI audit surfaces for
contract-bound execution.

The current slice is deliberately governance- and audit-forward: it proves the
boundary (verdicts as evidence signals, enforcement outside the agent, audit
append-only, deterministic gates on a handful of contracts) before the harness
itself is rebuilt. This ordering is a sequencing choice, not the shape of the
finished system. The harness-quality work — typed `ExecutionState`, objective
builder, strategy router, planner as runtime object, structured tool
observation protocol, general pre-commit evaluator, unified recovery state
machine, artifact disposition, memory learning hooks, trajectory-first evals —
is the primary next phase and the reason PACT exists. Audit and reporting
projections should follow from better execution state, not substitute for it.

### Execution State Type

The body runtime now has a typed, runtime-owned `ExecutionState` object for the
active PACT run. It is constructed at task start and cleared at task end.

Currently populated fields:

```text
task_id
agent
activation
objective
strategy
contract
plan
evidence
tool_observations
recovery_state
started_at
updated_at
```

`activation` is populated from existing task metadata when available.
`objective` is populated by the Wave 2 #1 objective builder when both
activation and contract are present; when either input is missing, it remains
null.
`strategy` is populated by the Wave 2 #2 strategy router when `objective` is
present; when `objective` is null, it remains null. The router is deterministic,
model-free, and reads only typed objective, contract, task metadata, and mission
context. Routing rules are ordered: escalated risk routes to approval-required
escalation; load-bearing target-file or external-authority ambiguities route to
clarification; external side effects route to approval-required planned side
effect handling; chat and operator-blocked contracts route to trivial direct
handling; remaining high-risk work routes to planned execution; code changes
route to planned execution by default; all other work routes to the tool loop.
Advisory hints for tool scope, model tier, memory, and budget are surfaced as
strategy notes but are not yet enforced by any runtime gate.
`contract` wraps the existing body work contract as a typed `WorkContract`.
`plan` is populated by the Wave 2 #3 planner builder when the selected strategy
requires planning. The builder is deterministic and model-free, and emits typed
ordered steps for the `code_change`, `file_artifact`, `external_side_effect`,
and `current_info` contract templates. Side-effecting plans enforce the
structural ASK invariant that an approval step appears before any step requiring
`external_state`. The plan is advisory in this checkpoint; `body.py` does not
yet execute plan steps directly, which is deferred to Wave 2 #3b.
`evidence` owns the existing in-memory `EvidenceLedger`; legacy flattened
evidence remains a projection of that ledger.
`recovery_state` is the runtime-owned advisory recovery machine. It exposes the
`RecoveryStatus` enum (`idle`, `retrying`, `replanning`, `fallback`,
`clarifying`, `escalated`, `blocked`, `failed`, `halted`, `expired`,
`superseded`) and the `NextAction` enum (`none`, `retry`, `replan`, `fallback`,
`clarify`, `escalate`, `block`, `fail`, `halt`). In this checkpoint it is
populated from typed observation failures and contract evaluation gaps for
visibility only; the existing `body.py` retry behavior is unchanged and will be
rewired to consume the machine in Wave 2 #5b.

The general pre-commit evaluator now produces a typed `PreCommitVerdict` from
runtime-owned `ExecutionState`. Its deterministic layers check, in order:
state completeness, recovery halt or terminal status, blocking recovery next
action, clarify/escalate strategy route, load-bearing ambiguity, approval
decision evidence, plan expected evidence, the Tier 1 honesty-check layer, and
the existing contract-specific `validate_completion` verdict. The honesty layer
uses the module-level `TOOL_ANNOUNCEMENT_PATTERNS` list in
`images/body/pact_engine.py` to block simulated tool use when agent prose
announces tool use but the runtime has no qualifying mediated tool-result
evidence. Its reason label is
`honesty:simulated_tool_use:<pattern>` with `mediated_tool_result` marked
missing. This is Tier 1 of the honesty invariant; Tier 2 specific-claim
grounding is deferred.

The plan-evidence layer is advisory until Wave 2 #3b because `body.py` does not
execute plan steps directly yet. In this checkpoint `body.py` uses the evaluator
as the runtime commit gate. A committable pre-commit verdict maps to the
contract verdict (`completed` or `blocked`). A non-committable
`contract:needs_action` verdict maps to `needs_action` and receives the existing
one-time retry path; after that retry is exhausted, it terminates blocked with
the original reason preserved. Every other non-committable reason terminates
blocked without a retry.

The objective builder is deterministic and model-free. Activation content is
used only as capped statement data and for ambiguity detection; it is not a
constraint source. Constraints come from task metadata, mission configuration,
contract terminal states, and per-kind defaults.

Initial ambiguity heuristics are deliberately small: current-information
temporal anchors and release category, code-change target files and validation
target, file-artifact output format, and external-side-effect authority scope.
Initial risk rules are ordered and fixed: untrusted/low trust escalates,
external side effects are high, code changes are high when target files are
missing otherwise medium, file artifacts and current information are medium,
and remaining work is low. Richer heuristics arrive only as downstream waves
surface real demand.

Placeholder fields are present but not yet populated by runtime logic:

```text
step_history
partial_outputs
errors
proposed_outcome
```

`tool_observations` now uses the Wave 1 structured `ToolObservation` protocol:
each observation records tool name, `ToolStatus`, structured data,
`ToolProvenance`, producer, timestamps, optional `ToolError`, `Retryability`,
`SideEffectClass`, ordered evidence classifications, and summary. The enum
classes are intentionally small and classify status, provenance, retryability,
and side-effect boundary without asking the model to infer tool outcome from
prose. `retryability` and `side_effects` are classified for operator
inspectability. `retryability` is now consumed by the advisory recovery state
machine, while side-effect evaluation remains future Wave 4 #3 work.

These placeholders intentionally do not implement Wave 2 routing, planning,
evaluator, body retry-path rewiring, or outcome logic.

### Verdict Signal

The body runtime emits `pact_verdict` through the existing agent signal channel.

Signal event:

```text
agent_signal_pact_verdict
```

Payload fields:

```text
task_id
kind
verdict
required_evidence
answer_requirements
missing_evidence
observed
source_urls
tools
reasons
```

`reasons` is the structured list returned by the pre-commit evaluator. It is
additive to the legacy signal payload; existing fields are preserved.

Runtime mapping semantics:

```text
committable=true -> verdict comes from the contract verdict
committable=false + contract:needs_action -> needs_action, one retry only
committable=false + any other reason -> blocked, terminal
```

Current verdict values:

```text
completed
blocked
needs_action
```

Registered contract kinds:

```text
current_info
code_change
file_artifact
external_side_effect
operator_blocked
mission_task
task
coordination
chat
```

`current_info` is the only contract kind in this slice with meaningful
contract-specific answer validation. The other registry entries establish named
runtime contracts and fail-closed lookup semantics before their full evaluators
are wired into all execution paths.

The verdict signal is audit evidence. It is not enforcement authority. The
gateway records and displays the signal, but the agent cannot grant itself
permission or mutate audit history by emitting it.

### Runtime Evaluator

The body runtime now uses a registry-backed `PactEvaluator` as the local
evaluation boundary for PACT work. Core contract definitions, activation
classification, evidence modeling, prompt material, blocker formatting, and
completion validation live in the body-local `pact_engine` module. Existing
`work_contract` module-level helper functions remain as compatibility wrappers
while body integration moves toward explicit runtime adapters.

The current evaluator types are:

```text
ActivationContext
EvidenceLedger
EvidenceView
EvaluationResult
WorkContract
ContractDefinition
```

`ActivationContext` captures intake context for contract selection:

```text
content
match_type
source
channel
author
mission_active
```

The body intake path now constructs an `ActivationContext` for inbound work and
classifies via `classify_activation`. The old `classify_work(content,
match_type, mission_active)` helper remains available as a wrapper for existing
tests and call sites.

`EvidenceLedger` is the runtime write-side evidence model. It records typed
entries for mediated tool results, provider-tool observations, local runtime
observations, and source URLs, then projects those entries into the legacy
evidence shape.

`EvidenceView` is the evaluator read-side projection of evidence fields into
tool results, observed signals, and source URLs. It is not yet a durable ledger.

PACT verdict audit signals and result artifact frontmatter now carry
`evidence_entries` alongside the legacy flattened evidence fields. These entries
preserve producer and typed evidence shape through existing durable audit and
artifact surfaces. They are an incremental durable projection, not yet a
standalone typed evidence ledger resource.

`EvaluationResult` owns verdict serialization. Runtime callers still receive the
same dictionary shape from `validate_completion`, preserving existing body,
signal, and artifact behavior.

### Contract Registry

The body runtime has a named PACT contract registry. Contract definitions
include:

```text
kind
summary
required_evidence
answer_requirements
allowed_verdicts
answer_contract
```

Unknown action contract kinds fail closed: contract construction raises an
error, and completion validation blocks with `known_contract_kind` as missing
evidence.

Foundational registered PACT kinds are:

| Kind | Current Role |
| --- | --- |
| `current_info` | Current or externally verifiable facts requiring fresh source/tool evidence. |
| `code_change` | Code changes requiring runtime changed-file evidence, validation evidence, and a summary that names both. |
| `file_artifact` | File/report/artifact-producing work requiring runtime artifact evidence plus a concrete artifact reference. |
| `external_side_effect` | Work that mutates external systems and requires authority plus outcome evidence. |
| `operator_blocked` | Explicit blocked work requiring a concrete blocker and operator/admin unblock action. |

Legacy/body runtime kinds are also registered so existing activation behavior
continues to classify safely:

```text
mission_task
task
coordination
chat
```

The registry and evaluator are not yet the final PACT engine. They are the first
durable implementation point for named contracts, typed activation context,
typed evidence views, typed verdict construction, contract prompt material, and
fail-closed contract lookup.

### Result Artifact Metadata

When the body runtime writes a saved result artifact under `.results/`, it writes
YAML frontmatter that may include `pact` and `pact_activation` objects.

Example:

```yaml
---
task_id: task-20260422-node
agent: test-1
timestamp: "2026-04-22T08:00:00Z"
turns: 3
pact:
  kind: current_info
  verdict: completed
  required_evidence:
    - current_source
    - source_url
  answer_requirements:
    - direct_answer
    - checked_date
  missing_evidence: []
  observed:
    - official source URL observed
  source_urls:
    - https://nodejs.org/en/blog/release/v24.15.0
  tools:
    - provider-web-search
pact_activation:
  content: Find the latest stable Node.js release
  match_type: direct
  source: idle_direct
  channel: dm-test-1
  author: operator
  mission_active: false
---
```

Artifacts without PACT metadata remain valid. Malformed frontmatter is surfaced
as metadata error where possible, but must not prevent listing other artifacts.
`pact_activation` is additive audit context. It records the activation inputs
that selected the contract; it is not authority and does not replace principal
policy or gateway mediation.

### Result Metadata API

The gateway exposes structured result metadata without changing the markdown
artifact endpoint.

Endpoints:

```text
GET /api/v1/agents/{name}/results
GET /api/v1/agents/{name}/results/{taskId}
GET /api/v1/agents/{name}/results/{taskId}/metadata
```

`GET /results` returns additive metadata on each item:

```text
task_id
has_metadata
metadata
pact
metadata_error
```

`GET /results/{taskId}/metadata` returns:

```text
task_id
metadata
pact
has_metadata
```

The markdown result endpoint remains the canonical artifact body and supports
download semantics. Metadata endpoints are read-only projections over saved
artifacts.

### PACT Run Projection API

The gateway exposes an initial read-only PACT run projection keyed by agent and
task ID:

```text
GET /api/v1/agents/{name}/pact/runs/{taskId}
```

The projection joins existing durable sources without creating a new authority
or storage layer:

```text
task_id
agent
activation:
  content
  match_type
  source
  channel
  author
  mission_active
contract:
  kind
  required_evidence
  answer_requirements
  allowed_terminal_states
evidence:
  observed
  source_urls
  artifact_paths
  changed_files
  validation_results
  evidence_entries
  tools
verdict:
  verdict
  missing_evidence
outcome
artifact:
  task_id
  url
  metadata_error
audit_events
sources
```

Current sources are result artifact frontmatter and append-only audit events.
The projection exists so operators and future tooling can inspect the work
contract, observed evidence, terminal verdict, linked result artifact, and
supporting audit records in one place. Persisted audit JSONL and result
artifacts remain the underlying records of fact for this implementation slice.

### PACT Audit Report API

The gateway exposes a read-only PACT audit report keyed by agent and task ID:

```text
GET /api/v1/agents/{name}/pact/runs/{taskId}/audit-report
```

The report is assembled from the PACT run projection and existing durable
sources. It does not mutate audit JSONL, result artifacts, or evidence storage.

```text
report_id
generated_at
agent
task_id
run
evidence_entries
artifact_refs
audit_events
integrity:
  algorithm
  hash
  scope
```

The initial integrity block uses SHA-256 over deterministic report content. The
hash scope intentionally excludes `generated_at` and the integrity block itself
so the same underlying PACT run produces a stable report hash across repeated
reads. The deterministic run projection included in that hash now includes the
verdict `reasons` list, so new reports hash the structured pre-commit reason
labels along with the legacy verdict fields. This is an integrity report, not
yet a cryptographic signature.

The gateway also exposes report hash verification:

```text
POST /api/v1/agents/{name}/pact/runs/{taskId}/audit-report/verify?hash=<sha256>
```

Verification rebuilds the current report from durable sources and compares the
current report hash with the supplied hash. If no hash is supplied, the endpoint
verifies the freshly rebuilt report against its own integrity block. The result
is read-only and machine-readable:

```text
valid
agent
task_id
algorithm
expected_hash
actual_hash
report_id
checked_at
reason
```

Callers may also submit a PACT audit report JSON body to the same endpoint. In
that mode the gateway checks the submitted report's agent, task ID, algorithm,
and embedded integrity hash against the current report rebuilt from durable
sources. A mismatch returns `valid: false` with a reason such as
`task_id_mismatch`, `agent_mismatch`, `unsupported_algorithm`, or
`hash_mismatch`.

### Log Correlation API

The gateway decorates agent audit log responses with result-artifact correlation
when a log event's `task_id` matches a saved result artifact.

Endpoints:

```text
GET /api/v1/agents/{name}/logs
GET /api/v1/admin/audit
```

Additive fields:

```text
has_result
result:
  task_id
  url
```

This decoration is response-time only. It must not mutate stored audit JSONL.
Audit remains append-only from the perspective of persisted events. Admin audit
queries preserve typed PACT `evidence_entries` already present on verdict events
and add result links when task artifacts exist, so audit export consumers do not
need to reconstruct basic PACT/result correlation from UI-only state.

### Operator Surfaces

The web UI exposes this slice through:

- PACT status chips on chat result artifacts
- agent Results tab with saved artifacts and PACT verdict summaries
- Activity log rendering for `agent_signal_pact_verdict`
- Activity-to-result linking by `task_id`

The UI may derive convenience displays, but the gateway API owns durable
correlation. UI code may fall back to client-side `task_id` matching for older
gateways, but new behavior should prefer API-provided `has_result` and `result`
fields.

### Implemented Invariants

- Enforcement remains outside the agent boundary.
- PACT verdicts are evidence signals, not authority.
- Audit logs are not mutated when decorated with result links.
- Admin audit exports include additive PACT/result correlation fields when
  result artifacts exist.
- Result metadata is additive and optional.
- Legacy artifacts without PACT frontmatter continue to work.
- Malformed metadata must not fail unrelated artifact listings.
- UI displays may fail open to "no metadata", but must not invent verdicts.
- Source URLs and tool names are observed evidence fields, not model-only claims
  when populated by runtime/provider observation paths.

### Current Limits

- The body runtime now records provider and local tool observations through a
  typed in-memory `EvidenceLedger`, and carries typed `evidence_entries` through
  PACT verdict audit signals and result artifact frontmatter. The durable
  evidence ledger is still represented by existing audit/artifact surfaces
  rather than a standalone typed PACT ledger resource.
- The named contract registry and body-local `PactEvaluator` exist in a
  dedicated `pact_engine` boundary, and `current_info`, `file_artifact`,
  `code_change`, and `operator_blocked` now have deterministic completion
  gates.
- `external_side_effect` is registered but not yet broadly classified or
  validated by a contract-specific evaluator.
- File-artifact work is classified for explicit artifact-producing requests.
  The body runtime materializes file-artifact completions into result artifacts,
  records the path as runtime-owned evidence, and requires the completion to
  include that concrete path.
- Code-change work is classified for explicit code/test/build fix requests. The
  body runtime records changed-file evidence from mediated `write_file` calls,
  records validation evidence from test/build `execute_command` calls, and
  requires completions to name both the changed files and passing validation
  commands.
- Operator-blocked work is classified for explicit blocker or missing operator
  input signals. The evaluator requires a concrete blocker plus an operator/admin
  action that would unblock the work, and treats the result as a valid blocked
  terminal outcome.
- Contract-validated blocked completions are terminal in the body runtime: they
  commit task completion with a blocked terminal outcome instead of entering the
  generic "call complete_task" retry path.
- PACT runs now have an initial read-only gateway projection keyed by task ID,
  assembled from result artifact frontmatter and audit events.
- PACT audit reports provide a deterministic integrity hash for the run,
  evidence entries, artifact references, and audit events assembled for a task.
- PACT audit report verification recomputes the report hash from durable
  sources and reports whether an expected hash matches the current report.
- Result artifacts are task-oriented markdown files, not a general artifact
  model.
- Log correlation is by `task_id`; it does not yet create a normalized execution
  graph.
- Audit export includes initial enriched PACT/result correlation on log-entry
  responses and a separate PACT audit report shape. The report is hashed but not
  yet cryptographically signed.

Known gaps:

- objective building is not yet a typed runtime stage
- strategy routing is mostly implicit in prompt/tool availability
- planning is mostly prompt-level and not represented as execution state
- execution state is still spread across body runtime fields rather than a
  single typed state object
- tool observations are partially typed as evidence, but not yet a complete
  tool-result protocol with retryability, side effects, and provenance
- reflection/evaluation exists for contracts, but not yet as a general
  pre-commit runtime stage
- recovery is not yet a unified state machine
- artifact disposition is not unified
- memory learning hooks are not part of the execution lifecycle
- trajectory evals cover the foundational body-runtime contracts, but not full
  execution replay or policy-gated workflows
- evidence has a typed runtime write model, but not a durable typed ledger
- contracts are registered by name, but validation remains limited to
  current-info, file-artifact, code-change, and operator-blocked contracts
- activation sources are represented for body message intake, but not yet across
  every gateway activation source
- outcome contracts are not first-class

## Initial Vertical Slice

The first implementation slice should prove PACT without overgeneralizing.

Scope:

1. Introduce typed `Activation`, `Objective`, `ExecutionContract`,
   `EvidenceLedger`, `EvaluationResult`, and `Outcome` objects in the body
   runtime.
2. Adapt DM/channel-triggered tasks into PACT activation objects.
3. Convert current `WorkContract` into the first `ExecutionContract`.
4. Convert provider tool and local tool observations into evidence ledger
   entries. Done for the body runtime's in-memory ledger; durable storage remains
   future work.
5. Treat current-info answer requirements as the first outcome contract.
6. Ensure `blocked` is a terminal outcome that finalizes task state. Done for
   body runtime work-contract validation.
7. Add trajectory tests for the foundational body-runtime contracts. Done for:
   - current-info answer succeeds with source evidence
   - current-info answer blocks without evidence
   - file-artifact answer succeeds with runtime artifact evidence
   - code-change answer succeeds with changed-file and validation evidence
   - operator-blocked answer finalizes as a blocked terminal outcome

Non-scope for the first slice:

- extracting PACT to a separate repository
- rewriting all body runtime state
- multi-agent delegation
- full artifact disposition
- UI redesign
- long-running workflow persistence

## Next Implementation Targets

The next Agency PACT work must rebuild the agentic execution harness before
expanding governance or compliance surfaces. The current slice produced audit
and reporting scaffolding on top of a runtime that does not yet have typed
execution state, a real planner, or structured tool observations. Shipping more
audit or reporting features first would lock in the wrong abstractions — a
signed report that projects from reconstructed artifact frontmatter is a signed
attestation over scaffolding, not over actual execution.

Work is organized into waves. Each wave depends on the one before it. Items
flagged **(Blocked on …)** must not start until their blockers land. PR reviews
should reject harness or compliance work that tries to skip ahead.

### Wave 1 — Harness Foundations

Load-bearing primitives. Everything else in PACT eventually projects from
these.

1. **Typed `ExecutionState` object.**
   Introduce a runtime-owned `ExecutionState` carrying activation, objective,
   contract, plan, step history, tool observations, evidence, partial outputs,
   errors, recovery state, and proposed outcome. Existing body runtime fields
   move behind this object incrementally.

   This also replaces the earlier "first-class PACT run resource" target. The
   read-only PACT run projection endpoint must eventually source from
   `ExecutionState` instead of reconstructing runs from artifact frontmatter
   and audit events.

2. **Structured tool observation protocol.**
   Normalize tool results into status, data, provenance, timestamps, errors,
   retryability, side effects, and evidence classification. The model must
   never need to infer whether a tool worked from prose. This is the shape
   every tool-produced evidence entry inherits, and it is a hard prerequisite
   for the recovery state machine, the general pre-commit evaluator, and
   `external_side_effect` validation.

### Wave 2 — Harness Capabilities

Capabilities that populate and act on `ExecutionState`. Cannot be reliably
built before Wave 1 lands.

1. **Objective builder.**
   Normalize activations into explicit typed objectives with statement, kind,
   constraints, deliverables, success criteria, ambiguity, assumptions, and
   risk. Preserve ambiguity when it changes required action or risk posture;
   do not force resolution the runtime cannot defend.

2. **Strategy router.**
   Choose execution mode before work starts: trivial direct response, compact
   work contract, explicit plan, long-running checkpointed work, clarification,
   delegation, or external-side-effect workflow. Routing considers task risk,
   available tools, memory, budget, model tier, and artifact needs.

3. **Planner as runtime object.**
   Represent plans as runtime objects with steps, expected evidence,
   dependencies, approval points, fallbacks, and stop conditions. Plans are
   optional for trivial work and required for complex, risky, or side-effecting
   work. A plan requiring unavailable or unauthorized capabilities is rejected
   to clarification or escalation before execution.

4. **General pre-commit evaluator.**
   Generalize pre-commit evaluation beyond contract-specific checks: objective
   satisfaction, evidence sufficiency, output format, artifact existence,
   uncertainty disclosure, validation results, and bounded model-assisted
   critique where deterministic checks are insufficient. Runs against the full
   `ExecutionState` before any outcome commit.

5. **Recovery and failure state machine. (Depends on Wave 1 #2.)**
   Make retry, replan, fallback, clarification, escalation, blocked, failed,
   halted, expired, and superseded explicit states with bounded loops and
   auditable reasons. Classification of retryability and side-effect presence
   comes from the structured tool observation protocol.

### Wave 3 — Harness Completions

Wraps up the end-to-end harness once capabilities are in place.

1. **Artifact disposition protocol.**
   Represent generated files, links, reports, patches, logs, screenshots, PRs,
   issues, and bundles as first-class outputs with visibility, retention, and
   publication rules. Disposition decisions check recipient authorization and
   data boundaries before publishing.

2. **Memory learning hooks.**
   Add lifecycle hooks for proposing reusable procedures, environment facts,
   task outcomes, and failed strategies without directly mutating durable
   preferences, identity-shaped behavior, or reviewed knowledge. Depends on
   outcome commit and the general pre-commit evaluator.

3. **Trajectory-first eval fixtures.**
   Expand evals to assert activation, route, plan, tool observations, evidence,
   state transitions, terminal outcome, final output checks, and audit/ledger
   projections. Depends on every earlier wave item so assertions have typed
   state to target.

### Wave 4 — Governance And Boundary Surfaces

Governance overlay work. Most items can parallelize with Wave 2/3 where they
do not introduce foundation dependencies.

1. **Central PACT evaluator extraction.**
   The first extraction step is complete: core classification, evidence, and
   evaluation logic now lives in `pact_engine`, with `work_contract` preserved
   as a compatibility facade. Next, move this boundary toward a backend-owned
   package with explicit body/runtime adapters. Largely packaging work — may
   parallelize with Wave 2.

2. **Durable typed evidence ledger.**
   Typed `evidence_entries` now survive through PACT verdict audit signals and
   result artifact frontmatter. Promote them into a standalone ledger with
   stable IDs, producer, provenance, visibility, contract relevance, and
   export semantics. The ledger is the durable projection of
   `ExecutionState.evidence` — do not invent a parallel state model.

3. **Outcome contract catalog completion.**
   `current_info`, `file_artifact`, `code_change`, and `operator_blocked` have
   deterministic paths. Remaining registered kinds (`external_side_effect`,
   plus future delegation, approval request, monitoring event, and memory
   proposal contracts) should land deterministic evaluators as each harness
   capability they depend on comes online.

   `external_side_effect` validation is **(Blocked on Wave 1 #2.)** Do not
   start before the structured tool observation protocol lands. A side-effect
   evaluator written against ad-hoc tool results will encode the wrong
   abstraction and need to be rewritten.

4. **Policy/admin observability.**
   Add administrative surfaces for contract health, blocked verdict trends,
   missing evidence categories, and agent/runtime compliance. Can start on
   existing verdict/evidence signals; richness increases as `ExecutionState`
   projections land.

### Wave 5 — Compliance Artifacts

Compliance and portable-attestation work. Most of this wave is blocked on
earlier foundations.

1. **Report generation from `ExecutionState`. (Blocked on Wave 1 #1.)**
   PACT reports must project directly from typed `ExecutionState`, not
   reconstruct runs from artifact frontmatter and audit events. Do not start
   before `ExecutionState` is real.

2. **Signed PACT reports. (Blocked on Wave 1 #1 and Wave 4 #2.)**
   Add report signing, key IDs, verification metadata, and exported report
   verification. Hash-only reports remain sufficient for local integrity until
   real `ExecutionState` and a durable evidence ledger exist. Signing a
   reconstructed-from-artifacts report produces an attestation over
   scaffolding, not over execution.

3. **Audit export correlation evolution.**
   Initial admin audit responses now preserve PACT verdict evidence references
   and add result artifact links. Once `ExecutionState` is real, audit export
   correlation should source from it rather than reconstructing correlation
   from `task_id` joins.

## Extraction Path

PACT should begin as an Agency-internal implementation until the runtime boundary
is proven. It should be extracted into a standalone dependency when:

- core objects have no Agency-specific imports
- adapters are explicit
- trajectory fixtures are provider-neutral
- at least three activation types are implemented
- at least three outcome contracts are implemented
- Agency consumes PACT through interfaces

Candidate standalone interfaces:

```text
ActivationSource
AuthorityResolver
ModelClient
ToolExecutor
EvidenceStore
ArtifactStore
MemoryStore
PolicyDecisionPoint
EventSink
Clock
```

Agency would then provide ASK-aware adapters for authority, policy, mediation,
audit, memory, artifacts, and disposition.

## Open Questions

- Should PACT define a serialized run format before extraction?
- Should execution contracts be authored in code, YAML, or a policy-backed schema?
- Which phases require durable persistence in the first Agency implementation?
- How much model-assisted evaluation is acceptable before human review is needed?
- Should PACT outcomes map directly to gateway task state, or should the gateway
  expose PACT runs as a separate resource?
- How should continuation activations be represented for approvals, clarifications,
  and long-running workflows?
- What is the minimal artifact model that works for files, PRs, reports,
  screenshots, and external links?
