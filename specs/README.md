# Specs

Durable architecture and design references for the Agency platform. Specs
describe *what the system is* and *why it's that way*; implementation plans
(time-bounded task lists) live in `docs/plans/` and are deleted once shipped.

## Layout

Specs are organized by domain. Cross-cutting and foundational specs sit at
the root of this directory.

| Subdirectory | Holds |
|---|---|
| [`comms/`](comms/) | Event framework, realtime agent comms, notifications |
| [`connector/`](connector/) | Connector implementation, credentials, source types, third-party integration |
| [`hub/`](hub/) | Hub package manager, deployments, versioning, OCI distribution |
| [`infra/`](infra/) | Gateway, API modularization, ports, service contract, container build, enforcer topology |
| [`knowledge/`](knowledge/) | Knowledge graph: ingestion, curation, ontology, ACLs, intelligence |
| [`mcp/`](mcp/) | MCP OAuth, thin proxy, web-fetch service |
| [`missions/`](missions/) _(experimental)_ | Mission system: composer, success criteria, health, fallback, brief deprecation |
| [`observability/`](observability/) | Trajectory monitoring, economics observability, audit instance tracking |
| [`policy/`](policy/) | Policy framework, principal/team models, multi-agent governance |
| [`routing/`](routing/) | Provider routing, model tiers, cost optimization, semantic caching |
| [`runtime/`](runtime/) | Agent lifecycle, memory (episodic/procedural/durable), meeseeks, reflection |
| [`security/`](security/) | Auth, consent tokens, credentials, mediation, hardening |
| [`slack/`](slack/) | Slack connectors, bridges, component architecture, and interactivity |
| [`web/`](web/) | Web UI design, setup wizard, modernization |

## Root-level specs

Specs at the root describe foundational platform concepts that span multiple
domains, or cross-cutting concerns that don't fit cleanly into a single
subdirectory:

- **Platform foundations** — `agency-platform.md`, `runtime.md`,
  `adapter-architecture.md`, `core-feature-maturity-matrix.md`,
  `core-pruning-rationale.md`, `platform-features-1-6.md`,
  `platform-identity-uuid-adoption.md`
- **Cross-cutting concerns** — `coordination.md`, `capacity-limits.md`,
  `context-compression.md`, `capability-registry.md`,
  `service-credential-tiers.md`, `neutral-surfaces-and-shims.md`,
  `agency-relay.md`
- **Repo & release evolution** — `monorepo-consolidation.md`,
  `python-to-go-port.md`, `build-versioning.md`,
  `graceful-docker-degradation.md`
- **Onboarding & UX** — `quickstart-provider-model-tier.md`,
  `responsiveness-and-onboarding.md`

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
