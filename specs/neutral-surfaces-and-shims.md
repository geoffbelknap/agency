# Neutral Surfaces And Shims

## Status

Draft.

## Purpose

This spec identifies the core Agency surfaces that must remain neutral with
respect to hosts, providers, and security tooling, and defines the shim layers
required to preserve Agency's contracts when users or contributors bring their
own components.

This spec complements `adapter-architecture.md` by focusing on concrete
platform surfaces rather than the high-level taxonomy alone.

## Summary

Agency is an opinionated agentic harness.

Users may bring their choice of:

- host backend
- model provider
- security tooling

Agency must still preserve:

- external enforcement
- complete mediation
- complete auditability
- explicit least privilege
- visible and recoverable trust boundaries

Neutrality therefore requires two things:

1. core surfaces that do not encode backend- or provider-specific assumptions
2. harness-owned shims that wrap adapter behavior into Agency-normalized
   contracts

## Non-Goals

- This spec does not introduce runtime pluralism today.
- This spec does not redefine PACT.
- This spec does not require all existing implementation-specific behavior to
  disappear immediately.

## Neutral Surfaces

### 1. Runtime Health And Lifecycle

The platform must reason about runtime health through normalized runtime
contracts rather than backend-specific container observations.

Neutral contract:

- runtime manifest
- runtime status
- runtime validate
- runtime stop/restart semantics
- normalized runtime health alerts

Non-neutral examples to eliminate over time:

- Docker-only event watchers
- Docker container-name parsing as the primary runtime-health mechanism
- operator guidance that assumes Docker is the active backend

Adapter ownership:

- host adapter provides backend-native lifecycle and observation

Shim ownership:

- host-state shim maps backend-native events and states into normalized runtime
  status, validate, and alert semantics

### 2. Inference And Provider Semantics

The body runtime and higher-level orchestration must consume normalized
inference contracts, not raw provider payloads or provider-name branches.

Neutral contract:

- normalized request envelope
- normalized response envelope
- normalized stream chunks
- normalized tool-call semantics
- additive provider-native metadata where needed for audit

Non-neutral examples to eliminate over time:

- core code parsing raw Anthropic/Gemini/OpenAI payloads
- provider-specific tool behavior in runtime logic
- provider-name branches in core execution paths
- provider-shaped environment contracts for runtime/enforcer mediation
- `openai-compatible` as a generic pseudo-provider instead of a real adapter
  descriptor

Adapter ownership:

- provider adapter translates requests, responses, streams, auth, and
  capabilities
- `api_format` selects the provider-native wire format used by adapter
  plumbing; it is not provider identity

Shim ownership:

- provider-tool shim adds Agency authority classification, evidence extraction,
  audit events, policy gates, and error normalization around provider-native
  tools

### 3. Model Selection And Tiering

Core orchestration must not depend on vendor model families or hard-coded model
IDs.

Neutral contract:

- model aliases
- tier names
- capability declarations
- pricing metadata
- routing preferences expressed as data

Non-neutral examples to eliminate over time:

- UI options hard-coded to `haiku`, `sonnet`, `opus`
- tier inference that prefers specific vendor model IDs in code

Adapter ownership:

- provider adapter and provider catalog expose model and capability metadata

Shim ownership:

- model-selection shim resolves policy goals such as `fast`, `standard`, or
  required capabilities into a concrete configured model

### 4. Provider Setup And Product UX

Setup, quickstart, and admin flows must render from provider descriptors rather
than from fixed first-party provider lists.

Neutral contract:

- provider display metadata
- credential schema
- optional base URL fields
- verification strategy
- installability
- setup limitations surfaced as provider metadata

Non-neutral examples to eliminate over time:

- hard-coded `anthropic` / `openai` / `google` lists in UI and CLI
- special-case refusal of OpenAI-compatible providers in setup flows
- provider verification implemented as hard-coded `switch` statements

Adapter ownership:

- provider adapter and provider catalog expose provider descriptors

Shim ownership:

- setup shim renders provider-specific setup/verification behavior through a
  common Agency flow

### 5. Embedding Providers

Knowledge and semantic retrieval must not bake a specific embedding provider
into core behavior.

Neutral contract:

- embedding provider name
- model
- endpoint
- credential env var
- vector dimensions
- supported batch behavior

Non-neutral examples to eliminate over time:

- hard-coded remote embedding endpoint in retrieval logic
- fixed credential env var outside adapter configuration
- assuming embedding dimensions from provider name in core code

Adapter ownership:

- embedding adapter translates text batches into vectors for local or remote
  embedding systems

Shim ownership:

- knowledge shim applies fail-closed fallback, vector dimension validation, and
  retrieval behavior independent of the chosen adapter

Current default:

- configured OpenAI embedding adapter with `text-embedding-3-small`
- explicit `none` mode for disabled embeddings
### 6. Runtime Context Endpoints And Operator Guidance

In-container helpers and operator-facing remediation must not assume a
Docker-era environment when Agency can run on multiple backends.

Neutral contract:

- gateway endpoint resolution
- comms/knowledge endpoint resolution
- host-access transport hints
- operator remediation guidance

Non-neutral examples to eliminate over time:

- `host.docker.internal` as an implicit default
- “start Docker” as the default fix for runtime availability
- `docker logs` as the default remediation instruction

Adapter ownership:

- host adapter exposes backend-native connectivity and diagnostics

Shim ownership:

- runtime-context shim provides backend-aware endpoint resolution and
  remediation text

Current implementation notes:

- runtime contexts should prefer injected endpoint variables such as
  `AGENCY_GATEWAY_URL`, `AGENCY_COMMS_URL`, and `AGENCY_KNOWLEDGE_URL`
- fallback gateway endpoints should be local mediation defaults, not
  Docker-specific host aliases
- Docker or Podman host aliases are valid only inside backend-specific adapter
  or proxy wiring

### 7. Security Policy, Approval, Consent, And Egress

Security concerns must be pluggable without becoming invisible or bypassing
Agency's core guarantees.

Neutral contract:

- validation findings
- authorization and approval decisions
- security mutations
- consent-token requirements
- egress policy decisions
- policy evaluation status
- runtime hardening results
- authority execution status
- risk classification

Non-neutral examples to eliminate over time:

- scattered policy logic with no consistent security extension surface
- hard-coded finding formats
- security-specific behavior embedded directly into unrelated product flows

Adapter ownership:

- security adapter supplies policy packs, validation engines, approval modules,
  and hardening checks

Shim ownership:

- security-decision shim maps heterogeneous findings and decisions into
  normalized Agency rationale, operator messaging, and fail-closed behavior

Current shared contracts:

- `internal/security.Decision`
- `internal/security.Finding`
- `internal/security.Mutation`
- `internal/security.ApprovalStatus`
- `internal/security.PolicyStepStatus`
- `internal/security.PolicyExceptionStatus`
- `internal/security.AuthorityExecutionStatus`
- `internal/security.RiskLevel`
- `internal/security.ParseRiskLevel(...)`
- `internal/security.IsSecurityAuditEvent(...)`

## Shim Types

Agency should standardize the following shim families.

### Host-State Shim

Normalizes:

- backend-native lifecycle state
- backend-native events
- backend-native health observations
- backend-native diagnostics

Into:

- runtime status
- runtime validate
- normalized mission/runtime health alerts
- operator-facing diagnostics

Priority rule:

- apply the host-state shim to core runtime/admin surfaces first
- treat mission health/watch integration as follow-on work because missions are
  still experimental and should not drive the base host-neutral architecture

Phase 1 implementation shape:

- introduce a host-observation surface that can answer:
  - runtime status by agent/runtime ID
  - runtime validate results
  - normalized lifecycle events or a polling fallback
  - backend-aware diagnostics/remediation
- remove Docker-only fallback errors from halt/restart paths when a runtime
  supervisor or host-state shim is available
- replace API/runtime health dependencies that currently expose Docker-specific
  status objects
- normalize backend-specific remediation so Docker guidance appears only when
  Docker is the active backend

Mission follow-on shape:

- migrate mission health off container-name parsing and onto runtime
  status/validate plus persisted runtime manifest data
- replace Docker-event-only enforcer/workspace watchers with a host-state shim
  that can emit:
  - runtime stopped unexpectedly
  - runtime restarted
  - mediation path degraded

Current code paths to migrate first:

- `internal/orchestrate/halt.go`
- `cmd/gateway/main.go`
- API route dependencies that currently inject `DockerStatus`

Mission follow-on code paths:

- `internal/orchestrate/mission_health.go`
- `internal/orchestrate/enforcer_watch.go`
- `internal/orchestrate/workspace_watch.go`

### Provider-Tool Shim

Normalizes:

- provider-native tool schemas
- provider-native tool events
- provider-native result formats
- provider-native errors

Into:

- Agency authority classes
- evidence ledger entries
- policy and approval checkpoints
- audit events
- normalized tool outcomes

### Model-Selection Shim

Normalizes:

- user policy goals
- runtime cost modes
- provider capabilities
- configured tier/catalog metadata

Into:

- concrete model selections without core code depending on vendor names

### Setup Shim

Normalizes:

- provider-specific verification logic
- provider-specific credential/base URL needs
- provider-specific install flows

Into:

- a single Agency quickstart/setup/admin flow

### Runtime-Context Shim

Normalizes:

- backend-specific endpoint reachability
- host-access conventions
- diagnostic/remediation text

Into:

- backend-neutral runtime helper behavior
- backend-aware operator fixes

### Security-Decision Shim

Normalizes:

- policy-pack findings
- approval outputs
- hardening results
- egress decisions
- authority execution outcomes

Into:

- Agency-standard decision points
- fail-closed enforcement outcomes
- operator-readable rationale

## Migration Rules

1. New host work should target normalized runtime status/validate surfaces
   rather than Docker-first observations.

2. New provider work should land through provider descriptors and normalized
   inference/tool contracts, not new provider-name branches in core logic.

3. New product setup flows should read from provider metadata rather than
   maintaining fixed provider lists.

4. New security work should expose normalized findings or decisions through the
   security surface rather than scattering bespoke policy logic.

5. Operator-facing remediation text should be either host-aware or neutral by
   default.

## Relationship To Other Specs

- `adapter-architecture.md` defines the top-level architectural taxonomy.
- `runtime.md` defines the runtime contract Agency expects.
- `provider-adapter.md` defines provider routing and capability specifics.
- `model-native-task-termination.md` is one concrete example of a provider shim
  normalizing provider-native behavior.
- `pact-governed-agent-execution.md` defines the execution protocol used by the
  current body runtime.
