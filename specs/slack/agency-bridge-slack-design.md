# Agency Bridge Slack: Slack As A First-Class Agency Conversation Client

**Date:** 2026-04-11
**Status:** Draft
**Scope:** Defines `agency-bridge-slack`, a foundational component that makes Slack a first-class client for Agency conversations. Covers conversation/thread mapping, identity mapping, Slack-native rendering of Agency interactions, approvals and control actions, dependency relationships to lower-level Slack components, and audit expectations. Does not define the underlying Slack platform primitive components in detail; those are covered by the Slack component architecture and component-specific specs.

## Problem

Agency already has a web UI chat interface, but many teams want to interact with agents from within Slack because that is where they already coordinate work, request help, and review outcomes.

Today, Slack-related components exist as disconnected primitives:

- `slack-events` for inbound Events API deliveries
- narrow outbound relays like `comms-to-slack`
- draft designs for Slack interactivity and admin operations

What is missing is a **canonical Agency-facing bridge** that treats Slack not as a one-off integration point, but as a supported conversation client comparable to the web UI.

Without that bridge:

1. Every Slack-using pack has to re-invent how Slack messages map to Agency threads.
2. User identity and authority mapping becomes inconsistent across Slack-based workflows.
3. There is no single place to define how agent replies, approvals, citations, retries, and status updates should appear in Slack.
4. Slack-specific UX behavior leaks downward into generic connectors or upward into pack specs.

## Goals

1. Make Slack a **first-class conversation surface** for Agency.
2. Define a canonical mapping between **Slack conversations and Agency threads**.
3. Define how **Slack user identity** maps into Agency principal context.
4. Define reusable **Slack-native UX patterns** for agent replies, status, approvals, and controls.
5. Keep the bridge **built on top of** generic Slack platform primitives instead of duplicating them.
6. Preserve ASK properties around auditability, mediation, least privilege, and explicit trust.

## Non-Goals

- Replacing the web UI. Slack is an additional supported client surface, not the only one.
- Defining Slack platform primitives such as Events API webhook verification or modal protocol details.
- Bundling privileged Slack admin actions into the bridge by default.
- Encoding pack-specific workflows or business logic in the bridge.
- Designing Discord or Mattermost in detail. This spec only establishes the Slack bridge and the shape of the bridge family.

## Design Principles

1. **Conversation-first.** The bridge exists to host Agency conversations naturally inside Slack.
2. **Bridge, not bot sprawl.** Slack-specific packs should not each invent their own ad hoc chat runtime.
3. **Built from primitives.** The bridge composes lower-level Slack components as needed instead of redefining Slack platform behavior itself.
4. **Explicit identity and auditability.** Slack users are not anonymous transport endpoints; they map to explicit Agency principal context.
5. **Graceful degradation.** Where Slack cannot model the web UI exactly, the bridge should degrade predictably rather than pretending parity it does not have.

## Component Role

`agency-bridge-slack` is a **higher-level product bridge**, not a low-level Slack primitive.

It should be treated as the canonical answer to:

> "How do I use Slack as the place where humans talk to Agency and manage the lifecycle of that work?"

It is **not** the component that verifies Slack webhook signatures, ingests all Slack protocol surfaces, or exposes every Slack API. Those responsibilities remain in the lower-level Slack components.

## Dependency Model

`agency-bridge-slack` depends on the Slack primitives needed for its enabled UX.

For a minimal chat-bridge deployment, that is typically:

- `slack-events`

Optional dependencies for richer UX:

- `slack-interactivity`
- `slack-commands`

It may optionally integrate with:

- `slack-app-home`
- `slack-canvas`

It should **not** require `slack-admin` by default. If a specific deployment wants privileged Slack actions, that should be an explicit elevated mode or an adjacent pack/component dependency.

```text
slack-events            ───> minimal agency-bridge-slack
slack-interactivity     ───> optional enhancement
slack-commands          ───> optional enhancement
slack-app-home          ───> optional enhancement
slack-canvas            ───> optional enhancement
slack-admin             ───> optional elevated integration only
```

## Core Responsibilities

### 1. Conversation Mapping

The bridge maps Slack-originated communication into Agency threads.

The canonical mappings:

- **DM conversation** → one Agency conversation thread per human↔agent conversation
- **Channel thread** → one Agency conversation thread per Slack thread
- **Top-level channel message that starts a new interaction** → creates a new Agency thread rooted at that Slack message
- **Reply in an existing Slack thread** → appended to the mapped Agency thread

The bridge owns the correlation metadata that keeps these mappings stable and auditable.

### 2. Identity Mapping

The bridge maps a Slack actor into Agency principal context.

At minimum, each inbound interaction should carry:

- Slack workspace/team ID
- Slack user ID
- channel or DM context
- originating message or thread identifiers

The bridge resolves these into an Agency-side principal envelope suitable for:

- access control
- audit attribution
- policy checks
- pack/business logic

The bridge must not treat Slack display text alone as identity. Identity is rooted in verified Slack platform identifiers.

### 3. Message Rendering

The bridge renders Agency output into Slack-native form.

This includes:

- ordinary agent replies
- citations and source links
- progress/status updates
- warnings and escalation notices
- structured sections for long outputs
- error states and retry guidance

Rendering rules:

- prefer thread continuity over channel spam
- keep concise outputs concise
- use richer Slack formatting only where it improves comprehension
- preserve references to Agency audit/correlation identifiers where needed

### 4. Control Surface

The bridge exposes common human controls inside Slack.

Examples:

- approve
- deny
- retry
- halt
- resume (where policy allows)
- escalate

These controls are bridge-level UX affordances. The underlying authority checks remain in Agency mediation and policy layers.

### 5. Structured Interaction UX

The bridge uses Slack interactive surfaces where they materially improve UX.

Examples:

- approval buttons
- confirmation actions
- lightweight forms
- modal-driven structured input

These are generic bridge capabilities. Workflow-specific meaning remains in packs or higher-level components.

### 6. Durable Artifact Delivery

When supported by deployment configuration, the bridge may publish durable outputs using Slack-native surfaces:

- App Home for inbox/status/task summaries
- Canvas for longer-form generated artifacts such as reports, handoffs, and incident summaries

These are enhancements, not the minimum viable bridge.

## Conversation Model

### Inbound Paths

The bridge should support one required and two optional entry points:

1. **DM-first interaction**
   A user sends a DM to the Agency Slack app and the bridge routes it into a new or continuing Agency conversation.

2. **Channel mention / thread interaction**
   A user mentions the app in a channel or replies within an already-mapped thread.

3. **Explicit invocation** (optional)
   A slash command, message action, or shortcut invokes Agency from an existing Slack context.

### Thread Semantics

Slack threads are the closest natural equivalent to Agency conversation threads in channel contexts.

Rules:

- if a Slack message starts a bridge conversation in-channel, subsequent agent and human messages should remain in that Slack thread by default
- a DM conversation remains the canonical root for that DM exchange
- the bridge should not silently fork one Slack thread into multiple Agency threads unless there is an explicit operator or user action that does so

### Conversation Metadata

Each mapped conversation should record:

- Slack team/workspace ID
- Slack channel ID or DM ID
- Slack root message timestamp when applicable
- Agency thread ID
- initiating Slack user ID
- bridge deployment identifier

This mapping belongs to the bridge's own durable state, not to any one pack's business entities.

## Identity And Authority Model

### Slack User → Agency Principal

The bridge establishes a principal context from Slack identity.

At minimum:

- `platform = slack`
- `workspace_id`
- `user_id`
- `channel_context`
- optional mapped human identity if the deployment has directory/linkage data

This principal context is passed into Agency mediation so that:

- audit logs attribute actions to the human actor behind the Slack event
- authorization checks can distinguish channel context and user identity
- packs can evaluate membership/admin status using stable IDs rather than free-form names

### Channel Context

Slack channel context matters and should be preserved explicitly.

Examples:

- DM vs public channel vs private channel
- thread vs top-level message
- shared channel or external workspace context if relevant later

The bridge should not collapse all Slack-originated work into a single undifferentiated user identity.

## UX Contract

### Agent Replies

Default behaviors:

- reply in the same DM or thread
- keep progress and intermediate updates lightweight
- avoid flooding channels with large multi-message bursts
- prefer one coherent reply over fragmented chatter unless streaming/status semantics require otherwise

### Long Outputs

For long outputs, the bridge should choose among:

- concise in-thread summary plus link/citation references
- threaded continuation
- App Home update
- Canvas publication

Choice depends on deployment settings and output type.

### Approvals And Confirmations

The bridge may present generic confirmation controls using Slack interactive elements.

Examples:

- "Approve"
- "Deny"
- "Retry"
- "Open details"

The meaning of these controls is defined by the Agency-side action being mediated, not by the bridge alone.

### Status And Errors

The bridge should standardize a few common Slack-visible states:

- queued
- running
- waiting on human input
- blocked
- completed
- failed
- halted

These are presentational states for human comprehension. They do not replace authoritative Agency lifecycle state.

## State Model

The bridge requires durable state for conversation mapping and UX continuity.

At minimum:

- Slack conversation/thread ↔ Agency thread bindings
- bridge configuration
- per-thread UX metadata needed for updates or controls

This bridge state is not pack business state and not agent Identity. It is bridge-owned operational state.

## Configuration Shape

Expected bridge-level configuration includes:

- enabled entry modes:
  - DMs
  - channel mentions
  - slash commands
  - shortcuts
- default agent or routing target
- allowed channels / channel policy
- whether App Home integration is enabled
- whether Canvas publication is enabled
- rendering preferences
- escalation / approval UX toggles

Exact schema belongs in the component spec or deployment schema, not this architecture-level draft.

## Security And ASK Considerations

### Mediation

All Slack-originated actions must flow through Agency mediation, not directly from Slack into agent runtimes.

### Identity

Slack identity is verified through Slack platform surfaces and carried into Agency principal context explicitly.

### Least Privilege

The bridge should depend only on the Slack components and scopes required for its configured mode.

Examples:

- basic bridge mode should not require admin scopes
- Canvas integration should only be enabled when needed
- App Home integration should be optional

### Auditability

Every inbound Slack interaction and every outbound Slack-rendered action should be correlatable with:

- Slack actor and message context
- Agency thread ID
- resulting mediated actions in Agency

### Authority

The bridge may surface controls like approve or halt, but it does not itself define who is authorized. Authority checks remain in Agency policy and mediation layers.

## Relationship To Packs

Packs should consume the bridge rather than reinvent Slack chat behavior.

Packs remain responsible for:

- workflow logic
- business rules
- domain-specific prompts and approvals
- pack-specific modal content or actions

The bridge remains responsible for:

- hosting those interactions naturally in Slack
- preserving thread and identity continuity
- rendering outcomes consistently

## Relationship To Other Bridges

This spec should establish the pattern for:

- `agency-bridge-discord`
- `agency-bridge-mattermost`

Shared conceptual contract:

- inbound message handling
- identity mapping
- thread/session mapping
- outbound response rendering
- interactive controls where supported
- artifact publication where supported
- audit correlation

Slack-specific details should stay in `agency-bridge-slack`, but the overall bridge role should remain portable.

## Implementation Plan

1. **Phase 1: Core conversation bridge**
   - DM and thread mapping
   - identity mapping
   - basic reply rendering
   - correlation/audit metadata

2. **Phase 2: Interactive controls**
   - approve/deny/retry/halt UX
   - modal or button-based bridge controls where appropriate

3. **Phase 3: Slash-command and shortcut entrypoints**
   - richer explicit invocation paths

4. **Phase 4: App Home integration**
   - inbox/status/task surfaces

5. **Phase 5: Canvas integration**
   - durable artifact publishing

6. **Phase 6: Optional elevated integrations**
   - carefully scoped use of `slack-admin` for privileged modes if a concrete need exists

## Testing

- Unit tests for Slack thread ↔ Agency thread mapping behavior
- Unit tests for identity envelope construction from Slack events
- Integration test: DM conversation round-trip from Slack into Agency and back
- Integration test: channel thread round-trip with correct thread continuity
- Integration test: approval/retry/halt controls route through mediation and preserve correlation IDs
- Integration test: App Home and Canvas optional integrations do not affect baseline bridge mode when disabled
- Adversarial test: malformed or ambiguous Slack context never results in silently mis-attributed Agency principal context

## Open Questions

1. **Should the bridge own any durable inbox/task model, or should it only project existing Agency state into Slack?** Initial bias: project existing Agency state; avoid creating a second task system.
2. **How much streaming should Slack get?** Initial bias: limited staged updates rather than token-by-token rendering.
3. **Should the bridge support multiple agent personas in one Slack deployment, or should that remain a pack-level routing decision?** Initial bias: bridge supports routing, packs decide semantics.
4. **Should Canvas publication be bridge-triggered automatically for certain output classes, or always explicit?** Initial bias: explicit or policy-driven, not implicit.
