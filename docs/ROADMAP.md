# Agency Roadmap

Last updated: 2026-04-01

## Not Yet Implemented

Features with design specs that haven't been built.

### Batch LLM Routing
**Spec:** `specs/routing/batch-llm-routing.md`
**Priority:** High (cost savings)

50% cost reduction for non-latency-sensitive LLM calls by routing through
provider batch APIs (Anthropic Message Batches, OpenAI Batch API). The
gateway classifies calls as interactive or batch-eligible (memory capture,
consolidation, evaluation) and routes accordingly.

**Dependencies:** None — additive feature on the gateway's internal LLM endpoint.

### MCP OAuth + Remote Transports
**Spec:** `specs/mcp/mcp-oauth.md`
**Priority:** Medium

OAuth 2.1 authentication for MCP servers, enabling remote MCP transports
(SSE, Streamable HTTP) instead of stdio-only. Required for cloud-hosted
MCP servers.

**Dependencies:** MCP spec evolution (upstream).

### Multi-Agent Coordination
**Spec:** `specs/coordination.md`
**Priority:** Future (v2)

Structured coordination between agents: task delegation, work splitting,
conflict resolution, shared context. Agents currently communicate via
channels but don't formally coordinate.

**Dependencies:** Principal model, team model.

---

## Partially Implemented — Visibility & Tooling

Enforcement works. These are operator-facing visibility and convenience
features.

### ~~Doctor Scope Audit~~ — DONE
Implemented: `agency admin doctor` shows per-agent scope declarations
(required + optional) and flags unscoped agents. Hub presets updated
with scope declarations.

Remaining: `agency grant --dry-run` to preview scope effects before applying.

### Trajectory Enhancements
**Priority:** Low
**Status:** Trajectory REST endpoint implemented (`GET /agents/{name}/trajectory`).
3 of 5 detectors working (repetition, cycle, error cascade). Missing:
progress stall, budget velocity, auto-halt on critical.

### ~~Mission Evaluations Endpoint~~ — DONE
Implemented: `GET /missions/{name}/evaluations`

### ~~Ontology Candidate Management~~ — DONE
Implemented: `GET /ontology/candidates`, `POST /ontology/promote`,
`POST /ontology/reject`

### ~~Trajectory REST Endpoint~~ — DONE
Implemented: `GET /agents/{name}/trajectory`

---

## Partially Implemented — Correctly Deferred

These are designed but intentionally deferred to v2 or later. The current
implementation covers v1 needs.

### Agent Quarantine & Decommission
**Spec:** `specs/runtime/agent-lifecycle.md`
**Priority:** v2

Two agent states not implemented: QUARANTINED (silent isolation on suspected
compromise) and DECOMMISSIONED (permanent removal with forensic preservation).
The halt model (supervised/immediate/emergency) covers all current use cases.

### Multi-Principal Authorization
**Spec:** `specs/policy/principal-model.md`
**Priority:** v2

Only the operator principal exists. Agent and team principals, trust
evolution, coverage chains, and halt authority monitoring are designed
but correctly deferred — single-operator model is sufficient for v1.

### Credential Hot Rotation (WebSocket Push)
**Spec:** `specs/security/credential-architecture.md` (Phase 2)
**Priority:** Low

Zero-downtime key rotation via WebSocket push from gateway to egress.
Currently rotation requires `agency infra reload` (SIGHUP). The socket
resolver is implemented — only the push notification is missing.

### Credential Export/Import + Cloud Backends
**Spec:** `specs/security/credential-architecture.md` (Phase 3)
**Priority:** Low

`agency creds export/import` for disaster recovery. Vault, AWS, Azure,
GCP backends via the SecretBackend interface. The file backend works
for single-host deployments.

### Body-Side Constraint Processing
**Spec:** `specs/missions/mid-session-constraint-push.md`
**Priority:** Medium

The gateway and enforcer constraint push infrastructure is 100% complete
(push API, severity classification, ack tracking, hash verification). The
body runtime has the hook endpoint but doesn't process constraint changes
mid-session. Operators bounce agents to apply changes.

### Auto-Generated Service Tools
**Spec:** `specs/connector/third-party-tool-integration.md`
**Priority:** Low

Tier 2 (auto-generate service tools from OpenAPI specs) and Tier 3 (vendor
manifests shipped as `agency.yaml`). Tier 1 (workspace pip/apt/env) is
complete. Operators write service YAML by hand today.

---

## Security Gaps (from Threat Model Review)

### Cross-Session MCP Tool Definition Tracking
**Priority:** Medium
**Threat:** `MCP tool definition tampering` (ASK Threat Catalog)

ToolTracker in the enforcer detects tool definition mutations within a
session, but not between sessions. An MCP server that changes its tool
definitions after the operator approved it is a supply chain risk — the
tool's contract changes silently without version bump.

**Fix:** Hash tool definitions at approval time, verify on session start.
Alert operator if definitions have changed since last approval.

### Infrastructure Service Authentication
**Priority:** Low (v2 for swarm)
**Threat:** `Unauthenticated internal services` (ASK Threat Catalog)

Comms, knowledge, and intake rely on Docker network isolation, not
per-request auth. Acceptable for single-host but a defense-in-depth
gap. If the mediation boundary is breached, everything inside is open.

**Fix:** Add service-to-service auth tokens for mediation network calls.
Only needed for multi-host (swarm) deployments.

---

## Summary

| Feature | Priority | Blocked by | Status |
|---------|----------|------------|--------|
| Batch LLM routing | High | Nothing | Not started |
| MCP tool definition tracking | Medium | Nothing | Not started |
| Body constraint processing | Medium | Nothing | Infra done, body integration pending |
| MCP OAuth | Medium | MCP spec | Not started |
| ~~Doctor scope audit~~ | ~~Medium~~ | | **Done** |
| ~~Trajectory endpoint~~ | ~~Low~~ | | **Done** |
| ~~Mission evaluations~~ | ~~Low~~ | | **Done** |
| ~~Ontology management~~ | ~~Low~~ | | **Done** |
| `agency grant --dry-run` | Low | Nothing | Not started |
| Credential hot rotation | Low | Nothing | Socket done, push pending |
| Credential export/import | Low | Nothing | Not started |
| Auto-generated service tools | Low | Nothing | Not started |
| Infra service auth | Low (v2) | Nothing | Not started |
| Agent quarantine | v2 | Nothing | Designed |
| Multi-principal auth | v2 | Nothing | Designed |
| Credential cloud backends | v2 | Nothing | Interface ready |
| Multi-agent coordination | v2 | Principal model | Designed |
