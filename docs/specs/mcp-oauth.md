## What This Document Covers

How Agency supports MCP servers that use OAuth 2.1 for authentication and HTTP-based transports (SSE, Streamable HTTP) in addition to the existing stdio transport. All designs maintain ASK tenet compliance — the agent never handles OAuth flows, never sees real tokens, and all remote connections are mediated.

> **Status:** Design document. Not yet implemented.

---

## Part 1: The Problem

Today, Agency supports MCP servers via stdio transport only. The body runtime spawns each MCP server as a subprocess and communicates over stdin/stdout. Auth is handled by injecting API keys as environment variables into the subprocess.

This works for locally-run MCP servers. It does not work for:

1. **Remote MCP servers** — hosted services reachable over HTTP (e.g., a company's internal MCP server, a SaaS tool's MCP endpoint). These require HTTP+SSE or Streamable HTTP transport, not stdio.

2. **OAuth-authenticated MCP servers** — servers that require OAuth 2.1 authorization flows (authorization code + PKCE per the MCP spec). The operator must complete a browser-based consent flow, and tokens must be refreshed periodically.

3. **Multi-tenant MCP servers** — a single remote server instance serving multiple agents, where each agent (or operator) has distinct credentials and scopes.

The core challenge: **OAuth flows are interactive and tokens are sensitive.** ASK requires that the agent never performs auth flows (tenet 1: constraints are external), never sees real tokens (tenet 4: access matches purpose), and all connections are mediated (tenet 3: mediation is complete).

---

## Part 2: Transport Model

### Three MCP Transports

| Transport | Connection | Agent Sees | Mediation |
|---|---|---|---|
| **stdio** | Subprocess on agent-internal network | stdin/stdout pipe | Workspace container (seccomp) |
| **SSE** | HTTP long-poll to remote server | Local HTTP endpoint | MCP proxy (network-level) |
| **Streamable HTTP** | HTTP POST/GET to remote server | Local HTTP endpoint | MCP proxy (network-level) |

### Key Principle: The Agent Always Talks Locally

Regardless of where the MCP server actually runs, the agent always connects to a local endpoint inside the isolation boundary. For stdio, that's a subprocess. For remote servers, it's a local proxy that handles the real connection.

```
Agent view (inside isolation boundary):
  MCP server "github-tools" → http://localhost:9100/mcp/github-tools

Reality (outside isolation boundary):
  localhost:9100 → MCP proxy sidecar → enforcer → egress → https://mcp.github.com
```

The agent cannot distinguish a local stdio server from a proxied remote server. This is intentional.

---

## Part 3: MCP Proxy Sidecar

### Architecture

Remote MCP connections are handled by a new **MCP proxy sidecar** that runs on the agent-internal network alongside the enforcer. It is a separate container with its own isolation boundary (ASK tenet 6: each enforcement layer has its own isolation boundary).

```
                     ┌──────────────────────────────────────────┐
                     │           Per-Agent Containers           │
                     │                                          │
                     │   enforcer ─────────────── egress ──── internet
                     │      │                                   │
                     │   agent-internal network                 │
                     │      │         │         │               │
                     │              mcp-proxy  workspace       │
                     │              (remote     (agent)        │
                     │               MCP)                      │
                     └──────────────────────────────────────────┘
```

### Responsibilities

The MCP proxy sidecar:

1. **Exposes local HTTP endpoints** for each remote MCP server on the agent-internal network.
2. **Manages outbound connections** to remote MCP servers via the egress proxy (all traffic is mediated).
3. **Handles OAuth token lifecycle** — stores tokens, refreshes before expiry, injects into outbound requests.
4. **Enforces server allowlist** — only connects to MCP server URLs declared in the capability registry.
5. **Translates protocols** — the agent sends MCP JSON-RPC; the proxy handles transport-specific framing (SSE, Streamable HTTP).
6. **Audit logs** all MCP requests and responses (tool calls, results, errors).

### What the MCP Proxy Does NOT Do

- It does not make policy decisions about which tools an agent can call (that's the enforcer's job).
- It does not hold LLM credentials (that's the egress proxy).
- It does not run inside the agent's container.

---

## Part 4: OAuth Flow

### Token Acquisition (Operator-Side)

OAuth authorization is an **operator action**, not an agent action. The operator completes the OAuth flow before (or independently of) agent start.

```bash
# Operator authorizes Agency to use a remote MCP server
agency cap enable github-mcp --oauth

# This:
# 1. Reads the MCP server's OAuth metadata (RFC 8414 discovery)
# 2. Opens a browser for the operator to complete the consent flow
# 3. Receives the authorization code via local redirect
# 4. Exchanges for access_token + refresh_token
# 5. Stores tokens in the token store (~/.agency/infrastructure/.mcp-tokens.json)
# 6. Records the grant in the audit log
```

The agent is not involved. The agent may not even be running when this happens.

### Token Storage

OAuth tokens are stored in a dedicated token store, separate from API keys:

```
~/.agency/infrastructure/
├── .service-keys.env       # Static API keys (existing)
└── .mcp-tokens.json        # OAuth tokens (new, 0600 permissions)
```

Token store schema:

```json
{
  "github-mcp": {
    "server_url": "https://mcp.github.com",
    "access_token": "gho_xxxx",
    "refresh_token": "ghr_xxxx",
    "token_type": "Bearer",
    "expires_at": "2026-03-07T22:00:00Z",
    "scopes": ["repo", "read:org"],
    "obtained_at": "2026-03-07T14:00:00Z",
    "metadata_url": "https://mcp.github.com/.well-known/oauth-authorization-server"
  }
}
```

The agent never sees this file. It is mounted read-only into the MCP proxy sidecar only.

### Token Refresh

The MCP proxy sidecar handles token refresh automatically:

1. Before each request, check if the access token expires within 5 minutes.
2. If expiring, use the refresh token to obtain a new access token.
3. Update the token store (the proxy has write access to its own token cache; the canonical store is operator-side).
4. If refresh fails, log an error and mark the capability as degraded (the agent's tool call fails with a clear error, not a leaked auth error).
5. Audit log records every refresh.

Token refresh never requires operator interaction unless the refresh token itself has expired (which requires a new authorization flow).

### Token Refresh and the Canonical Store

Two copies of tokens exist:

| Location | Owner | Purpose |
|---|---|---|
| `~/.agency/infrastructure/.mcp-tokens.json` | Operator (host) | Canonical store. Written by `agency cap enable --oauth`. Read by Phase 6 mount. |
| `/etc/mcp-proxy/tokens.json` (inside proxy) | MCP proxy sidecar | Working copy. Proxy reads on start, refreshes in-place. |

On agent restart, Phase 6 copies canonical tokens into the proxy's mount. If the proxy refreshed tokens during the session, those refreshed tokens need to be written back to the canonical store on graceful halt. This prevents the next start from using a stale access token.

```
Start:  canonical → proxy mount (read)
Run:    proxy refreshes tokens in its own mount
Halt:   proxy mount → canonical (write-back on graceful halt only)
```

On emergency halt, tokens are not written back. The next start uses the canonical store, which may have an expired access token. The proxy's first action will be to refresh it.

---

## Part 5: Capability Registry Integration

### Extended MCPServerSpec

The `MCPServerSpec` model gains transport and auth fields:

```python
class MCPServerSpec(BaseModel):
    # Existing (stdio)
    command: str = ""
    args: list[str] = []
    env: dict[str, str] = {}

    # New: transport
    transport: Literal["stdio", "sse", "streamable-http"] = "stdio"
    url: str = ""  # Remote server URL (required for sse/streamable-http)

    # New: auth
    auth_type: Literal["none", "api-key", "oauth"] = "none"
    oauth_metadata_url: str = ""  # RFC 8414 discovery URL
    oauth_scopes: list[str] = []  # Requested scopes
```

### Registry Entry for a Remote MCP Server

```yaml
# registry/mcp-servers/github-mcp.yaml
kind: mcp-server
name: github-mcp
version: "1.0.0"
description: GitHub MCP server (remote, OAuth)
source: local

spec:
  transport: sse
  url: https://mcp.github.com/sse
  auth_type: oauth
  oauth_metadata_url: https://mcp.github.com/.well-known/oauth-authorization-server
  oauth_scopes:
    - repo
    - read:org

requires:
  network:
    - mcp.github.com

permissions:
  network: true
```

### Operator Workflow

```bash
# Add a remote MCP server
agency cap add mcp github-mcp \
  --url https://mcp.github.com/sse \
  --transport sse \
  --oauth

# Authorize (opens browser)
agency cap enable github-mcp --oauth

# Or with a pre-obtained token (skip browser flow)
agency cap enable github-mcp --key $GITHUB_TOKEN

# Check status (shows token expiry, refresh status)
agency cap list
```

### Phase 3 and Phase 6 Changes

**Phase 3 (Constraints):**
- For remote MCP entries, generate `mcp-proxy-config.json` instead of adding to `mcp-servers.json`.
- The proxy config maps capability names to remote URLs, transport types, and auth types.
- Add the MCP server's domain to the egress allowlist.

**Phase 6 (Body):**
- If any remote MCP entries exist, start the MCP proxy sidecar container.
- Mount `.mcp-tokens.json` read-only into the proxy.
- Mount `mcp-proxy-config.json` read-only into the proxy.
- Connect the proxy to the agent-internal network.
- The body runtime's `mcp-servers.json` gets entries pointing to `http://mcp-proxy:<port>/mcp/<name>` — the agent sees these as regular HTTP MCP servers.

---

## Part 6: Security Properties

### ASK Tenet Compliance

| Tenet | How It's Met |
|---|---|
| 1. Constraints are external | OAuth flow runs operator-side. Token lifecycle managed by proxy sidecar outside agent's boundary. Agent cannot initiate, modify, or observe OAuth flows. |
| 2. Every action leaves a trace | Proxy audit-logs every MCP request, token refresh, and connection event. Agent has no write access to proxy logs. |
| 3. Mediation is complete | All remote MCP traffic routes through the proxy sidecar, then through the egress proxy. No direct path from agent to remote MCP server. |
| 4. Access matches purpose | OAuth scopes are declared in the capability entry. Tokens are scoped to those permissions. Agent gets a local endpoint, not a bearer token. |
| 5. Superego is read-only | Proxy config, OAuth metadata, and token store are read-only to the agent. |
| 6. Separate isolation boundaries | MCP proxy is its own container, separate from enforcer and workspace. |

### Credential Isolation

The agent never sees OAuth tokens. The data flow:

```
Agent                   MCP Proxy              Egress           Remote MCP
  │                        │                     │                   │
  │── MCP JSON-RPC ───────>│                     │                   │
  │   (no auth headers)    │── HTTPS + Bearer ──>│── HTTPS ─────────>│
  │                        │   (real token)      │                   │
  │<── MCP result ─────────│<── response ────────│<──────────────────│
```

The agent sends bare MCP JSON-RPC to a local HTTP endpoint. The proxy injects the OAuth token into the outbound HTTPS request. The agent never handles, sees, or stores the token.

### Token Exfiltration Prevention

Same pattern as the enforcer's service credential swap:

1. The proxy validates that the outbound request URL matches the declared `url` in the capability entry.
2. Tokens are never injected into requests to URLs that don't match the registered server.
3. The agent cannot redirect the proxy to send tokens to an attacker-controlled endpoint.

### Refresh Token Security

Refresh tokens are long-lived and high-value. Protections:

1. Stored in `.mcp-tokens.json` with 0600 permissions on the host.
2. Mounted read-only into the proxy (the proxy writes refreshed tokens to its own ephemeral cache, not back to the mount).
3. Write-back to canonical store happens only on graceful halt, via the halt sequence (not the proxy writing to the host).
4. If the proxy container is compromised, the attacker gets the current access token (short-lived) but the refresh token in the read-only mount cannot be exfiltrated to a different endpoint (URL validation).

---

## Part 7: Edge Cases

### Token Expiry During Long Tasks

If an access token expires mid-task:
1. The proxy detects the 401 response from the remote server.
2. Refreshes the token automatically.
3. Retries the request with the new token.
4. The agent sees a successful response (slightly delayed).

If the refresh token is also expired:
1. The proxy returns a clear error to the agent: "OAuth authorization expired for github-mcp. Operator re-authorization required."
2. The agent's tool call fails with this message.
3. An audit event is recorded.
4. The operator runs `agency cap enable github-mcp --oauth` to re-authorize.

### Multiple Agents, Same Remote Server

Each agent gets its own MCP proxy sidecar instance. Token stores can be shared (same OAuth grant) or per-agent (different OAuth grants with different scopes).

```bash
# Shared token (default — all agents use the same OAuth grant)
agency cap enable github-mcp --oauth

# Per-agent token (agent-specific OAuth grant)
agency cap enable github-mcp --oauth --agent atlas
agency cap enable github-mcp --oauth --agent bolt
```

### Stdio Servers That Need OAuth Tokens

Some MCP servers run locally (stdio) but need an OAuth token passed as an environment variable. This is already supported by the existing auth model:

```bash
# Pre-obtain a token, store it as a capability key
agency cap enable my-local-mcp --key $OAUTH_TOKEN
```

The token is injected as an env var into the subprocess. No proxy needed. However, this token won't auto-refresh. For auto-refresh of stdio server tokens, the MCP proxy can act as a token manager without proxying the connection:

```yaml
spec:
  transport: stdio
  command: "node"
  args: ["server.js"]
  auth_type: oauth
  oauth_metadata_url: https://auth.example.com/.well-known/oauth-authorization-server
```

In this case, the proxy refreshes the token and writes it to a file that the stdio server reads, or the start sequence re-injects the env var on refresh. This is a secondary concern — most OAuth MCP servers will use HTTP transport.

### Fallback: Static Token for OAuth Servers

Operators who don't want the interactive OAuth flow can provide a pre-obtained token:

```bash
agency cap enable github-mcp --key $MY_TOKEN
```

This stores the token as a static credential (same as API keys). No refresh, no browser flow. When it expires, the operator provides a new one. This is a valid workflow for personal access tokens and long-lived API tokens.

---

## Part 8: Implementation Plan

### Phase A: Transport Model

1. Extend `MCPServerSpec` with `transport`, `url`, `auth_type` fields.
2. Update `capabilities add mcp` to accept `--url`, `--transport`, `--oauth` flags.
3. Update registry schema validation.
4. No runtime changes yet — just data model.

### Phase B: MCP Proxy Sidecar

1. Build `agency/images/mcp-proxy/` container image.
2. Implement HTTP-to-SSE and HTTP-to-Streamable-HTTP proxying.
3. Implement token injection into outbound requests.
4. Implement URL validation (anti-exfiltration).
5. Implement audit logging.
6. Add health check endpoint.

### Phase C: OAuth Token Lifecycle

1. Implement RFC 8414 metadata discovery.
2. Implement authorization code + PKCE flow in the CLI (`agency cap enable --oauth`).
3. Implement token storage (`.mcp-tokens.json`).
4. Implement token refresh in the proxy sidecar.
5. Implement token write-back on graceful halt.

### Phase D: Start Sequence Integration

1. Update Phase 3 to generate `mcp-proxy-config.json` for remote entries.
2. Update Phase 6 to start the MCP proxy sidecar when remote entries exist.
3. Update `mcp-servers.json` generation to point remote entries at proxy local endpoints.
4. Update egress allowlist to include remote MCP server domains.
5. Update `agency doctor` to verify proxy isolation properties.

### Phase E: Body Runtime HTTP MCP Client

1. Add HTTP MCP client to body runtime (currently stdio only).
2. Support connecting to local proxy endpoints.
3. Handle connection lifecycle (reconnect on SSE drop, etc.).

---

*See also: Capability-Registry.md for the capability management model. Agency-Platform-Specification.md for the enforcer credential swap pattern. Agent-Lifecycle.md for the start sequence phases.*
