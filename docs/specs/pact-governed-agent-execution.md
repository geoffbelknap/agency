# PACT: Governed Agent Execution

## Status

Draft.

## Purpose

PACT is a governed execution model for agentic work. It binds every activation
to a principal, objective, execution contract, evidence ledger, trajectory, and
terminal outcome.

PACT exists to make agents good at work without making governance an afterthought.
It is not a chat model, prompt style, agent persona, or tool wrapper. It is the
runtime shape of agentic execution.

ASK governs agents as principals. PACT governs agent work as contract-bound
execution. Agency is the reference implementation of both.

## Non-Goals

- PACT is not an agent framework competing with orchestration libraries.
- PACT is not limited to inbound messages, chat, DMs, or human prompts.
- PACT does not define model provider APIs.
- PACT does not replace ASK enforcement, mediation, audit, or principal policy.
- PACT does not make the agent itself the enforcement boundary.

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

4. **Evidence is observed, not asserted.**
   Model-authored claims are not evidence. Evidence is recorded by the runtime,
   mediated tools, provider-tool events, artifact stores, policy decisions, and
   other trusted observation points.

5. **Trajectories matter.**
   A good final answer is insufficient if the path violated constraints, skipped
   required evidence, forged tool use, exceeded bounds, or hid uncertainty.

6. **Failure is first-class.**
   Blocked, needs-clarification, escalated, halted, expired, superseded, and
   failed outcomes are legitimate terminal states when they are explicit,
   auditable, and contract-consistent.

7. **Quality and governance are separate layers.**
   PACT defines how agentic work is executed well. ASK defines how that work is
   governed. Agency binds them through external enforcement, mediation, audit,
   least privilege, runtime verification, and governed knowledge.

8. **The runtime commits outcomes.**
   The model may draft plans, call tools, and propose outputs. The runtime decides
   whether a plan can execute, whether evidence satisfies the contract, and
   whether an outcome can be committed.

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
```

Objectives are not merely summaries. They are the target against which planning,
execution, evaluation, and outcome commitment are checked.

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
evaluation boundary for PACT work. Existing module-level helper functions remain
as compatibility wrappers, but registry lookup, contract construction,
activation classification, prompt material, blocker formatting, and completion
validation route through the evaluator.

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
| `operator_blocked` | Explicit blocked work requiring a blocker and unblock condition. |

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

### Log Correlation API

The gateway decorates agent audit log responses with result-artifact correlation
when a log event's `task_id` matches a saved result artifact.

Endpoint:

```text
GET /api/v1/agents/{name}/logs
```

Additive fields:

```text
has_result
result:
  task_id
  url
```

This decoration is response-time only. It must not mutate stored audit JSONL.
Audit remains append-only from the perspective of persisted events.

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
- Result metadata is additive and optional.
- Legacy artifacts without PACT frontmatter continue to work.
- Malformed metadata must not fail unrelated artifact listings.
- UI displays may fail open to "no metadata", but must not invent verdicts.
- Source URLs and tool names are observed evidence fields, not model-only claims
  when populated by runtime/provider observation paths.

### Current Limits

- The body runtime now records provider and local tool observations through a
  typed in-memory `EvidenceLedger`, but the durable evidence ledger is still
  represented by runtime observations, provider metadata, audit events, and
  artifact frontmatter rather than a typed PACT ledger resource.
- The named contract registry and body-local `PactEvaluator` exist, and
  `current_info`, `file_artifact`, and `code_change` now have deterministic
  completion gates.
- `external_side_effect` and `operator_blocked` are registered but not yet
  broadly classified or validated by contract-specific evaluators.
- File-artifact work is classified for explicit artifact-producing requests.
  The body runtime materializes file-artifact completions into result artifacts,
  records the path as runtime-owned evidence, and requires the completion to
  include that concrete path.
- Code-change work is classified for explicit code/test/build fix requests. The
  body runtime records changed-file evidence from mediated `write_file` calls,
  records validation evidence from test/build `execute_command` calls, and
  requires completions to name both the changed files and passing validation
  commands.
- Contract-validated blocked completions are terminal in the body runtime: they
  commit task completion with a blocked terminal outcome instead of entering the
  generic "call complete_task" retry path.
- PACT runs are not yet first-class gateway resources.
- Result artifacts are task-oriented markdown files, not a general artifact
  model.
- Log correlation is by `task_id`; it does not yet create a normalized execution
  graph.
- Audit export does not yet include enriched PACT/result correlation as a
  separate report shape.

Known gaps:

- evidence has a typed runtime write model, but not a durable typed ledger
- contracts are registered by name, but validation remains limited to
  current-info, file-artifact, and code-change contracts
- activation sources are represented for body message intake, but not yet across
  every gateway activation source
- planning is mostly prompt-level
- trajectory evals are limited
- artifact disposition is not unified
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
7. Add trajectory tests for:
   - current-info answer succeeds with source evidence
   - current-info answer blocks without evidence
   - simulated tool use retries then blocks
   - vague source phrasing fails the answer contract
   - published blocker finalizes execution state

Non-scope for the first slice:

- extracting PACT to a separate repository
- rewriting all body runtime state
- multi-agent delegation
- full artifact disposition
- UI redesign
- long-running workflow persistence

## Next Implementation Targets

The next Agency PACT work should deepen the runtime contract rather than add
more ad hoc UI surfaces.

Priority targets:

1. **Central PACT evaluator extraction.**
   Move the body-local evaluator boundary toward a backend-owned PACT evaluator
   module with explicit body/runtime adapters.

2. **Durable typed evidence ledger.**
   Promote the body runtime's in-memory `EvidenceLedger` into durable evidence
   entries with producer, provenance, visibility, and contract relevance.

3. **First-class PACT run resource.**
   Expose an execution/run view keyed by activation or task ID that joins
   objective, contract, evidence, verdict, artifact, and audit references.

4. **Audit export correlation.**
   Include PACT verdicts, result artifacts, and evidence references in signed
   audit exports without requiring UI reconstruction.

5. **Outcome contract validation beyond current information.**
   `file_artifact` and `code_change` now have deterministic paths. Next, wire
   `external_side_effect` and `operator_blocked` into real classification paths
   and deterministic evaluators before generalizing to more complex workflows.

6. **Policy/admin observability.**
   Add administrative surfaces for contract health, blocked verdict trends,
   missing evidence categories, and agent/runtime compliance.

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
