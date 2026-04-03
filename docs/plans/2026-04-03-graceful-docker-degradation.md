# Graceful Docker Degradation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Gateway starts and serves the REST API even when Docker is unavailable, degrading container operations gracefully.

**Architecture:** Reactive detection — no polling. A `DockerStatus` wrapper catches Docker client errors as they occur and flips an availability flag. Success after failure flips it back. All Docker-dependent API handlers check the flag and return 503 when unavailable. Config gains `auto_restore_infra` for optional automatic infra restoration on reconnect.

**Tech Stack:** Go, Docker client SDK, chi router

**Spec:** `docs/specs/graceful-docker-degradation.md`

---

### Task 1: Add `auto_restore_infra` config field

**Files:**
- Modify: `internal/config/config.go`

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

### Task 2: Add DockerStatus reactive wrapper

**Files:**
- Create: `internal/docker/status.go`
- Create: `internal/docker/status_test.go`

- [ ] **Step 1: Write tests**

Create `internal/docker/status_test.go`:
```go
package docker

import (
	"errors"
	"testing"
)

func TestStatus_InitiallyAvailable(t *testing.T) {
	s := NewStatus(&Client{})
	if !s.Available() {
		t.Error("should be available with non-nil client")
	}
}

func TestStatus_InitiallyUnavailable(t *testing.T) {
	s := NewStatus(nil)
	if s.Available() {
		t.Error("should be unavailable with nil client")
	}
}

func TestStatus_DetectsFailure(t *testing.T) {
	s := NewStatus(&Client{})
	s.RecordError(errors.New("connection refused"))
	if s.Available() {
		t.Error("should be unavailable after Docker error")
	}
}

func TestStatus_NonDockerErrorDoesNotFlip(t *testing.T) {
	s := NewStatus(&Client{})
	s.RecordError(errors.New("container not found"))
	if !s.Available() {
		t.Error("non-Docker errors should not flip availability")
	}
}

func TestStatus_RecoveryOnSuccess(t *testing.T) {
	s := NewStatus(nil) // starts unavailable
	s.RecordSuccess()
	if !s.Available() {
		t.Error("should recover on success")
	}
}

func TestStatus_ReconnectCallbackFires(t *testing.T) {
	fired := false
	s := NewStatus(nil)
	s.OnReconnect = func() { fired = true }
	s.RecordSuccess() // transition: unavailable -> available
	if !fired {
		t.Error("OnReconnect should fire on recovery")
	}
}

func TestStatus_ReconnectCallbackDoesNotFireWhenAlreadyAvailable(t *testing.T) {
	fired := false
	s := NewStatus(&Client{})
	s.OnReconnect = func() { fired = true }
	s.RecordSuccess() // already available -> no transition
	if fired {
		t.Error("OnReconnect should not fire when already available")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/docker/ -run TestStatus -v`
Expected: FAIL — NewStatus not defined

- [ ] **Step 3: Implement DockerStatus**

Create `internal/docker/status.go`:
```go
package docker

import (
	"strings"
	"sync/atomic"
)

// Status tracks Docker availability reactively. No polling — availability
// is determined by observing successes and failures on Docker API calls.
type Status struct {
	available   atomic.Bool
	OnReconnect func() // called once when Docker transitions from unavailable to available
}

// NewStatus creates a Docker status tracker. If dc is nil, starts unavailable.
func NewStatus(dc *Client) *Status {
	s := &Status{}
	s.available.Store(dc != nil)
	return s
}

// Available returns whether Docker is currently considered reachable.
func (s *Status) Available() bool {
	return s.available.Load()
}

// RecordSuccess marks Docker as available. If transitioning from unavailable,
// fires the OnReconnect callback (if set).
func (s *Status) RecordSuccess() {
	was := s.available.Swap(true)
	if !was && s.OnReconnect != nil {
		s.OnReconnect()
	}
}

// RecordError checks if the error indicates Docker itself is unavailable
// (connection refused, not responding, etc.) vs a normal operational error
// (container not found, image pull failed). Only Docker-level failures
// flip the availability flag.
func (s *Status) RecordError(err error) {
	if err == nil {
		return
	}
	if isDockerUnavailable(err) {
		s.available.Store(false)
	}
}

// isDockerUnavailable returns true if the error indicates the Docker daemon
// itself is unreachable, as opposed to a normal API error.
func isDockerUnavailable(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	patterns := []string{
		"Cannot connect to the Docker daemon",
		"connection refused",
		"no such host",
		"i/o timeout",
		"context deadline exceeded",
		"Docker not responding",
		"dial unix",
		"docker: ",
	}
	for _, p := range patterns {
		if strings.Contains(msg, p) {
			return true
		}
	}
	return false
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/docker/ -run TestStatus -v`
Expected: PASS (all 7 tests)

- [ ] **Step 5: Commit**

```bash
git add internal/docker/status.go internal/docker/status_test.go
git commit -m "feat: reactive DockerStatus tracker — no polling"
```

---

### Task 3: Make Docker client optional at startup

**Files:**
- Modify: `internal/docker/client.go` — add TryNewClient
- Modify: `cmd/gateway/main.go` — use TryNewClient, create Status, guard Docker-dependent startup

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

- [ ] **Step 2: Update main.go — replace fatal Docker init with TryNewClient**

Replace:
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
dockerStatus := docker.NewStatus(dc)
```

- [ ] **Step 3: Guard reconciliation and other Docker-dependent startup code**

Wrap with `if dc != nil { ... }`:
- Reconciliation block
- Workspace watcher
- Any other `dc.` usage before the HTTP server starts

- [ ] **Step 4: Wire auto-restore callback**

After route registration:
```go
if cfg.AutoRestoreInfra {
    dockerStatus.OnReconnect = func() {
        logger.Info("Docker reconnected — auto-restoring infrastructure")
        ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
        defer cancel()
        newDC := docker.TryNewClient(logger)
        if newDC == nil {
            logger.Warn("auto-restore: Docker reconnect succeeded but client creation failed")
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
    }
}
```

- [ ] **Step 5: Build and verify**

Run: `go build ./cmd/gateway/`
Expected: compiles clean

- [ ] **Step 6: Commit**

```bash
git add internal/docker/client.go cmd/gateway/main.go
git commit -m "feat: gateway starts in degraded mode when Docker unavailable"
```

---

### Task 4: Guard API handlers with Docker availability checks

**Files:**
- Modify: `internal/api/routes.go` — add DockerStatus to handler, guard endpoints
- Modify: `internal/orchestrate/agent.go` — handle nil Docker client

- [ ] **Step 1: Add DockerStatus to RouteOptions and handler**

In `internal/api/routes.go`, add to `RouteOptions`:
```go
DockerStatus *docker.Status
```

In `handler` struct:
```go
dockerStatus *docker.Status
```

In `RegisterRoutesWithOptions`, wire it:
```go
if opts.DockerStatus != nil {
    h.dockerStatus = opts.DockerStatus
}
```

- [ ] **Step 2: Add dockerRequired helper**

```go
func (h *handler) dockerRequired(w http.ResponseWriter) bool {
	if h.dockerStatus != nil && !h.dockerStatus.Available() {
		writeJSON(w, 503, map[string]string{
			"error": "Docker is not available. Container operations are unavailable.",
		})
		return false
	}
	return true
}
```

- [ ] **Step 3: Guard Docker-dependent handlers**

Add `if !h.dockerRequired(w) { return }` as first line in:
- `infraUp`, `infraDown`, `infraRebuild`, `infraReload`
- `startAgent`, `haltAgent`, `restartAgent`, `resumeAgent`

- [ ] **Step 4: Add Docker status to infraStatus response**

In `infraStatus`, add to the response map:
```go
"docker": func() string {
    if h.dockerStatus != nil && !h.dockerStatus.Available() {
        return "unavailable"
    }
    return "available"
}(),
```

- [ ] **Step 5: Handle nil Docker client in AgentManager**

In `internal/orchestrate/agent.go`, update `getRunningContainers` to return empty map if cli is nil:
```go
func (am *AgentManager) getRunningContainers(ctx context.Context) map[string]containerInfo {
	if am.cli == nil {
		return make(map[string]containerInfo)
	}
	// ... existing code
}
```

- [ ] **Step 6: Wire RecordError/RecordSuccess into Docker-dependent handlers**

Where handlers call Docker and get errors back, record the outcome:
```go
err := h.infra.EnsureRunning(ctx)
if err != nil {
    if h.dockerStatus != nil {
        h.dockerStatus.RecordError(err)
    }
    writeJSON(w, 500, map[string]string{"error": err.Error()})
    return
}
if h.dockerStatus != nil {
    h.dockerStatus.RecordSuccess()
}
```

Apply this pattern to `infraUp`, `infraDown`, `startAgent`, `restartAgent`, and `infraStatus` (the most frequently called Docker-touching endpoints).

- [ ] **Step 7: Build and verify**

Run: `go build ./cmd/gateway/`
Expected: compiles clean

- [ ] **Step 8: Commit**

```bash
git add internal/api/routes.go internal/orchestrate/agent.go
git commit -m "feat: guard Docker-dependent handlers, reactive status tracking"
```

---

### Task 5: Update CLI status display for degraded mode

**Files:**
- Modify: `internal/apiclient/client.go` — add Docker field
- Modify: `internal/cli/commands.go` — show Docker status

- [ ] **Step 1: Add Docker field to InfraStatusResponse**

In `internal/apiclient/client.go`:
```go
Docker string `json:"docker,omitempty"`
```

- [ ] **Step 2: Update CLI status display**

In `internal/cli/commands.go` `statusCmd`, after the web URL line:
```go
if infraResp.Docker == "unavailable" {
    fmt.Printf("  Docker:  %s\n", red.Render("unavailable"))
}
```

Update infrastructure component display:
```go
for _, ic := range infraResp.Components {
    icon := green.Render("●")
    if infraResp.Docker == "unavailable" {
        icon = dim.Render("?")
    } else if ic["health"] != "healthy" && ic["state"] != "running" {
        icon = red.Render("○")
    }
    // ... rest of display logic unchanged
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

### Task 6: Tests and smoke test

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
Expected: normal output, no Docker warning

- [ ] **Step 5: Manual smoke test (Docker unavailable)**

Quit Docker Desktop, then:
Run: `agency serve stop && agency serve restart && agency status`
Expected: gateway starts, status shows `Docker: unavailable`, infra shows `?` markers

- [ ] **Step 6: Manual smoke test (Docker reconnect)**

Start Docker Desktop, then run `agency infra up && agency status`
Expected: Docker status clears on successful infra operation, infra shows normal state

- [ ] **Step 7: Update spec status and commit**

```bash
# Update spec status to Implemented
git add docs/specs/graceful-docker-degradation.md docs/plans/2026-04-03-graceful-docker-degradation.md
git commit -m "feat: graceful Docker degradation — gateway survives Docker outages"
git push origin main
```
