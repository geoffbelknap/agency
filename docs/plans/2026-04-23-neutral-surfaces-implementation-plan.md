# Neutral Surfaces Implementation Plan

## Status

In progress.

## Purpose

This plan sequences the refactors needed to make Agency neutral across host,
provider, embedding, and security surfaces while preserving its core harness
contracts.

The plan is intentionally prioritized to reduce architectural drift before
adding more features.

## Guiding Rule

Do not add new backend-, provider-, embedding-, or policy-specific branches in
core logic while this plan is incomplete. New work should either:

- land behind an adapter
- land behind a shim
- or explicitly be called out as temporary debt

## Phase 1 — Core Runtime And Admin Host Neutrality

### Goal

Move core runtime/admin health and operator lifecycle reasoning off
Docker-first assumptions and onto normalized runtime contracts.

### Work

1. Replace `DockerStatus` route dependencies with a backend-neutral runtime or
   backend health surface in API wiring.
2. Remove Docker-only fallback behavior from halt/restart paths where a host
   adapter or runtime contract already exists.
3. Normalize operator-facing remediation text so it is backend-aware or generic
   instead of Docker-specific by default.

### Concrete Tasks

1. Introduce a host-state observation seam used by orchestration and API code.
   It should provide:
   - runtime status lookup
   - runtime validate
   - normalized lifecycle events when available
   - polling fallback when event streams are unavailable
   - backend-aware diagnostics/remediation
2. Replace `DockerStatus` route dependencies with a backend-neutral runtime or
   backend health surface in API wiring.
3. Remove Docker-only fallback logic in `HaltController` where a runtime
   supervisor is already the primary control path.
   Specifically eliminate user-facing errors like:
   - `cleanup existing runtime: docker is not available`
4. Normalize remediation/help text in orchestrate and runtime-context helpers so
   Docker guidance appears only when Docker is actually the active backend.

### File-Level Checklist

- `internal/orchestrate/halt.go`
  - remove Docker-only fallback text and backend assumptions
- `cmd/gateway/main.go`
  - stop wiring Docker-only health/watch/status objects into neutral paths
- `internal/api/routes.go`
  - replace `DockerStatus`-centric route wiring where neutrality is required
- `internal/api/agents/routes.go`
  - replace `DockerStatus`-centric route wiring where neutrality is required
- `internal/api/infra/routes.go`
  - replace `DockerStatus`-centric route wiring where neutrality is required
- `internal/api/infra/handlers_infra.go`
  - stop recording runtime health through Docker-only status
- shared remediation helpers
  - remove default Docker-era guidance

### Suggested Implementation Order

1. Define the host-state shim contract and gateway wiring.
2. Replace API `DockerStatus` dependencies.
3. Convert halt/resume lifecycle handling to the new contract.
4. Clean up remediation text and residual Docker-first operator guidance.

### Primary Targets

- `internal/orchestrate/halt.go`
- `cmd/gateway/main.go`
- `internal/api/routes.go`
- `internal/api/agents/routes.go`
- `internal/api/infra/routes.go`
- `internal/api/infra/handlers_infra.go`
- shared runtime-context remediation helpers

### Exit Criteria

- non-Docker backends report runtime/admin health through the same normalized
  path
- halt/restart paths no longer fail with Docker-only errors when another
  backend is active
- operator diagnostics do not default to Docker guidance unless Docker is the
  active backend

## Phase 2 — Provider Neutrality

### Goal

Remove provider/model-family assumptions from core orchestration and setup
flows.

### Work

1. Replace hard-coded model defaults with catalog- or tier-driven selection.
2. Replace tier inference tables that embed vendor model IDs with data-driven
   model-selection logic.
3. Move quickstart provider verification to provider descriptors instead of
   hard-coded provider switches.
4. Make admin/setup flows render from provider catalog metadata rather than a
   fixed provider list.
5. Reduce provider discovery heuristics by moving capability and alias metadata
   into provider descriptors where possible.

### Primary Targets

- `internal/orchestrate/runtime_supervisor.go`
- `internal/orchestrate/start.go`
- `cmd/gateway/quickstart.go`
- `web/src/app/screens/AdminProviderTools.tsx`
- `web/src/app/screens/setup/ProvidersStep.tsx`
- `internal/cli/provider_discovery.go`
- provider catalog/config normalization points

### Exit Criteria

- core orchestration no longer defaults to vendor model IDs
- setup/admin flows can onboard provider-catalog entries without editing fixed
  provider lists
- provider verification logic is descriptor-driven for supported providers

### Completed So Far

- Body runtime, internal body context, knowledge synthesis, task-tier selection,
  evaluation LLM fallback, and host adapter workspace defaults now use neutral
  aliases (`fast`, `standard`, `frontier`) instead of Claude-family aliases.
- Meeseeks runtime startup now passes the selected subagent alias directly
  instead of prepending a provider family prefix.
- Audit cost estimation now reads model prices from `routing.yaml` metadata
  instead of carrying embedded provider pricing.
- Usage UI no longer guesses provider pricing client-side; it displays
  gateway/catalog-reported cost and marks missing pricing as unpriced.
- Provider catalog descriptors now drive setup/admin quickstart ordering,
  credential names, credential detection, provider verification, routing
  installation, egress policy, and credential defaults.
- `openai-compatible` is no longer a pseudo-provider. New unknown providers
  should be represented as real provider adapter descriptors.
- `api_format` is treated as adapter wire-format selection, not provider
  identity.
- Runtime/body scoped LLM auth now uses `AGENCY_LLM_API_KEY` instead of
  provider-named credential env vars.
- Runtime/body enforcer addressing now uses `AGENCY_ENFORCER_URL` only; the
  provider-shaped `OPENAI_API_BASE` compatibility alias was removed.
- Generic platform tests now use neutral fixture providers (`provider-a`,
  `provider-b`) unless the test is explicitly about bundled catalog/provider
  adapter behavior.
- Backend readiness and Hub OCI scripts no longer default provider setup to
  OpenAI. They preserve an existing `llm_provider` or require an explicit
  provider adapter env var for the provider-specific subtest/bootstrap path.
- Generic enforcer tests now use neutral provider/model fixtures. Remaining
  provider-specific enforcer tests are adapter translation coverage.

### Intentional Remaining Provider-Specific References

These references are allowed for this pass and should not trigger more cleanup:

- OpenAI embedding adapter defaults in knowledge embedding code, tests, and
  docs. The embedding surface is adapter-configured but defaults to OpenAI.
- Anthropic and Gemini enforcer/internal-LLM adapter implementation and adapter
  translation tests.
- Bundled provider catalog metadata and tests that explicitly verify bundled
  provider descriptors, quickstart ordering, credential metadata, and catalog
  hydration.
- Google Drive connector and `google_service_account` fixtures where the
  connector/auth type itself is the subject under test.
- `openai-compatible` references only as a negative example: it is not a
  platform-level provider identity.
- SSRF/URL-safety blocked-host references such as `metadata.google.internal`.

### Done Criteria For This Pass

- The provider/provider-model scan contains only the intentional exceptions
  listed above.
- No runtime/body/host launch path exports provider-shaped LLM env contracts
  such as `OPENAI_API_BASE`.
- Backend readiness scripts do not silently select a provider; they require an
  explicit provider only when bootstrapping provider-specific setup.
- Broad Go validation and focused Python/Web validation pass, or any remaining
  failures are documented as unrelated pre-existing issues.

### Completion Checkpoint

Status: complete for the provider/runtime/backend neutrality pass.

Validation run:

- `go test ./internal/...`
- `go test ./...` from `images/enforcer`
- `.venv/bin/python -m pytest images/tests/test_pack.py images/tests/test_embedding_providers.py images/tests/test_knowledge_synthesizer.py images/tests/body/test_error_signal.py images/tests/test_body_provider_tools.py images/tests/test_routing.py images/tests/test_credential_swap.py images/tests/test_swap_handlers.py`
- `npm test -- Admin.test.tsx AgentActivityTab.test.tsx Overview.test.tsx ProvidersStep.test.tsx AdminProviderTools.test.tsx Usage.test.tsx StartingAgentStep.test.tsx` from `web/`
- `bash -n` for touched readiness/OCI scripts
- `git diff --check`

Final scan status: remaining provider-specific references match the intentional
exceptions above.

## Phase 3 — Delegation And Product Model Neutrality

### Goal

Remove vendor-specific model-family assumptions from delegation and
product-facing mission configuration.

### Work

1. Replace `haiku` / `sonnet` / `opus` UI choices with aliases, tiers, or
   capability-based presets.
2. Rework Meeseeks/default delegation model selection to use model aliases or
   tier/capability goals rather than vendor family names.
3. Update validation/tests to reflect neutral model selection semantics.

### Primary Targets

- `internal/orchestrate/meeseeks.go`
- mission composer config
- mission wizard Meeseeks UI
- model validation/tests around Meeseeks and mission model selection

### Exit Criteria

- delegation defaults are expressed without vendor family assumptions
- mission configuration UI no longer hard-codes Anthropic family names

## Phase 4 — Security Surface Unification

### Goal

Make existing policy, approval, consent, hardening, and egress mechanisms look
like one coherent security extension surface.

### Work

1. Define a normalized security finding/decision shape.
2. Identify existing policy, consent, egress, and hardening modules that should
   emit that shape.
3. Introduce a security-decision shim that maps those results into common
   operator rationale and fail-closed decision points.
4. Document which security concerns are adapter-owned versus harness-owned.

### Completed So Far

- `internal/security` now defines shared contracts for:
  - authorization decisions
  - doctor/security findings
  - security mutations
  - approval status
  - policy chain status
  - policy exception status
  - authority execution status
- `internal/authz` now aliases authorization decisions to the shared security
  decision contract.
- Admin doctor checks now use the shared security finding contract.
- Consent requirements used by connector and service models now resolve through
  one shared model/security contract.
- Admin egress mutations now return a shared security mutation plus the updated
  egress state.
- Slack consent approval state, policy exception routing, MCP policy exception
  status, tool approval records, and authority invocation execution now use the
  shared security status vocabulary.
- Audit summarization now uses the shared security event classifier to count
  security findings and emit non-null `findings_count` mission metrics.
- Runtime context constraint severity now exposes an additive normalized
  `risk_level` using the shared security risk vocabulary.

### Primary Targets

- policy and capability approval models: partially normalized
- consent-token requirement models: normalized
- egress enforcement surfaces: mutation responses normalized
- runtime hardening/doctor findings: normalized
- audit/risk classification outputs: audit finding counts started; context
  severity bridged to normalized risk levels

### Exit Criteria

- security-related subsystems can emit normalized findings/decisions
- operator-facing security rationale uses one common shape
- security extensions do not bypass Agency mediation, audit, or trust-boundary
  guarantees

## Phase 5 — Embedding Adapter Neutrality

### Goal

Make semantic vector generation configurable through embedding adapters while
keeping OpenAI embeddings as the default configured adapter.

### Work

1. Keep `none` as explicit disabled mode and fail-closed fallback.
2. Default knowledge embeddings to the OpenAI embedding adapter.
3. Make endpoint, model, credential env var, and dimensions configurable.
4. Keep remote embedding calls mediated through egress.

### Completed So Far

- `images/knowledge/embedding.py` now defaults `KNOWLEDGE_EMBED_PROVIDER` to
  `openai`.
- OpenAI embedding adapter configuration now supports endpoint, credential env
  var, model, and dimension overrides.
- Embedding tests cover default OpenAI selection and configurable endpoint/auth.
- Knowledge feature docs now describe embeddings as an adapter surface with
  OpenAI configured by default.

### Exit Criteria

- knowledge/retrieval code does not assume a hard-coded remote embedding
  endpoint or credential env var
- default OpenAI embedding behavior remains configured and documented
- unsupported or broken embedding adapters fall back to disabled embeddings
  without crashing the knowledge service

## Phase 6 — Runtime Context Neutrality

### Goal

Remove Docker-era assumptions from in-container helpers and shared runtime
context behavior.

### Work

1. Replace implicit `host.docker.internal` defaults with a host/runtime-context
   shim.
2. Replace Docker-specific remediation text in shared runtime contexts.
3. Ensure runtime helper behavior is backend-aware without leaking backend
   details into unrelated runtime logic.

### Completed So Far

- Knowledge synthesizer gateway defaults now fall back to local gateway
  mediation instead of `host.docker.internal` when infra does not inject
  `AGENCY_GATEWAY_URL`.
- Root and internal shared exception helpers now use
  `RuntimeBackendNotAvailable` and Agency infra log/status guidance instead of
  Docker-specific remediation.
- Root comms, knowledge, and intake access loggers now describe `/health`
  suppression as generic health-probe noise rather than Docker healthcheck
  noise.
- Enforcer constraint delivery and egress credential-resolution comments now
  describe backend behavior generically instead of naming a single host
  backend implementation.
- Image resolution and Python image-build helper diagnostics now describe
  container image clients instead of Docker clients unless the reference is
  intentionally about Dockerfile semantics or gateway-proxy adapter aliases.

### Primary Targets

- knowledge synthesizer gateway defaults: normalized
- shared exception/help text: normalized
- other runtime context helpers with implicit Docker assumptions: scan in
  progress; remaining hits are historical/backend-specific docs or explicit
  gateway-proxy adapter wiring

### Exit Criteria

- in-container helpers resolve gateway/comms endpoints through a normalized
  backend-aware mechanism
- runtime context diagnostics are no longer Docker-specific by default

## Phase 7 — Mission Host Neutrality

### Goal

Move experimental mission health/watch logic off Docker-specific assumptions
without letting mission surfaces drive the core host architecture.

### Work

1. Rework mission health monitoring to consume runtime status/validate surfaces
   instead of Docker container-name/state inspection.
2. Replace Docker-event-only enforcer/workspace watchers with a host-state shim
   abstraction.
3. Ensure mission-facing alerts and pause behavior consume normalized runtime
   lifecycle signals rather than backend-specific container semantics.

### Concrete Tasks

1. Refactor `MissionHealthMonitor` to depend on runtime status plus mission
   target resolution, not Docker container names.
   Replace:
   - workspace/enforcer container-name construction
   - `ListAgencyContainerStates(...)`
   With:
   - runtime manifest/agent target lookup
   - runtime status/validate evaluation
2. Replace `EnforcerWatcher` and `WorkspaceWatcher` with host-state-shim-backed
   monitors.
   The normalized events needed immediately are:
   - runtime stopped unexpectedly
   - runtime restarted
   - mediation degraded or missing
3. Use polling fallback where event support is not yet available for a backend.

### Completed So Far

- `MissionHealthMonitor` now accepts the runtime status contract and prefers
  `RuntimeSupervisor.Get`/`Validate` for mission execution-health checks.
- Gateway mission-health wiring now passes the shared runtime supervisor, with
  backend component listing retained only as a fallback while host event shims
  are incomplete.
- Mission health, enforcer watcher, and workspace watcher comments/logs now
  describe runtime/backend behavior generically instead of Docker event streams.
- Enforcer and workspace watchers now consume a normalized `HostStateSource`
  event shim. Backend-specific container events are translated at the adapter
  edge into runtime component actions (`started`, `stopped`, `degraded`).

### Primary Targets

- `internal/orchestrate/mission_health.go`
- `internal/orchestrate/enforcer_watch.go`
- `internal/orchestrate/workspace_watch.go`
- `cmd/gateway/main.go`

### Exit Criteria

- non-Docker backends receive mission health and crash alerts through the same
  normalized path
- mission monitoring no longer depends on Docker container names or Docker
  event streams as the primary truth source

## Risks

- Host-neutrality work will expose places where runtime contracts are still too
  weak to replace Docker-specific observation directly.
- Provider-neutrality work may uncover product UX assumptions that rely on
  first-party providers being special.
- Security unification work can sprawl if it tries to redesign policy and
  consent systems all at once.

## Sequencing Notes

- Phase 1 should happen before more host/backend features land.
- Phase 2 should happen before adding more provider-specific setup or provider
  tool features.
- Phase 4 should begin with normalization and shims, not a full security system
  rewrite.
- Phase 7 is intentionally after the core runtime/admin phases because mission
  remains experimental and should not drive the base host-neutral architecture.

## Completion Rule

This plan is complete when:

- host reasoning is normalized
- provider/model assumptions are data-driven
- embedding behavior is adapter-configured
- delegation/product model selection is neutral
- security concerns share a coherent extension surface
- runtime contexts no longer default to Docker-era assumptions
- mission host reasoning is normalized without Docker-first assumptions

At that point, follow-up work can decide whether runtime itself is ready to
become a first-class adapter surface.
