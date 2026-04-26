# Slack Commands Connector

**Date:** 2026-04-11
**Status:** Draft
**Scope:** Defines `slack-commands`, a generic Agency hub connector for Slack slash commands. Covers request verification, payload normalization, synchronous acknowledgement, deferred responses, routing, and dependency boundaries. Does not define pack-specific commands or workflow semantics.

## Problem

Slash commands remain one of Slack's most common explicit invocation paths. They are distinct from Events API and Interactivity payloads:

- they arrive at a dedicated Request URL
- they require acknowledgement within Slack's response window
- they often carry `response_url` and sometimes `trigger_id`
- they are intentionally explicit user invocations, not ambient event stream traffic

Agency currently has no generic slash-command primitive, which forces command-shaped workflows to either overload other connectors or build ad hoc Slack integrations.

## Goals

1. Provide a generic slash-command ingress connector.
2. Verify Slack signatures and normalize command payloads.
3. Support immediate acknowledgement and deferred follow-up responses.
4. Route commands into agents or bridge components without embedding pack-specific logic.
5. Keep this surface separate from Events API and Interactivity.

## Non-Goals

- No command-specific business logic.
- No modal/interactivity handling beyond carrying `trigger_id` through when present.
- No admin-scoped Slack operations.

## Design

### 1. Component role

`slack-commands` is a generic ingress primitive for Slack slash commands.

It owns:

- request verification
- payload normalization
- route matching
- ACK strategy
- deferred response primitives via `response_url`

It does not own:

- pack-specific command names or semantics
- generic message ingress
- interactivity webhook handling

### 2. `connector.yaml`

```yaml
kind: connector
name: slack-commands
version: "1.0.0"
description: >
  Receives Slack slash command invocations, verifies them, normalizes
  the payload, acknowledges within Slack's response window, and routes
  them into Agency.

requires:
  services:
    - slack
  credentials:
    - key: slack_signing_secret
      description: Slack app signing secret
    - key: slack_bot_token
      description: Slack bot token, used for deferred responses when needed

source:
  type: webhook
  path: /webhooks/slack-commands
  webhook_auth:
    type: hmac_sha256
    secret_credref: slack_signing_secret
    header: X-Slack-Signature
    timestamp_header: X-Slack-Request-Timestamp
    prefix: "v0="
    max_skew_seconds: 300
  body_format: form_urlencoded
  ack_strategy: immediate_200

routes:
  - match:
      command: /agency
    target:
      agent: "${command_target_agent}"
    brief: >
      Slack slash command invoked.
      Command: {{ payload.command }}
      Text: {{ payload.text }}
      User: {{ payload.user_id }}
      Channel: {{ payload.channel_id }}
      Trigger ID: {{ payload.trigger_id if payload.trigger_id else "n/a" }}
```

### 3. Normalized payload

The connector should normalize Slack's command form fields into a consistent payload envelope:

- `command`
- `text`
- `user_id`
- `user_name`
- `channel_id`
- `channel_name`
- `team_id`
- `team_domain`
- `response_url`
- `trigger_id` when present

### 4. Response model

The connector supports two response patterns:

- **Immediate ACK**
  Return 200 quickly so Slack does not treat the command as failed.

- **Deferred response**
  Use `response_url` for follow-up content after the agent or bridge finishes processing.

### 5. Generic tool surface

If the connector exposes tools, they should remain generic:

- `slack_command_respond`
  Post a deferred response to a command via `response_url`

- `slack_command_open_modal`
  Best-effort modal open for command payloads carrying `trigger_id`

No workflow-specific command tools belong here.

## Dependency relationship

`agency-bridge-slack` may consume this connector as one of its explicit invocation paths.

## Security

- HMAC verification against the Slack signing secret
- no raw secrets exposed to agents
- all follow-up responses mediated and audited

## Open Questions

1. Should command responses live as generic tools here or be centralized in `slack-interactivity`? Initial bias: keep command-specific response helpers here.
