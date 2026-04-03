# Mediation Network Hardening

## Status: Proposed

## Problem

The `agency-mediation` network is flat — all infrastructure containers (egress, comms, knowledge, intake, embeddings, web-fetch) share a single Docker network. If an enforcer is compromised (the bridge between an untrusted agent workspace and mediation), the attacker gains access to every infra service.

This spec addresses hardening at three threat tiers:

| Tier | Escape Type | Current Exposure | Realistic? |
|------|-------------|-----------------|------------|
| 1 | Container escape (workspace → agent-internal network) | Enforcer only. Correct by design. | Yes — sandbox escapes happen |
| 2 | Enforcer compromise (→ mediation network) | All infra services, gateway via host.docker.internal | Plausible if enforcer HTTP proxy has a vulnerability |
| 3 | Kernel-level escape (→ host) | Everything: credentials store + key, Docker socket, all files | Rare — Docker/kernel CVE required |

Tier 1 is already well-contained (ASK Tenet 3). This spec focuses on tier 2 hardening.

**Tier 3 is an accepted risk.** A kernel-level Docker escape gives the attacker full host access — at that point no application-level mitigation is meaningful. Credential key separation, filesystem hardening, and similar measures provide negligible value when the attacker owns the kernel. This is a container runtime vulnerability, not an application architecture gap. We apply defense-in-depth at the container boundary (seccomp profiles, no-new-privileges, CAP_DROP ALL, user namespaces where supported) to reduce the probability of tier 3, but we do not attempt to survive it.

## Current Architecture

```
workspace ──[agent-internal]──> enforcer ──[mediation]──> egress
                                                      ──> comms
                                                      ──> knowledge
                                                      ──> intake
                                                      ──> embeddings
                                                      ──> web-fetch
```

The enforcer is the sole bridge. Workspaces cannot reach mediation. This is correct. The problem is that mediation itself is unsegmented — a compromised enforcer sees everything.

## Proposed Changes

### 1. Segment the Mediation Network

Replace the single `agency-mediation` network with purpose-specific networks. Each enforcer connects only to the networks its agent needs.

```
agency-egress-mediation     ← enforcer, egress
agency-comms-mediation      ← enforcer, comms
agency-knowledge-mediation  ← enforcer, knowledge, embeddings
agency-intake-mediation     ← intake (no enforcer — intake receives webhooks from gateway)
agency-web-fetch-mediation  ← enforcer, web-fetch
```

**Impact:** A compromised enforcer that only uses LLM routing (egress) cannot read comms channels or write to the knowledge graph. Blast radius drops from "all infra" to "services the agent actually uses."

**Enforcer connection logic:** The enforcer's mediation routes already know which upstream service handles each path:
- `/mediation/comms/*` → comms container
- `/mediation/knowledge/*` → knowledge container  
- `/mediation/web-fetch/*` → web-fetch container
- LLM proxy → egress container

Connect the enforcer to only the networks for services the agent has granted capabilities for. An agent without the `web-fetch` capability doesn't get connected to `agency-web-fetch-mediation`.

**Trade-off:** More Docker networks (currently 4 → up to 8). Docker has a default limit of ~30 networks per daemon. With per-agent networks on top, this could become a constraint for large deployments. Monitor and document the limit.

### 2. Gateway Socket Access Tightening

The gateway Unix socket (`~/.agency/run/gateway.sock`) serves a restricted router with no BearerAuth:
- `GET /api/v1/health`
- `POST /api/v1/agents/{name}/signal`
- `POST /api/v1/internal/llm`
- `GET /api/v1/internal/credentials/resolve`
- `GET /api/v1/infra/status`
- `GET /api/v1/channels`
- `GET /api/v1/channels/{name}/messages`
- `POST /api/v1/channels/{name}/messages`

Currently, only the egress container has this socket bind-mounted (for credential resolution). No agent or workspace container has access. This is correct.

**Hardening:** Add `X-Agency-Caller` header validation to the socket router. Each infra container that uses the socket declares its identity; the socket router validates the caller against an allowlist per endpoint. This prevents a compromised egress container from using the socket to relay signals, post to comms, or call the internal LLM.

**Allowlist:**
| Endpoint | Allowed Callers |
|----------|----------------|
| `/api/v1/health` | any |
| `/api/v1/internal/credentials/resolve` | egress |
| `/api/v1/internal/llm` | enforcer (via TCP, not socket) |
| `/api/v1/agents/{name}/signal` | enforcer (via TCP, not socket) |
| `/api/v1/channels/*` | comms |
| `/api/v1/infra/status` | any |

Note: enforcers use TCP via `host.docker.internal`, not the socket. The socket is currently only used by egress.

### 3. Docker Socket Exposure Audit

No agent or infra container should have access to the Docker socket (`/var/run/docker.sock`). This is already the case — verify with a startup check.

**Implementation:** Add a reconciliation check at gateway startup that inspects all `agency.managed=true` containers and flags any with `/var/run/docker.sock` in their bind mounts. Emit a platform alert if found.

## Implementation Sequence

1. **Gateway socket caller validation** — smallest change, immediate value. Add middleware to socket router.
2. **Docker socket audit check** — trivial to add to reconciliation.  
3. **Mediation network segmentation** — largest change. Requires updating `ensureEgress`, `ensureComms`, `ensureKnowledge`, `ensureIntake`, `ensureWebFetch`, enforcer startup, and network creation/teardown.

## ASK Tenets Addressed

- **Tenet 3 (Mediation is complete):** Segmented networks ensure mediation paths are service-specific, not shared.
- **Tenet 4 (Least privilege):** Enforcers connect only to networks for granted capabilities.
- **Tenet 7 (Constraint history):** Network segmentation decisions based on capability grants are logged.
- **Tenet 16 (Quarantine):** Segmented networks make quarantine more surgical — disconnect one network instead of killing the container.
