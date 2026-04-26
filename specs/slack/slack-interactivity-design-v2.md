# Slack Interactivity Connector: Generic Interactive Surface With Realistic Sync/Async Semantics

**Date:** 2026-04-11
**Status:** Draft
**Scope:** Revises the Slack interactivity connector design so it reflects Slack's actual timing and response semantics while remaining generic. Covers Interactivity & Shortcuts webhook handling, route-level sync vs async modes, modal lifecycle primitives, `response_url`, and generic consent issuance capability. Does not define pack-specific interaction workflows.

## Problem

Slack interactivity payloads are not just another async event stream:

- they must be acknowledged promptly
- some require synchronous response behavior
- `trigger_id`-based modal opens are time-sensitive
- `view_submission` can return synchronous validation or update semantics

A generic Slack interactivity connector is still the right abstraction, but it must model these protocol realities correctly and keep workflow-specific behavior out of the connector.

## Goals

1. Provide a generic interactivity primitive for Slack.
2. Model **route-level** sync vs async handling rather than one global async rule.
3. Support modal lifecycle primitives and `response_url`.
4. Keep the connector reusable across packs and bridges.
5. Allow optional generic consent-token issuer behavior without embedding workflow specifics.

## Non-Goals

- No community-, CISO-, or pack-specific approval workflows in the connector core.
- No generic Slack Events API handling.
- No slash command ingress.
- No admin-scoped Slack authority.

## Design

### 1. Component role

`slack-interactivity` owns generic handling of:

- `block_actions`
- `view_submission`
- `view_closed`
- `shortcut`
- `message_action`

It is the Slack platform primitive for interactive UX, not the place where business workflows are defined.

### 2. Handling modes

Slack interactivity must support two distinct handling modes:

- **`async_ack`**
  The connector acknowledges immediately and dispatches the event asynchronously.
  Use when no synchronous Slack response is required.

- **`sync_response`**
  The connector routes the interaction through a bounded synchronous response path.
  Use when the interaction needs:
  - modal open on a live `trigger_id`
  - `view_submission` validation errors
  - immediate modal update/push semantics

Handling mode is declared per route, not globally.

### 3. `connector.yaml`

```yaml
kind: connector
name: slack-interactivity
version: "2.0.0"
description: >
  Receives Slack Interactivity & Shortcuts payloads, normalizes them,
  supports route-level synchronous or asynchronous handling, and exposes
  generic modal and follow-up response primitives.

requires:
  services:
    - slack
  credentials:
    - key: slack_signing_secret
      description: Slack app signing secret
    - key: slack_bot_token
      description: Slack bot token for modal and follow-up API calls

source:
  type: webhook
  path: /webhooks/slack-interactivity
  webhook_auth:
    type: hmac_sha256
    secret_credref: slack_signing_secret
    header: X-Slack-Signature
    timestamp_header: X-Slack-Request-Timestamp
    prefix: "v0="
    max_skew_seconds: 300
  body_format: form_urlencoded_payload

routes:
  - match:
      payload_type: shortcut
      callback_id: some_callback
    handling_mode: sync_response
    target:
      agent: "${interactivity_target_agent}"

  - match:
      payload_type: block_actions
    handling_mode: async_ack
    target:
      agent: "${interactivity_target_agent}"
```

### 4. Generic tools

- `slack_view_open`
- `slack_view_update`
- `slack_view_push`
- `slack_interaction_respond`

Tool contract notes:

- `slack_view_open` is only reliable when invoked inside the synchronous handling path while `trigger_id` is still valid
- asynchronous follow-up should prefer `slack_interaction_respond`, `slack_view_update`, or `slack_view_push` against already-established UI state

### 5. Synchronous response contract

For `sync_response` routes, the connector must support response shapes such as:

- plain ACK
- modal open
- view update
- view push
- validation errors for `view_submission`

The exact internal mechanism is implementation-specific, but the spec must not pretend these interactions can always be deferred safely.

### 6. `response_url`

The connector should pass `response_url` through in normalized payloads where present and expose a generic follow-up response primitive for asynchronous replies or updates.

### 7. Generic consent issuer role

If enabled, `slack-interactivity` may act as a generic consent-token issuer:

- collect authorized witness clicks
- produce signed consent tokens for configured operation kinds
- remain generic about what those operations mean

Any workflow-specific approval UX remains in packs or higher-level components.

## Relationship to bridge and packs

- `agency-bridge-slack` may consume this connector for Slack-native controls and structured interactions
- packs may use it for workflow-specific modal/approval UX
- neither the bridge nor packs should require the connector to embed their business semantics

## Security

- verified Slack signatures
- no raw token exposure to agents
- mediated and audited view/follow-up operations
- explicit separation from admin-scoped Slack operations

## Open Questions

1. Should external select/options loading be folded into this connector or modeled separately? Initial bias: keep it here unless complexity proves otherwise.
