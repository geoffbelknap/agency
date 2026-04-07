# Gateway Socket Proxy Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace all `host.docker.internal` container-to-gateway communication with a socat-based socket proxy on the Docker mediation network, fixing cross-platform networking on Linux, macOS, and WSL.

**Architecture:** A minimal socat container bridges `~/.agency/run/gateway.sock` to TCP `gateway:8200` on the mediation network. Credential resolution uses a separate socket (`gateway-cred.sock`) mounted only into egress. All `ExtraHosts` and `host.docker.internal` references are eliminated.

**Tech Stack:** Go (gateway, orchestration), Python (egress key resolver), Docker, socat, Alpine Linux

**Spec:** `docs/specs/gateway-socket-proxy.md`

---

## File Map

| File | Action | Responsibility |
|------|--------|---------------|
| `images/gateway-proxy/Dockerfile` | Create | socat proxy container image |
| `Makefile` | Modify | Add gateway-proxy to build targets |
| `cmd/gateway/main.go` | Modify | Split socket into two (proxy-safe + credential), chmod 0600 |
| `internal/api/routes.go` | Modify | Split RegisterSocketRoutes into two functions |
| `internal/orchestrate/infra.go` | Modify | Add ensureGatewayProxy(), remove ExtraHosts, update egress socket mount |
| `internal/orchestrate/enforcer.go` | Modify | GATEWAY_URL → gateway:8200, remove ExtraHosts |
| `images/egress/key_resolver.py` | Modify | Update socket path, remove HTTP fallback |

---

### Task 1: Create the gateway-proxy Docker image

**Files:**
- Create: `images/gateway-proxy/Dockerfile`
- Modify: `Makefile:14` (CORE_IMAGES list)

- [ ] **Step 1: Create the Dockerfile**

```dockerfile
FROM alpine:3.21
RUN apk add --no-cache socat
USER nobody
ENTRYPOINT ["socat", "TCP-LISTEN:8200,fork,reuseaddr", "UNIX-CONNECT:/run/gateway.sock"]
```

Write to `images/gateway-proxy/Dockerfile`.

- [ ] **Step 2: Add gateway-proxy to Makefile build targets**

In `Makefile`, line 14, add `gateway-proxy` to the CORE_IMAGES list:

```makefile
CORE_IMAGES = body enforcer comms knowledge intake egress workspace web-fetch gateway-proxy
```

- [ ] **Step 3: Build the image and verify**

Run: `make gateway-proxy`
Expected: Image `agency-gateway-proxy:latest` built successfully (~5MB).

Verify: `docker run --rm agency-gateway-proxy:latest socat -V | head -1`
Expected: socat version output.

- [ ] **Step 4: Commit**

```
git add images/gateway-proxy/Dockerfile Makefile
git commit -m "feat: add gateway-proxy Docker image (socat TCP-to-socket bridge)"
```

---

### Task 2: Split the gateway Unix socket into two

**Files:**
- Modify: `cmd/gateway/main.go:921-954` (socket creation)
- Modify: `internal/api/routes.go:45-66` (RegisterSocketRoutes)

- [ ] **Step 1: Create RegisterCredentialSocketRoutes in routes.go**

In `internal/api/routes.go`, after the existing `RegisterSocketRoutes` function (line 66), add a new function that registers only the credential resolution endpoint:

```go
// RegisterCredentialSocketRoutes registers the credential-only socket router.
// This socket is mounted exclusively by the egress container for credential
// resolution. It is NOT bridged to TCP — credentials never traverse a Docker network.
func RegisterCredentialSocketRoutes(r chi.Router, cfg *config.Config, dc *docker.Client, logger *log.Logger, opts RouteOptions) {
	h := newHandler(cfg, dc, logger, opts)

	r.Route("/api/v1", func(r chi.Router) {
		r.Get("/internal/credentials/resolve", h.resolveCredential)
	})
}
```

- [ ] **Step 2: Remove credential resolve from RegisterSocketRoutes**

In `internal/api/routes.go`, in `RegisterSocketRoutes` (line 61), remove the credential resolution endpoint:

Remove this line:
```go
		r.Get("/internal/credentials/resolve", h.resolveCredential)
```

The proxy-safe socket router should now register these endpoints only:
- `GET /api/v1/health`
- `POST /api/v1/agents/{name}/signal`
- `POST /api/v1/internal/llm`
- `GET /api/v1/infra/status`
- `GET /api/v1/channels`
- `GET /api/v1/channels/{name}/messages`
- `POST /api/v1/channels/{name}/messages`

- [ ] **Step 3: Create the credential socket in main.go**

In `cmd/gateway/main.go`, after the existing socket creation block (around line 933), add a second socket for credential resolution. Also change the existing socket permissions from 0666 to 0600.

Find the block starting at line 924:
```go
	sockDir := filepath.Join(cfg.Home, "run")
	os.MkdirAll(sockDir, 0755)
	sockPath := filepath.Join(sockDir, "gateway.sock")
	os.Remove(sockPath)                              // clean up stale socket
	os.Remove(filepath.Join(cfg.Home, "gateway.sock")) // clean up legacy location
	unixListener, err := net.Listen("unix", sockPath)
	if err != nil {
		logger.Warn("could not create Unix socket", "err", err)
	} else {
		os.Chmod(sockPath, 0666) // world-accessible — access controlled by bind mount, not file perms
```

Replace with:
```go
	sockDir := filepath.Join(cfg.Home, "run")
	os.MkdirAll(sockDir, 0755)

	// Proxy-safe socket — bridged to TCP by the gateway-proxy container.
	// Does NOT include credential resolution endpoints.
	sockPath := filepath.Join(sockDir, "gateway.sock")
	os.Remove(sockPath)
	os.Remove(filepath.Join(cfg.Home, "gateway.sock")) // clean up legacy location
	unixListener, err := net.Listen("unix", sockPath)
	if err != nil {
		logger.Warn("could not create Unix socket", "err", err)
	} else {
		os.Chmod(sockPath, 0600)
```

Then, after the existing socket server goroutine and defer block (around line 953), add the credential socket:

```go
	// Credential-only socket — mounted exclusively by egress for credential
	// resolution. Never bridged to TCP (ASK Tenet 7: credentials never
	// traverse a Docker network).
	credSockPath := filepath.Join(sockDir, "gateway-cred.sock")
	os.Remove(credSockPath)
	credListener, err := net.Listen("unix", credSockPath)
	if err != nil {
		logger.Warn("could not create credential socket", "err", err)
	} else {
		os.Chmod(credSockPath, 0600)
		credRouter := chi.NewRouter()
		credRouter.Use(chiMiddleware.Recoverer)
		api.RegisterCredentialSocketRoutes(credRouter, cfg, dc, logger, routeOpts)
		credServer := &http.Server{
			Handler:      credRouter,
			ReadTimeout:  30 * time.Second,
			WriteTimeout: 5 * time.Minute,
		}
		go func() {
			logger.Info("Credential socket listening", "path", credSockPath)
			if err := credServer.Serve(credListener); err != nil && err != http.ErrServerClosed {
				logger.Warn("credential socket error", "err", err)
			}
		}()
		defer func() {
			credServer.Close()
			os.Remove(credSockPath)
		}()
	}
```

- [ ] **Step 4: Build and verify**

Run: `go build ./cmd/gateway/`
Expected: Compiles without errors.

Run: `go test ./internal/api/... -run TestSocket -v` (if socket route tests exist)
Expected: PASS

- [ ] **Step 5: Commit**

```
git add cmd/gateway/main.go internal/api/routes.go
git commit -m "feat: split gateway socket into proxy-safe and credential-only sockets"
```

---

### Task 3: Add ensureGatewayProxy to infra orchestration

**Files:**
- Modify: `internal/orchestrate/infra.go:196-208` (startup order)
- Modify: `internal/orchestrate/infra.go` (add ensureGatewayProxy function)

- [ ] **Step 1: Add the ensureGatewayProxy function**

In `internal/orchestrate/infra.go`, before `ensureEgress()` (line 449), add:

```go
func (inf *Infra) ensureGatewayProxy(ctx context.Context) error {
	if err := images.Resolve(ctx, inf.cli, "gateway-proxy", inf.Version, inf.SourceDir, inf.BuildID, inf.log); err != nil {
		return fmt.Errorf("resolve gateway-proxy image: %w", err)
	}
	name := containerName("gateway-proxy")
	if inf.isRunning(ctx, name) && inf.isCurrentBuild(ctx, name) && inf.isHealthyOrNoCheck(ctx, name) {
		return nil
	}
	_ = inf.stopAndRemove(ctx, name, stopTimeoutFor("gateway-proxy"))

	runDir := filepath.Join(inf.Home, "run")

	hc := containers.HostConfigDefaults(containers.RoleInfra)
	hc.NetworkMode = container.NetworkMode(mediationNet)
	hc.ReadonlyRootfs = true
	hc.Binds = []string{
		runDir + ":/run:ro",
	}
	hc.Resources.Memory = 16 * 1024 * 1024      // 16MB
	hc.Resources.NanoCPUs = 250_000_000          // 0.25 CPU
	pidsLimit := int64(32)
	hc.Resources.PidsLimit = &pidsLimit

	if _, err := containers.CreateAndStart(ctx, inf.cli,
		name,
		&container.Config{
			Image:    defaultImages["gateway-proxy"],
			Hostname: "gateway-proxy",
			Labels: map[string]string{
				"agency.managed":      "true",
				"agency.role":         "infra",
				"agency.component":    "gateway-proxy",
				"agency.build.id":     images.ImageBuildLabel(ctx, inf.cli, defaultImages["gateway-proxy"]),
				"agency.build.gateway": inf.BuildID,
			},
			Healthcheck: defaultHealthChecks["gateway-proxy"],
		},
		hc, nil,
	); err != nil {
		return err
	}

	// Ensure alias "gateway" is set so containers can reach it via http://gateway:8200
	inf.connectIfNeeded(ctx, name, mediationNet, []string{"gateway"})

	if err := inf.waitRunning(ctx, name, 10*time.Second); err != nil {
		return err
	}
	return inf.waitHealthy(ctx, name, 15*time.Second)
}
```

- [ ] **Step 2: Add defaultImages and health check entries**

In `internal/orchestrate/infra.go`, add to `defaultImages` map (around line 44):

```go
	"gateway-proxy": "agency-gateway-proxy:latest",
```

Add to `defaultHealthChecks` map (around line 48):

```go
	"gateway-proxy": {
		Test:        []string{"CMD-SHELL", "socat -T1 TCP:127.0.0.1:8200 UNIX-CONNECT:/run/gateway.sock"},
		Interval:    5 * time.Second,
		Timeout:     3 * time.Second,
		StartPeriod: 3 * time.Second,
		Retries:     3,
	},
```

- [ ] **Step 3: Add gateway-proxy to startup sequence**

In `internal/orchestrate/infra.go`, find the `components` slice (around line 196-208). Add `gateway-proxy` as the first component so it starts before everything else:

```go
	components := []struct {
		name   string
		desc   string
		ensure func(context.Context) error
	}{
		{"gateway-proxy", "Starting gateway proxy", inf.ensureGatewayProxy},
		{"egress", "Starting egress proxy", inf.ensureEgress},
		// ... rest unchanged
	}
```

Note: `ensureGatewayProxy` must complete before other components start if they depend on `gateway:8200`. If the current startup is parallel, ensure gateway-proxy is in a serial pre-phase or that other containers handle gateway-proxy not being ready (retry logic).

- [ ] **Step 4: Build and verify**

Run: `go build ./cmd/gateway/`
Expected: Compiles without errors.

- [ ] **Step 5: Commit**

```
git add internal/orchestrate/infra.go
git commit -m "feat: add ensureGatewayProxy to infra boot sequence"
```

---

### Task 4: Remove ExtraHosts and update GATEWAY_URL in enforcer

**Files:**
- Modify: `internal/orchestrate/enforcer.go:110,127,173`

- [ ] **Step 1: Change GATEWAY_URL default**

In `internal/orchestrate/enforcer.go`, line 110, change:

```go
		"GATEWAY_URL":        "http://host.docker.internal:8200",
```

to:

```go
		"GATEWAY_URL":        "http://gateway:8200",
```

- [ ] **Step 2: Change GATEWAY_URL config override**

In `internal/orchestrate/enforcer.go`, line 127, change:

```go
				env["GATEWAY_URL"] = "http://host.docker.internal:" + cf.GatewayAddr[idx+1:]
```

to:

```go
				env["GATEWAY_URL"] = "http://gateway:8200"
```

The gateway port is always 8200 on the proxy — the proxy bridges to whatever port the gateway listens on via the socket. The config override is no longer needed.

- [ ] **Step 3: Remove ExtraHosts from enforcer**

In `internal/orchestrate/enforcer.go`, line 173, remove or comment out:

```go
	enforcerHostConfig.ExtraHosts = []string{"host.docker.internal:host-gateway"}
```

- [ ] **Step 4: Build and verify**

Run: `go build ./cmd/gateway/`
Expected: Compiles without errors.

- [ ] **Step 5: Commit**

```
git add internal/orchestrate/enforcer.go
git commit -m "feat: enforcer uses gateway:8200 via socket proxy, remove ExtraHosts"
```

---

### Task 5: Remove ExtraHosts and update gateway refs in infra containers

**Files:**
- Modify: `internal/orchestrate/infra.go:502-514` (egress)
- Modify: `internal/orchestrate/infra.go:626-654` (knowledge)
- Modify: `internal/orchestrate/infra.go:565` (comms)
- Modify: `internal/orchestrate/infra.go:738` (intake)

- [ ] **Step 1: Update egress — change socket mount and remove ExtraHosts**

In `internal/orchestrate/infra.go`, find the egress socket mount block (around lines 502-514).

Change the socket mount from `gateway.sock` to `gateway-cred.sock`. The existing block:

```go
	if fileExists(filepath.Join(runDir, "gateway.sock")) {
		binds = append(binds, runDir+":/app/gateway-run:rw")
		env["GATEWAY_SOCKET"] = "/app/gateway-run/gateway.sock"
```

Replace with:

```go
	credSockPath := filepath.Join(runDir, "gateway-cred.sock")
	if fileExists(credSockPath) {
		binds = append(binds, credSockPath+":/app/gateway-cred.sock:ro")
		env["GATEWAY_SOCKET"] = "/app/gateway-cred.sock"
```

Add the proxy-based GATEWAY_URL for non-sensitive operations:

```go
	env["GATEWAY_URL"] = "http://gateway:8200"
```

Remove the HTTP fallback block that sets `GATEWAY_URL` to `host.docker.internal` (around lines 506-509).

Remove ExtraHosts at line 514:
```go
	hc.ExtraHosts = []string{"host.docker.internal:host-gateway"}
```

- [ ] **Step 2: Update knowledge — use proxy, remove socket mount and ExtraHosts**

In `internal/orchestrate/infra.go`, find the knowledge gateway block (around lines 626-637). Replace the socket/HTTP fallback logic:

```go
	sockDir := filepath.Join(inf.Home, "run")
	sockPath := filepath.Join(sockDir, "gateway.sock")
	if fileExists(sockPath) {
		env["AGENCY_GATEWAY_URL"] = "http+unix:///run/agency/gateway.sock"
		binds = append(binds, sockDir+":/run/agency:ro")
```

Replace with:

```go
	env["AGENCY_GATEWAY_URL"] = "http://gateway:8200"
```

Remove the `else` fallback block that sets `AGENCY_GATEWAY_URL` to `host.docker.internal`.

Remove the socket bind mount (the `binds = append` line for `/run/agency:ro`).

Remove ExtraHosts at line 654:
```go
	hc.ExtraHosts = []string{"host.docker.internal:host-gateway"}
```

- [ ] **Step 3: Remove ExtraHosts from comms**

In `internal/orchestrate/infra.go`, line 565, remove:
```go
	hc.ExtraHosts = []string{"host.docker.internal:host-gateway"}
```

- [ ] **Step 4: Remove ExtraHosts from intake**

In `internal/orchestrate/infra.go`, line 738, remove:
```go
	hc.ExtraHosts = []string{"host.docker.internal:host-gateway"}
```

- [ ] **Step 5: Build and verify**

Run: `go build ./cmd/gateway/`
Expected: Compiles without errors.

- [ ] **Step 6: Commit**

```
git add internal/orchestrate/infra.go
git commit -m "feat: remove ExtraHosts from all infra containers, use gateway:8200 proxy"
```

---

### Task 6: Update egress key_resolver.py

**Files:**
- Modify: `images/egress/key_resolver.py:31-33,44-48,69-103`

- [ ] **Step 1: Update SocketKeyResolver socket path**

In `images/egress/key_resolver.py`, the `SocketKeyResolver.__init__` (around line 31-33) takes a socket path. This is set from the `GATEWAY_SOCKET` env var. No change needed to the class itself — the env var was updated in Task 5 to point to `/app/gateway-cred.sock`.

Verify the constructor uses the passed path:
```python
def __init__(self, socket_path: str):
    self._socket_path = socket_path
```

This is correct — the path comes from env var, updated in Task 5.

- [ ] **Step 2: Remove HTTPKeyResolver class**

In `images/egress/key_resolver.py`, delete the `HTTPKeyResolver` class (around lines 69-103). This was the `host.docker.internal` HTTP fallback. It is no longer needed — all non-credential gateway communication goes through `http://gateway:8200` and credential resolution uses the socket.

- [ ] **Step 3: Remove any fallback logic that creates HTTPKeyResolver**

Search the egress codebase for where `HTTPKeyResolver` is instantiated and remove the fallback. This is likely in the main egress setup code. Replace with socket-only resolution:

```python
# Credential resolution — socket only, never HTTP
socket_path = os.environ.get("GATEWAY_SOCKET", "/app/gateway-cred.sock")
key_resolver = SocketKeyResolver(socket_path)
```

- [ ] **Step 4: Verify egress image builds**

Run: `make egress`
Expected: Image builds without errors.

- [ ] **Step 5: Commit**

```
git add images/egress/key_resolver.py
git commit -m "feat: egress uses credential socket only, remove HTTP fallback"
```

---

### Task 7: Integration test — full infra up

- [ ] **Step 1: Rebuild gateway with all changes**

Run: `make install`
Expected: Gateway binary installed, daemon restarted.

- [ ] **Step 2: Rebuild all images**

Run: `make images && make web`
Expected: All images build including `agency-gateway-proxy`.

- [ ] **Step 3: Tear down and restart infrastructure**

Run: `agency infra down && agency infra up`
Expected: All services start successfully, including `gateway-proxy`.

- [ ] **Step 4: Verify gateway-proxy is running**

Run: `docker ps --filter "label=agency.component=gateway-proxy" --format '{{.Names}} {{.Status}}'`
Expected: `agency-infra-gateway-proxy Up ... (healthy)`

- [ ] **Step 5: Verify no ExtraHosts on any container**

Run: `docker inspect --format '{{.Name}} {{.HostConfig.ExtraHosts}}' $(docker ps -q --filter "label=agency.managed=true") | grep -v "web"`
Expected: Every container shows `[]` for ExtraHosts (web is excluded — it uses host networking).

- [ ] **Step 6: Verify gateway proxy reaches the gateway**

Run: `docker exec agency-infra-gateway-proxy socat -T1 TCP:127.0.0.1:8200 UNIX-CONNECT:/run/gateway.sock`
Expected: Connects and disconnects cleanly.

- [ ] **Step 7: Verify enforcer can reach gateway via proxy**

Run: `docker exec agency-infra-gateway-proxy wget -q -O- http://127.0.0.1:8200/api/v1/health`
Expected: Health response from gateway.

Find an enforcer container name:
Run: `docker ps --filter "label=agency.component=enforcer" --format '{{.Names}}' | head -1`

Then test from inside the enforcer:
Run: `docker exec <enforcer-name> wget -q -O- http://gateway:8200/api/v1/health`
Expected: Health response from gateway.

- [ ] **Step 8: Verify credential resolution is NOT available via proxy**

Run: `docker exec <enforcer-name> wget -q -O- "http://gateway:8200/api/v1/internal/credentials/resolve?name=test" 2>&1`
Expected: 404 or 405 (endpoint not registered on proxy-safe socket).

- [ ] **Step 9: Verify agents can respond to messages**

Send a test message to an agent via the web UI or CLI:
Run: `agency comms send dm-<agent-name> "hello, test message"`
Expected: Agent processes the message and responds (LLM call succeeds through enforcer → gateway proxy → gateway → egress → Anthropic).

- [ ] **Step 10: Verify two sockets exist**

Run: `ls -la ~/.agency/run/`
Expected: Both `gateway.sock` and `gateway-cred.sock` present with mode `0600`.

- [ ] **Step 11: Commit any fixes**

If any issues were found and fixed during integration testing, commit them:

```
git add -A
git commit -m "fix: integration test fixes for gateway socket proxy"
```

---

### Task 8: Clean up gateway config.yaml

- [ ] **Step 1: Remove gateway_addr override if set to 0.0.0.0**

Check `~/.agency/config.yaml` for any `gateway_addr: "0.0.0.0:8200"` that was set during debugging. Reset to default:

```yaml
gateway_addr: "127.0.0.1:8200"
```

The gateway no longer needs to listen on anything other than loopback — all container traffic goes through the socket proxy.

- [ ] **Step 2: Restart gateway**

Run: `agency serve restart`
Expected: Daemon restarted, listening on `127.0.0.1:8200`.

- [ ] **Step 3: Verify containers still work**

Run: `agency infra status`
Expected: All services healthy.
