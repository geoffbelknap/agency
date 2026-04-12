# Slack Admin Connector

**Date:** 2026-04-11
**Status:** Draft
**Scope:** Defines `slack-admin`, a privileged Agency hub connector for Slack admin-scoped operations. Covers authority boundaries, likely operation classes, and separation from generic messaging/interactivity connectors. Does not define pack-specific approval or invite workflows.

## Problem

Some Slack operations are materially more privileged than normal message, modal, or command usage:

- workspace invites
- user-group operations
- other admin-scoped actions

These require stronger scopes and represent a distinct trust boundary. If mixed into ordinary Slack connectors, least privilege and audit clarity suffer.

## Goals

1. Isolate privileged Slack operations into a separate connector.
2. Keep admin-scoped authority out of generic Slack messaging/interactivity components.
3. Ensure high-authority Slack operations are explicitly mediated and auditable.
4. Give packs a clean place to depend on admin capabilities when truly required.

## Non-Goals

- No ordinary message ingress or interactivity handling.
- No generic Slack chat behavior.
- No built-in approval semantics; those remain in higher-level primitives and pack logic.

## Design

### 1. Component role

`slack-admin` is the privileged Slack admin plane.

It owns operations that require materially stronger scopes than generic Slack chat/app surfaces.

### 2. Likely operation classes

- workspace invites
- user-group lookup or membership changes where privileged scopes are needed
- admin-only workspace operations required by concrete packs

The exact tool set should stay intentionally narrow and expand only for concrete use cases.

### 3. Generic design rules

- each tool maps to a narrow admin capability
- no broad passthrough Slack admin API call tool
- every operation must be explicitly audited
- packs using this connector should generally combine it with explicit human approval mechanisms such as consent tokens when the action is consequential

### 4. Relationship to other components

- `agency-bridge-slack` should not depend on `slack-admin` by default
- packs may depend on `slack-admin` when they truly need privileged Slack authority
- `slack-interactivity` or other connectors may collect human approval, but `slack-admin` should remain the execution surface for the privileged action itself

## Security

- separate credentials/scopes from ordinary Slack components
- no agent visibility into raw tokens
- explicit mediation and audit trail for every operation

## Open Questions

1. Should user-group reads stay in `slack-admin` if some can be done with non-admin scopes? Initial bias: keep only genuinely stronger-scope operations here and leave ordinary read operations to lower-authority components where feasible.
