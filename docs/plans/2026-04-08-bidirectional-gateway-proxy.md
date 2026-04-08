# Bidirectional Gateway Proxy Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the gateway-proxy bidirectional so the host gateway can reach containers on internal Docker networks — fixing macOS Docker Desktop compatibility without changing any service client code.

**Architecture:** The proxy already bridges container→gateway (`TCP:8200 → UNIX:/run/gateway.sock`). Add reverse socat bridges for gateway→container (`host:PORT → service:8080`). Connect the proxy to the `agency-operator` network (non-internal) so port bindings publish to the host on macOS. The existing Go code already calls `localhost:8202/8204/8205` — it just works.

**Tech Stack:** socat (already in proxy image), shell entrypoint script

---

## What Changes

| File | Change |
|---|---|
| `images/gateway-proxy/entrypoint.sh` | Create — multi-socat entrypoint |
| `images/gateway-proxy/Dockerfile` | Modify — use entrypoint script |
| `internal/orchestrate/infra.go` | Modify — add port bindings and operator network to proxy |
| `internal/docker/client.go` | Modify — remove container IP lookup, use localhost:8202 only |
| `internal/orchestrate/infra.go` | Modify — remove `commsViaSocket`, simplify `ensureSystemChannels` |
| `test_e2e.sh` | Modify — fix `grep -P` for macOS |
| `CLAUDE.md` (agency + workspace) | Modify — document proxy, `-q` rule |

## What Does NOT Change

All service client code — `comms/client.go`, `knowledge/proxy.go`, `ws/comms_relay.go`, `api/events/handlers_intake.go`, `api/hub/handlers_hub.go`, `orchestrate/hub_health.go`. They already use `localhost:PORT`. That keeps working.

---

### Task 1: Bidirectional Proxy

**Files:**
- Create: `images/gateway-proxy/entrypoint.sh`
- Modify: `images/gateway-proxy/Dockerfile`

- [ ] **Step 1: Create the entrypoint script**

```bash
#!/bin/sh
# Bidirectional gateway proxy.
# Direction 1 (container→gateway): TCP:8200 → UNIX:/run/gateway.sock
# Direction 2 (gateway→container): TCP:PORT → TCP:service:8080
set -e

# Direction 1: container→gateway (existing behavior)
socat TCP-LISTEN:8200,fork,reuseaddr UNIX-CONNECT:/run/gateway.sock &

# Direction 2: gateway→services
socat TCP-LISTEN:8202,fork,reuseaddr TCP:comms:8080 &
socat TCP-LISTEN:8204,fork,reuseaddr TCP:knowledge:8080 &
socat TCP-LISTEN:8205,fork,reuseaddr TCP:intake:8080 &

echo "gateway-proxy: all bridges started"
wait -n
echo "gateway-proxy: a bridge process exited, shutting down"
exit 1
```

- [ ] **Step 2: Update the Dockerfile**

```dockerfile
FROM alpine:3.21
RUN apk add --no-cache socat
COPY entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh
USER nobody
ENTRYPOINT ["/entrypoint.sh"]
```

- [ ] **Step 3: Commit**

```bash
git add images/gateway-proxy/
git commit -m "feat: make gateway-proxy bidirectional with reverse service bridges"
```

---

### Task 2: Proxy Container Config

**Files:**
- Modify: `internal/orchestrate/infra.go` (~line 524-570, `ensureGatewayProxy`)

- [ ] **Step 1: Add port bindings and operator network**

Add port bindings for the reverse bridges. Connect to `agency-operator` (non-internal) so ports publish to the host on macOS:

```go
hc.PortBindings = nat.PortMap{
	"8202/tcp": []nat.PortBinding{{HostIP: "127.0.0.1", HostPort: "8202"}},
	"8204/tcp": []nat.PortBinding{{HostIP: "127.0.0.1", HostPort: "8204"}},
	"8205/tcp": []nat.PortBinding{{HostIP: "127.0.0.1", HostPort: "8205"}},
}
```

Connect to operator network in addition to gateway network:

```go
netCfg := &network.NetworkingConfig{
	EndpointsConfig: map[string]*network.EndpointSettings{
		gatewayNet:  {Aliases: []string{"gateway"}},
		operatorNet: {},
	},
}
```

- [ ] **Step 2: Build and verify ports publish**

```bash
go build -o agency ./cmd/gateway/
make gateway-proxy
docker rm -f agency-infra-gateway-proxy
agency -q infra up
curl -sf http://localhost:8202/health
curl -sf http://localhost:8204/health
```

Expected: Health responses from comms and knowledge

- [ ] **Step 3: Commit**

```bash
git add internal/orchestrate/infra.go
git commit -m "feat: publish reverse proxy ports for gateway→container traffic"
```

---

### Task 3: Remove Dead Code

**Files:**
- Modify: `internal/docker/client.go` (~line 208-230)
- Modify: `internal/orchestrate/infra.go` (~line 1374-1409)

- [ ] **Step 1: Remove container IP lookup from CommsRequest**

In `docker/client.go`, delete `commsURL()` and `commsHTTPClient()`. Update `CommsRequest` to use `http://localhost:8202` directly (the proxy handles routing).

- [ ] **Step 2: Remove commsViaSocket from infra.go**

Delete the `commsViaSocket` function. Simplify `ensureSystemChannels` to use only `inf.Comms.CommsRequest()` — one path, no fallback.

- [ ] **Step 3: Remove comms host port binding**

The comms container no longer needs its own port binding (line ~714). The proxy publishes 8202 instead. Verify knowledge and intake port bindings can also be removed since the proxy handles them.

- [ ] **Step 4: Build and test**

```bash
go build -o agency ./cmd/gateway/
go test ./...
```

- [ ] **Step 5: Commit**

```bash
git add internal/docker/client.go internal/orchestrate/infra.go
git commit -m "fix: remove container IP fallbacks, single path through proxy"
```

---

### Task 4: E2E Test Fixes

**Files:**
- Modify: `test_e2e.sh`

- [ ] **Step 1: Fix grep -P for macOS**

Replace all 4 instances of `grep -oP 'pattern'` with POSIX-compatible `sed`:

```bash
# Before:
USAGE_ERRORS=$(echo "$USAGE" | grep -oP 'Errors:\s+\K\d+' || echo "0")

# After:
USAGE_ERRORS=$(echo "$USAGE" | sed -n 's/.*Errors:[[:space:]]*\([0-9]*\).*/\1/p')
[ -z "$USAGE_ERRORS" ] && USAGE_ERRORS=0
```

- [ ] **Step 2: Add bidirectional proxy test to Phase 28**

```bash
# gateway→comms via proxy
COMMS_HEALTH=$(curl -sf http://localhost:8202/health 2>&1) || COMMS_HEALTH=""
if echo "$COMMS_HEALTH" | grep -q '"status"'; then
    pass "gateway→comms proxy bridge works"
else
    fail "gateway→comms proxy bridge failed"
fi
```

- [ ] **Step 3: Run E2E**

```bash
make all && ./test_e2e.sh
```

- [ ] **Step 4: Commit**

```bash
git add test_e2e.sh
git commit -m "fix: macOS compat for e2e tests, add bidirectional proxy test"
```

---

### Task 5: Documentation

**Files:**
- Modify: `CLAUDE.md` (agency repo)
- Modify: `CLAUDE.md` (workspace root)

- [ ] **Step 1: Update agency CLAUDE.md container topology**

Add to the gateway-proxy documentation:

```
The proxy is bidirectional:
- Container→gateway: TCP:8200 → UNIX:~/.agency/run/gateway.sock
- Gateway→comms: localhost:8202 → proxy:8202 → comms:8080
- Gateway→knowledge: localhost:8204 → proxy:8204 → knowledge:8080
- Gateway→intake: localhost:8205 → proxy:8205 → intake:8080
```

Add Docker Management Principle #11:
```
11. **Never call containers directly from the gateway** — use localhost ports routed through the gateway-proxy. Container IPs are not routable from the host on macOS Docker Desktop.
```

- [ ] **Step 2: Update workspace CLAUDE.md**

Add Engineering Rule #4:
```
4. **Always use `-q` flag when running agency CLI commands** in scripts and Claude Code sessions. Spinner output wastes tokens.
```

- [ ] **Step 3: Commit**

```bash
git add CLAUDE.md ../../CLAUDE.md
git commit -m "docs: bidirectional proxy architecture, -q flag rule"
```
