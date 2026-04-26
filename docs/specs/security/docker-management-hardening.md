---
description: "Establish durable abstractions that make correct Docker management automatic and incorrect usage impossible. Fix the ..."
---

# Docker Management Hardening & Security Foundation

## Goal

Establish durable abstractions that make correct Docker management automatic and incorrect usage impossible. Fix the 3 critical security issues (unauthenticated gateway, enforcer token bypass, socket permissions) and the 6 critical Docker issues (orphan containers, restart policy, failClosed, meeseeks network isolation, image pruning, log rotation).

## Architecture

Three new packages provide the building blocks. All existing container/network creation sites migrate to use them. A gateway auth middleware closes the security gap. CLAUDE.md codifies the principles for all future development.

## Tech Stack

Go 1.26, Docker SDK, chi router, crypto/subtle

---

## 1. Container Defaults Builder

**Package:** `internal/orchestrate/containers`
**File:** `defaults.go`

A builder that produces `container.HostConfig` with mandatory fields pre-populated by role. Callers overlay their specific binds, env, network mode, etc.

### Roles and defaults

| Field | Workspace | Enforcer | Infra | Meeseeks |
|-------|-----------|----------|-------|----------|
| Memory | 512MB | 128MB | 256MB (128 intake) | 512MB |
| CPU (NanoCPUs) | 2e9 (2 cores) | 0.5e9 | 1e9 | 1e9 |
| PidsLimit | 512 | 256 | 1024 | 512 |
| RestartPolicy | on-failure max 3 | on-failure max 3 | unless-stopped | no |
| CapDrop | ALL | ALL | ALL | ALL |
| CapAdd | NET_BIND_SERVICE | NET_BIND_SERVICE | NET_BIND_SERVICE | NET_BIND_SERVICE |
| SecurityOpt | no-new-privileges + seccomp | no-new-privileges | no-new-privileges | no-new-privileges |
| LogConfig | json-file, 10m, 3 files | json-file, 10m, 3 files | json-file, 10m, 3 files | json-file, 5m, 2 files |
| ReadonlyRootfs | true | true | false | true |

### API

```go
type ContainerRole string
const (
    RoleWorkspace ContainerRole = "workspace"
    RoleEnforcer  ContainerRole = "enforcer"
    RoleInfra     ContainerRole = "infra"
    RoleMeeseeks  ContainerRole = "meeseeks"
)

// HostConfigDefaults returns a HostConfig with all mandatory fields set.
// Callers use the returned value and add Binds, NetworkMode, Tmpfs, ExtraHosts.
func HostConfigDefaults(role ContainerRole) *container.HostConfig

// SeccompProfile returns the embedded seccomp JSON. Falls back to the
// compiled-in default if the on-disk file is missing.
func SeccompProfile(homeDir string) string
```

The seccomp JSON is embedded via `//go:embed` so it can never be silently absent.

### Migration

Every `container.HostConfig{}` literal in `infra.go`, `enforcer.go`, `workspace.go`, `meeseeks_start.go` is replaced with:
```go
hc := containers.HostConfigDefaults(containers.RoleWorkspace)
hc.Binds = append(hc.Binds, ...)
hc.NetworkMode = container.NetworkMode(netName)
hc.Tmpfs = map[string]string{"/tmp": "size=512m,..."}
```

---

## 2. Container Lifecycle Guard

**File:** `lifecycle.go` in the same package

Wraps `ContainerCreate` + `ContainerStart` with automatic cleanup on start failure.

```go
type CreateConfig struct {
    Name       string
    Config     *container.Config
    HostConfig *container.HostConfig
    NetConfig  *network.NetworkingConfig
    Platform   *specs.Platform
}

// CreateAndStart creates a container and starts it. If start fails,
// the created container is automatically removed. Returns container ID.
func CreateAndStart(ctx context.Context, cli DockerClient, cfg CreateConfig) (string, error)

// StopAndRemove stops (with timeout) then force-removes a container.
// Ignores "not found" errors. Safe to call on already-removed containers.
func StopAndRemove(ctx context.Context, cli DockerClient, name string, timeout int) error
```

### Invariant

After `CreateAndStart` returns an error, no orphan container exists. This is enforced by the function, not by callers.

### Migration

Every `cli.ContainerCreate` + `cli.ContainerStart` sequence (10+ sites) is replaced with a single `containers.CreateAndStart()` call.

---

## 3. Network Factory

**File:** `networks.go` in the same package

```go
// CreateInternalNetwork creates a Docker bridge network with Internal: true
// and standard agency labels. Returns a cleanup function.
func CreateInternalNetwork(ctx context.Context, cli DockerClient, name string, labels map[string]string) (func(), error)

// CreateEgressNetwork creates a non-internal network (for egress proxy).
func CreateEgressNetwork(ctx context.Context, cli DockerClient, name string, labels map[string]string) (func(), error)
```

### Invariant

`CreateInternalNetwork` always sets `Internal: true`. There is no parameter to disable it. The only way to create a non-internal network is `CreateEgressNetwork`, which is used exclusively for the egress proxy.

All networks get label `agency.managed=true` for orphan detection.

### Migration

- Agent network creation in `infra.go` → `CreateInternalNetwork`
- Meeseeks network creation in `meeseeks_start.go` → `CreateInternalNetwork` (fixes the missing `Internal: true`)
- Egress network in `infra.go` → `CreateEgressNetwork`
- `connectToAgentNetworks` filter changes from name prefix to label filter `agency.managed=true`

---

## 4. Shared Docker Client

**File:** `internal/orchestrate/docker.go` (or modify existing `internal/docker/client.go`)

A single `*client.Client` created in `main.go` and injected into all orchestration constructors.

### Migration

Remove `client.NewClientWithOpts(client.FromEnv, ...)` from:
- `NewInfra()`
- `NewEnforcer()`
- `NewWorkspace()`
- `MeeseeksStartSequence.Run()`
- `NewHaltController()`
- `NewMissionHealthMonitor()`
- `NewWorkspaceWatcher()`
- `NewEnforcerWatcher()`
- `TeardownMeeseeks()`

All accept a `*client.Client` parameter instead.

---

## 5. Gateway Auth Middleware

**File:** `internal/api/middleware_auth.go`

```go
// BearerAuth returns chi middleware that validates the Authorization header
// or X-Agency-Token header against the configured token using constant-time
// comparison. Exempt paths: /api/v1/health
func BearerAuth(token string) func(http.Handler) http.Handler
```

### Applied in `main.go`

```go
r.Route("/api/v1", func(r chi.Router) {
    r.Use(api.BearerAuth(cfg.Token))
    api.RegisterRoutesWithOptions(r, cfg, dc, logger, opts)
})
```

### Unix socket

The Unix socket gets a separate, restricted router that only exposes endpoints needed by infra containers (health, signal relay, constraint delivery). Not the full operator API. Socket permissions change from `0666` to `0660`.

### WebSocket

The `/ws` endpoint requires the token as the first message (type: `auth`), matching what we already implemented in agency-web's `ws.ts`.

---

## 6. Enforcer Token Validation Fix

**File:** `images/enforcer/middleware.go`

Remove the `strings.HasPrefix(token, "agency-scoped-")` bypass. All tokens validated against the `am.keys` map only:

```go
func (am *AuthMiddleware) isValidToken(token string) bool {
    am.mu.RLock()
    defer am.mu.RUnlock()
    _, ok := am.keys[token]
    return ok
}
```

---

## 7. Image Pruning in Release Mode

**File:** `internal/images/resolve.go`

After `pullAndTag` succeeds in release mode, call `pruneOldImages` — the same function already used in dev mode. This ensures old pulled images are cleaned up on every `infra up`.

Also fix `NoCache: true` in dev mode — remove it and rely on the `CACHE_BUST` build arg for invalidation. This allows Docker layer caching for unchanged layers (apt-get, pip install).

---

## 8. Startup Reconciliation

**File:** `internal/orchestrate/reconcile.go`

On gateway startup (in `main.go`, after Docker client creation), run a reconciliation pass:

```go
func Reconcile(ctx context.Context, cli *client.Client, knownAgents []string, logger *log.Logger) error
```

1. List all containers with label `agency.managed=true`
2. For Meeseeks containers (label `agency.type=meeseeks-*`): stop and remove all (Meeseeks are ephemeral)
3. For agent containers: if the agent name is not in `knownAgents`, stop and remove
4. List all networks with label `agency.managed=true` — remove any with zero connected containers
5. List all images with `agency-` prefix — prune any not tagged `:latest`

Log all reconciliation actions at WARN level so operators see what was cleaned up.

---

## 9. `failClosed` Fix

**File:** `internal/orchestrate/start.go`

Replace the current `exec kill -TERM 1` pattern with proper Docker stop+remove:

```go
func (ss *StartSequence) failClosed(ctx context.Context) {
    ss.Log.Warn("fail-closed teardown", "agent", ss.AgentName)
    timeout := 10
    for _, name := range []string{wsName, enfName} {
        containers.StopAndRemove(ctx, ss.Docker.Client(), name, timeout)
    }
}
```

Uses the `StopAndRemove` helper from the lifecycle guard package.

---

## 10. `agency doctor` Docker Checks

Add new checks to the existing doctor command:

- **Orphan containers**: containers with `agency.*` labels not matching known agents
- **Orphan networks**: networks with `agency.managed=true` and zero endpoints
- **Dangling images**: images with `agency-` prefix not tagged `:latest`
- **Disk usage**: total Docker disk usage via `docker system df`
- **Log sizes**: sum of container log sizes, warn if >1GB total
- **PID limits**: verify all running workspace containers have PID limits set
- **Network isolation**: verify all agent networks have `Internal: true`

---

## 11. CLAUDE.md Docker Principles

Add to the agency repo's CLAUDE.md:

```markdown
## Docker Management Principles

1. **Never construct HostConfig directly** — use `containers.HostConfigDefaults(role)` and overlay
2. **Never call ContainerCreate without lifecycle guard** — use `containers.CreateAndStart()`
3. **Never create networks without the factory** — use `containers.CreateInternalNetwork()`
4. **Never create a new Docker client** — inject the shared client from main.go
5. **All containers MUST have**: log rotation (10m/3), PID limits, CPU limits, CAP_DROP ALL
6. **Workspace/enforcer restart policy is `on-failure` max 3** — never `unless-stopped`
7. **Infra containers own `unless-stopped`** — they survive gateway restarts
8. **Image pruning runs after every resolve** (pull or build) — not just dev mode
9. **Gateway startup reconciles Docker state** — orphan containers/networks cleaned up
10. **Labels are the source of truth** — all agency containers/networks labeled `agency.managed=true`
```

---

## 12. Meeseeks Delegation Validation (ASK Tenet 11)

**File:** `internal/api/handlers_meeseeks.go` (spawn handler) and `internal/orchestrate/meeseeks_start.go`

ASK Tenet 11: "Delegation cannot exceed delegator scope." Currently, Meeseeks inherits the parent's constraints (`:ro` mount) and receives `GrantedCaps` from the parent, but there is no validation that the Meeseeks' effective tool set is a subset of the parent's authorized tools.

### Spawn-Time Validation

Add a check in the `spawnMeeseeks` handler before launching the start sequence:

```go
// handlers_meeseeks.go — inside spawnMeeseeks, after resolving parent capabilities
func validateDelegationBounds(parentCaps []string, meeseeksCaps []string) error {
    parentSet := make(map[string]bool, len(parentCaps))
    for _, c := range parentCaps {
        parentSet[c] = true
    }
    for _, c := range meeseeksCaps {
        if !parentSet[c] {
            return fmt.Errorf("meeseeks capability %q exceeds parent scope (ASK tenet 11)", c)
        }
    }
    return nil
}
```

This runs before `MeeseeksStartSequence` — a failing validation returns 403 to the caller and no containers are created.

### Enforcer Policy Narrowing

The Meeseeks enforcer must also enforce narrowed scope at runtime, not just at spawn:

- The Meeseeks enforcer receives the parent's policy file (already mounted `:ro`)
- Add a `AGENCY_MEESEEKS_CAPS` environment variable listing the granted capabilities
- The enforcer rejects tool calls not in the granted set (fail-closed)

### Audit

Log the delegation check result (pass/fail, parent caps, requested caps) to the parent agent's audit trail.

---

## 13. Memory Mutation Audit (ASK Tenet 25)

**File:** `internal/orchestrate/workspace.go` and `images/enforcer/`

ASK Tenet 25: "Identity writes are logged with provenance." The `/agency/memory` directory is mounted `:rw` into the workspace. The agent can write freely — learned procedures, episodic memory, personality notes — with no audit trail. If an XPIA attack corrupts memory, there is no provenance log and no rollback capability.

### Approach: Periodic Hash Inventory

Rather than intercepting every filesystem write (which would require FUSE or inotify overhead), use a periodic hash-based inventory:

**Enforcer-side memory auditor** (new goroutine in enforcer):

```go
// memory_audit.go — runs inside enforcer container
// Every 60 seconds (and on session end), compute SHA-256 of each file in /agency/memory
// Compare against last known inventory
// For any changed/new/deleted files: log structured audit entry with:
//   - filename, old_hash, new_hash, size_delta, timestamp
//   - "provenance: agent" (since only the agent can write to this mount)
// Persist inventory as memory-inventory.json in enforcer audit dir
```

### Rollback Capability

The enforcer maintains the hash inventory. The gateway can:
1. `GET /agents/{name}/memory/audit` — list all memory mutations with timestamps
2. `POST /agents/{name}/memory/rollback?to={timestamp}` — restore from the named volume's snapshot

For rollback to work, the workspace named volume (`agency-{name}-workspace-data`) is snapshotted before each session start using `docker volume` operations. This gives point-in-time recovery.

### Mount Change

No mount mode change needed — memory stays `:rw` for the agent. The enforcer audits the directory from its own mount of the same named volume (`:ro` in enforcer, `:rw` in workspace). This requires adding a bind mount of the memory directory to the enforcer container:

```go
// enforcer.go — add to binds
memoryDir := filepath.Join(agentDir, "memory")
binds = append(binds, memoryDir + ":/agency/memory:ro")
```

The enforcer reads but cannot write to memory — maintaining the principle that audit is written by infrastructure, not the agent.

---

## Test Strategy

- Unit tests for `HostConfigDefaults` — verify all mandatory fields for each role
- Unit tests for `CreateAndStart` — mock Docker client, verify cleanup on start failure
- Unit tests for `CreateInternalNetwork` — verify `Internal: true` always set
- Unit tests for `BearerAuth` middleware — valid token, invalid token, missing header, constant-time
- Unit tests for `validateDelegationBounds` — parent superset passes, child exceeds fails
- Unit tests for memory hash inventory — detect additions, modifications, deletions
- Integration test for reconciliation — create orphan containers, run reconcile, verify cleanup
- Integration test for Meeseeks spawn with over-scoped caps — verify 403 rejection
- Existing E2E tests (`test_e2e.sh`) verify agent start/stop lifecycle still works

## ASK Compliance Notes

This spec addresses the following ASK tenet violations identified in the 2026-03-30 compliance review:

| Tenet | Issue | Section |
|-------|-------|---------|
| 3 | Gateway API unauthenticated | 5 (BearerAuth middleware) |
| 3 | Enforcer token prefix bypass | 6 (token validation fix) |
| 3 | Unix socket world-writable + full API | 5 (restricted router, `0660`) |
| 3 | Meeseeks network missing `Internal: true` | 3 (network factory) |
| 11 | Meeseeks tool-set not validated against parent | 12 (delegation validation) |
| 25 | Memory mutations not audited | 13 (memory hash inventory) |

After implementation, the platform moves from **ASK-NON-COMPLIANT** to **ASK-COMPLIANT** with all 25 tenets passing or addressed.
