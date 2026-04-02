---
description: "1. Container identity is invisible in audit logs. Containers are ephemeral — egress, enforcers, and other infra compo..."
status: "Implemented"
---

# Audit Instance Tracking & Agent Error Signals

**Status:** Implemented
**Date:** 2026-03-22
**Last updated:** 2026-04-01
**Context:** Debugging a 401 LLM failure revealed that audit logs lack container identity (making it impossible to distinguish which container instance handled a request) and that LLM errors are silently swallowed (the user sees nothing when an agent can't respond).

**Implementation notes:** Both features are implemented. Agent lifecycle IDs are stored in `agent.yaml` (`LifecycleID` field in `internal/models/agent_config.go`), stamped on audit events via `internal/logs/writer.go`. Error signals flow through the existing signal infrastructure: body runtime emits `error` signals, `_AUDITABLE_SIGNALS` in `agency_core/core/signals.py` includes `error`, the gateway WebSocket hub (`internal/ws/hub.go`) broadcasts `agent_signal_*` events with agent-scoped filtering, signal promotion runs in `internal/ws/signal_promotion.go` (with tests), and `agency log` in `internal/cli/commands.go` formats `agent_signal_error` events. The enforcer LLM handler (`images/enforcer/llm.go`) contributes error context.

## Problem

1. **Container identity is invisible in audit logs.** Containers are ephemeral — egress, enforcers, and other infra components restart, get recreated, or scale. Audit logs record events by agent name only. When diagnosing failures, there is no way to determine which container instance was active at a given time, or where one instance's lifetime ends and another begins. Agents can also be deleted and recreated with the same name, interleaving unrelated audit histories.

2. **LLM errors are silent to the user.** When the enforcer receives a 401, 502, timeout, or other error from the upstream provider, the body runtime silently fails to generate a reply. The user sees nothing in agency-web, the CLI, or any API consumer. The only trace is in the enforcer's audit JSONL, which requires manual forensics to discover.

## Design

### Feature 1: Agent Lifecycle ID + Container Instance ID

Two orthogonal identifiers for audit forensics:

**Agent lifecycle ID**
- UUID v4 generated at `agency create` time.
- Stored in the agent's `agent.yaml` as `lifecycle_id`. Requires adding a `LifecycleID string` field to the `AgentConfig` struct in `agency-gateway/internal/models/agent_config.go`.
- **Backward compatibility:** When the gateway loads an existing `agent.yaml` that has no `lifecycle_id`, it generates one and writes it back. This is a one-time migration — existing agents get a lifecycle ID on first load after this feature ships. Audit events before migration have no `lifecycle_id`; consumers must tolerate the field being absent.
- Stamped on **every** audit event for that agent (both gateway and enforcer logs).
- Survives container restarts. Does NOT survive delete + recreate — a new `agency create` generates a new lifecycle ID.
- Purpose: distinguish "henrybot9000 created March 1" from "henrybot9000 created March 22" when audit logs share the same agent name directory.
- **Injection strategy:** The Go gateway `logs.Writer` accepts a `lifecycle_id` at construction time (set when the agent is loaded) and injects it into every event automatically. Call sites do not pass it in the detail map. The Python-side `AuditLog` writer in `agency/audit/log.py` follows the same pattern — accepts lifecycle_id at init, injects on every record.

**Container instance ID**
- Docker short container ID (first 12 hex characters), sourced from the gateway's existing `ServiceMap.ContainerID`.
- Stamped on **lifecycle events only**: `start_phase`, `agent_started`, `agent_halted`, `agent_restarted`, `CONFIG_RELOAD`, `start_failed`, `restart_failed`, and infra container lifecycle events.
- NOT stamped on per-request events (LLM_DIRECT_STREAM, task_delivered, etc.) — correlate by timestamp against surrounding lifecycle events.
- Purpose: distinguish which container was running when a lifecycle transition occurred.

**Visibility:** Operators only. Neither ID is exposed to the agent boundary (ASK Tenet 1 — enforcement topology stays outside the agent's view).

**Audit log entry example (gateway lifecycle event):**
```json
{
  "timestamp": "2026-03-22T05:13:51Z",
  "source": "gateway",
  "event": "agent_started",
  "agent": "henrybot9000",
  "lifecycle_id": "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
  "instance_id": "57fdf86601bb"
}
```

**Audit log entry example (enforcer per-request event):**

Note: Gateway events use `"event"` as the type key; enforcer events use `"type"`. This is an existing convention — the gateway `Writer.Write()` sets `"event"`, while the enforcer `AuditLogger` sets `"type"`. Both are recognized by the log reader's `eventType()` function. This spec preserves the existing convention.

```json
{
  "timestamp": "2026-03-22T05:14:20Z",
  "source": "enforcer",
  "type": "LLM_DIRECT_STREAM",
  "agent": "henrybot9000",
  "lifecycle_id": "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
  "status": 401,
  "model": "claude-sonnet"
}
```

### Feature 2: Agent Error Signals

When an agent encounters an error that prevents it from completing work, it emits a structured `error` signal. This is a general-purpose error reporting mechanism — not LLM-specific. As Agency matures and agents gain connectors, tools, and other capabilities, any component can emit errors through this same signal.

The initial implementation covers LLM call failures (replacing the existing `progress_update` signal currently emitted on LLM failure in `body.py` around line 1055-1061). The old `progress_update` emission must be removed, not supplemented.

**Signal schema:** The `error` signal has a fixed envelope and a `category`-specific `data` payload. The envelope fields are always present; the data fields vary by category.

```json
{
  "signal_type": "error",
  "timestamp": "2026-03-22T05:14:20Z",
  "data": {
    "category": "<string: error category>",
    "message": "<string: human-readable summary>",
    "stage": "<string: where in the chain it failed>",
    "status": "<int: HTTP status if applicable, null otherwise>",
    ...category-specific fields
  }
}
```

**Envelope fields (always present):**

| Field | Type | Description |
|-------|------|-------------|
| `category` | string | Error domain. Namespaced by component: `llm.*`, `connector.*`, `tool.*`, `comms.*`, etc. |
| `message` | string | Human-readable summary suitable for display to operators |
| `stage` | string | Where in the processing chain the failure occurred. Category-specific values. |

**Reserved category prefixes:**

| Prefix | Component | Example categories |
|--------|-----------|-------------------|
| `llm.*` | LLM call path | `llm.call_failed`, `llm.context_exceeded` |
| `connector.*` | Connector integrations | `connector.auth_failed`, `connector.timeout` |
| `tool.*` | Agent tool execution | `tool.execution_failed`, `tool.permission_denied` |
| `comms.*` | Communication channels | `comms.delivery_failed` |
| `system.*` | Platform-level errors | `system.resource_exhausted` |

New components register their own categories under their prefix. No central registry required — the prefix convention provides namespace isolation.

**Initial category: `llm.call_failed`**

```json
{
  "signal_type": "error",
  "timestamp": "2026-03-22T05:14:20Z",
  "data": {
    "category": "llm.call_failed",
    "message": "LLM call failed: authentication rejected by provider (401)",
    "stage": "provider_auth",
    "status": 401,
    "correlation_id": "henrybot9000-idle-reply-1774156384-1",
    "model": "claude-sonnet",
    "retries_attempted": 3
  }
}
```

The `correlation_id` follows the existing body runtime format: `{agent_name}-{task_id}-{counter}`, sent as the `X-Correlation-Id` header to the enforcer. This allows cross-referencing body-side errors with enforcer-side audit entries.

The `retries_attempted` field is 0 for non-retryable errors (400-level from enforcer, bad model, etc.) where the body raises immediately without retry.

**Stage values for `llm.*` categories:**

| Stage | Meaning |
|-------|---------|
| `proxy_unreachable` | Enforcer couldn't connect to egress proxy |
| `provider_auth` | 401/403 from upstream — credential swap failed or key invalid |
| `provider_rate_limit` | 429 from upstream provider |
| `provider_error` | 5xx from upstream provider |
| `timeout` | No response within deadline |
| `request_rejected` | 400-level from enforcer (bad model, body too large, budget exceeded) |
| `response_malformed` | HTTP 200 from upstream but response body is unparseable (corrupt JSON, truncated SSE stream) |

Other categories define their own stage values. The only constraint is that `stage` is a short snake_case string that operators can filter on.

**Signal flow — changes required at each step:**

1. **Body runtime** receives HTTP error from enforcer, calls `_emit_signal("error", {...})` which appends to `agent-signals.jsonl`. *Existing infrastructure — no change needed. Replace the current `progress_update` emission with this structured `error` signal.*
2. **SignalWatcher** daemon (polls every 2s) picks up the signal, converts to `agent_signal_error` audit event. *Requires change: add `"error"` to `_AUDITABLE_SIGNALS` in `agency/core/signals.py`. Without this, the signal is silently dropped and never reaches the audit log or any downstream consumer.*
3. **Gateway WebSocket hub** broadcasts `agent_signal_*` events to subscribed clients. *Requires change: add `agent_signal_*` event type matching to the `matches()` function in `agency-gateway/internal/ws/hub.go`, routing events through the existing agent subscription filter. The current default case in `matches()` returns `true` (broadcast to all clients), which would leak signals across agent boundaries — violating ASK Tenet 24. Events must be scoped to clients subscribed to the specific agent.*
4. **REST API** serves the event as part of `GET /api/v1/agents/{name}/logs` for polling consumers. *Existing infrastructure — no change needed.*
5. **CLI** `agency log` displays it via the `formatEventDetail` function. *Requires change: add `agent_signal_error` case. Format: `{category}: {stage} ({status}) {message-truncated}`*

**WebSocket broadcast extension:** The gateway's WebSocket hub currently broadcasts comms messages only. This design extends it to also broadcast `agent_signal_*` events to clients subscribed to the corresponding agent. This benefits ALL signal types — progress updates, findings, task completion, and errors all become real-time. No new WebSocket endpoints; the existing subscription mechanism is extended with proper agent-scoped filtering.

**Consumer rendering:**
- **Agency-web:** Displays as inline system message in conversation thread.
- **CLI:** `agency log` shows: `2026-03-22T05:14:20  agent_signal_error   llm.call_failed: provider_auth (401) authentication rejected by provider`
- **API consumers:** Receive structured JSON via WebSocket subscription or REST polling. No rendering prescribed — consumers decide presentation.

### Error Classification Logic

The body runtime must map HTTP responses from the enforcer to the correct `llm.*` stage. The enforcer already returns distinct HTTP status codes and error messages:

| Enforcer response | Stage |
|-------------------|-------|
| Connection refused / dial error | `proxy_unreachable` |
| HTTP 401 or 403 from upstream | `provider_auth` |
| HTTP 429 from upstream | `provider_rate_limit` |
| HTTP 5xx from upstream | `provider_error` |
| HTTP 502 "upstream LLM error" from enforcer | `timeout` or `provider_error` (disambiguate via error message) |
| HTTP 400 from enforcer (bad model, body too large) | `request_rejected` |
| HTTP 429 from enforcer (budget exceeded) | `request_rejected` |
| HTTP 200 with unparseable body | `response_malformed` |

Future components (connectors, tools, etc.) implement their own classification logic for their category prefix, following the same pattern: map the failure to a `stage` string and a human-readable `message`.

## ASK Tenet Compliance

- **Tenet 1 (constraints external):** Instance IDs and lifecycle IDs are operator-facing only. Agents do not see them. The error signal's `stage` field describes where in the request chain the failure occurred, but the body runtime already sees the HTTP status code and error message from the enforcer — no new information leaks to the agent.
- **Tenet 2 (every action leaves a trace):** Lifecycle IDs improve trace quality by making agent incarnations distinguishable. Container instance IDs link audit events to specific container lifetimes. Error signals ensure LLM failures leave a user-visible trace, not just an audit log entry.
- **Tenet 3 (mediation complete):** No change to mediation paths. Signals flow through existing mediation (signal files, gateway audit, WebSocket hub).
- **Tenet 5 (no blind trust):** Error signals make failures visible instead of silent. Operators can see and act on credential/connectivity issues immediately.
- **Tenet 24 (knowledge access bounded by authorization):** WebSocket broadcast of `agent_signal_*` events must be scoped to clients subscribed to the specific agent. The hub's `matches()` function must route these events through agent subscription filtering, not the permissive default case.

## Out of Scope

- Automatic retry or self-healing on LLM errors (body runtime already retries at the HTTP level; this spec is about visibility, not remediation).
- Error signal aggregation or alerting thresholds (future work).
- Agent-visible error details beyond what the body runtime already sees in HTTP responses.
- Standardizing the `event` vs `type` key convention between gateway and enforcer audit logs (existing convention, works today, not worth disrupting).
