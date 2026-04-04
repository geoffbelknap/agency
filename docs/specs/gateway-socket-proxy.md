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
  Gateway process → ~/.agency/run/gateway.sock

Docker (agency-mediation network):
  gateway-proxy (socat) ─── mounts socket ──→ gateway.sock
    ↑ TCP :8200, alias "gateway"
    │
    ├── enforcer (per-agent) → http://gateway:8200
    ├── egress              → http://gateway:8200
    ├── knowledge           → http://gateway:8200
    ├── comms (no gateway dep currently)
    ├── intake (no gateway dep currently)
    └── web-fetch (no gateway dep currently)

  web (host networking) → http://127.0.0.1:8200 (unchanged)
  workspace (agent-internal) → http://enforcer:3128 (unchanged)
```

### Proxy Container

- **Image:** Alpine 3.21 + socat (~5MB)
- **Name:** `agency-infra-gateway-proxy`
- **Entrypoint:** `socat TCP-LISTEN:8200,fork,reuseaddr UNIX-CONNECT:/run/gateway.sock`
- **Network:** `agency-mediation` with alias `gateway`
- **Mount:** `~/.agency/run/:/run/:ro`
- **Resources:** 16MB memory, 0.25 CPU, 32 pids
- **Read-only rootfs**, no tmpfs needed
- **Health check:** `socat -T1 TCP:127.0.0.1:8200 /dev/null` (verifies listener and socket)
- **Labels:** `agency.managed=true`, `agency.role=infra`, `agency.component=gateway-proxy`
- **No ExtraHosts, no exposed ports, no credentials**

### How It Works

The gateway already creates a Unix socket at `~/.agency/run/gateway.sock` with a restricted router (internal endpoints only — credential resolution, agent signals, budget). The socket router is already implemented; this spec does not change its endpoint set. The proxy container mounts the socket directory read-only and runs socat to bridge it to TCP port 8200. Other containers resolve `gateway` via Docker DNS and make plain HTTP requests.

socat with `fork` opens a new socket connection per request. Gateway restarts (which recreate the socket) are transparent — no stale connections, no reconnect logic needed.

## Container Changes

### ExtraHosts Removed

Every container currently has `ExtraHosts: ["host.docker.internal:host-gateway"]`. This is removed from all containers. No container gets a route to the host IP.

### Environment Variable Changes

| Container | Before | After |
|-----------|--------|-------|
| Enforcers | `GATEWAY_URL=http://host.docker.internal:8200` | `GATEWAY_URL=http://gateway:8200` |
| Egress | `GATEWAY_URL=http://host.docker.internal:8200` + socket mount | `GATEWAY_URL=http://gateway:8200`, remove socket mount |
| Knowledge | `AGENCY_GATEWAY_URL=http://host.docker.internal:8200` | `AGENCY_GATEWAY_URL=http://gateway:8200` |

### Egress Credential Resolution

Egress currently resolves credentials via two paths: Unix socket (primary) and HTTP to `host.docker.internal` (fallback). Both are replaced with `http://gateway:8200`. Credentials still travel over the socket internally (proxy → socket → gateway), never over a Docker network. The egress `key_resolver.py` is simplified to a single HTTP path.

Note: real API keys are still mounted as read-only files into egress for the credential swap at request time. The gateway API is only used for metadata resolution, not for fetching raw key material.

### Unchanged

- **Web container:** host networking, proxies to `127.0.0.1:8200` directly
- **Workspaces:** talk to enforcer only, no gateway access
- **Credential lifecycle:** gateway owns CRUD, egress reads mounted files
- **Per-agent network isolation:** unchanged
- **Egress credential file mounts:** unchanged (read-only)

## Boot Sequence

```
1. Gateway (host) — creates Unix socket at ~/.agency/run/gateway.sock
2. Docker networks (agency-mediation, agency-egress-net, per-agent)
3. gateway-proxy — mounts socket, listens on TCP 8200
4. egress — needs mediation network + gateway proxy
5. comms, knowledge, intake, web-fetch, embeddings
6. web (independent, host networking)
7. Per-agent enforcers — need mediation network + gateway proxy
8. Per-agent workspaces — need agent-internal + enforcer
```

## Failure Handling

| Scenario | Behavior |
|----------|----------|
| Gateway not running (no socket) | Proxy starts, connections fail. Health check fails. Downstream retries. |
| Gateway restarts (socket recreated) | socat reconnects per-request via fork. No proxy restart needed. |
| Proxy crashes | Enforcer budget checks fail (401, proceeds). Signals queue. `infra up` restarts. |
| Proxy healthy, gateway slow | Timeouts handled by callers (5s for enforcers). |

## Security Analysis

### Improvements Over Current Model

- **Eliminates ExtraHosts from all containers.** No container gets a route to the host IP. Today a compromised enforcer can probe `host.docker.internal` on any port. After, it can only reach `gateway:8200` on the Docker network.
- **Reduces lateral movement surface.** The host IP is no longer reachable from any container.
- **ASK Tenet 3 (mediation is complete):** strengthened — gateway access is a named service on the mediation network, not a host escape hatch.
- **ASK Tenet 7 (least privilege):** the proxy has no credentials, no writable mounts, no business logic.

### Unchanged Risks

- Any container on `agency-mediation` can reach the gateway API (same as today).
- Non-sensitive traffic (budget checks, signals, LLM routing) travels as HTTP on the Docker bridge. This data is not secret.
- Credential file mounts in egress remain the same (read-only).

### Socket Permissions

The Unix socket is created with mode `0666`. Access is controlled by bind mount scope — only the proxy container mounts `~/.agency/run/`. No other container should mount this directory.

## Implementation Scope

### New Files

| File | Purpose |
|------|---------|
| `images/gateway-proxy/Dockerfile` | Alpine + socat, 3 lines |
| `Makefile` update | Add `gateway-proxy` to build targets |

### Modified Files (Code)

| File | Change |
|------|--------|
| `internal/orchestrate/infra.go` | Add `ensureGatewayProxy()` in boot sequence. Remove ExtraHosts from egress, comms, knowledge, intake, web-fetch. Remove socket mount from egress. |
| `internal/orchestrate/enforcer.go` | `GATEWAY_URL` → `http://gateway:8200`. Remove ExtraHosts. |
| `images/egress/key_resolver.py` | Remove socket resolver and HTTP fallback. Single path: `http://gateway:8200`. |

### Specs to Update

| Spec | What Changes |
|------|-------------|
| `docs/infrastructure.md` | Network topology, add gateway-proxy |
| `docs/security.md` | Network mediation flow diagram |
| `docs/specs/infra-service-contract.md` | Network assignments |
| `docs/specs/port-standardization-service-discovery.md` | Service map, remove localhost bindings for gateway |
| `docs/specs/infrastructure-llm-routing.md` | Gateway URL references |
| `docs/specs/agency-web-container.md` | Remove host.docker.internal reference |
| `docs/specs/mediation-network-hardening.md` | Threat model update |
| `docs/specs/enforcer-consolidation.md` | Network topology diagrams |
| `docs/specs/egress-jwt-credential-swap.md` | Credential resolution path |
| `docs/specs/web-fetch-service.md` | Mediation routing |
| `CLAUDE.md` | Architecture overview |

### Not Changing

- Gateway stays on the host
- Credential lifecycle stays in the gateway
- Egress credential file mounts (read-only) unchanged
- Per-agent network isolation unchanged
- Web container (host networking) unchanged
- Workspace → enforcer path unchanged
