# Mediation Network Hardening

## Status: Approved

## Problem

The `agency-mediation` network is flat — all infrastructure containers (egress, comms, knowledge, intake, embeddings, web-fetch) and all enforcers share a single Docker network. If an enforcer is compromised (the bridge between an untrusted agent workspace and mediation), the attacker gains access to every infra service. Additionally, infra services talk directly to each other (intake→knowledge, knowledge→comms), creating a cross-service dependency tangle that makes network segmentation impractical without multi-homing every container.

This spec addresses both problems: eliminate cross-service dependencies by routing all inter-service communication through the gateway, then simplify the network topology to match.

### Threat Tiers

| Tier | Escape Type | Current Exposure | Realistic? |
|------|-------------|-----------------|------------|
| 1 | Container escape (workspace → agent-internal network) | Enforcer only. Correct by design. | Yes — sandbox escapes happen |
| 2 | Enforcer compromise (→ mediation network) | All infra services, gateway via socket proxy | Plausible if enforcer HTTP proxy has a vulnerability |
| 3 | Kernel-level escape (→ host) | Everything: credentials store + key, Docker socket, all files | Rare — Docker/kernel CVE required |

Tier 1 is already well-contained (ASK Tenet 3). This spec focuses on tier 2 hardening.

**Tier 3 is an accepted risk.** A kernel-level Docker escape gives the attacker full host access — at that point no application-level mitigation is meaningful. We apply defense-in-depth at the container boundary (seccomp profiles, no-new-privileges, CAP_DROP ALL) to reduce the probability of tier 3, but we do not attempt to survive it.

## Current Architecture

```
workspace ──[agent-internal]──> enforcer ──[agency-mediation]──> egress
                                                             ──> comms
                                                             ──> knowledge ──> embeddings
                                                             ──> intake
                                                             ──> web-fetch
                                                             ──> gateway-proxy
```

All services share the flat `agency-mediation` network. Cross-service dependencies:

| Source | Destination | Mechanism | Purpose |
|--------|------------|-----------|---------|
| Intake | knowledge:8080 | Direct HTTP | Graph ingest from connectors |
| Intake | comms:8080 | Direct HTTP | Work item event notifications |
| Intake | egress:3128 | HTTPS_PROXY | Connector polling (external APIs) |
| Knowledge | comms:8080 | Direct HTTP | Curator notifications |
| Knowledge | egress:3128 | HTTPS_PROXY | External embedding APIs |
| Knowledge | embeddings:11434 | Direct HTTP | Local Ollama vector generation |
| Knowledge | gateway:8200 | HTTP via proxy | Internal LLM endpoint |
| Web-fetch | egress:3128 | HTTPS_PROXY | Page fetching |

These cross-service dependencies prevent meaningful network segmentation — splitting the mediation network just results in multi-homing every container onto every sub-network.

## Design: Hub-and-Spoke Through Gateway

### Principle

Every service talks to the gateway. No service talks to another service directly. The gateway is the hub for all inter-service communication.

This eliminates cross-service dependencies: each service only needs DNS resolution for `gateway` and (where needed) `egress`. The network topology simplifies to match.

### Network Topology

```
                         ┌──────────┐
                         │ Gateway  │
                         │(host)    │
                         └────┬─────┘
                              │ gateway.sock
                       ┌──────┴──────┐
                       │gateway-proxy│
                       └──────┬──────┘
                              │
           ┌──────[agency-gateway]──────────┐
           │      │      │      │      │    │
        ┌──┴──┐┌──┴──┐┌──┴──┐┌──┴──┐┌──┴──┐│
        │comms││know-││in-  ││web- ││emb- ││
        │     ││ledge││take ││fetch││edd- ││
        └─────┘└─────┘└─────┘└──┬──┘└─────┘│
                                │          │
           ┌────[agency-egress-int]─────────┤
           │                               │
        ┌──┴──┐                            │
        │egres├──[egress-net]──> internet   │
        └─────┘                            │
                                           │
     ┌────[agency-alice-internal]──────┐    │
     │                                │    │
  ┌──┴──────┐    ┌──────────┐         │    │
  │workspace│───>│ enforcer │─────────┼────┘
  └─────────┘    └──────────┘         │
```

### Networks

| Network | Internal? | Containers | Purpose |
|---------|-----------|-----------|---------|
| `agency-gateway` | yes | gateway-proxy (alias `gateway`), comms, knowledge, intake, web-fetch, embeddings, all enforcers | Hub — gateway access for all services and agents |
| `agency-egress-int` | yes | egress, knowledge, intake, web-fetch | Services reach the egress proxy container |
| `agency-egress-ext` | no | egress | Egress proxy reaches the internet (unchanged, renamed from `agency-egress-net`) |
| `agency-{name}-internal` | yes | workspace, enforcer | Per-agent isolation (unchanged) |
| `agency-operator` | no | web, relay | Operator tools (unchanged) |

**Total: 4 fixed networks + 1 per agent + 1 operator.**

Networks removed: `agency-mediation` (replaced by gateway + egress), `agency-internal` (unused today, dead code).

### Inter-Service Communication Changes

All direct service-to-service HTTP calls are replaced with gateway-routed equivalents.

**Intake changes:**

| Current | Proposed |
|---------|----------|
| `http://knowledge:8080` for graph ingest | `http://gateway:8200/api/v1/graph/ingest` |
| `http://comms:8080` for work item events | Gateway event bus (intake emits events, gateway routes to subscribers) |

Intake already has `HTTPS_PROXY=http://egress:3128` for connector polling — unchanged, resolved via `agency-egress-int` network.

**Knowledge changes:**

| Current | Proposed |
|---------|----------|
| `http://comms:8080` for curator notifications | Gateway event bus (knowledge emits events, gateway routes) |
| `http://gateway:8200` for internal LLM | Unchanged — already goes through gateway |

Knowledge reaches embeddings at `http://embeddings:11434` — unchanged, both are on `agency-gateway`. Knowledge reaches egress at `http://egress:3128` for external embedding APIs — unchanged, resolved via `agency-egress-int` network.

**Web-fetch changes:**

None. Web-fetch reaches egress via `agency-egress-int` network. No other service dependencies.

**Comms changes:**

None. Comms has no outbound service dependencies today.

### Enforcer Network Connections

Enforcers connect to:
- `agency-{name}-internal` — primary network (workspace communication)
- `agency-gateway` — gateway access (signals, budget, health) + service access (comms, knowledge, web-fetch via mediation proxy)
- `agency-egress-int` — egress access (LLM proxy)

All enforcers get the same networks. Capability-based restriction happens at the enforcer proxy level (mediation routes), not the network level. An enforcer without web-fetch capability won't proxy traffic to web-fetch even though it can resolve the hostname.

### Knowledge Removed from Agent-Internal Networks

The current `connectToAgentNetworks` function connects knowledge to every `agency-{name}-internal` network. This is removed.

**Rationale:** The enforcer already mediates all workspace→knowledge traffic through `/mediation/knowledge/*` on the constraint port. Having knowledge directly reachable on agent-internal is a second, unmediated path that violates ASK Tenet 3. The enforcer reaches knowledge via `agency-gateway`.

The `connectToAgentNetworks` function is deleted — knowledge was its only caller.

### Embeddings as Knowledge Implementation Detail

Embeddings (Ollama) remains a separate container for resource isolation (3 GB memory budget, GPU potential). It is not an independent service — knowledge is its only consumer. Embeddings joins `agency-gateway` with hostname `embeddings`. No dedicated network.

### ExtraHosts Cleanup

Gateway-proxy and egress currently have `ExtraHosts: []string{"gateway:host-gateway"}`. These are vestiges from before the gateway socket proxy. With `agency-gateway` network and DNS alias `gateway`, they are redundant and removed.

### Gateway Socket Caller Validation

Defense-in-depth on the socket API surface. Add `X-Agency-Caller` header validation middleware to both socket routers.

**Proxy-safe socket (via gateway-proxy TCP):**

| Endpoint | Allowed Callers |
|----------|----------------|
| `GET /api/v1/health` | any |
| `POST /api/v1/infra/internal/llm` | enforcer, knowledge |
| `POST /api/v1/agents/{name}/signal` | enforcer |
| `POST /api/v1/comms/channels/*` | comms |
| `GET /api/v1/infra/status` | any |
| `POST /api/v1/graph/ingest` | intake |

**Credential socket (direct mount):**

| Endpoint | Allowed Callers |
|----------|----------------|
| `GET /api/v1/creds/internal/resolve` | egress |

Containers set `X-Agency-Caller` to their component name. The middleware rejects requests from unrecognized callers. This is not authentication (a compromised container can spoof the header) — it's defense-in-depth that prevents accidental cross-service access and makes the expected call graph explicit.

### Docker Socket Audit

Add a reconciliation check at gateway startup that inspects all `agency.managed=true` containers and flags any with `/var/run/docker.sock` in their bind mounts. Emit a platform alert if found.

## Blast Radius Analysis

**Before (flat mediation):**
- Compromised enforcer → can reach egress, comms, knowledge, intake, embeddings, web-fetch, gateway
- Container escape past enforcer → can reach knowledge (on agent-internal), enforcer, gateway

**After (hub-and-spoke):**
- Compromised enforcer → can reach gateway, comms, knowledge, web-fetch, embeddings (via `agency-gateway`) + egress (via `agency-egress-int`). Service-level access unchanged, but enforcer mediation routes restrict what it will actually proxy.
- Container escape past enforcer → can reach enforcer and gateway only. Cannot reach knowledge (removed from agent-internal). Cannot reach comms, egress, web-fetch, intake, embeddings.

**Primary security win:** Removing knowledge from agent-internal eliminates the unmediated workspace→knowledge path. A workspace container escape no longer grants direct knowledge graph access.

**Secondary win:** The hub-and-spoke model makes the communication graph explicit and auditable. All inter-service traffic flows through gateway endpoints with standard logging. Direct HTTP calls between services are eliminated.

**Future win:** The simplified topology makes service-to-service authentication straightforward to add. Each service authenticates to the gateway with a scoped token. The gateway validates caller identity per endpoint. This upgrades the `X-Agency-Caller` header from defense-in-depth to real authentication.

## Scaling Properties

**Adding an agent:** Create one internal network + start workspace + start enforcer + connect enforcer to gateway + egress. No changes to service networks, no fan-out connections.

**Adding a service:** Put it on `agency-gateway` (+ `agency-egress-int` if it needs outbound). Every enforcer that needs it adds a mediation route. No new networks.

**Docker network budget:** 4 fixed + 1 per agent + 1 operator. Docker default limit is ~30 networks, supporting ~25 concurrent agents per host.

**Gateway as bottleneck:** Inter-service traffic is low-volume (curator notifications, graph ingests, work item events). The gateway already handles all agent management, WebSocket hub, and LLM routing. Adding inter-service relay is negligible load.

## Implementation Sequence

### Phase 1: Decouple Inter-Service Communication

Route all direct service-to-service HTTP calls through the gateway. No network changes — services still share `agency-mediation`. This is independently deployable and testable.

1. Add gateway relay endpoints for intake→knowledge graph ingest (gateway already has `POST /api/v1/graph/ingest`)
2. Route intake graph ingest calls through gateway instead of direct HTTP to knowledge
3. Route intake work item notifications through gateway event bus instead of direct HTTP to comms
4. Route knowledge curator notifications through gateway event bus instead of direct HTTP to comms
5. Remove `KNOWLEDGE_URL`, `COMMS_URL` environment variables from intake and knowledge containers
6. Update `NO_PROXY` to remove cross-service hostnames

### Phase 2: Network Topology Swap

Once no service-to-service direct calls remain:

7. Create `agency-gateway` network (internal), migrate gateway-proxy with alias `gateway`
8. Create `agency-egress-int` network (internal), connect egress + knowledge + intake + web-fetch
9. Move all infra services from `agency-mediation` to `agency-gateway`
10. Update enforcer startup: connect to `agency-gateway` + `agency-egress-int` instead of `agency-mediation`
11. Remove `connectToAgentNetworks` for knowledge
12. Remove ExtraHosts from gateway-proxy and egress
13. Remove `agency-mediation` and `agency-internal` network creation
14. Update `ensureNetworks` constants

### Phase 3: Socket Hardening

15. Add `X-Agency-Caller` middleware to socket routers
16. Add Docker socket audit check to gateway startup reconciliation

### Migration Strategy

New network topology applies on next `agency start <agent>` or `agency infra up`. Running agents continue on old networks until restarted. `ensureNetworks` creates new networks immediately but only removes `agency-mediation` once no running containers reference it.

## ASK Tenets Addressed

- **Tenet 3 (Mediation is complete):** Knowledge removed from agent-internal eliminates unmediated workspace→knowledge path. All inter-service traffic routed through gateway (audited mediation point).
- **Tenet 4 (Least privilege):** Services only connect to networks they need. Enforcer capability restrictions at proxy level prevent unused service access.
- **Tenet 7 (Constraint history):** Network topology changes logged at gateway startup. Inter-service calls through gateway inherit standard audit logging.
- **Tenet 16 (Quarantine):** Hub-and-spoke enables surgical quarantine — disconnect an agent's enforcer from `agency-gateway` to sever all service access without affecting other agents.
