# Slack Canvas Connector

**Date:** 2026-04-11
**Status:** Draft
**Scope:** Defines `slack-canvas`, a generic Agency hub connector for publishing durable Slack-native artifacts to Canvas. Covers create/update/publish primitives and content delivery boundaries. Does not define pack-specific document semantics.

## Problem

Some Agency outputs are too durable or structured to fit well as ordinary thread replies:

- reports
- handoff documents
- incident summaries
- decision logs

Slack Canvas is a natural publishing target for these artifacts, but Agency has no generic Canvas primitive today.

## Goals

1. Provide a generic Canvas publishing primitive.
2. Support create/update/publish operations for durable Slack-native artifacts.
3. Keep Canvas separate from message ingress, commands, and admin scopes.
4. Allow bridges and packs to publish durable outputs without embedding Slack-specific publishing logic.

## Non-Goals

- No chat ingress.
- No workflow-specific document semantics.
- No attempt to turn Canvas into a knowledge graph or system of record.

## Design

### 1. Component role

`slack-canvas` is a publishing primitive for durable Slack-native artifacts.

It owns:

- canvas creation/update operations
- rendering Agency-produced long-form outputs into Canvas content

### 2. Typical use cases

- incident summary publication
- mission handoff notes
- operator briefing documents
- durable report artifacts

### 3. Generic tools

- `slack_canvas_create`
- `slack_canvas_update`
- `slack_canvas_publish_artifact`

These should operate on generic content payloads and metadata, not pack-specific document types.

### 4. Relationship to bridge

`agency-bridge-slack` may use Canvas as an optional durable-output target for conversations whose result is better represented as a document than as a thread reply.

## Security

- only the minimum Slack scopes needed for Canvas operations
- no admin-scoped authority
- all publication mediated and audited

## Open Questions

1. Should Canvas publication be explicit-only, or should bridges/packs be able to declare policy-driven auto-publication for certain output types? Initial bias: explicit or policy-driven, never implicit by default.
