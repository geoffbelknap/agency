**Date:** 2026-03-21
**Status:** Implemented (all 3 phases complete)
**Scope:** MCP server architecture, gateway API surface, hot-reload developer experience
**Last updated:** 2026-04-01

## Problem

The MCP server (`agency mcp-server`) is a long-lived stdio process started by Claude Code. It contained tool handlers compiled into the gateway binary, each with its own argument marshaling, REST client call, and response formatting logic. This creates two problems:

1. **Stale code after rebuild.** (Resolved.) The MCP server is now a stateless proxy. Gateway restarts update tool behavior for the running MCP server automatically.

2. **Duplicated routing and formatting.** (Resolved.) The old handler layer has been deleted. Formatting lives in the gateway's MCP call handler (`internal/api/mcp_call.go`) and format helpers (`internal/api/mcp_format.go`). Tool definitions are registered in `internal/api/mcp_register.go` alongside REST routes — single source of truth.

3. **Tool definition drift.** (Resolved.) Tool definitions are served by `GET /api/v1/mcp/tools` from the gateway. The MCP server has no tool registry.

## Goals

1. The MCP server becomes a stateless stdio-to-REST proxy with no business logic, no formatting, and no tool-specific code.
2. Tool definitions (name, description, inputSchema) are served by the gateway REST API, making the gateway the single source of truth.
3. Tool invocations are proxied to the gateway REST API, which returns MCP-ready content blocks.
4. Rebuilding and restarting the gateway daemon automatically updates tool behavior for the already-running MCP server process. No MCP server restart needed.
5. The MCP server binary shrinks to ~200 lines of protocol handling.
6. No change to how Claude Code discovers or starts the MCP server (`agency mcp-server`).

## Non-Goals

- Changing the MCP transport from stdio to SSE/WebSocket. Stdio remains the transport.
- Supporting third-party MCP tool registration through this mechanism. This is about Agency's own tools.
- Removing the gateway REST API or changing its existing endpoint contracts. Existing REST consumers are unaffected.
- Real-time tool list push (e.g., WebSocket notification when tools change). Polling on `tools/list` is sufficient.

## Design

### New Gateway Endpoints

Two new endpoints on the gateway REST API serve as the MCP backend.

#### `GET /api/v1/mcp/tools`

Returns the complete list of MCP tool definitions in the format expected by the MCP `tools/list` response.

**Response:**
```json
{
  "tools": [
    {
      "name": "agency_init",
      "description": "Bootstrap Agency on a fresh host...",
      "inputSchema": {
        "type": "object",
        "properties": {
          "operator": {"type": "string", "description": "Operator name"},
          "force": {"type": "boolean", "default": false}
        }
      }
    }
  ]
}
```

Tool definitions are constructed inside the gateway from its own route table. When a REST endpoint is added or modified, the corresponding MCP tool definition updates automatically because both are derived from the same handler registration.

#### `POST /api/v1/mcp/call`

Executes a tool call and returns MCP-formatted content blocks.

**Request:**
```json
{
  "name": "agency_init",
  "arguments": {
    "operator": "geoff",
    "force": true
  }
}
```

**Response (success):**
```json
{
  "content": [
    {"type": "text", "text": "Agency initialized successfully."}
  ],
  "isError": false
}
```

**Response (error):**
```json
{
  "content": [
    {"type": "text", "text": "Error: gateway not running. Start it with: agency serve"}
  ],
  "isError": true
}
```

The response shape matches the MCP `tools/call` result directly. The proxy inserts it into the JSON-RPC response envelope without inspection.

#### Environment Variable Forwarding

Some tools need environment variables from the MCP server's process (e.g., `ANTHROPIC_API_KEY`, `OPENAI_API_KEY` for `agency_init`). The proxy forwards a defined set of environment variables as a header:

```
X-Agency-Env: ANTHROPIC_API_KEY=sk-ant-...,OPENAI_API_KEY=sk-...
```

The gateway extracts these and makes them available to the tool handler. The allowed set is hardcoded in the proxy (not open-ended) to avoid leaking arbitrary environment state.

### Thin Proxy Architecture

The MCP server process reduces to:

```
stdio (JSON-RPC) → proxy → HTTP (gateway REST API)
```

The proxy handles exactly three JSON-RPC methods:

| Method | Proxy behavior |
|--------|---------------|
| `initialize` | Return static capabilities (no gateway call needed) |
| `tools/list` | `GET /api/v1/mcp/tools` → return `{"tools": ...}` |
| `tools/call` | `POST /api/v1/mcp/call` with `{name, arguments}` → return content blocks |

The proxy has no tool registry, no handler map, no formatting code, and no knowledge of what tools exist. It is a dumb pipe with JSON-RPC framing.

**Gateway connectivity:** The proxy resolves the gateway address the same way the current MCP server does (default `http://127.0.0.1:8200`, configurable via `~/.agency/config.yaml`). If the gateway is unreachable, the proxy returns an MCP error content block: `"Error: Gateway not running. Start it with: agency serve"`.

**Auth:** The proxy reads the operator token from `~/.agency/config.yaml` and sends it as `X-Agency-Token` on every request, identical to the current `apiclient.Client` behavior.

### Where Formatting Lives

Today, `format.go` converts raw REST JSON into human-readable summaries (agent lists grouped by status, audit logs with timestamps, doctor check results with PASS/FAIL). This formatting is MCP-specific — the REST API returns structured JSON, and the MCP layer reshapes it for LLM consumption.

With the thin proxy, the gateway's `/api/v1/mcp/call` endpoint owns this formatting. The handler for each tool call can:

1. Call the internal service layer (same code the REST endpoint uses).
2. Format the result into a text string optimized for LLM consumption.
3. Return it as an MCP content block.

This means the formatting logic moves from the `mcp` package into the gateway's handler layer (or a shared formatting package). The key difference: it now runs in the gateway process, so rebuilding the gateway updates formatting behavior.

**Alternative considered: eliminate formatting entirely.** Return raw JSON and let the LLM interpret it. This was rejected because structured formatting materially improves tool output quality for the LLM (e.g., `"3 agents. running: scout, analyst. stopped: archivist"` is better than a raw JSON array with nested status objects). The formatting is cheap to maintain and high-value.

### Tool Definition Generation

Tool definitions should be generated from the gateway's own handler registrations rather than maintained as a separate data structure. The recommended approach:

1. Each MCP-exposed REST handler registers its MCP metadata (tool name, description, input schema) alongside its HTTP route.
2. The `/api/v1/mcp/tools` endpoint collects all registered metadata and returns it.
3. The `/api/v1/mcp/call` endpoint dispatches by tool name to the corresponding handler.

This eliminates the current pattern where tool definitions and handler functions are registered in parallel (`reg.Register(ToolDef{...}, func(...){...})`), replacing it with a single registration point.

### Proxy Lifecycle and Gateway Restarts

The proxy must handle gateway restarts gracefully:

- **Gateway restart during a call:** The HTTP request fails. The proxy returns an MCP error block. The LLM retries or reports the error. No crash.
- **Gateway restart between calls:** The next `tools/list` or `tools/call` hits the new gateway. Tools update automatically.
- **Gateway not started:** Same as today — error message directing the user to run `agency serve`.
- **Proxy process never needs restart.** It has no state beyond the gateway URL and auth token. Both are read from config on startup and do not change during a session.

### What Was Deleted

These files were removed during the migration:

| File | Disposition |
|------|------------|
| `mcp/handlers.go` | Deleted |
| `mcp/handlers_admin.go` | Deleted |
| `mcp/handlers_hub.go` | Deleted |
| `mcp/format.go` | Formatting moved to `internal/api/mcp_format.go`, original deleted |
| `mcp/tools.go` | Deleted (replaced by `internal/api/mcp_register.go`) |

The `mcp/server.go` file is now ~182 lines: JSON-RPC loop + three method handlers that proxy to REST. The `mcp/proxy.go` (~135 lines) handles HTTP communication with the gateway. `apiclient/client.go` was retained — it is used by the CLI, not the MCP server.

The MCP package now contains only: `server.go`, `proxy.go`, and their tests.

## API Changes

### New Endpoints

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| GET | `/api/v1/mcp/tools` | Token | List all MCP tool definitions |
| POST | `/api/v1/mcp/call` | Token | Execute a tool call, return MCP content blocks |

### Unchanged

All existing REST endpoints remain unchanged. The MCP endpoints are additive. External REST consumers (CLI, agency-web) are unaffected.

## Migration Path (Complete)

All three phases have been completed:

- **Phase 1** (done): Gateway endpoints `/api/v1/mcp/tools` and `/api/v1/mcp/call` added. Tool definitions registered in `internal/api/mcp_register.go`, call dispatch in `internal/api/mcp_call.go`, tool list serving in `internal/api/mcp_tools.go`.
- **Phase 2** (done): MCP server rewritten to proxy mode (`mcp/server.go` + `mcp/proxy.go`). All tools work through the gateway. Hot-reload on gateway restart works.
- **Phase 3** (done): Old handler files, format.go, and tools.go deleted. The `mcp` package contains only `server.go`, `proxy.go`, and tests. `apiclient/client.go` was retained for CLI use.

The MCP server now has 85 tools (up from 64 at spec time), all served by the gateway. The proxy supports exponential backoff retry on gateway restarts.

## ASK Compliance

| Tenet | Status | Notes |
|-------|--------|-------|
| 1. Constraints external | OK | MCP proxy runs outside agent boundary. Agents never interact with MCP tools — only the operator does via Claude Code. |
| 2. Every action traced | OK | Tool calls flow through the gateway REST API, which logs all requests. Audit coverage is preserved (and potentially improved, since all calls now go through the standard API middleware). |
| 3. Mediation complete | OK | No new unmediated paths. The proxy connects to the same gateway endpoint that all other consumers use. |
| 4. Least privilege | OK | The MCP proxy has the same access as the current MCP server (operator token). No privilege change. |
| 5. No blind trust | OK | Token auth on the new endpoints. No implicit trust grants. |
| 17. Instructions from verified principals | OK | The MCP server receives instructions from Claude Code (the operator's tool). The proxy does not change the principal model. Tool calls are still operator-initiated. |
| 18. Unknown entities default to zero trust | OK | The new endpoints require token auth. Unauthenticated calls are rejected. |

## Testing

- **Unit tests:** Proxy JSON-RPC handling (initialize, tools/list, tools/call dispatch, gateway-down error).
- **Integration tests:** Gateway MCP endpoints return correct tool definitions and execute tool calls.
- **E2E:** Claude Code session exercising tools through the proxy, verifying output matches expectations.
- **Hot-reload test:** Rebuild gateway, verify `tools/list` returns updated definitions and `tools/call` uses new behavior without MCP server restart.
