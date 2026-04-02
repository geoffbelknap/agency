## What This Document Covers

The design for delivering constraint changes to running agents mid-session through the Context API. Covers the full data flow from operator action through gateway, enforcer sidecar, and Body runtime — including atomic delivery, severity classification, acknowledgement verification, failure handling, and audit trail.

> **Scope:** This spec covers only mid-session constraint push. Quarantine, decommission, named policy registry, multi-agent coordination, and other unbuilt features are out of scope. This feature is the foundational delivery mechanism those features will build on.

## ASK Tenets Enforced

| Tenet | How |
|---|---|
| 1 — Constraints are external and inviolable | Gateway never talks to Body directly. Enforcer mediates all constraint delivery. Body cannot influence enforcement. |
| 2 — Every action leaves a trace | All audit events written by the component performing the action, never by the agent. |
| 6 — Constraint changes are atomic and acknowledged | Pointer swap in enforcer memory — Body sees old or new, never partial. Ack is hash-verified by enforcer. |
| 7 — Constraint history is immutable and complete | Append-only constraint version log in gateway. Every version hash and timestamp preserved. |
| 9 — Halt authority is asymmetric | Auto-halt on unacked CRITICAL is enforcer-initiated. Agent cannot self-resume. |

## Design Decisions

| Decision | Choice | Rationale |
|---|---|---|
| Delivery mechanism | WebSocket push (no polling) | Polling wastes resources and masks failures. WebSocket gives real-time delivery. If connection drops, operator is alerted immediately. |
| Constraint state authority | Enforcer sidecar holds authoritative state | Natural extension of existing mediation role. Atomicity is trivial (pointer swap). Keeps enforcement outside agent boundary (Tenet 1). |
| Severity classification | Auto-classify with operator escalation only | Common path is hands-free. Operator can escalate but never downgrade. Aligns with asymmetric authority (Tenet 9). |
| Ack timeouts | Tiered by severity | CRITICAL unacked for 60s is a very different situation than LOW unacked for 60s. Higher severity = shorter timeout. |
| Acknowledgement model | Body acks, enforcer verifies | ASK requires runtime-level ack (not LLM-level) that is verifiable by enforcement layer. Body hashes constraints, enforcer independently verifies the hash. |

---

## Part 1: Data Flow

```
Operator (CLI/API)
    │
    │  POST /api/v1/agents/{name}/context/push
    │  (new constraints + optional severity override)
    ▼
┌─────────────────────────────────────────────┐
│  Gateway                                     │
│                                              │
│  1. Validate new constraint set              │
│  2. Compute delta from current sealed state  │
│  3. Auto-classify severity from change type  │
│  4. Apply operator escalation if provided    │
│  5. Sign & seal new constraint set           │
│  6. Push via WebSocket to enforcer           │
│  7. Start ack timeout timer (by severity)    │
│  8. Audit log: constraint_change_initiated   │
└──────────────┬──────────────────────────────┘
               │  WebSocket (mediation network)
               ▼
┌─────────────────────────────────────────────┐
│  Enforcer (per-agent sidecar)                │
│                                              │
│  1. Receive new constraint set               │
│  2. Validate signature                       │
│  3. Atomic swap: replace constraint state    │
│  4. Notify Body via local HTTP signal        │
│  5. Wait for Body ack (hash)                 │
│  6. Verify hash matches delivered set        │
│  7. Report ack (or timeout) to gateway       │
│  8. Audit log: constraint_change_delivered   │
└──────────────┬──────────────────────────────┘
               │  Local HTTP (agent-internal network)
               ▼
┌─────────────────────────────────────────────┐
│  Body Runtime (agent container)              │
│                                              │
│  1. Receive constraint-change notification   │
│  2. Fetch new constraints from enforcer      │
│     GET http://enforcer:8080/constraints     │
│  3. Apply to active session                  │
│  4. Compute SHA-256 hash of constraint state │
│  5. POST hash to enforcer ack endpoint       │
│     POST http://enforcer:8080/constraints/ack│
│  6. Behave per severity (continue/pause/stop)│
└─────────────────────────────────────────────┘
```

**Key properties:**

- Gateway never talks to Body directly (Tenet 1).
- Enforcer holds authoritative constraint state, not the Body.
- Atomicity = pointer swap in enforcer memory — Body sees old or new, never partial (Tenet 6).
- Every step audit-logged by the component performing it, not by the agent (Tenet 2).

---

## Part 2: Gateway Context API Endpoints

Six new REST endpoints under `/api/v1/agents/{name}/context/`:

| Endpoint | Method | Purpose | Called by |
|---|---|---|---|
| `/context/constraints` | GET | Current effective constraint set for agent | Operator, tooling |
| `/context/exceptions` | GET | Active exceptions with expiry timestamps | Operator, tooling |
| `/context/policy` | GET | Effective policy with full inheritance chain | Operator, tooling |
| `/context/changes` | GET | Change history (delta log since agent start) | Operator, tooling |
| `/context/push` | POST | Push new constraint set to running agent | Operator (CLI/API) |
| `/context/status` | GET | Current delivery state (pending/acked/timeout) | Operator, tooling |

The first four are read-only query endpoints exposing what the agent is currently operating under. They replace the spec's `agency.context.*()` functions as REST equivalents.

### Push endpoint

`POST /api/v1/agents/{name}/context/push`

Request body:

```json
{
  "constraints": { ... },
  "severity_override": "HIGH",
  "reason": "security finding"
}
```

- `constraints` — required. Full constraint set (not a delta). Gateway computes the delta internally for classification and audit.
- `severity_override` — optional. Can only escalate above auto-classified severity, never downgrade.
- `reason` — required. Free text, goes into audit trail.

Response (202 Accepted):

```json
{
  "change_id": "chg_a1b2c3",
  "version": 3,
  "severity": "HIGH",
  "status": "pending",
  "ack_timeout_seconds": 10
}
```

### Status endpoint

`GET /api/v1/agents/{name}/context/status`

Response:

```json
{
  "change_id": "chg_a1b2c3",
  "version": 3,
  "severity": "HIGH",
  "status": "acked",
  "pushed_at": "2026-03-21T14:30:00Z",
  "acked_at": "2026-03-21T14:30:02Z"
}
```

Status values: `pending`, `acked`, `timeout`, `hash_mismatch`, `halted`.

**No endpoint for the Body to call on the gateway.** The Body only talks to the enforcer. The enforcer talks to the gateway over WebSocket.

---

## Part 3: Enforcer Constraint Endpoints

Two new local HTTP endpoints on the enforcer sidecar, reachable only from the agent-internal network.

### GET /constraints

Returns the current effective constraint set.

```json
{
  "version": 3,
  "hash": "a1b2c3...",
  "severity": "MEDIUM",
  "constraints": { ... },
  "sealed_at": "2026-03-21T14:30:00Z"
}
```

`version` is a monotonically increasing integer per agent session. Body uses this to detect whether it's behind. `hash` is what the Body must echo back in its ack.

### POST /constraints/ack

Body submits its computed hash to acknowledge receipt.

```json
{
  "version": 3,
  "hash": "a1b2c3..."
}
```

Enforcer verifies: does the hash match what was delivered at that version? If yes, ack is valid. If hash doesn't match, enforcer treats it as a failed ack — same escalation path as timeout.

### Notification mechanism

When the enforcer swaps in new constraints, it sends a lightweight HTTP request to the Body runtime at a well-known local endpoint:

`POST http://body:8090/hooks/constraint-change`

```json
{
  "version": 3,
  "severity": "MEDIUM"
}
```

This is just a signal — enough for the Body to know it needs to fetch and ack. No constraint data inline. Body must fetch from enforcer to get the real data, ensuring the enforcer is the single source of truth.

### Startup compatibility

On first boot, the enforcer seeds its constraint state from the mounted `constraints.yaml` (the existing Phase 3 path). The `GET /constraints` endpoint works identically whether constraints arrived at startup or via mid-session push. Body runtime doesn't need to know which path delivered them.

---

## Part 4: Severity Classification

### Auto-classification rules

| Change type | Default severity | Rationale |
|---|---|---|
| Exception grant | LOW | Expanding access, no disruption needed |
| Policy loosening | LOW | Less restrictive, agent continues normally |
| Policy tightening | MEDIUM | Agent may need to adjust behavior |
| Capability revocation | HIGH | Agent loses access to something it may be using |
| Exception revocation | HIGH | Previously granted exception withdrawn |
| Operator emergency push | CRITICAL | Operator explicitly flagged as emergency |

Operator can escalate any classification but never downgrade. Attempting to downgrade returns 400 with an explanation.

### Ack timeout tiers

| Severity | Ack timeout | On timeout |
|---|---|---|
| LOW | 60s | Alert operator |
| MEDIUM | 30s | Alert operator |
| HIGH | 10s | Alert operator, recommend halt |
| LOW/MEDIUM/HIGH | 2x timeout | Auto-halt (enforcer-initiated) |
| CRITICAL | 5s | Alert operator |
| CRITICAL | 15s | Auto-halt |

Two-stage for LOW/MEDIUM/HIGH: first alert, then auto-halt if still no ack at double the timeout. CRITICAL gets a tighter window — 5s alert, 15s halt.

### Expected agent behavior after ack

| Severity | Expected behavior |
|---|---|
| LOW | Continue current task, operate under new constraints |
| MEDIUM | Pause current task at next safe point, apply, resume |
| HIGH | Stop current task, apply, await operator instruction |
| CRITICAL | Immediate stop — enforcer halts agent if Body doesn't stop itself within 5s of ack |

For CRITICAL, the enforcer doesn't trust the Body to stop on its own. If the Body acks but doesn't cease activity within 5s, the enforcer initiates a halt anyway.

---

## Part 5: WebSocket Protocol

### Connection topology

The gateway-to-enforcer WebSocket is a **new outbound connection** — the gateway initiates a WebSocket client connection to each enforcer sidecar on the mediation network. This is separate from the existing client-facing `/ws` hub (which broadcasts to CLI/web consumers). Each running agent has one dedicated gateway → enforcer WebSocket.

### Gateway → Enforcer: constraint push

```json
{
  "type": "constraint_push",
  "agent": "dev-assistant",
  "change_id": "chg_a1b2c3",
  "version": 3,
  "severity": "MEDIUM",
  "constraints": { ... },
  "hash": "a1b2c3...",
  "reason": "policy tightening per security review",
  "timestamp": "2026-03-21T14:30:00Z"
}
```

### Enforcer → Gateway: ack report

```json
{
  "type": "constraint_ack",
  "agent": "dev-assistant",
  "change_id": "chg_a1b2c3",
  "version": 3,
  "status": "acked",
  "body_hash": "a1b2c3...",
  "timestamp": "2026-03-21T14:30:02Z"
}
```

Status values: `acked`, `timeout`, `hash_mismatch`.

### Connection failure handling

1. **Enforcer detects disconnect** — starts reconnect with exponential backoff (1s, 2s, 4s, max 30s).
2. **Gateway detects disconnect** — immediately alerts operator: "Enforcer for agent {name} unreachable — constraint delivery unavailable."
3. **During disconnection** — agent continues operating under last-known constraints. No constraint pushes are possible. Gateway queues any pending pushes.
4. **On reconnect** — gateway replays any queued pushes in order. Enforcer processes them sequentially, same ack flow.
5. **If disconnected > 5 minutes** — gateway escalates to operator with option to halt the agent. Running an agent that can't receive constraint updates is a policy decision for the operator, not an automatic action.

**No silent failures.** Every state transition (connected → disconnected → reconnecting → reconnected) is audit-logged and the operator is notified.

---

## Part 6: Audit Trail

Every step produces a structured audit event written by the component performing the action (Tenet 2).

### Event types

| Event | Written by | Key fields |
|---|---|---|
| `constraint_change_requested` | Gateway | change_id, agent, initiator, severity, reason |
| `constraint_change_classified` | Gateway | change_id, auto_severity, operator_override, final_severity |
| `constraint_change_sealed` | Gateway | change_id, version, hash |
| `constraint_change_pushed` | Gateway | change_id, agent, enforcer_delivery_time |
| `constraint_change_delivered` | Enforcer | change_id, version, old_hash, new_hash |
| `constraint_change_notified` | Enforcer | change_id, body_notification_time |
| `constraint_ack_received` | Enforcer | change_id, version, body_hash, match (bool) |
| `constraint_ack_timeout` | Enforcer | change_id, version, timeout_seconds, severity |
| `constraint_ack_verified` | Enforcer | change_id, version, verified (bool) |
| `agent_halted_unacked` | Enforcer | change_id, agent, reason |
| `enforcer_ws_disconnected` | Gateway | agent, timestamp, last_known_state |
| `enforcer_ws_reconnected` | Gateway | agent, timestamp, queued_pushes_count |

### Constraint history (Tenet 7)

The gateway maintains an append-only log of every constraint version an agent has operated under. Each entry includes:

- Version number
- SHA-256 hash of constraint set
- Sealed timestamp
- Ack timestamp (or timeout/failure indicator)
- Initiator (who requested the change)
- Severity

This log is queryable via `GET /context/changes` and is never modified — only appended.

---

## Out of Scope

These features are designed elsewhere but not part of this spec:

- **Quarantine** (agent-lifecycle.md Part 5) — uses constraint push mechanism but adds process termination and network severance.
- **Decommission** (agent-lifecycle.md Part 6) — permanent termination, not a constraint change.
- **Named policy registry** (policy-framework.md Part 4) — feeds into what constraints get pushed, but the registry itself is separate.
- **Two-key exception model** (policy-framework.md Part 5) — affects exception grant/revocation severity, but the validation logic is separate.
- **Exception lifecycle** (agent-lifecycle.md Part 3) — expiry warnings and auto-revocation will use this push mechanism but are a separate feature.
- **Trust changes** — always take effect at next session start per ASK, never mid-session. Not delivered via this mechanism.

---

## Identified Unbuilt Features (Inventory)

For reference, the full list of designed-but-unbuilt features discovered during this design:

| Feature | Spec location | Status |
|---|---|---|
| Mid-session constraint push (this spec) | agent-lifecycle.md Part 3 | This document |
| Quarantine | agent-lifecycle.md Part 5 | Not implemented |
| Decommission | agent-lifecycle.md Part 6 | Not implemented |
| Named policy registry | policy-framework.md Part 4 | Not implemented |
| Two-key exception validation | policy-framework.md Part 5 | Partial (format only) |
| Redelegation | policy-framework.md Part 6 | Not implemented |
| Exception routing | policy-framework.md Part 7 | Not implemented |
| Multi-agent coordination | coordination.md | Not implemented (v2) |
| Team model v2 | team-model.md | Not implemented |
| Capability marketplace | capability-registry.md | Not implemented |
| Slack Events API | roadmap.md | Not implemented |
| Human chat UI | roadmap.md v4 | Not implemented |
| Platform bridging | roadmap.md v4 | Not implemented |
| File parsing: compliance.yaml, roles.yaml, departments/, teams/, functions/ | agency-platform.md | Not implemented |
| Python-to-Go model port | python-to-go-port-design.md | In progress (Phase 0) |
