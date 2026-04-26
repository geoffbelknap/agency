# Slack Component Architecture: Webhook-First Platform Primitives and Agency Bridge

**Date:** 2026-04-11
**Status:** Draft
**Scope:** Defines the target Slack component architecture for Agency and `agency-hub`. Establishes the supported Slack platform primitives, the higher-level `agency-bridge-slack` bridge component, the removal path for `slack-ops`, and the responsibility boundaries between generic Slack infrastructure and pack-specific workflow behavior. Does not define the full implementation details of any one component; those belong in their component-specific specs.

## Problem

Agency's current Slack component story grew incrementally:

- `slack-events` handles inbound Events API webhooks
- `slack-ops` polls Slack history and mixes ingress with pack-like workflow assumptions
- narrow relays like `comms-to-slack` and `red-team-escalations-to-slack` push Agency messages outward

This leaves four architectural gaps:

1. **Uneven component boundaries.** Some components are transport primitives (`slack-events`), some are workflow-shaped (`slack-ops`), and some are pack-specific relays.
2. **Missing major Slack surfaces.** Slack's platform includes Events API, Interactivity & Shortcuts, slash commands, App Home, and Canvas. Agency does not yet have a coherent map for these surfaces.
3. **No canonical "Slack as Agency UI" layer.** Today there is no foundational component whose job is to make Slack feel like a first-class replacement for the web chat interface.
4. **Polling remains conceptually over-weighted.** Polling Slack history is useful for demos or constrained environments, but it is not the primary architecture Slack wants apps to use and it should not shape the rest of the design.

The result is unnecessary ambiguity when deciding whether new Slack behavior belongs in a generic connector, a pack, or a bridge-like product component.

## Goals

1. Define a clear, durable **Slack component inventory** for Agency and `agency-hub`.
2. Keep generic Slack **platform primitives** separate from Agency-specific UX behavior.
3. Introduce `agency-bridge-slack` as the canonical **Slack-native Agency conversation bridge**.
4. Remove `slack-ops` and remove polling from the target architecture.
5. Ensure the supported component set covers the **Slack surfaces most users actually want**.
6. Keep pack-specific workflow behavior out of generic Slack components.

## Non-Goals

- Defining every field of every Slack component schema in this document.
- Designing Discord or Mattermost in detail. This spec only establishes the Slack side and the cross-platform implications for the bridge family.
- Preserving backward compatibility with `slack-ops`. A breaking change is acceptable.
- Defining pack-specific approval flows, callback IDs, channel semantics, or admin workflows.

## Design Principles

1. **Webhook-first, not polling-first.** The supported Slack architecture assumes publicly reachable webhook ingress for Slack Events API, Interactivity, and slash commands.
2. **Platform primitive vs product behavior.** Generic Slack surface handling belongs in Slack connectors. Agency conversation semantics belong in `agency-bridge-slack`.
3. **Trust boundaries stay explicit.** Admin-scoped Slack operations remain separate from ordinary messaging and interactivity surfaces.
4. **Pack specificity stays in packs.** Generic connectors expose reusable capabilities. Packs decide how to use them.
5. **Broad surface coverage without connector sprawl.** Components should map to stable Slack platform surfaces, not to every app-specific use case.

## Target Component Inventory

### 1. `slack-events`

**Purpose:** Generic inbound Slack Events API intake.

**Owns:**

- webhook verification
- payload normalization for Events API deliveries
- route matching and event dispatch
- generic event ingress into Agency

**Typical surfaces covered:**

- channel and DM messages
- reactions
- membership/activity events
- other Events API event types that fit route-based dispatch

**Does not own:**

- Interactivity & Shortcuts webhook handling
- slash commands
- App Home publishing
- admin-scoped Slack APIs
- Agency conversation/thread mapping

**Rationale:** The Events API is a stable, generic Slack ingress surface and deserves its own primitive.

### 2. `slack-interactivity`

**Purpose:** Generic inbound Slack interactive surface handling.

**Owns:**

- Interactivity & Shortcuts webhook verification and normalization
- synchronous vs asynchronous response handling contracts
- modal lifecycle primitives
- `response_url`-based follow-up responses
- message action / shortcut ingress
- optional consent-token issuer capability as a generic primitive

**Typical surfaces covered:**

- `block_actions`
- `view_submission`
- `view_closed`
- `shortcut`
- `message_action`

**Does not own:**

- pack-specific approval flows
- community- or CISO-specific UX
- Slack admin operations
- Agency conversation/session semantics

**Rationale:** Slack interactivity has timing and payload constraints distinct from Events API and should remain a separate primitive.

### 3. `slack-commands`

**Purpose:** Generic slash-command ingress.

**Owns:**

- slash command request verification
- slash command payload normalization
- ACK and deferred-response contract
- routing command invocations to Agency

**Does not own:**

- broader interactive payload handling
- admin operations
- Agency bridge session semantics

**Rationale:** Slash commands are a distinct Slack ingress surface with their own request shape and UX expectations.

### 4. `slack-app-home`

**Purpose:** Generic App Home surface management.

**Owns:**

- handling App Home open events if needed by the component model
- `views.publish`
- Home tab rendering and refresh primitives

**Good uses:**

- personal inboxes
- assigned task summaries
- agent status panels
- approvals dashboards
- quick-launch controls

**Does not own:**

- general chat ingress
- modal interactivity
- admin APIs

**Rationale:** App Home is a distinct Slack-native UI surface and should not be buried inside unrelated connectors.

### 5. `slack-canvas`

**Purpose:** Generic durable Slack-native artifact publishing.

**Owns:**

- create/update/publish operations for Canvas content
- rendering durable Agency outputs into Slack-native long-form artifacts

**Good uses:**

- reports
- incident summaries
- handoff notes
- decision logs
- recurring status documents

**Does not own:**

- inbound chat or event ingress
- interactivity routing
- admin operations

**Rationale:** Canvas is a reusable publishing surface, not a workflow-specific feature.

### 6. `slack-admin`

**Purpose:** Privileged Slack admin plane.

**Owns:**

- workspace invite operations
- user-group and admin-scoped operations
- any other high-authority Slack APIs requiring materially stronger scopes

**Does not own:**

- ordinary message ingress
- interactive UX primitives
- App Home
- Canvas publishing

**Rationale:** Admin-scoped Slack authority is a different trust boundary and must remain isolated from general-purpose Slack components.

### 7. `agency-bridge-slack`

**Purpose:** Slack as a first-class Agency conversation client.

**Owns:**

- Slack DM/channel/thread to Agency thread mapping
- identity mapping between Slack users and Agency principals
- rendering Agency messages into Slack-native replies
- presenting approvals, retries, halt controls, and status updates inside Slack
- citations, attachments, and structured agent output rendering
- optional App Home inbox integration
- optional Canvas publication for durable outputs

**Built on top of:**

- `slack-events` for a minimal chat bridge
- optionally `slack-interactivity`
- optionally `slack-commands`
- optionally `slack-app-home`
- optionally `slack-canvas`
- only optionally `slack-admin` for privileged bridge modes

**Does not own:**

- the generic Slack platform primitives themselves
- pack-specific business workflows

**Rationale:** The bridge is product behavior, not just integration plumbing. It should be modeled explicitly instead of leaking into generic Slack connectors.

## Components Explicitly Out Of Scope

### Polling connector

Polling Slack history is **not** part of the target architecture.

Reasons:

- webhook-first is the supported Slack architecture
- polling is not required for most users
- polling should not distort the design of the generic Slack component family
- if someone needs polling later, they can build a dedicated fallback connector without forcing the rest of the system to optimize around it

### `slack-ops`

`slack-ops` is obsolete in this architecture and should be removed from the hub.

It currently mixes:

- ingress transport concerns
- Slack API usage patterns
- workflow assumptions
- pack/demo behavior

This spec treats it as a legacy component that can take a breaking change or be removed entirely.

## Responsibility Boundaries

### What belongs in generic Slack components

- Slack request verification
- Slack payload normalization
- generic route matching
- generic response primitives
- generic publishing/update primitives
- credential and scope requirements specific to a Slack platform surface
- reusable consent issuance capability, if kept generic

### What belongs in packs

- callback IDs
- channel choices
- admin approval semantics
- workflow-specific modal layouts
- business rules
- policy and escalation logic
- community-, CISO-, or product-specific behavior

### What belongs in `agency-bridge-slack`

- mapping human Slack interactions into Agency threads
- presenting Agency output naturally inside Slack
- preserving conversational continuity
- bridge-level controls like halt/retry/approve
- creating the feeling of "Slack as the Agency chat UI"

## Dependency Graph

```text
slack-events            ───> agency-bridge-slack (minimum chat bridge)
slack-interactivity     ───> agency-bridge-slack (optional enhancement)
slack-commands          ───> agency-bridge-slack (optional enhancement)

slack-app-home          ───> agency-bridge-slack (optional)
slack-canvas            ───> agency-bridge-slack (optional)
slack-admin             ───> packs or privileged bridge modes only

comms-to-slack          ───> remains a narrow leaf connector
red-team-escalations-
to-slack                ───> remains a narrow leaf connector
```

`comms-to-slack` and similar outbound mirrors remain valid, but they are not part of the foundational Slack platform architecture. They are leaf connectors for narrow use cases.

## Migration Plan

### Phase 1: Establish the target architecture

- Adopt this component inventory as the reference model.
- Treat `slack-ops` as obsolete immediately.
- Update planning and pack design documents to reference the new component set.

### Phase 2: Build missing platform primitives

- `slack-interactivity`
- `slack-commands`
- `slack-app-home`
- `slack-canvas`
- `slack-admin`

`slack-events` remains the existing base for Events API ingestion.

### Phase 3: Build `agency-bridge-slack`

- define the Slack-to-Agency conversation contract
- implement thread mapping and identity mapping
- support message rendering, approvals, retries, and status updates
- optionally add App Home and Canvas integrations

### Phase 4: Remove `slack-ops`

- delete it from the hub
- replace any demo pack dependency with `agency-bridge-slack` or other webhook-first components

### Phase 5: Align leaf connectors

- keep narrow outbound connectors like `comms-to-slack`
- ensure they are clearly documented as leaf connectors, not foundational Slack components

## Cross-Platform Implication

This architecture implies a broader bridge family:

- `agency-bridge-slack`
- `agency-bridge-discord`
- `agency-bridge-mattermost`

The bridge family should share a common conceptual contract:

- inbound human message handling
- identity mapping
- thread/session mapping
- outbound agent response rendering
- approvals and interaction controls
- artifact publication where supported
- audit correlation across platform and Agency traces

This spec does not define that contract in detail, but `agency-bridge-slack` should be designed so that a shared bridge abstraction remains possible.

## ASK Alignment

- **Tenet 1 (constraints external and inviolable):** strong-scope Slack operations remain separated into `slack-admin`, preserving explicit trust boundaries.
- **Tenet 2 (every action leaves a trace):** each component should continue to emit mediation-layer audit records for inbound and outbound Slack actions.
- **Tenet 3 (mediation is complete):** Slack interactions with Agency flow through explicit connectors or bridge components, not ad hoc direct calls from agent bodies.
- **Tenet 6 (all trust is explicit and auditable):** component boundaries make scope and authority visible instead of implicit.
- **Tenet 7 (least privilege):** different Slack surfaces map to different components and therefore can map to narrower credentials and scopes.
- **Tenet 24 (instructions only from verified principals):** inbound human instructions flow through verified Slack surfaces with explicit identity mapping, not undifferentiated chat transport.

## Open Questions

1. **Should external select/options loading live inside `slack-interactivity` or be a sibling component?** Initial bias: keep it inside `slack-interactivity` unless implementation pressure proves otherwise.
2. **Should `app_home_opened` handling live in `slack-events` or `slack-app-home`?** Initial bias: let `slack-app-home` own the semantic surface even if it reuses Events API infrastructure under the hood.
3. **Does `agency-bridge-slack` need a minimal dependency mode without App Home or Canvas?** Initial bias: yes; App Home and Canvas should be optional enrichments.
4. **Should leaf outbound relays eventually share a common outbound Slack posting primitive?** Initial bias: maybe, but not required to adopt this architecture.
