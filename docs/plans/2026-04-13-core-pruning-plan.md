# Core Pruning Plan For Early Agency Adoption

Status: draft  
Last updated: 2026-04-13

Related:

- [2026-04-13-core-feature-maturity-matrix.md](/Users/geoffbelknap/Documents/GitHub/agency-workspace/agency/docs/plans/2026-04-13-core-feature-maturity-matrix.md)
- [2026-04-14-0.2.x-core-release-gates.md](/Users/geoffbelknap/Documents/GitHub/agency-workspace/agency/docs/plans/2026-04-14-0.2.x-core-release-gates.md)
- [README.md](/Users/geoffbelknap/Documents/GitHub/agency-workspace/agency/README.md)

## Goal

Define a smaller, harder Agency core that can support real early users without
pretending the broader platform surface is equally ready.

This plan is intentionally stricter than the current maturity matrix. The
question here is not "does some implementation exist?" It is:

- does this need to be in the first believable Agency product?
- can a new user succeed with it reliably?
- does it strengthen the core value proposition, or distract from it?

## Working Definition Of Core

For the first believable Agency release, "core" should mean:

- one operator
- one machine
- one or a few agents
- local or small-team use
- strong ASK guarantees
- direct message workflow
- simple setup, simple recovery, simple audit

If a feature is mainly about ecosystems, packaging, multi-agent orchestration,
connector breadth, or graph sophistication beyond the basic agent loop, it is
not core unless it is proven to be necessary for the first users.

## Hard Read Of The Current Surface

Current repo and product surface are significantly broader than the believable
alpha core:

- CLI exposes roughly `196` subcommands in
  [internal/cli/commands.go](/Users/geoffbelknap/Documents/GitHub/agency-workspace/agency/internal/cli/commands.go).
- The README currently sells all of these at once:
  - teams and coordinators
  - packs and deployments
  - hub package management
  - connector ecosystem
  - knowledge graph intelligence and governance
  - routing optimization
  - principal registry and ACL
- Large code areas exist outside the immediate "secure agent runtime" path:
  - `internal/hub` is large and operationally complex
  - `internal/events`, `internal/knowledge`, `internal/ws`, `internal/consent`,
    `internal/relayhooks`, and `internal/evaluation` all widen the surface
  - image code is substantial in `images/knowledge`, `images/intake`,
    `images/web-fetch`, and connector-facing paths

The result is not just implementation risk. It is product dilution. A new user
cannot tell what Agency is actually ready to do well.

## Recommended Core Line

These should define `agency-core` in practice, whether or not the binary name
changes:

### Keep In Core

- agent create / start / stop / restart / show
- direct message workflow
- channel read/write only as needed to support DM and simple collaboration
- setup / quickstart / infra up / status / admin doctor
- provider configuration and basic routing
- budget enforcement and usage reporting
- audit logging and log inspection
- event-driven runtime delivery inside the gateway:
  - platform events
  - webhook/event ingestion
  - subscription routing
  - operator and agent wake-up based on events, not polling by default
- a trimmed graph core:
  - durable knowledge store
  - graph query / context retrieval
  - enough write/update behavior for agents to get better over time
- a stable build surface for other clients:
  - REST API as product surface, not just implementation detail
  - OpenAPI as canonical contract
  - MCP server as first-class operator/programmatic interface
- core web UI for:
  - setup
  - agents
  - direct messages
  - basic activity/audit visibility
- ASK enforcement path:
  - enforcer
  - egress mediation
  - credential isolation
  - fail-closed startup and teardown

### Keep, But Treat As Secondary Core Support

- simple presets
- minimal capability management needed for common presets
- WebSockets where they materially improve operator UX or comms fanout
  - realtime DM/chat updates
  - live status and activity updates
  - gateway/comms bridge behavior
- a thin SDK/client layer generated or stabilized from the OpenAPI contract

These can stay in the repo and binary, but should not be front-and-center in
the product story unless they are required for the first-user workflow.

## Demote Out Of Core

These areas look real enough to keep developing, but should stop defining the
mainline product promise.

### 1. Teams, Coordinators, And Missions

Recommendation: move to `experimental` product tier.

Reason:

- important long-term differentiator, but not needed for initial user success
- broad coordinator vision is explicitly called `Partial` already
- adds a large operator-model burden before the single-agent path is tight

Keep only if needed internally for architecture continuity, but remove from the
headline onboarding path.

### 2. Packs, Deployments, And Hub Package Lifecycle

Recommendation: remove from core release scope.

Reason:

- this is a platform distribution story, not a first-user story
- package install, source management, publishing, upgrades, OCI distribution,
  and durable deployments multiply failure modes
- `internal/hub`, deployment commands, and hub readiness scripts are a large
  operational surface with weak value for first adopters

This should be a later "ecosystem" layer, not a `0.2.x` core identity.

### 3. Connector Breadth

Recommendation: move all connector families out of core.

This includes:

- intake polling and webhook machinery as a product promise
- Slack connector family
- Drive authority runtime
- external relayhook-heavy flows

Reason:

- connector quality is uneven
- each connector adds auth, lifecycle, consent, ingress, and debugging burden
- these features are useful, but they are not necessary to prove Agency's main
  value: governed agents doing real work safely

If one connector must remain, pick one narrow reference connector and treat it
as a demo integration, not a pillar of the product.

Important distinction:

- **event-driven delivery is core**
- **broad connector inventory is not**

The gateway event bus and subscription model belong in core. The long tail of
connector implementations does not.

### 4. Knowledge Graph Ingestion And Governance

Recommendation: keep a narrower graph core and demote the rest.

Keep in core:

- graph-backed retrieval/context for agent work
- durable graph state that compounds useful knowledge over time
- basic query and stats needed to make the feature operable

Demote from core story:

- broad ingestion surface
- ontology operations
- classification review workflows
- quarantine/release operator flows
- insight saving
- graph intelligence / curator / governance surfaces

Reason:

- "agents get smarter and faster over time because the graph compounds useful
  context" is part of the Agency story
- the sprawl came from turning that into a second product with ingestion,
  governance, ontology, and review subsystems
- keep the compounding-knowledge primitive, not the whole graph platform

### 5. Routing Optimizer And Trust Calibration Surfaces

Recommendation: demote from core.

Reason:

- basic provider routing matters; optimizer workflows do not
- suggestions / approval / trust calibration are sophistication layers
- first users need predictable execution more than adaptive optimization

### 6. Principal Registry And Advanced ACL Surface

Recommendation: keep the underlying model, hide most operator-facing surface.

Reason:

- ASK-aligned identity and authorization matter architecturally
- the user-facing `registry`, `principals`, `classification`, and advanced
  governance commands make the product feel larger and less approachable than
  it should right now

Use the model internally. Expose less of it until operators actually need it.

### 7. API / SDK / Client Surface

Recommendation: promote to core and make it explicit.

Keep in core:

- stable REST API behavior
- canonical OpenAPI contract at
  [internal/api/openapi.yaml](/Users/geoffbelknap/Documents/GitHub/agency-workspace/agency/internal/api/openapi.yaml)
- MCP server as a supported operator and automation interface
- a deliberate client/SDK story for Agency-built and third-party consumers

Reason:

- Agency is not only a CLI and web app; it is a platform other clients need to
  build on
- the repo already treats the OpenAPI spec as canonical and exposes an MCP
  server, but the product plan has not treated this as a first-class core area
- if the API is core, compatibility, shape, and client ergonomics need to be
  release-gated like other core surfaces

Practical implication:

- define a supported API subset for early adopters
- keep generated or thin clients aligned to that subset
- avoid shipping internal-only endpoints as if they are durable public product

### 8. Meeseeks, Notifications, Semantic Cache, Miscellaneous Side Systems

Recommendation: demote to experimental immediately.

Reason:

- these do not appear to be central to the first-user success path
- each one consumes explanation, docs, UI space, and maintenance attention

## Product Strategy Recommendation

Do not fork the repo yet.

First, create three tiers inside the existing repo:

### Core

Supported, documented, release-gated, shown by default in CLI help and web nav.

### Experimental

Available behind an explicit flag and clearly marked as not part of the core
product promise.

### Internal

Kept for architectural continuity or future work, but not marketed, not shown
prominently, and not required for early-user success.

## Concrete Mechanisms

### 1. Reduce Surface Area Before Removing Code

Start by hiding, not deleting:

- trim default CLI help
- move advanced commands under an `experimental` or `x` namespace
- hide non-core web nav items behind a feature flag
- rewrite README and docs homepage around the smaller core

This gets the product honest before it gets smaller.

### 2. Make Release Gates Match The Smaller Product

`Blocking` should be only:

- setup / quickstart
- provider configuration
- infra up
- create/start/show agent
- DM send/reply
- event publication and event-driven task delivery for the supported core cases
- graph retrieval path required by the shipped agent loop
- audit/log visibility
- budget/usage visibility
- REST API and MCP behavior for the supported core subset
- basic web UI flows for the above
- installability

Everything else should be `Soft-blocking` at most, or removed from the release
gate entirely.

### 3. Use Feature Flags For Surfaces, Not ASK Enforcement

Feature flags are good for:

- web nav
- CLI command visibility
- routes for non-core product areas
- connector families
- missions/teams UX
- graph governance UX

Feature flags are not the right tool for:

- enforcement boundaries
- audit paths
- mediation paths
- credential isolation

Those remain invariant.

### 3a. Event-Driven Core Does Not Require WebSockets Everywhere

The core requirement is that Agency behaves as an event-driven system:

- sources produce events
- the gateway routes them by subscription
- agents and operators react to events without poll loops being the primary
  product model

`internal/ws` appears to matter mainly for realtime delivery and bridge fanout:

- web UI live updates
- comms bridge propagation
- agent signal broadcasting

That is valuable, and parts of it likely remain core for product feel. But the
core architectural commitment is the event bus and subscription model, not
"everything must be websocket-first."

### 4. Branch Only When Product Semantics Diverge

Use branches for active experimental tracks such as:

- hub/package ecosystem work
- mission/coordinator UX
- connector suites
- graph-heavy product directions

Do not fork until the product thesis truly diverges. Right now the problem looks
like surface management, not codebase incompatibility.

## Immediate Cuts I Would Make

If the goal is "some people can start using Agency soon," I would immediately
stop treating these as core:

- teams
- coordinators
- missions
- packs
- deployments
- hub publishing and upgrade flows
- connector ecosystem breadth
- Slack as a first-class story
- Drive admin as a first-class story
- graph ingestion/governance/classification
- routing optimizer approvals
- meeseeks
- notifications
- semantic cache

I would keep the product story brutally simple:

"Agency runs one or a few governed agents with real isolation, mediated tool
use, auditable execution, event-driven operation, graph-backed context that
improves over time, and a usable direct-message workflow."

That story is both true and differentiated.

## Recommended Execution Order

1. Rewrite product positioning around the smaller core.
2. Reclassify features into `core`, `experimental`, and `internal`.
3. Hide non-core CLI and web surfaces behind explicit flags.
4. Shrink release gates to match the core product.
5. Only then decide what code should actually move, freeze, or be split.

## Strong Opinion

Agency's advantage is not "it has more features than other agent systems."
Agency's advantage is that the secure runtime and governance model appear much
more serious than most of the market.

Leaning into breadth right now weakens that advantage.

The right near-term move is not to ship more platform. It is to become
unambiguous about the one thing Agency already seems able to do well:

- governed agents
- real isolation
- mediated execution
- auditable operator workflow

Everything else should earn its way back into core later.
