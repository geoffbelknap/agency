---
description: "When the gateway binary is rebuilt and restarted (make install), there's a ~3 second blackout. The MCP proxy (separat..."
---

# Gateway Restart Resilience

## Problem

When the gateway binary is rebuilt and restarted (`make install`), there's a ~3 second blackout. The MCP proxy (separate process) gets HTTP errors on tool calls, surfacing failures to Claude Code. The web UI's WebSocket connection drops, losing real-time updates until the user refreshes.

## Design

Two independent changes. No modifications to the gateway itself.

### 1. MCP Proxy Retry

The MCP proxy (`agency mcp-server`) is a separate process that forwards tool calls via HTTP to the gateway at `http://127.0.0.1:8200`. When the gateway is down, these calls fail with connection refused.

**Change:** Add retry with exponential backoff to the proxy's HTTP call path. On connection refused, connection reset, or HTTP 502/503:

- Retry schedule: 200ms, 400ms, 800ms, 1.6s, 3.2s, 6.4s, 12.8s, 25.6s
- Max total elapsed time: 30 seconds
- If all retries exhausted, return the error to Claude Code
- Retries are transparent — no protocol changes, no buffering

**Files:**
- `agency-gateway/internal/mcp/proxy.go` — add retry logic around the HTTP call in `CallTool`

### 2. Web UI WebSocket Auto-Reconnect

The web UI connects to `ws://localhost:8200/ws` for real-time updates. When the gateway restarts, the connection drops.

**Change:** Add reconnect logic to the WebSocket client:

- On `close` or `error`, reconnect with exponential backoff: 500ms, 1s, 2s, 4s, capped at 10s
- On successful reconnect, the hub automatically registers the new client — no re-subscription needed
- Show "Reconnecting..." indicator while disconnected
- Stop retrying after 5 minutes of continuous failure

**Files:**
- `agency-web/src/app/lib/socket.ts` (or equivalent) — add reconnect logic
- `agency-web/src/app/screens/Channels.tsx` — show reconnecting indicator

## Out of Scope

- Zero-downtime HTTP restart (socket handoff / exec) — not needed since clients can retry
- Reverse proxy in front of gateway — overkill
- MCP protocol changes — the proxy handles retry transparently
- Gateway shutdown sequence changes — current 10s graceful shutdown is fine

## Cross-Platform

Both changes are pure application logic (HTTP retry, WebSocket reconnect). No OS-specific APIs. Works identically on Linux, macOS, and Windows.
