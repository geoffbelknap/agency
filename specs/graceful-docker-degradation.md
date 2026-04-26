# Graceful Docker Degradation

## Status: Rejected (2026-04-21)

Rejected in favour of honest fail-closed behaviour. When a container
backend is configured but unreachable, the gateway halts startup with
`%s client is required` rather than serving a partial surface.

Rationale: ASK tenet #4 ("enforcement failure defaults to denial")
requires that a missing mediation plane stop the platform, not silently
degrade it. A gateway serving reads from filesystem state while the
enforcer network is unreachable is a partial-trust surface — operators
may believe the platform is running when the policy plane is actually
down. Failing fast surfaces the real problem immediately.

The read-only conveniences this spec proposed (inspect config,
list credentials, view agents) are available via `agency admin doctor`
and direct filesystem inspection of `~/.agency/` when the gateway is
down. They do not require a running gateway.

## Problem

The gateway hard-fails on startup if Docker is unavailable. This means:
- `agency status` returns nothing useful when Docker Desktop is restarting
- A Docker crash takes down the entire platform, including the REST API
- No way to inspect platform state or agent configs without Docker

Docker Desktop on macOS can go away for several reasons: app restart, update, crash, user quit. The gateway should survive this and degrade gracefully.

## Proposed Behavior

### Startup

| Docker State | Current Behavior | Proposed Behavior |
|-------------|------------------|-------------------|
| Available | Start normally | No change |
| Unavailable | Fatal error, gateway doesn't start | Start in degraded mode |
| Returns mid-session | N/A | Reconnect automatically |

**Degraded mode:** Gateway starts, REST API listens, CLI works. Operations that require Docker (infra up, agent start/stop, container inspect) return clear errors indicating Docker is unavailable. Read-only operations (agent list, config view, credential management, status) work normally using filesystem state.

### Runtime

When Docker goes away after startup:

1. **Detect disconnection** — Docker client calls start failing. Set an internal `dockerAvailable` flag to false.
2. **API responses** — Include `"docker": "unavailable"` in status responses. Container-dependent fields (workspace/enforcer state) show `"unknown"` instead of `"stopped"`.
3. **Auto-reconnect** — Background goroutine polls Docker availability every 30 seconds. On reconnect, flip the flag back. Infrastructure restoration is manual by default (operator runs `agency infra up`).
4. **Optional auto-restore** — `auto_restore_infra: true` in config.yaml causes the gateway to automatically run `infra up` when Docker reconnects. Default: false (operator must be explicit).
5. **No crash** — No panic, no fatal log, no process exit. The gateway stays up.

### Status Display

```
Agency v0.1.6 (abc1234, 2026-04-02)
  Gateway: http://127.0.0.1:8200
  Web UI:  http://127.0.0.1:8280
  Docker:  unavailable ⚠

Infrastructure:
  ? egress         unknown
  ? comms          unknown
  ...
```

## Configuration

```yaml
# config.yaml
auto_restore_infra: false  # default: operator runs `agency infra up` after Docker reconnects
```

When `auto_restore_infra: true`, the gateway automatically runs the equivalent of `agency infra up` when Docker connectivity is restored. A log entry and platform event are emitted so the operator knows it happened.

## Implementation

### 1. Make Docker client optional in gateway startup

`cmd/gateway/main.go` currently creates the Docker client early and fails if it can't connect. Change to:
- Try to connect; if it fails, log a warning and set `dockerClient = nil`
- Pass the nil-able client through to handlers
- Handlers check for nil before Docker operations

### 2. Add Docker health monitor

Background goroutine that:
- Pings Docker every 30 seconds when disconnected
- On reconnect: creates new client, runs reconciliation, updates handler references
- If `auto_restore_infra` is set, runs `EnsureRunning` after reconnect
- On disconnect: sets flag, logs warning, emits platform event

### 3. Guard Docker-dependent operations

Operations that need Docker:
- `infra up/down/rebuild` — return 503 with "Docker unavailable"
- `agent start/stop/restart` — return 503
- Container status in `listAgents`/`showAgent` — return `"unknown"` for workspace/enforcer fields
- `infraStatus` — return components as `"unknown"` state

Operations that don't need Docker (should always work):
- `agent create/delete` (filesystem only)
- `creds set/list/show/delete`
- `status` (with degraded container info)
- `hub search/install`
- `mission create/assign`
- Config and preset management

### 4. CLI messaging

When Docker is unavailable, CLI commands that require it should print:
```
Error: Docker is not available. Container operations are unavailable.
Start Docker Desktop and try again, or use 'agency status' to see platform state.
```

Not a stack trace. Not "connection refused". A human sentence.

## Non-Goals

- Automatically restarting Docker (that's the OS/user's job)
- Running agents without Docker (Docker is the isolation boundary)
- Queueing operations for when Docker returns (too complex, unclear semantics)
