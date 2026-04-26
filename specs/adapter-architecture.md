# Adapter Architecture

## Status

Draft.

## Purpose

This spec defines Agency as an opinionated agentic harness with pluggable
integration surfaces. It separates:

- what Agency owns as non-negotiable harness behavior
- what adapters may vary
- what shims must normalize so Agency can still satisfy its contracts
- where PACT fits relative to the harness and the body runtime

This document is intentionally cross-cutting. Existing specs for runtime,
provider integration, task termination, and governed execution should align to
this vocabulary instead of each redefining platform boundaries locally.

## Summary

Agency is the harness.

Agency provides:

- external enforcement
- complete mediation
- complete auditability
- explicit least privilege
- visible and recoverable trust boundaries
- operator-facing lifecycle and runtime control

Agency permits pluggability at specific infrastructure surfaces, but that
pluggability does not weaken the harness contract. Adapters integrate external
elements. Harness-owned shims normalize those elements into Agency-governed
contracts.

PACT is not the harness. PACT is the execution-governance protocol implemented
by the current body runtime.

Current implementation note: the body runtime is not yet where it needs to be
as an independent agent runtime. One major reason is that governance-first PACT
concerns have dominated the design too early. Agency's target architecture is a
strong runtime first, with powerful governance layered into it, not a runtime
whose quality depends on governance compensating for weak core execution.

## Non-Goals

- This spec does not define a new runtime implementation.
- This spec does not replace the runtime contract in `runtime.md`.
- This spec does not make execution governance freely swappable.
- This spec does not remove ASK as the outer architectural constraint.
- This spec does not require all existing code to be adapterized immediately.

## Design Principles

1. **Agency is the harness, not merely a collection of integrations.**
   Host selection, provider mediation, policy application, runtime control,
   audit capture, and operator safety remain Agency responsibilities.

2. **Adapters integrate external systems; they do not redefine Agency.**
   An adapter may translate protocols or backend semantics, but it must not
   become the authority for mediation, audit completeness, or trust boundaries.

3. **Shims preserve Agency contracts around adapter behavior.**
   If an adapter exposes behavior that is too weak, too provider-specific, or
   too backend-specific for direct use, Agency wraps it in a shim that restores
   normalized platform semantics.

4. **Outer ASK invariants are not adapter-configurable.**
   A user may choose a host, provider, or security toolchain, but may not use
   those choices to bypass external enforcement, complete mediation,
   auditability, least privilege, or visible trust boundaries.

5. **Execution discipline is runtime-owned, not platform-universal.**
   Agency may host different runtimes in the future. The current body runtime
   uses PACT. Another runtime could use a different execution discipline,
   provided the Agency harness can still mediate, audit, introspect, and halt
   it.

## Top-Level Adapter Surfaces

Agency currently recognizes three top-level adapter surfaces:

- `host`
- `provider`
- `security`
- `embedding`

`runtime` is intentionally deferred as a future adapter surface. Agency has a
runtime contract today, but only one full agent runtime implementation exists:
the body runtime. This spec treats runtime pluralism as a future extension, not
as a mature pluggable surface today.

### Host Adapters

Host adapters own how Agency realizes and observes runtimes on a specific host
or backend.

Responsibilities:

- create, reconcile, and stop runtime instances
- provision runtime-local networks, mounts, and transport paths
- surface backend health and diagnostics
- map backend-native state into Agency runtime status/validate contracts

Examples:

- docker
- podman
- containerd
- apple-container

Host adapters must not become the source of truth for mission health semantics.
They provide backend observations. Agency decides how those observations map to
runtime health, operator warnings, and fail-closed behavior.

### Provider Adapters

Provider adapters own translation between Agency's normalized inference
contracts and provider-native APIs.

Responsibilities:

- request translation
- response translation
- streaming translation
- auth scheme handling
- tool-call semantics mapping
- termination metadata mapping
- provider capability declaration
- model and pricing discovery where supported

Provider adapters must stop provider quirks at the adapter boundary. Core
runtime logic must consume normalized request, response, and stream contracts,
not raw provider payloads.

Provider identity comes from adapter/provider descriptors. A provider named
`openai-compatible` is not a platform concept; contributors should register a
real provider adapter descriptor for the system they want to use. The
`api_format` field is a wire-format selector inside provider adapter plumbing,
not the provider's identity.

### Embedding Adapters

Embedding adapters own vector-generation integration for knowledge and other
semantic retrieval surfaces.

Responsibilities:

- endpoint and auth configuration
- model and dimension declaration
- batch request translation
- local versus remote transport selection
- egress mediation for remote embedding APIs

Agency defaults this surface to the OpenAI embedding adapter using
`text-embedding-3-small`, while keeping `none` as an explicit disabled mode and
fail-closed fallback.

### Security Adapters

Security adapters own pluggable security analysis, validation, and policy
specialization within the Agency harness.

Responsibilities:

- validation and finding generation
- policy pack specialization
- approval or consent augmentation
- runtime hardening checks
- egress or destination-policy specialization
- audit enrichment and risk classification

Security adapters may evaluate, constrain, classify, and validate. They must
not replace Agency's ownership of mediation placement, audit transport, or
trust-boundary visibility.

## Adapters Versus Shims

Adapters and shims solve different problems.

### Adapter

An adapter speaks the foreign system's language.

Examples:

- a provider adapter that knows Anthropic request/response semantics
- a host adapter that knows Podman socket and lifecycle behavior
- a security adapter that knows a specific policy pack's finding format

### Shim

A shim speaks Agency's language around the adapted element.

Shims normalize, constrain, enrich, or wrap adapter behavior so Agency can
still satisfy its contracts.

Examples:

- a provider-tool shim that adds authority classification, evidence extraction,
  audit events, and approval hooks around provider-native tools
- a host-state shim that turns backend-specific events into normalized runtime
  status and validate semantics
- a security shim that maps heterogeneous findings into standardized operator
  rationale and fail-closed decisions

In short:

- adapters integrate
- shims domesticate

Agency needs both.

## Why Shims Are Required

Adapters alone are insufficient because external systems do not naturally emit
all of the structure Agency needs.

Agency still needs to know, for example:

- what authority class an action exercised
- whether an action was mediated
- what evidence it produced
- what should appear in audit or result artifacts
- how errors should be normalized
- whether an action should be approval-gated
- how to preserve operator intelligibility

Whenever an adapter output is too raw or too element-specific, a harness-owned
shim must wrap it before core logic depends on it.

## Agency-Owned Core

The following remain harness-owned and are not adapter surfaces:

- enforcement placement
- audit transport and completeness
- mediation topology
- operator-facing lifecycle control
- principal and trust-boundary visibility
- durable memory review surfaces
- runtime introspection surfaces
- fail-closed decision boundaries

Adapters may feed these concerns. They do not own them.

## PACT Placement

PACT is not the Agency harness.

PACT is the execution-governance protocol implemented by the current body
runtime. It governs how that runtime:

- frames activations into objective-bound work
- records evidence and observations
- evaluates pre-commit state
- decides commit, retry, clarify, escalate, or block
- binds final outcomes to auditable runtime decisions

PACT is therefore:

- central to the body runtime
- portable in principle
- not required to be the only possible execution protocol across all future
  runtimes

Most users will want to tune PACT rather than replace it. That tuning belongs
in policy packs, validators, thresholds, and approval rules. However, at the
platform architecture level, a future alternative runtime could implement a
different execution discipline, provided Agency can still enforce ASK
invariants around it.

This also implies an explicit rebalance for current work: governance cannot be
a substitute for runtime quality. The body runtime must improve as an agent
runtime in its own right, and PACT must evolve from a governance-first design
center into an execution discipline layered onto a capable runtime.

## Runtime Surface (Deferred)

Agency has a runtime contract today, but not a mature runtime adapter ecosystem
yet.

Current state:

- one real agent runtime implementation exists: the body runtime
- PACT is the execution protocol used by that runtime
- delegation mechanisms such as Meeseeks are body-runtime features, not
  top-level platform adapters

Future state:

- `runtime` may become a first-class adapter surface once multiple Agency-
  compatible runtimes exist
- each runtime may declare its own execution discipline and delegation modes
- Agency will still own outer mediation, audit, introspection, and halt
  contracts

Until that future exists, runtime pluralism should be discussed as a design
target, not treated as a fully realized extension point.

## Current Maturity By Surface

### Host

Real adapter ecosystem exists today.

- multiple backends are available
- host probing and selection are implemented
- some higher-level monitoring and operator flows still leak Docker assumptions

### Provider

Real adapter ecosystem exists today.

- multiple provider families and compatibility modes exist
- translation layers are implemented
- some setup, discovery, and policy surfaces still branch on specific provider
  names or model families

### Security

Conceptual surface exists; adapterization is partial.

- policy, consent, validation, hardening, and egress concerns already exist
- they are not yet consistently expressed as one unified adapter surface

### Runtime

Contract exists; adapter ecosystem does not yet.

- the body runtime is the sole implementation
- PACT is central to that runtime

### Embedding

Adapter surface exists in knowledge. Ecosystem maturity is early.

- OpenAI is the default configured remote adapter
- Ollama and Voyage-style implementations exist
- setup and egress policy should be descriptor/config driven as this surface
  matures

## Migration Rules

New work should follow these rules:

1. Core logic must not branch on concrete host, provider, or security element
   names outside adapter registration or shim composition.

2. Core logic must not parse raw provider payloads outside provider adapters.

3. Core logic must not reason about runtime health from Docker-specific state
   when a normalized runtime status/validate surface exists.

4. UI and setup flows should prefer catalog- and registry-driven metadata over
   hard-coded provider or model-family lists.

5. Embedding behavior must be configured through embedding adapters, not hidden
   provider assumptions in knowledge or retrieval code.

6. Provider-native tools must not bypass Agency evidence, audit, and authority
   classification. A shim must wrap them if the raw provider surface does not
   already supply what Agency requires.

7. Security specialization should extend Agency's decision points, not replace
   the harness's ownership of mediation, audit, and trust boundaries.

## Consequences

If Agency follows this model:

- host and provider support can grow without eroding platform contracts
- security tooling can become pluggable without dissolving ASK boundaries
- foreign elements can be introduced incrementally through adapters and shims
- the body runtime can remain opinionated today while runtime pluralism remains
  possible later

If Agency does not follow this model:

- direct integrations will keep leaking provider and backend semantics into
  core logic
- operator-facing guarantees will vary unpredictably by chosen component
- future runtime pluralism will be much harder to support cleanly

## Relationship To Other Specs

- `runtime.md` defines the runtime contract Agency expects a runtime to obey.
- `provider-adapter.md` defines concrete provider-routing and capability work.
- `model-native-task-termination.md` defines one provider-shimmed runtime
  contract around termination semantics.
- `pact-governed-agent-execution.md` defines the execution protocol used by the
  current body runtime.

This spec sits above them and defines the architectural ownership boundaries
between harness, adapters, shims, and runtime-specific execution discipline.
