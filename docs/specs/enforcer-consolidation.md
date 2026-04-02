## Problem

The enforcement layer is split across too many moving parts:

1. **Analysis** runs as a shared container on the mediation network. The enforcer calls it over HTTP on every LLM request (rate limiting, usage recording) and the body calls it on every tool output (XPIA scanning). These are per-request operations that pay a network round-trip for no architectural benefit — analysis holds no cross-agent state that couldn't live in-process.

2. **Network topology is fragile.** Infrastructure services (comms, knowledge, analysis) are connected to each agent's internal network via post-create `docker network connect`. These connections don't survive Docker auto-restarts. A workspace watcher re-attaches them reactively, but there's still a connectivity gap on every crash.

Both problems share a root cause: enforcement responsibilities are scattered across containers instead of consolidated in the enforcer, which is the designated mediation boundary (ASK Tenet 3).

---

## Design

### Principle

The enforcer is the agent's sole gateway to the outside world. Every request from the workspace — LLM calls, comms messages, knowledge queries, tool executions — flows through the enforcer. The enforcer is the only container that bridges the agent-internal and mediation networks.

### Change 1: Fold Analysis into Enforcer

Move analysis capabilities into the enforcer process. No shared state is needed — each capability is per-agent or stateless.

| Capability | Current Location | New Location | Notes |
|---|---|---|---|
| Rate limiting | analysis `/rate-limit/*` | Enforcer in-process | Per-provider, in-memory with TTL. Enforcer already sees response headers. |
| Usage recording | analysis `/usage` | Enforcer in-process | Enforcer already tracks local cost in `budget_tracker`. Write to audit log directly. |
| Budget enforcement | analysis `/budget-check` | Enforcer in-process | Merge with existing `budget_tracker`. Remove dual-layer tracking. |
| XPIA scanning | analysis `/scan/mcp-output` | Enforcer reverse proxy | Body calls enforcer; enforcer runs scan before returning result. Or embed scanner in body (125 lines, stateless). |
| Metrics | analysis `/metrics` | Enforcer in-process → gateway | Enforcer collects per-model stats, gateway aggregates on demand from audit logs (already does this for budget). |

**What gets deleted:**
- `images/analysis/` — entire service
- `agency-infra-analysis` container
- Analysis health check, network connections, Docker image build
- `analysis_client.go` in enforcer (replaced by in-process calls)
- Post-create network connect of analysis to agent networks

**Migration path:**
1. Move rate limiter state into enforcer (Go port of `rate_limiter.py`, ~150 lines)
2. Merge budget tracking into enforcer's existing `budget_tracker` (remove fire-and-forget HTTP calls)
3. Embed XPIA patterns in enforcer or body (125 lines of pattern matching)
4. Remove analysis container from `infra.go` startup
5. Update `ConnectInfraToAgent` to only connect comms + knowledge

### Change 2: Route Comms + Knowledge Through Enforcer

Eliminate direct workspace-to-mediation traffic. The workspace talks only to the enforcer; the enforcer proxies to comms and knowledge.

**Current flow (fragile):**
```
workspace ──(internal net)──→ comms:8080    (via post-create network connect)
workspace ──(internal net)──→ knowledge:8080 (via post-create network connect)
workspace ──(internal net)──→ enforcer:3128  (LLM only)
```

**Proposed flow (robust):**
```
workspace ──(internal net)──→ enforcer:3128  (everything)
enforcer  ──(mediation net)──→ comms:8080
enforcer  ──(mediation net)──→ knowledge:8080
enforcer  ──(mediation net)──→ egress:3128   (external APIs)
```

**Implementation:**
1. Add reverse proxy routes in enforcer for comms and knowledge endpoints
2. Set `AGENCY_COMMS_URL=http://enforcer:3128/comms` in workspace env (or use HTTP_PROXY routing)
3. Remove comms and knowledge from `ConnectInfraToAgent` — they no longer need agent-internal network access
4. `ConnectInfraToAgent` becomes empty (or removed entirely)
5. Workspace watcher no longer needs infra reconnect logic (no post-create connections to lose)

**ASK compliance:** This *improves* Tenet 3. Currently the body bypasses enforcer for comms/knowledge traffic. After this change, all traffic is mediated and auditable.

---

## Network Topology After Consolidation

```
┌─────────────────────────────────────┐
│         agent-internal network       │
│                                     │
│  ┌───────────┐                      │
│  │ workspace  │                      │
│  │ (body)     │                      │
│  └─────┬──────┘                      │
│        │                             │
│  ┌─────┴──────┐                     │
│  │ enforcer   │──────────────────────┼──┐
│  │ (mediation │                     │  │
│  │  gateway)  │                     │  │
│  └────────────┘                     │  │
└─────────────────────────────────────┘  │
                                         │
┌────────────────────────────────────────┼─┐
│         mediation network              │ │
│                                        │ │
│  ┌────────┐ ┌───────────┐ ┌─────────┐ │ │
│  │ comms   │ │ knowledge │ │ egress  │←┘ │
│  └────────┘ └───────────┘ └─────────┘    │
│                                          │
│  ┌────────┐                              │
│  │ intake  │                              │
│  └────────┘                              │
└──────────────────────────────────────────┘
```

- **Workspace**: agent-internal only. Zero mediation access. Custom seccomp profile for execution-layer confinement.
- **Enforcer**: bridges internal → mediation. Sole exit point.
- **Comms, Knowledge, Egress, Intake**: mediation only. No agent-internal access needed.
- **No post-create network stitching.** All networks assigned at container creation.

---

## What Gets Simpler

| Before | After |
|---|---|
| 6 containers per agent (workspace, enforcer, + 3 infra connections) | 2 containers per agent (workspace, enforcer) |
| `ConnectInfraToAgent` with retries on every start | Nothing — no cross-network connections needed |
| Workspace watcher re-attaches networks on crash | Workspace watcher only alerts (no reconnect needed) |
| Analysis container + health check + network | Gone |
| Body talks to 4 services directly | Body talks to enforcer only |
| Dual-layer budget tracking (enforcer + analysis) | Single budget tracker in enforcer |
| XPIA scanning via HTTP round-trip | In-process pattern match |

---

## Implementation Order

1. **Route comms + knowledge through enforcer** — ✅ Implemented 2026-03-26. Enforcer serves /mediation/ routes on port 8081. Audit logging added for Tenet 2 compliance.

2. **Fold analysis into enforcer** — ✅ Implemented 2026-03-26. In-process rate limiter, budget tracker uses existing BudgetTracker, XPIA scanning runs automatically in LLM proxy path. Analysis container removed. ConnectInfraToAgent deleted.

3. **Clean up** — ✅ Done. ConnectInfraToAgent removed, workspace watcher simplified, analysis removed from infra status/rebuild MCP tools.

---

## Risks

- **Enforcer becomes critical path** — it already is. This doesn't change the blast radius, just makes the dependency explicit.
- **Rate limiting becomes per-agent** — currently shared across agents at the analysis layer. If two agents hit the same provider, they each track limits independently. Mitigation: enforcer reads response headers directly, so provider-side limits are respected regardless.
