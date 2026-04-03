# Graceful Docker Degradation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Gateway starts and serves the REST API even when Docker is unavailable, degrading container operations gracefully.

**Architecture:** Add a `DockerMonitor` that wraps the Docker client with an availability flag and a background reconnect loop. All Docker-dependent code paths check the flag before proceeding. Config gains `auto_restore_infra` for optional automatic infra restoration on reconnect.

**Tech Stack:** Go, Docker client SDK, chi router

**Spec:** `docs/specs/graceful-docker-degradation.md`

---

### Task 1: Add `auto_restore_infra` config field

**Files:**
- Modify: `internal/config/config.go` — add field to Config and configFile structs
- Test: existing `internal/config/` tests cover Load()

- [ ] **Step 1: Add the field to Config and configFile**

In `internal/config/config.go`, add to `Config`:
```go
AutoRestoreInfra bool // auto-run infra up when Docker reconnects (default false)
```

Add to `configFile`:
```go
AutoRestoreInfra bool `yaml:"auto_restore_infra,omitempty"`
```

Add to `Load()` after the EgressToken line:
```go
cfg.AutoRestoreInfra = cf.AutoRestoreInfra
```

- [ ] **Step 2: Build and verify**

Run: `go build ./cmd/gateway/`
Expected: compiles clean

- [ ] **Step 3: Commit**

```bash
git add internal/config/config.go
git commit -m "feat: add auto_restore_infra config field"
```

---

### Task 2: Make Docker client optional at startup

**Files:**
- Modify: `internal/docker/client.go` — add `TryNewClient()` that returns nil on failure
- Modify: `cmd/gateway/main.go` — use TryNewClient, continue on nil
- Test: manual (Docker dependency)

- [ ] **Step 1: Add TryNewClient to docker/client.go**

Add after `NewClient()`:
```go
// TryNewClient attempts to create a Docker client. Returns nil (not an error)
// if Docker is unavailable — the gateway can start in degraded mode.
func TryNewClient(logger interface{ Warn(msg string, keyvals ...interface{}) }) *Client {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		logger.Warn("Docker client unavailable, starting in degraded mode", "err", err)
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := cli.Ping(ctx); err != nil {
		logger.Warn("Docker not responding, starting in degraded mode", "err", err)
		return nil
	}
	return &Client{cli: cli}
}
```

- [ ] **Step 2: Add Ping method to Client**

Add to `internal/docker/client.go`:
```go
// Ping checks if Docker is responsive. Returns nil if healthy.
func (c *Client) Ping(ctx context.Context) error {
	if c == nil || c.cli == nil {
		return fmt.Errorf("Docker client not initialized")
	}
	_, err := c.cli.Ping(ctx)
	return err
}
```

- [ ] **Step 3: Update main.go to use TryNewClient**

In `cmd/gateway/main.go`, replace:
```go
dc, err := docker.NewClient()
if err != nil {
    return fmt.Errorf("docker: %w", err)
}
logger.Info("docker connected")
```

With:
```go
dc := docker.TryNewClient(logger)
if dc != nil {
    logger.Info("docker connected")
} else {
    logger.Warn("docker unavailable — gateway starting in degraded mode")
}
```

- [ ] **Step 4: Guard reconciliation**

Wrap the reconciliation block:
```go
if dc != nil {
    knownAgents := listAgentNames(cfg.Home)
    reconcileCtx, reconcileCancel := context.WithTimeout(context.Background(), 30*time.Second)
    orchestrate.Reconcile(reconcileCtx, dc.RawClient(), knownAgents, logger)
    reconcileCancel()
}
```

- [ ] **Step 5: Guard workspace watcher and other Docker-dependent startup code**

Any other code between Docker init and HTTP server start that uses `dc` must be guarded with `if dc != nil`. Scan for `dc.` references and wrap them.

- [ ] **Step 6: Build and verify**

Run: `go build ./cmd/gateway/`
Expected: compiles clean

- [ ] **Step 7: Commit**

```bash
git add internal/docker/client.go cmd/gateway/main.go
git commit -m "feat: gateway starts in degraded mode when Docker unavailable"
```

---

### Task 3: Add DockerMonitor for health checking and reconnect

**Files:**
- Create: `internal/docker/monitor.go`
- Test: `internal/docker/monitor_test.go`

- [ ] **Step 1: Write test for DockerMonitor**

Create `internal/docker/monitor_test.go`:
```go
package docker

import (
	"testing"
	"time"
)

func TestMonitor_InitiallyAvailable(t *testing.T) {
	m := NewMonitor(nil, false, nil) // nil client = unavailable
	if m.Available() {
		t.Error("should be unavailable with nil client")
	}
}

func TestMonitor_SetClient(t *testing.T) {
	m := NewMonitor(nil, false, nil)
	if m.Available() {
		t.Error("should start unavailable")
	}
	// Simulate reconnect by setting a non-nil client
	m.SetClient(&Client{})
	if !m.Available() {
		t.Error("should be available after SetClient")
	}
}

func TestMonitor_SetUnavailable(t *testing.T) {
	m := NewMonitor(&Client{}, false, nil)
	if !m.Available() {
		t.Error("should start available")
	}
	m.SetUnavailable()
	if m.Available() {
		t.Error("should be unavailable after SetUnavailable")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/docker/ -run TestMonitor -v`
Expected: FAIL — NewMonitor not defined

- [ ] **Step 3: Implement DockerMonitor**

Create `internal/docker/monitor.go`:
```go
package docker

import (
	"context"
	"sync"
	"time"

	"github.com/docker/docker/client"
)

// Monitor tracks Docker availability and reconnects in the background.
type Monitor struct {
	mu               sync.RWMutex
	dc               *Client
	available        bool
	autoRestore      bool
	onReconnect      func() // called when Docker comes back (infra restore)
	logger           interface{ Info(msg string, keyvals ...interface{}); Warn(msg string, keyvals ...interface{}) }
	stopCh           chan struct{}
}

// NewMonitor creates a Docker availability monitor. If dc is nil, starts
// in degraded mode. The onReconnect callback is called when Docker returns
// (only if autoRestore is true).
func NewMonitor(dc *Client, autoRestore bool, logger interface{ Info(msg string, keyvals ...interface{}); Warn(msg string, keyvals ...interface{}) }) *Monitor {
	return &Monitor{
		dc:          dc,
		available:   dc != nil,
		autoRestore: autoRestore,
		logger:      logger,
		stopCh:      make(chan struct{}),
	}
}

// Available returns whether Docker is currently reachable.
func (m *Monitor) Available() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.available
}

// Client returns the current Docker client (may be nil).
func (m *Monitor) Client() *Client {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.dc
}

// SetClient sets a new Docker client and marks Docker as available.
func (m *Monitor) SetClient(dc *Client) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.dc = dc
	m.available = true
}

// SetUnavailable marks Docker as unavailable.
func (m *Monitor) SetUnavailable() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.available = false
}

// SetOnReconnect sets the callback for Docker reconnection.
func (m *Monitor) SetOnReconnect(fn func()) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onReconnect = fn
}

// Start begins the background health check loop. Call Stop() to terminate.
func (m *Monitor) Start() {
	go m.loop()
}

// Stop terminates the background health check loop.
func (m *Monitor) Stop() {
	close(m.stopCh)
}

func (m *Monitor) loop() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-m.stopCh:
			return
		case <-ticker.C:
			m.check()
		}
	}
}

func (m *Monitor) check() {
	wasAvailable := m.Available()

	// Try to ping Docker
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		if wasAvailable {
			if m.logger != nil {
				m.logger.Warn("Docker connection lost")
			}
			m.SetUnavailable()
		}
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := cli.Ping(ctx); err != nil {
		if wasAvailable {
			if m.logger != nil {
				m.logger.Warn("Docker not responding", "err", err)
			}
			m.SetUnavailable()
		}
		return
	}

	// Docker is available
	if !wasAvailable {
		if m.logger != nil {
			m.logger.Info("Docker reconnected")
		}
		m.SetClient(&Client{cli: cli})

		m.mu.RLock()
		autoRestore := m.autoRestore
		onReconnect := m.onReconnect
		m.mu.RUnlock()

		if autoRestore && onReconnect != nil {
			if m.logger != nil {
				m.logger.Info("auto-restoring infrastructure (auto_restore_infra=true)")
			}
			onReconnect()
		}
	}
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/docker/ -run TestMonitor -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/docker/monitor.go internal/docker/monitor_test.go
git commit -m "feat: DockerMonitor for health checking and reconnect"
```

---

### Task 4: Wire DockerMonitor into gateway startup

**Files:**
- Modify: `cmd/gateway/main.go` — create Monitor, start loop, pass to handlers

- [ ] **Step 1: Create and start the monitor**

In `cmd/gateway/main.go`, after the `TryNewClient` block, add:
```go
dockerMon := docker.NewMonitor(dc, cfg.AutoRestoreInfra, logger)
dockerMon.Start()
defer dockerMon.Stop()
```

- [ ] **Step 2: Wire reconnect callback for auto-restore**

After the monitor is created, before the HTTP server:
```go
// Wire auto-restore: when Docker reconnects and auto_restore_infra is set,
// bring up infrastructure automatically.
if cfg.AutoRestoreInfra {
    // The onReconnect callback will be set after the infra handler is created.
    // For now, just note it — we'll wire it in the route options.
}
```

The actual wiring happens after `api.RegisterRoutesWithOptions` since we need the infra handler. Add after route registration:
```go
dockerMon.SetOnReconnect(func() {
    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
    defer cancel()
    // Re-create infra manager with the reconnected client
    newDC := dockerMon.Client()
    if newDC == nil {
        return
    }
    infra, err := orchestrate.NewInfra(cfg.Home, cfg.Version, newDC.RawClient(), logger, cfg.HMACKey)
    if err != nil {
        logger.Warn("auto-restore: failed to create infra manager", "err", err)
        return
    }
    infra.SourceDir = cfg.SourceDir
    infra.BuildID = cfg.BuildID
    infra.GatewayAddr = cfg.GatewayAddr
    infra.GatewayToken = cfg.Token
    infra.EgressToken = cfg.EgressToken
    if err := infra.EnsureRunning(ctx); err != nil {
        logger.Warn("auto-restore: infra up failed", "err", err)
    } else {
        logger.Info("auto-restore: infrastructure restored")
    }
})
```

- [ ] **Step 3: Pass monitor to route options**

Add `DockerMonitor` to `api.RouteOptions`:
```go
routeOpts.DockerMonitor = dockerMon
```

- [ ] **Step 4: Build and verify**

Run: `go build ./cmd/gateway/`
Expected: compiles (RouteOptions field added in Task 5)

- [ ] **Step 5: Commit**

```bash
git add cmd/gateway/main.go
git commit -m "feat: wire DockerMonitor into gateway startup"
```

---

### Task 5: Guard API handlers with Docker availability checks

**Files:**
- Modify: `internal/api/routes.go` — add DockerMonitor to RouteOptions and handler, guard Docker-dependent endpoints
- Modify: `internal/orchestrate/agent.go` — handle nil Docker client in List/Show

- [ ] **Step 1: Add DockerMonitor to RouteOptions and handler**

In `internal/api/routes.go`, add to `RouteOptions`:
```go
DockerMonitor *docker.Monitor
```

In `handler` struct:
```go
dockerMon *docker.Monitor
```

In `RegisterRoutesWithOptions`, wire it:
```go
if opts.DockerMonitor != nil {
    h.dockerMon = opts.DockerMonitor
}
```

- [ ] **Step 2: Add dockerRequired helper**

Add to `routes.go`:
```go
// dockerRequired returns true if Docker is available. If not, writes a 503
// response with a human-readable error and returns false.
func (h *handler) dockerRequired(w http.ResponseWriter) bool {
	if h.dockerMon != nil && !h.dockerMon.Available() {
		writeJSON(w, 503, map[string]string{
			"error": "Docker is not available. Container operations are unavailable.",
		})
		return false
	}
	return true
}
```

- [ ] **Step 3: Guard Docker-dependent handlers**

Add `if !h.dockerRequired(w) { return }` as the first line in:
- `infraUp`
- `infraDown`
- `infraRebuild`
- `infraReload`
- `startAgent`
- `stopAgent` / `haltAgent`
- `restartAgent`
- `resumeAgent`

- [ ] **Step 4: Add Docker status to infraStatus response**

In `infraStatus`, add to the response map:
```go
"docker": func() string {
    if h.dockerMon != nil && !h.dockerMon.Available() {
        return "unavailable"
    }
    return "available"
}(),
```

- [ ] **Step 5: Handle nil Docker client in agent List/Show**

In `internal/orchestrate/agent.go`, update `getRunningContainers` to return empty map if the Docker client is nil:
```go
func (am *AgentManager) getRunningContainers(ctx context.Context) map[string]containerInfo {
	if am.cli == nil {
		return make(map[string]containerInfo)
	}
	// ... existing code
}
```

This makes workspace/enforcer status default to `"stopped"` when Docker is unavailable. Update the status display logic to show `"unknown"` when Docker is down — this requires passing Docker availability into `loadAgentDetail` or checking it at the API layer.

- [ ] **Step 6: Build and verify**

Run: `go build ./cmd/gateway/`
Expected: compiles clean

- [ ] **Step 7: Commit**

```bash
git add internal/api/routes.go internal/orchestrate/agent.go
git commit -m "feat: guard Docker-dependent handlers with availability check"
```

---

### Task 6: Update CLI status display for degraded mode

**Files:**
- Modify: `internal/cli/commands.go` — show Docker status in status output
- Modify: `internal/apiclient/client.go` — add Docker field to InfraStatusResponse

- [ ] **Step 1: Add Docker field to InfraStatusResponse**

In `internal/apiclient/client.go`:
```go
Docker string `json:"docker,omitempty"`
```

- [ ] **Step 2: Update status display in CLI**

In `internal/cli/commands.go` `statusCmd`, after the web URL line:
```go
if infraResp.Docker == "unavailable" {
    fmt.Printf("  Docker:  %s\n", red.Render("unavailable ⚠"))
} 
```

When Docker is unavailable, update the infrastructure display to show `?` instead of colored dots:
```go
for _, ic := range infraResp.Components {
    icon := green.Render("●")
    if infraResp.Docker == "unavailable" {
        icon = dim.Render("?")
    } else if ic["health"] != "healthy" && ic["state"] != "running" {
        icon = red.Render("○")
    }
    // ... rest of display logic
}
```

- [ ] **Step 3: Build and verify**

Run: `go build ./cmd/gateway/`
Expected: compiles clean

- [ ] **Step 4: Commit**

```bash
git add internal/cli/commands.go internal/apiclient/client.go
git commit -m "feat: CLI shows Docker availability in status display"
```

---

### Task 7: Integration test and final commit

**Files:**
- All modified files

- [ ] **Step 1: Run full Go test suite**

Run: `go test ./...`
Expected: all pass

- [ ] **Step 2: Run enforcer tests**

Run: `cd images/enforcer && go test ./... && cd ../..`
Expected: all pass

- [ ] **Step 3: Run Python tests**

Run: `python3 -m pytest images/tests/ -x -q --timeout=30 --ignore=images/tests/test_realtime_comms_e2e.py --ignore=images/tests/test_comms_e2e.py`
Expected: all pass

- [ ] **Step 4: Manual smoke test (Docker available)**

Run: `make install && agency status`
Expected: normal output with gateway/web URLs, no Docker warning

- [ ] **Step 5: Manual smoke test (Docker unavailable)**

Stop Docker Desktop, then:
Run: `agency serve restart && agency status`
Expected: gateway starts, status shows `Docker: unavailable ⚠`, infra shows `?` markers

- [ ] **Step 6: Manual smoke test (Docker reconnect)**

Start Docker Desktop, wait 30s, then:
Run: `agency status`
Expected: Docker status clears, infra shows normal state (or auto-restores if configured)

- [ ] **Step 7: Update spec status**

Change `docs/specs/graceful-docker-degradation.md` status from "Approved" to "Implemented".

- [ ] **Step 8: Final commit**

```bash
git add -A
git commit -m "feat: graceful Docker degradation — gateway survives Docker outages"
git push origin main
```
