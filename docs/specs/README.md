# Specs

Durable architecture and design references for the Agency platform. Specs
describe *what the system is* and *why it's that way*; implementation plans
(time-bounded task lists) live in `docs/plans/` and are deleted once shipped.

## Layout

Specs are organized by domain. Cross-cutting and foundational specs sit at
the root of this directory.

| Subdirectory | Holds |
|---|---|
| [`slack/`](slack/) | Slack connectors, bridges, component architecture, and interactivity |
| [`knowledge/`](knowledge/) | Knowledge graph: ingestion, curation, ontology, ACLs, intelligence |
| [`hub/`](hub/) | Hub package manager, deployments, versioning, OCI distribution |
| [`security/`](security/) | Auth, consent tokens, credentials, mediation, hardening |
| [`runtime/`](runtime/) | Agent lifecycle, memory (episodic/procedural/durable), meeseeks, reflection |
| [`policy/`](policy/) | Policy framework, principal/team models, multi-agent governance |
| [`connector/`](connector/) | Connector implementation, credentials, source types, third-party integration |
| [`routing/`](routing/) | Provider routing, model tiers, cost optimization, semantic caching |
| [`web/`](web/) | Web UI design, setup wizard, modernization |

## Root-level specs

Specs at the root describe foundational platform concepts that span multiple
domains, or cross-cutting concerns that don't fit cleanly into a single
subdirectory:

- **Platform foundations** — `agency-platform.md`, `runtime.md`,
  `adapter-architecture.md`, `core-feature-maturity-matrix.md`,
  `core-pruning-rationale.md`, `platform-features-1-6.md`,
  `platform-identity-uuid-adoption.md`
- **Infrastructure & API** — `2026-04-06-api-modularization-design.md`,
  `api-path-consolidation.md`, `infra-service-contract.md`,
  `port-standardization-service-discovery.md`,
  `gateway-restart-resilience.md`, `gateway-socket-proxy.md`,
  `container-build-standards.md`, `infrastructure-llm-routing.md`,
  `enforcer-consolidation.md`, `mediation-network-hardening.md`
  (note: also referenced from security)
- **Tasks & missions** — `missions.md`, `mission-composer.md`,
  `mission-success-criteria.md`, `mission-health-observability.md`,
  `brief-deprecation-and-budget-model.md`, `fallback-policies.md`,
  `mid-session-constraint-push.md`
- **Observability** — `trajectory-monitoring.md`,
  `economics-observability.md`, `audit-instance-tracking-and-error-signals.md`
- **Comms & events** — `event-framework.md`, `realtime-agent-comms.md`,
  `operator-notifications.md`, `notification-management.md`
- **MCP & integrations** — `mcp-oauth.md`, `mcp-thin-proxy.md`,
  `web-fetch-service.md`, `capability-registry.md`
- **Misc cross-cutting** — `coordination.md`, `capacity-limits.md`,
  `agency-relay.md`, `service-credential-tiers.md`,
  `neutral-surfaces-and-shims.md`, `python-to-go-port.md`,
  `monorepo-consolidation.md`, `quickstart-provider-model-tier.md`,
  `responsiveness-and-onboarding.md`, `context-compression.md`,
  `graceful-docker-degradation.md`, `build-versioning.md`

## Conventions

- **Naming** — non-dated, kebab-case names. The dated `2026-MM-DD-` prefix
  was a sprint-era convention; new specs should not use it.
- **Tier annotations** — specs whose subjects are `TierExperimental` or
  `TierInternal` in `internal/features/registry.go` should carry a short
  blockquote near the top noting the tier, so readers don't take
  experimental behavior as core.
- **Cross-references** — relative paths only. From a subdirectory, root
  specs are `../foo.md`; cross-subdirectory specs are `../bar/foo.md`.
- **Specs vs runbooks** — operational procedures (how to do X) live in
  `docs/runbooks/`. Architectural intent (why X exists, how it's shaped)
  lives here.
