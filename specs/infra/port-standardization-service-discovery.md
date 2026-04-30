**Date:** 2026-03-20
**Status:** Approved
**Scope:** Gateway service discovery, localhost port bindings, port standardization

## Problem

Agency's internal services use arbitrary high ports (18080-18200) hardcoded across Go and Python code. Multiple services bind directly to host ports. There is no service discovery — adding or removing a service requires code changes in multiple files. This creates brittleness, non-standard port usage, and unnecessary host exposure.

## Goals

1. Standardize internal ports to conventional values (8080 for HTTP, 3128 for proxies)
2. Single host-facing port for the gateway (8200, configurable) — all external access through one entry point
3. Label-based service discovery so services can be added/removed without code changes
4. Remove direct host port bindings on comms and intake
5. Route external webhooks through the gateway

## Non-Goals

- Docker socket security wrapper (defense-in-depth against our own code — not needed now)
- Migrating Python orchestration code to gateway REST API (follow-on work)
- Docker daemon hardening (operator responsibility, documented but not enforced by Agency)

## Design

### Phase 1: Service Discovery + Direct Localhost Bindings

#### Service Labels

All containers created by the gateway orchestrator get standard labels:

```
agency.service=true
agency.service.name=comms
agency.service.port=8080
agency.service.health=/health
agency.service.network=agency-mediation
agency.service.hmac=<hmac(container-name, secret)>
```

The HMAC label provides cryptographic provenance verification — the gateway can confirm it created a container even after restart. The HMAC key is stored in `~/.agency/config.yaml` (which already holds the gateway auth token).

#### Service Map (`agency-gateway/internal/services/`)

A new package that maintains a live map of discovered services.

```
services/
  map.go       — ServiceMap type, URL()/Healthy()/All() methods
  watcher.go   — Docker event subscription, label parsing
  provenance.go — HMAC signing and dual-layer verification
  health.go    — periodic health polling
```

**ServiceMap interface:**

```go
type ServiceMap struct {
    mu       sync.RWMutex
    services map[string]*Service
    known    map[string]bool   // in-memory creation tracking
    hmacKey  []byte            // from config.yaml
}

func (m *ServiceMap) URL(name string) string   // e.g. "http://agency-comms:8080"
func (m *ServiceMap) Healthy(name string) bool
func (m *ServiceMap) All() []Service
func (m *ServiceMap) Register(id string, labels map[string]string) error
func (m *ServiceMap) Deregister(id string)
```

**Provenance verification (dual-layer per ASK tenet 6):**

1. **In-memory tracking:** The gateway records container IDs it creates in `known`. On Docker start events, if the ID is in `known`, trust immediately.
2. **HMAC verification:** If the ID is not in `known` (e.g., after gateway restart), verify `agency.service.hmac` matches `hmac(container-name, hmacKey)`. Accept if valid, reject and log if not.
3. **Rejection logging:** Any container with `agency.service=true` that fails both checks is logged to `~/.agency/audit/` and excluded from the service map.

**Event watching:** A goroutine calls the Docker events API filtered to `type=container`. On `start` events, it reads labels and runs provenance verification. On `stop`/`die` events, it removes the service. On gateway startup, it scans all running containers to rebuild the map.

**Health checking:** A background loop polls each service's health endpoint (from `agency.service.health` label) every 10 seconds. Services are marked healthy/unhealthy in the map. Unhealthy services remain in the map (they may recover) but `agency status` surfaces their state.

#### Localhost Port Bindings

Each infra service binds to a unique `127.0.0.1` port so the gateway (host process) can reach it directly. No intermediary container needed.

| Service | Container Port | Host Binding |
|---------|---------------|-------------|
| comms | 8080 | 127.0.0.1:8202 |
| knowledge | 8080 | 127.0.0.1:8204 |
| intake | 8080 | 127.0.0.1:8205 |

Bindings are localhost-only — not externally accessible. Cross-platform (Linux, WSL2, macOS).

#### Host Port Binding Changes

| Service | Before | After |
|---------|--------|-------|
| Comms | `0.0.0.0:18091` (host-bound) | `127.0.0.1:8202` (localhost only) |
| Intake | `0.0.0.0:18095` (host-bound) | `127.0.0.1:8205` (localhost only) |
| Knowledge | No host binding | `127.0.0.1:8204` (localhost only) |

#### Webhook Routing

External webhooks currently hit `0.0.0.0:18095` directly. With this change, they go through the gateway:

```
External sender → localhost:8200/api/v1/intake/webhook → agency-intake:8080/webhook
```

The gateway already has intake API endpoints. This routes the external path through them, centralizing auth and audit.

#### Agent-Internal Network Connectivity

Currently, `ConnectInfraToAgent` (infra.go) connects comms and knowledge directly to each agent's internal network so the body runtime can reach them by hostname. This connectivity model is preserved — comms and knowledge remain connected to agent-internal networks with aliases. The body runtime continues to reach them directly (e.g., `http://comms:8080`). What changes is:

- The port in the URL (from `18091` to `8080`)
- The URL is set via env var (`AGENCY_COMMS_URL`) rather than hardcoded in the body runtime
- No host port binding is needed — the body runtime reaches comms via the Docker network, not via the host

XPIA scanning runs inside the enforcer process — no separate container or network connection is needed.

#### Comms Container Network Fix

The current comms container creation doesn't set `NetworkMode` explicitly — it relies on a separate `connectIfNeeded` call. This must be fixed to set `NetworkMode: mediationNet` directly (matching knowledge), with the `connectIfNeeded` call removed.

#### Code Changes (Phase 1)

**New files:**
- `agency-gateway/internal/services/` — service map package (4 files)

**Modified files (gateway):**
- `agency-gateway/internal/orchestrate/infra.go` — add labels to all container creation, add localhost port bindings (8202-8205), fix comms NetworkMode
- `agency-gateway/internal/orchestrate/enforcer.go` — add labels to enforcer creation
- `agency-gateway/internal/orchestrate/workspace.go` — set env vars from service map instead of hardcoded URLs
- `agency-gateway/internal/docker/client.go` — replace `http://localhost:18091` with `http://localhost:8202`
- `agency-gateway/internal/ws/comms_bridge.go` — replace `ws://localhost:18091` with `ws://localhost:8202`
- `agency-gateway/internal/knowledge/proxy.go` — rewrite to use HTTP to `localhost:8204` instead of docker exec
- `agency-gateway/internal/orchestrate/start.go` — replace docker exec budget config with HTTP to `localhost:8203/budget-configure`
- `agency-gateway/internal/api/` — webhook endpoint forwards to `localhost:8205`
- `agency-gateway/cmd/gateway/main.go` — initialize ServiceMap, start event watcher

### Phase 2: Port Standardization

With the service map abstracting all port references, changing ports is a configuration change.

#### New Port Scheme

| Port | Role | Convention |
|------|------|-----------|
| `8080` | Application HTTP service | Standard non-privileged HTTP. Used by: comms, knowledge, intake |
| `3128` | Forward proxy | Standard HTTP proxy. Used by: enforcer, egress |
| `8081` | Enforcer constraint port | Mediation proxy routes (comms, knowledge) — no auth required. Used by: enforcer |
| `8200` | Gateway REST API | Agency default (configurable via `~/.agency/config.yaml`). Host-facing. |
| `8202` | Comms host binding | Gateway-to-comms. Localhost only. |
| `8204` | Knowledge host binding | Gateway-to-knowledge. Localhost only. |
| `8205` | Intake host binding | Gateway-to-intake. Localhost only. |

#### Changes Per Service

| Service | Old Port | New Port | Files to Change |
|---------|----------|----------|----------------|
| Comms | `18091` | `8080` | `agency/services/comms/comms_server.py`, `infra.go` healthcheck |
| Knowledge | `18092` | `8080` | `agency/services/knowledge/server.py`, `infra.go` healthcheck |
| Intake | `18095` | `8080` | `agency/images/intake/intake_server.py`, `infra.go` healthcheck |
| Enforcer | `18080` | `3128` | `agency/images/enforcer/main.go`, `enforcer.go` healthcheck, Dockerfile EXPOSE |
| Egress | `3128` | `3128` | (unchanged) |
| Gateway | `18200` | `8200` | `cmd/gateway/main.go` default flag value |

#### Body Runtime Port References

The body runtime (`agency/images/body/`) has hardcoded fallback URLs throughout. These must be updated:

| File | Old Reference | New Reference |
|------|--------------|---------------|
| `body.py` | `http://comms:18091` | `http://comms:8080` (via `AGENCY_COMMS_URL` env var) |
| `body.py` | `http://knowledge:18092` | `http://knowledge:8080` (via `AGENCY_KNOWLEDGE_URL` env var) |
| `body.py` | `http://enforcer:18080` | `http://enforcer:3128` (via `AGENCY_ENFORCER_URL` env var) |
| `ws_listener.py` | `http://agency-comms:18091` | `http://agency-comms:8080` |
| `entrypoint.sh` | `http://enforcer:18080/health` | `http://enforcer:3128/health` |
| `approval.py` | `http://comms:18091` | `http://comms:8080` |

All body runtime code should prefer the env var and use the new port only as a fallback default.

#### Workspace Environment Variables

Set by the orchestrator from the service map — no hardcoded ports:

```
AGENCY_ENFORCER_URL=http://enforcer:3128/v1
HTTP_PROXY=http://{scoped_key}:x@enforcer:3128
HTTPS_PROXY=http://{scoped_key}:x@enforcer:3128
```

The `NO_PROXY` list remains hostname-based (no ports): `enforcer,comms,knowledge,localhost,127.0.0.1`.

#### Python Code (Interim)

Python orchestration code in `agency/core/infrastructure.py` still creates containers with the `docker` SDK. For Phase 2, update the hardcoded port numbers to match the new scheme. Full migration to the gateway REST API is a follow-on effort.

#### Gateway Port Configuration

The gateway listen address defaults to `127.0.0.1:8200`, configurable via:
- CLI flag: `agency serve --http 127.0.0.1:8200`
- Config: `~/.agency/config.yaml` → `gateway_addr: "127.0.0.1:8200"`

Containers do not connect to the gateway TCP port directly. Instead, a gateway socket proxy (`agency-infra-gateway-proxy`) bridges the gateway's Unix socket to `gateway:8200` on the Docker mediation network. See `specs/infra/gateway-socket-proxy.md`.

#### Backward Compatibility

None needed. `agency infra down && agency infra up` recreates all containers. No persistent state depends on port numbers. The gateway port change requires restarting the daemon, which `agency infra up` triggers.

#### HMAC Key Rotation

The HMAC key is stored in `~/.agency/config.yaml`. If rotated, existing container labels become unverifiable via HMAC. The in-memory tracking layer handles currently-running containers. After rotation, `agency infra down && agency infra up` recreates all containers with new HMAC labels. No separate rotation mechanism is needed.

#### Gateway Port Conflict Note

Port 8200 is also HashiCorp Vault's default port. If an operator runs HashiCorp Vault on the same host, the gateway port is configurable via `~/.agency/config.yaml`.

## Container Topology (After)

```
Host:
  agency (gateway binary)
    REST API:  127.0.0.1:8200
    Sockets:   ~/.agency/run/gateway.sock (proxy-safe)
               ~/.agency/run/gateway-cred.sock (credential resolution)

Docker (agency-mediation network):
  agency-infra-gateway-proxy :8200  (socat → gateway.sock, alias "gateway")
  agency-infra-comms         :8080  → 127.0.0.1:8202
  agency-infra-knowledge     :8080  → 127.0.0.1:8204
  agency-infra-intake        :8080  → 127.0.0.1:8205
  agency-infra-egress        :3128  (also mounts gateway-cred.sock)

Docker (agency-{name}-internal network, per agent):
  agency-{name}-enforcer   :3128
  agency-{name}-workspace  (no listener)

Docker (agency-egress-net):
  agency-infra-egress      :3128 → internet
```

## ASK Compliance

| Tenet | Status | Notes |
|-------|--------|-------|
| 1. Constraints external | OK | Service discovery runs in the gateway (outside agent boundary). Agents cannot see or influence the service map. |
| 2. Every action traced | OK | Service map changes are derived from Docker events, which are logged. Provenance verification failures logged to audit. |
| 3. Mediation complete | OK | Removing host port bindings on comms/intake eliminates direct external access. All paths go through the gateway. Webhook ingress is now mediated. |
| 4. Least privilege | OK | Services only reachable via their Docker network — labels don't change connectivity. |
| 5. Governance operator-owned | OK | Service map derived from what the operator starts. Label scheme and HMAC key are operator-controlled. |
| 6. Isolation boundaries | OK | Dual-layer provenance (memory + HMAC) provides layered verification. Service discovery labels and HMAC provenance run in the gateway (outside agent boundary). |

## Testing

- **Unit tests:** ServiceMap, provenance HMAC, event handling
- **Integration tests:** Container creation with labels → discovery → URL resolution
- **E2E:** `agency infra up` → verify localhost port bindings → `agency create/start` → verify agent can reach comms/knowledge via enforcer → webhook delivery through gateway
- **Doctor checks:** Verify no services are host-bound except gateway (localhost only)
