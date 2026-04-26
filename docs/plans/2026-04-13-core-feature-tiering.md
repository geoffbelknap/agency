# Core Feature Tiering

Status: draft  
Last updated: 2026-04-13

Related:

- [Core Pruning Plan](2026-04-13-core-pruning-plan.md)
- [Core Feature Maturity Matrix](2026-04-13-core-feature-maturity-matrix.md)
- [Release Gates 0.2.x](../runbooks/release-gates-0.2.x.md)

## Purpose

This document classifies Agency product surfaces into:

- `Core` — supported, user-visible, release-gated
- `Experimental` — intentionally available for continued development, but not
  part of the main product promise
- `Internal` — needed for architecture or future work, but not a user-facing
  product commitment

This is the working source for:

- README and docs slimming
- CLI help and command visibility
- web navigation and feature flagging
- release-gate scope

## Tier Definitions

| Tier | Meaning |
|------|---------|
| `Core` | Essential to the first believable Agency product. Must be visible, coherent, and release-gated. |
| `Experimental` | Valuable and worth developing, but not required for first-user success. Should be clearly marked and often hidden behind flags or secondary entry points. |
| `Internal` | Architectural plumbing, support code, or future-facing surface that should not be marketed or treated as a durable user-facing product area. |

## Tiering Matrix

| Feature Area | Tier | Why | Default Visibility | Release Gate | Notes |
|--------------|------|-----|--------------------|--------------|-------|
| Agent runtime core (`identity` / `constraints` / workspace / enforcer / body) | `Core` | This is the strongest and most differentiated part of Agency. | Visible by default in docs, CLI, and web. | `Blocking` | This remains the center of the product story. |
| Agent lifecycle management | `Core` | Create/start/stop/show/restart are required for any believable user workflow. | Visible by default. | `Blocking` | Includes status, health, and recovery basics. |
| Dynamic agent reconfiguration | `Core` | Updating a running agent without recreate is a core operator workflow. | Visible by default. | `Blocking` | Keep scope to live config updates that affect shipped DM/runtime behavior. |
| Comms / direct-message workflow | `Core` | DM is the primary operator interaction model. | Visible by default. | `Blocking` | DM should remain the primary demo and onboarding path. |
| Shared channels / simple collaboration | `Core` | Minimal channel support helps collaboration and backs comms behavior. | Visible by default, but kept simple. | `Blocking` | Keep the simple channel model; avoid overselling coordination semantics. |
| Event bus / routing / subscriptions | `Core` | Event-driven operation is part of the intended architecture and should replace polling as the default platform model. | Visible by default where it explains core behavior. | `Blocking` | Core is the event model, not every event-producing feature. |
| WebSockets / realtime fanout | `Core` | Realtime delivery matters for DM/chat feel and operator visibility. | Visible where needed, not as a standalone product pillar. | `Soft-blocking` | Treat as support for core event/comms UX, not the product thesis itself. |
| Model provider configuration and basic routing | `Core` | Agents must reliably reach a provider and execute work. | Visible by default. | `Blocking` | Keep provider setup and basic routing in core. |
| Routing optimizer / suggestion approvals / trust calibration UX | `Experimental` | Useful later, but not needed for first-user success. | Hidden behind explicit advanced/admin surface. | `Non-blocking` | Core users need predictable routing, not adaptive optimization workflows. |
| Budget enforcement | `Core` | Budget control is part of governed operation and a meaningful product claim. | Visible by default. | `Blocking` | Keep operator visibility tied to real execution. |
| Usage tracking / economics | `Core` | Users need to see cost and token usage if Agency claims governed execution. | Visible by default. | `Blocking` | Keep this tightly tied to audit/log surfaces. |
| Audit logging | `Core` | ASK and the product story both depend on durable, infrastructure-written audit. | Visible by default. | `Blocking` | This should remain a flagship capability. |
| Security / mediation / credential isolation | `Core` | Non-negotiable architectural foundation. | Visible by default. | `Blocking` | Never feature-flag the enforcement path itself. |
| Trajectory monitoring | `Experimental` | Interesting oversight surface, but not required for the first believable release. | Secondary admin surface only. | `Soft-blocking` | Can be promoted later if it becomes a strong operator workflow. |
| Knowledge graph retrieval / query / context | `Core` | Agents getting smarter and faster over time is part of the Agency story. | Visible by default. | `Blocking` | Keep graph as a retrieval/context primitive, not a giant standalone platform. |
| Knowledge graph stats | `Core` | Minimal stats help make the graph operable and debuggable. | Visible in admin/debug views, not as a headline feature. | `Blocking` | Keep lightweight. |
| Knowledge graph ingestion | `Experimental` | Valuable, but the broad ingestion story created feature sprawl. | Secondary or flagged surface only. | `Soft-blocking` | Keep a narrow path for continued iteration without making it a central promise. |
| Knowledge graph governance (classification / review / quarantine / ontology candidates) | `Experimental` | This is important long-term, but too large and operator-heavy for core right now. | Hidden behind advanced/admin surfaces. | `Non-blocking` | Preserve internally; do not market as part of first-user product. |
| Ontology operations | `Internal` | Useful support machinery for the graph system, but not part of the product promise. | Hidden from default product surfaces. | `Non-blocking` | Keep available only where needed for graph development/ops. |
| Missions | `Experimental` | Valuable, but not required for the first strong single-agent workflow. | Hidden behind experimental/product flag. | `Non-blocking` | Do not make missions part of core onboarding. |
| Team mission routing / failover / deconfliction | `Experimental` | Real work exists here, but it adds too much model complexity to the first user story. | Hidden behind experimental surface. | `Soft-blocking` | Keep testing it, but stop treating it as mainline. |
| Advanced coordinator delegation / multi-agent coordination UX | `Experimental` | Important future differentiator, but not yet complete enough for core. | Hidden behind experimental surface. | `Non-blocking` | This remains explicitly outside the first believable product. |
| Teams | `Experimental` | A useful abstraction for the future, but not necessary to get people using Agency now. | Hidden behind experimental surface. | `Non-blocking` | Keep docs and nav secondary at most. |
| Packs / declarative deployment | `Experimental` | Strong ecosystem idea, but too far from the first-user success path. | Hidden behind experimental surface. | `Non-blocking` | Stop presenting packs as central to early adoption. |
| Hub package management | `Experimental` | Important for ecosystem growth, not for core product clarity. | Hidden behind experimental/admin surface. | `Non-blocking` | This includes source management, install/update, and publishing lifecycle. |
| Durable deployments / instance runtime surface | `Experimental` | Operationally heavy and not needed for local/small-team early adoption. | Hidden behind experimental/admin surface. | `Non-blocking` | Keep for platform work, not mainline messaging. |
| Connectors / intake as a broad product area | `Experimental` | Event-driven architecture is core; connector breadth is not. | Hidden behind experimental surface. | `Non-blocking` | Keep a narrow reference path if needed, but do not sell connector breadth. |
| Slack connector family | `Experimental` | Too rough and too broad to remain part of core positioning. | Hidden behind experimental surface. | `Non-blocking` | Includes bridge and modal/interactivity work. |
| Drive authority runtime | `Experimental` | Useful specialized capability, not part of the smallest believable Agency. | Hidden behind experimental/admin surface. | `Non-blocking` | Continue validating, but keep out of core promise. |
| Web UI core shell / setup / agents / DM / basic activity | `Core` | Early users will rely on the web product for the main workflow. | Visible by default. | `Blocking` | This is the supported UI subset. |
| Web UI mutable non-destructive operator flows for core surfaces | `Core` | Core product surfaces must be operable from the web UI. | Visible by default. | `Blocking` | Constrain this to the true core areas. |
| Web UI surfaces for missions / packs / hub / graph governance / advanced admin | `Experimental` | These widen the interface faster than the product can support. | Hidden behind flags or secondary nav. | `Non-blocking` | Keep available only when explicitly enabled. |
| Relay-hosted web access | `Experimental` | Useful distribution path, but not required for first local adoption. | Secondary/optional surface only. | `Soft-blocking` if included in tester path, otherwise `Non-blocking` | Decide separately whether relay is part of onboarding. |
| REST API | `Core` | Agency needs a stable surface for the web app, CLI, MCP, and third-party clients. | Visible by default in docs and contract references. | `Blocking` | Treat this as a product surface, not just implementation detail. |
| OpenAPI contract | `Core` | The repo already treats it as canonical; product planning should match that reality. | Visible by default to builders/integrators. | `Blocking` | Define a supported subset explicitly if needed. |
| MCP server | `Core` | A first-class operator and automation interface for Agency. | Visible by default to builder/operator audiences. | `Blocking` | Keep aligned with the supported API subset. |
| Thin SDK / generated client layer | `Core` | Needed so Agency and third parties can build against a stable contract. | Visible to builders, but secondary to end-user onboarding. | `Soft-blocking` | Start with one supported client path rather than many hand-built clients. |
| Principal registry and advanced ACL operator surface | `Experimental` | Architecturally important, but too much visible governance surface too early. | Hidden behind advanced/admin surface. | `Non-blocking` | Keep the model; expose less of the operator UX. |
| Classification admin UX | `Experimental` | Too specialized for early users. | Hidden behind advanced/admin surface. | `Non-blocking` | Related to graph governance sprawl. |
| Capabilities management (minimal preset and common capability setup) | `Core` | Users need enough capability management to use shipped agents. | Visible by default, but constrained. | `Soft-blocking` | Keep the simple path; avoid making capability registry management a central story. |
| Capability registry power-user surfaces (custom MCP/service registration, marketplace-style flows) | `Experimental` | Valuable builder workflow, but not core to first-user product success. | Hidden behind advanced/admin surface. | `Non-blocking` | Continue development without centering it. |
| Notifications | `Experimental` | Helpful, but not core to the first supported workflow. | Hidden behind experimental/admin surface. | `Non-blocking` | Can be promoted later if operator use proves out. |
| Meeseeks | `Experimental` | Not part of the mainline product story right now. | Hidden behind experimental/admin surface. | `Non-blocking` | Keep out of onboarding and core docs. |
| Semantic cache | `Internal` | Support optimization, not user-facing product area. | Hidden from default surfaces. | `Non-blocking` | Keep as internal implementation detail unless it proves strategic. |
| Evaluation / eval surfaces | `Internal` | Useful for development and release confidence, not core end-user product. | Hidden from default product surfaces. | `Non-blocking` | Internal quality tooling unless intentionally productized later. |
| Release / installability (CLI, Homebrew, GHCR) | `Core` | If people cannot install and run Agency, none of the rest matters. | Visible by default in onboarding/docs. | `Blocking` | This is part of the product, not just operations. |

## Default Visibility Rules

### Show By Default

- agent lifecycle
- DM/chat
- setup and quickstart
- provider setup
- budget/usage/audit for core flows
- graph retrieval/query in its trimmed core form
- core web UI surfaces
- API/MCP/OpenAPI builder surface
- installability and local runtime operations

### Hide Behind Experimental Flags Or Secondary Entry Points

- missions
- teams
- coordinators
- packs
- hub lifecycle
- connector breadth
- Slack and Drive productized flows
- graph ingestion and governance workflows
- routing optimizer workflows
- principal/classification power-user governance UX
- relay-hosted access unless explicitly part of a release path

### Keep Out Of Product Positioning

- semantic cache
- eval tooling
- ontology admin
- other support machinery that exists for implementation or future work

## Immediate Follow-Ons

This matrix should drive four near-term changes:

1. Rewrite [README.md](/Users/geoffbelknap/Documents/GitHub/agency-workspace/agency/README.md) and docs landing pages around the `Core` set.
2. Trim CLI help and command discoverability so `Experimental` and `Internal`
   surfaces are not presented like equal peers.
3. Hide non-core web navigation and routes behind explicit feature flags.
4. Rewrite release gates so only `Core` items are `Blocking` by default.

## Working Principle

Do not delete experimental work first. First make the product honest about what
is core.

Once visibility and release scope match reality, it becomes much easier to
decide what should be hardened, what should be frozen, and what should be split
later.
