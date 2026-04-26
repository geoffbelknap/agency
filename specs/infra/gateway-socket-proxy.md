# Gateway Socket Proxy

**Status:** Approved
**Date:** 2026-04-04

## Problem

Agency containers reach the gateway via `host.docker.internal:8200`. This breaks on Linux Docker Engine where the gateway listens on `127.0.0.1` (unreachable from bridge-networked containers). Docker Desktop (macOS, Windows) tunnels through a VM so it works there, but the inconsistency causes failures on Linux and WSL deployments.

Every container that needs gateway access carries `ExtraHosts: ["host.docker.internal:host-gateway"]`, scattering platform-specific networking hacks across the entire fleet.

## Solution

Replace all `host.docker.internal` usage with a **gateway socket proxy** — a minimal socat container that bridges the gateway's Unix socket to a TCP port on the Docker mediation network. All containers reach the gateway by Docker DNS name (`gateway:8200`), eliminating platform-specific networking entirely.

### Architecture

```
Host:
  Gateway process → ~/.agency/run/gateway.sock (mode set by gateway process)

Docker (agency-mediation network):
  gateway-proxy (socat) ─── mounts socket ──→ gateway.sock
    ↑ TCP :8200, alias "gateway"
    │
    ├── enforcer (per-agent) → http://gateway:8200  (signals, budget, LLM)
    ├── egress              → http://gateway:8200  (non-sensitive only)
    ├── knowledge           → http://gateway:8200  (LLM routing)
    ├── comms (no gateway dep currently)
    ├── intake (no gateway dep currently)
    └── web-fetch (no gateway dep currently)

  egress also mounts gateway.sock directly for credential resolution
    (sensitive path — never traverses Docker network)

  web (host networking) → http://127.0.0.1:8200 (unchanged)
  workspace (agent-internal) → http://enforcer:3128 (unchanged)
```

### Proxy Container

- **Image:** Alpine 3.21 + socat (~5MB)
- **Name:** `agency-infra-gateway-proxy`
- **Entrypoint:** `socat TCP-LISTEN:8200,fork,reuseaddr UNIX-CONNECT:/run/gateway.sock`
- **Network:** `agency-mediation` with alias `gateway`
- **Mount:** `~/.agency/run/:/run/:ro`
- **Resources:** 16MB memory, 0.25 CPU, 32 pids (~300 req/sec max at 100ms avg)
- **Read-only rootfs**, no tmpfs needed
- **Health check:** `socat -T1 TCP:127.0.0.1:8200 UNIX-CONNECT:/run/gateway.sock` (verifies full chain: TCP → socket)
- **Labels:** `agency.managed=true`, `agency.role=infra`, `agency.component=gateway-proxy`
- **No ExtraHosts, no exposed ports, no credentials**
- Proxy is a transparent HTTP-to-Unix-socket bridge. Headers (including `X-Agency-Caller`) are passed through unchanged.

### How It Works

The gateway already creates a Unix socket at `~/.agency/run/gateway.sock` with a restricted router (internal endpoints only — agent signals, budget, LLM routing). The socket router is already implemented; this spec does not change its endpoint set.

The proxy container mounts the socket directory read-only and runs socat to bridge it to TCP port 8200. Other containers resolve `gateway` via Docker DNS and make plain HTTP requests. Container DNS resolution is confined to Docker DNS; DNS spoofing is not a viable attack from within a container.

socat with `fork` opens a new socket connection per request. Gateway restarts (which recreate the socket) are transparent — no stale connections, no reconnect logic needed. Each concurrent connection consumes one socat child process.

### Credential Resolution Isolation

The gateway socket router exposes internal endpoints including credential resolution (`/api/v1/creds/internal/resolve`). This endpoint **must not** be reachable via the TCP proxy — it would allow any container on `agency-mediation` to resolve credentials.

**Mitigation:** The gateway socket router must be split into two routers:

1. **Proxy-safe router** — endpoints exposed via the TCP proxy (signals, budget, LLM routing, health). This is what the socat proxy bridges to.
2. **Credential router** — credential resolution only, on a separate socket or the same socket with path-based access control.

In practice, the simplest approach: the gateway creates **two Unix sockets**:

- `~/.agency/run/gateway.sock` — proxy-safe endpoints (signals, budget, LLM, infra status). Mounted by the gateway-proxy container.
- `~/.agency/run/gateway-cred.sock` — credential resolution only. Mounted by egress only.

This ensures credential resolution is never reachable from the Docker network. Only containers with the explicit bind mount can access it.

## Container Changes

### ExtraHosts Removed

Every container currently has `ExtraHosts: ["host.docker.internal:host-gateway"]`. This is removed from all containers. No container gets a route to the host IP.

### Environment Variable Changes

| Container | Before | After |
|-----------|--------|-------|
| Enforcers | `GATEWAY_URL=http://host.docker.internal:8200` | `GATEWAY_URL=http://gateway:8200` |
| Egress | `GATEWAY_URL=http://host.docker.internal:8200` + socket mount | `GATEWAY_URL=http://gateway:8200` (non-sensitive), keeps `gateway-cred.sock` mount (credential resolution) |
| Knowledge | `AGENCY_GATEWAY_URL=http://host.docker.internal:8200` | `AGENCY_GATEWAY_URL=http://gateway:8200` |

### Egress Credential Resolution

Egress has **two communication paths** to the gateway:

1. **Credential resolution (sensitive):** Direct Unix socket mount of `~/.agency/run/gateway-cred.sock`. Credentials never traverse a Docker network. The egress `key_resolver.py` socket resolver is retained for this path.
2. **Non-sensitive operations (signals, budget):** HTTP via `http://gateway:8200` through the proxy. Same as all other containers.

Real API keys are still mounted as read-only files into egress for the credential swap at request time. The credential socket is only used for metadata resolution (mapping credential names to providers).

### Unchanged

- **Web container:** host networking, proxies to `127.0.0.1:8200` directly
- **Workspaces:** talk to enforcer only, no gateway access
- **Credential lifecycle:** gateway owns CRUD, egress reads mounted files
- **Per-agent network isolation:** unchanged
- **Egress credential file mounts:** unchanged (read-only)

## Boot Sequence

```
1. Gateway (host) — creates Unix sockets:
   - ~/.agency/run/gateway.sock (proxy-safe endpoints)
   - ~/.agency/run/gateway-cred.sock (credential resolution)
2. Docker networks (agency-mediation, agency-egress-net, per-agent)
3. gateway-proxy — verify socket exists, mount it, listen on TCP 8200
4. egress — needs mediation network + gateway proxy + credential socket
5. comms, knowledge, intake, web-fetch, embeddings
6. web (independent, host networking)
7. Per-agent enforcers — need mediation network + gateway proxy
8. Per-agent workspaces — need agent-internal + enforcer
```

Gateway-proxy startup validation: verify `~/.agency/run/gateway.sock` exists and is readable before starting socat.

## Failure Handling

| Scenario | Behavior |
|----------|----------|
| Gateway not running (no socket) | Proxy starts, connections fail. Health check fails. Downstream retries. |
| Gateway restarts (socket recreated) | socat reconnects per-request via fork. No proxy restart needed. |
| Proxy crashes | Enforcer budget checks fail (401, proceeds). Signals queue. `infra up` restarts. |
| Proxy healthy, gateway slow | Timeouts handled by callers (5s for enforcers). |
| Credential socket unavailable | Egress credential resolution fails. Credential swap fails. LLM calls return 401. Non-fatal for proxy (separate socket). |

## Security Analysis

### Improvements Over Current Model

- **Eliminates ExtraHosts from all containers.** No container gets a route to the host IP. Today a compromised enforcer can probe `host.docker.internal` on any port. After, it can only reach `gateway:8200` on the Docker network.
- **Reduces lateral movement surface.** The host IP is no longer reachable from any container.
- **Credential resolution isolated.** Only egress can resolve credentials (via dedicated socket mount). The TCP proxy does not expose credential endpoints. Today any container with `host.docker.internal` could hit the credential endpoint on the gateway.
- **ASK Tenet 3 (mediation is complete):** strengthened — gateway access is a named service on the mediation network, not a host escape hatch.
- **ASK Tenet 7 (least privilege):** the proxy has no credentials, no writable mounts, no business logic. Access is constrained by bind mount scope, with socket modes set by the gateway process for runtime compatibility.

### Socket Permissions

Socket access control is enforced primarily by bind-mount scope: only designated containers
receive each socket mount. The gateway process sets socket modes at creation time, and those
modes must remain compatible with container runtime users across Linux Docker CE and Docker Desktop.

| Socket | Mounted by | Purpose |
|--------|-----------|---------|
| `gateway.sock` | gateway-proxy only | Non-sensitive API (signals, budget, LLM) |
| `gateway-cred.sock` | egress only | Credential resolution |

No other container mounts either socket directory.

### Unchanged Risks (Addressed by Follow-Up Spec)

- Any container on `agency-mediation` can reach the gateway proxy API (same as today, but now scoped to non-sensitive endpoints).
- Non-sensitive traffic (budget checks, signals, LLM routing) travels as HTTP on the Docker bridge. This data is not secret.
- Credential file mounts in egress remain the same (read-only).
- **A compromised enforcer on the flat `agency-mediation` network can still reach egress, comms, knowledge, and web-fetch.** This is the existing threat model — no regression. See follow-up spec below.

### Follow-Up: Mediation Network Segmentation

This spec intentionally keeps the flat `agency-mediation` network as an interim state. A follow-up spec will segment it to isolate internet-facing services:

```
agency-mediation (internal services only, no internet access)
  ├── comms
  ├── knowledge
  ├── intake
  ├── embeddings
  └── gateway-proxy

agency-egress-mediation (LLM/API traffic — carries credentials at swap time)
  ├── egress
  └── enforcers (secondary connection)

agency-fetch-mediation (web content retrieval — processes untrusted content)
  ├── web-fetch
  └── enforcers (secondary connection)

agency-egress-net (internet access, existing)
  ├── egress
  └── web-fetch
```

**Rationale:** Egress and web-fetch both touch the public internet but have different threat profiles. Egress carries injected API credentials (exfiltration risk). Web-fetch processes untrusted page content (inbound attack risk / XPIA). Sharing a network means a compromise of either enables pivoting to the other. Separate mediation networks prevent this lateral movement.

The segmentation spec will cover: network topology changes, enforcer multi-network connection logic, mediation proxy routing updates, and impact on the existing mediation-network-hardening proposal.

### Proxy Container Security Posture

- Read-only rootfs, no writable mounts — cannot log or persist state locally
- No credentials, no business logic — purely a TCP-to-socket bridge
- All mediation failures detectable via gateway logs (proxy is transparent)
- 32 pid limit prevents fork bombs
- 16MB memory sufficient for ~100 concurrent socat child processes

## Implementation Scope

### New Files

| File | Purpose |
|------|---------|
| `images/gateway-proxy/Dockerfile` | Alpine + socat, 3 lines |
| `Makefile` update | Add `gateway-proxy` to build targets |

### Modified Files (Code)

| File | Change |
|------|--------|
| `cmd/gateway/main.go` | Create second Unix socket (`gateway-cred.sock`) with credential-only router. Change `gateway.sock` permissions from 0666 to 0600. |
| `internal/api/socket_routes.go` | Split socket router: proxy-safe router (gateway.sock) and credential router (gateway-cred.sock). Remove credential resolve from proxy-safe router. |
| `internal/orchestrate/infra.go` | Add `ensureGatewayProxy()` in boot sequence. Remove ExtraHosts from egress, comms, knowledge, intake, web-fetch. Update egress socket mount to `gateway-cred.sock`. |
| `internal/orchestrate/enforcer.go` | `GATEWAY_URL` → `http://gateway:8200`. Remove ExtraHosts. |
| `images/egress/key_resolver.py` | Remove HTTP fallback to `host.docker.internal`. Keep socket resolver for credentials. Add HTTP path via `GATEWAY_URL` for non-sensitive calls. |

### Specs to Update

| Spec | What Changes |
|------|-------------|
| `docs/infrastructure.md` | Network topology, add gateway-proxy |
| `docs/security.md` | Network mediation flow diagram |
| `specs/infra/infra-service-contract.md` | Network assignments |
| `specs/infra/port-standardization-service-discovery.md` | Service map, remove localhost bindings for gateway |
| `specs/infra/infrastructure-llm-routing.md` | Gateway URL references |
| `specs/web/agency-web-container.md` | Remove host.docker.internal reference |
| `specs/mediation-network-hardening.md` | Threat model update |
| `specs/infra/enforcer-consolidation.md` | Network topology diagrams |
| `specs/egress-jwt-credential-swap.md` | Credential resolution path |
| `specs/mcp/web-fetch-service.md` | Mediation routing |
| `CLAUDE.md` | Architecture overview |

### Not Changing

- Gateway stays on the host
- Credential lifecycle stays in the gateway
- Egress credential file mounts (read-only) unchanged
- Per-agent network isolation unchanged
- Web container (host networking) unchanged
- Workspace → enforcer path unchanged
