# Slack App Home Connector

**Date:** 2026-04-11
**Status:** Draft
**Scope:** Defines `slack-app-home`, a generic Agency hub connector for Slack App Home experiences. Covers App Home publishing, home-open handling, and Home tab rendering/update primitives. Does not define App Home content for any specific pack or bridge.

## Problem

App Home is a distinct Slack-native surface for persistent, user-specific UI. It is useful for:

- inbox views
- task summaries
- approvals dashboards
- personal status surfaces

Agency has no generic App Home primitive, so any App Home use would otherwise be embedded awkwardly into unrelated Slack connectors or packs.

## Goals

1. Provide a generic App Home connector.
2. Support Home tab publication and refresh.
3. Keep App Home separate from message ingress and interactivity.
4. Allow `agency-bridge-slack` or packs to project state into Slack App Home without reinventing the transport.

## Non-Goals

- No pack-specific Home tab layouts.
- No general chat or slash command ingress.
- No admin-scoped Slack authority.

## Design

### 1. Component role

`slack-app-home` is a rendering/publishing primitive for Slack App Home.

It owns:

- `views.publish`
- optional handling of home-open events
- App Home refresh/update primitives

### 2. `connector.yaml`

```yaml
kind: connector
name: slack-app-home
version: "1.0.0"
description: >
  Publishes and refreshes Slack App Home views for Agency-driven
  experiences such as inboxes, status panels, and approvals dashboards.

requires:
  services:
    - slack
  credentials:
    - key: slack_bot_token
      description: Slack bot token used for views.publish
```

### 3. Generic tools

- `slack_app_home_publish`
  Publish a Home tab view for a specific Slack user

- `slack_app_home_refresh`
  Re-publish the current Home tab for a specific user based on caller-provided content

These tools should accept generic view payloads, not workflow-specific content types.

### 4. Relationship to other components

- `agency-bridge-slack` may use App Home as an optional inbox/status surface
- packs may use it for dashboards or approvals summaries
- it should remain distinct from `slack-events`, even if home-open event ingestion reuses Events API underneath

## Security

- uses only the Slack scopes required for Home tab publication
- no admin-scoped operations
- mediated and audited outbound publication

## Open Questions

1. Should `app_home_opened` be routed through `slack-events` or semantically owned by this component? Initial bias: semantically owned here, even if implemented atop the same lower-level event path.
