# Agency Relay — Design Spec

**Date:** 2026-04-04
**Status:** Draft
**Service domain:** tinyfleck.io

## Overview

Agency Relay is a managed network access layer that lets operators access their locally-running Agency instance from any browser, without opening ports, configuring DNS, or managing certificates. The operator runs `agency relay connect`, a persistent outbound WebSocket connects to Cloudflare infrastructure, and the operator can access their Agency web UI at `app.tinyfleck.io`.

The relay has **no governance authority** over the operator's Agency. It is a convenience layer — a network pipe the operator opts into. If the relay goes down or the operator's account is disabled, their Agency continues running with full local access. The relay cannot halt agents, modify constraints, change trust levels, or alter any enforcement behavior. It is a passthrough.

## Architecture

```
Operator's Machine                     Cloudflare (tinyfleck.io)              Browser
┌─────────────────┐                    ┌──────────────────────┐              ┌──────────────┐
│ agency gateway   │                   │ Worker               │              │ app.tinyfleck│
│ localhost:8200   │                   │  - route + auth      │◄── HTTPS ──►│ .io          │
│                  │                   │  - session cookies    │              │              │
│ agency-web       │                   │  - edge cache        │              │ agency-web   │
│ localhost:8280   │                   │  - rate limiting     │              │ SPA from R2  │
│                  │                   │                      │              └──────────────┘
│ relay client ────┼──── outbound ────►│ Durable Object       │
│ (in gateway      │     WSS 443      │  - holds tunnel WSS  │
│  binary)         │                   │  - holds access token│
│                  │                   │  - bridges browser   │
│ refresh secret   │                   │    WS ↔ tunnel WS   │
│ (never leaves)   │                   │  - hibernates when   │
└─────────────────┘                    │    no browsers open  │
                                       │                      │
                                       │ R2: agency-web SPA   │
                                       │ D1: accounts, waitlist│
                                       └──────────────────────┘
```

Both access paths work simultaneously:
- **Local:** `localhost:8280` — unchanged, always available
- **Relay:** `app.tinyfleck.io` — additive, opt-in, operator can disconnect/revoke at any time

## Cloudflare Components

### Worker (stateless)

Routes requests, handles auth, manages sessions, serves static assets.

**Routes:**
- `GET /` → R2 (agency-web SPA)
- `GET /assets/*` → R2 (static assets, immutable cache)
- `GET /__agency/config` → relay-specific config (API base URL, relay session token)
- `* /api/v1/*` → DO (proxy to tunnel)
- `WS /ws` → DO (bridge browser WebSocket to tunnel)
- `WS /tunnel` → DO (operator tunnel endpoint)
- `POST /auth/*` → OAuth flows (GitHub, Google)
- `POST /api/device/*` → device auth for CLI
- `GET /waitlist` → landing/waitlist page
- `POST /waitlist` → submit waitlist form
- `* /admin/*` → admin UI + API (your account only)

**Responsibilities:**
- TLS termination (automatic, Cloudflare-managed)
- OAuth2 flow handling (GitHub + Google)
- Session cookie validation
- Route to correct DO by account ID
- Edge caching (Cache API, per-account keyed)
- Rate limiting (per session, default 1000 req/min)
- CORS headers for `app.tinyfleck.io`

### Durable Object (per instance)

One DO per connected Agency instance. Holds the tunnel WebSocket, bridges browser requests to the operator's gateway.

**State:**
- `access_token` — short-lived gateway access token (15 min TTL), refreshed by relay client
- `tunnel_ws` — WebSocket connection to operator's relay client
- `browser_ws[]` — active browser WebSocket connections
- `pending_requests` — map of request ID → response waiter
- `session_role` — `owner` (v1), future: `viewer`, `admin`
- `online` — whether tunnel is connected

**Behavior:**
- Receives browser HTTP requests from Worker, creates request frames, sends down tunnel
- Injects `Authorization: Bearer {access_token}` into proxied requests (browser never sees the token)
- Correlates response frames to pending requests by ID
- Bridges browser WebSocket ↔ tunnel WebSocket for real-time events
- Accepts `token_refresh` frames from relay client, swaps to new access token
- Hibernates when no browser sessions are active (Hibernatable WebSockets API)
- Wakes on incoming message (browser request or tunnel frame)
- On tunnel disconnect: notifies connected browsers with `{"type": "tunnel_offline"}`, marks instance offline
- On tunnel reconnect: notifies browsers with `{"type": "tunnel_online"}`

**Request timeout:** 30 seconds. If tunnel doesn't respond, return 504 Gateway Timeout.
**Tunnel offline:** Worker checks DO state before routing. If offline, return 503 with "Instance offline" page.

### D1 Database

**Tables:**

```sql
accounts (
  id TEXT PRIMARY KEY,
  email TEXT NOT NULL,
  name TEXT NOT NULL,
  provider TEXT NOT NULL,        -- 'github' | 'google'
  provider_id TEXT NOT NULL,
  status TEXT NOT NULL,          -- 'approved' | 'active' | 'disabled'
  role TEXT NOT NULL DEFAULT 'user',
  created_at TEXT NOT NULL,      -- account created on first OAuth sign-in after approval
  approved_at TEXT,              -- when admin approved from waitlist
  last_login TEXT
  -- Note: accounts are only created after admin approves a waitlist entry
  -- AND the user completes OAuth. No 'waitlisted' status here.
)

waitlist (
  id TEXT PRIMARY KEY,
  email TEXT NOT NULL,
  name TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'pending',  -- 'pending' | 'approved' | 'denied'
  company TEXT,                  -- optional
  role_title TEXT,               -- optional
  use_case TEXT,                 -- optional: "what are you building?"
  referral_source TEXT,          -- optional: "how did you hear about us?"
  utm_source TEXT,
  utm_medium TEXT,
  utm_campaign TEXT,
  utm_content TEXT,
  referrer TEXT,                 -- HTTP Referer header
  geo_country TEXT,              -- from CF-IPCountry header
  geo_region TEXT,               -- from cf.region
  ip_hash TEXT,                  -- hashed, not raw IP
  created_at TEXT NOT NULL,
  reviewed_at TEXT               -- when admin approved/denied
)

instances (
  id TEXT PRIMARY KEY,
  account_id TEXT NOT NULL REFERENCES accounts(id),
  name TEXT,
  status TEXT NOT NULL,          -- 'online' | 'offline'
  agency_version TEXT,
  agency_build TEXT,
  last_connected TEXT,
  edge_location TEXT,            -- Cloudflare colo
  protocol_version INTEGER
)

sessions (
  id TEXT PRIMARY KEY,
  account_id TEXT NOT NULL REFERENCES accounts(id),
  role TEXT NOT NULL DEFAULT 'owner',
  created_at TEXT NOT NULL,
  expires_at TEXT NOT NULL
)

connection_log (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  instance_id TEXT NOT NULL,
  event TEXT NOT NULL,           -- 'connect' | 'disconnect' | 'token_refresh' | 'error'
  timestamp TEXT NOT NULL,
  metadata TEXT                  -- JSON: edge_location, version, error details
)

admin_audit (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  admin_id TEXT NOT NULL,
  action TEXT NOT NULL,          -- 'approve' | 'deny' | 'disable' | 'enable'
  target_id TEXT NOT NULL,
  timestamp TEXT NOT NULL,
  metadata TEXT                  -- JSON: reason, notes
)
```

### R2 Bucket

**Bucket:** `relay-assets`

Hosts the agency-web SPA. Versioned by Agency release:
- `/current/` — latest stable build
- `/v{version}/` — pinned versions for compatibility
- `index.html` — short TTL (5 min) for updates
- Hashed assets (JS, CSS) — immutable, long TTL

The Worker checks the `agency_version` from the tunnel handshake and can serve a matching SPA version if needed for compatibility.

## Tunnel Protocol

Single multiplexed WebSocket carrying all traffic — REST request/response pairs and real-time WebSocket frames.

**Connection:** `wss://relay.tinyfleck.io/tunnel`

### Frame Format

All frames are JSON objects with a `type` field.

**Handshake (relay client → DO, first frame):**
```json
{
  "type": "handshake",
  "version": 1,
  "relay_token": "rt_...",
  "access_token": "at_...",
  "agency_version": "0.8.2",
  "agency_build": "a1b2c3d"
}
```

**Token refresh (relay client → DO, periodic):**
```json
{
  "type": "token_refresh",
  "access_token": "at_new..."
}
```

**REST request (DO → relay client):**
```json
{
  "type": "request",
  "id": "req_abc123",
  "method": "GET",
  "path": "/api/v1/agents",
  "headers": {"Accept": "application/json"},
  "body": null
}
```

**REST response (relay client → DO):**
```json
{
  "type": "response",
  "id": "req_abc123",
  "status": 200,
  "headers": {"Content-Type": "application/json"},
  "body": [{"name": "analyst", "status": "running"}]
}
```

**WebSocket open (DO → relay client, browser wants /ws):**
```json
{
  "type": "ws_open",
  "id": "ws_def456",
  "path": "/ws"
}
```

**WebSocket frame (bidirectional):**
```json
{
  "type": "ws_frame",
  "id": "ws_def456",
  "data": {"v": 1, "type": "agent_status", "agent": "analyst", "status": "running"}
}
```

**WebSocket close (either direction):**
```json
{
  "type": "ws_close",
  "id": "ws_def456"
}
```

### Heartbeats

No application-level heartbeat. WebSocket protocol-level pings (handled by Cloudflare and the Go WebSocket library) keep the connection alive. This avoids waking the DO for keepalive traffic.

### Protocol Versioning

The `version` field in the handshake identifies the tunnel protocol version. The Worker adapts to the client's version. Support policy: current version (N) and previous version (N-1). N-1 is deprecated with a warning message pushed down the tunnel. Drop N-1 support after 3 months.

## Authentication & Token Model

### OAuth (browser sessions)

GitHub and Google OAuth2 via Cloudflare Worker. Both providers normalize to a unified account identity — if someone signs in with GitHub first and Google later using the same email, they get the same account.

Browser sessions use HTTP-only secure cookies scoped to `app.tinyfleck.io`. Session expiry: configurable, default 7 days.

### Device Auth (CLI → browser)

First-time `agency relay connect` uses the device authorization flow:

1. CLI calls `POST relay.tinyfleck.io/api/device/code` → `{code: "ABCD-1234", verify_url, interval}`
2. CLI prints URL and code, operator opens browser
3. Operator authenticates with GitHub/Google, confirms device code
4. CLI polls `POST relay.tinyfleck.io/api/device/token` at interval → `{relay_token, account_id}`
5. CLI saves relay token to `~/.agency/relay.yaml`

The relay token is a long-lived credential stored on the operator's machine. It authenticates the tunnel connection to Cloudflare. It is NOT the gateway token.

### Short-Lived Access Tokens (relay → gateway)

The relay does not hold a long-lived gateway credential. Instead:

1. On `agency relay connect`, the gateway generates:
   - A **refresh secret** (long-lived, stored in `~/.agency/relay.yaml`, never leaves the machine)
   - An **access token** (short-lived, 15 min TTL, HMAC-signed by gateway's existing HMAC key)
2. Relay client sends the access token in the handshake. DO holds it.
3. Every ~10 minutes, the relay client calls the local gateway (`localhost:8200`) with the refresh secret to get a new access token.
4. Relay client sends a `token_refresh` frame to the DO with the new token.
5. DO swaps to the new token. Old token expires naturally.

**Properties:**
- The DO never holds a long-lived credential. A compromised DO yields a token that expires in ≤15 minutes.
- The refresh secret never leaves the operator's machine. It's used for a localhost HTTP call only.
- Gateway validates access tokens statelessly — HMAC signature + expiry check. No token registry, no database lookup.
- Revocation is instant: `agency relay revoke` deletes the refresh secret. Next token refresh fails. Access token expires within 15 minutes.

**Access token format:**
```
base64(json({
  "sub": "relay",
  "iat": unix_timestamp,
  "exp": unix_timestamp,  // +15 minutes
  "jti": random_id
})) + "." + base64(hmac_sha256(payload, gateway_hmac_key))
```

Gateway validates by: split on `.`, verify HMAC, check `exp > now`, check `sub == "relay"`.

### Relay Token Refresh Endpoint (gateway)

New gateway endpoint for the relay client:

```
POST /api/v1/relay/token
Authorization: Bearer {refresh_secret}
→ {"access_token": "at_...", "expires_in": 900}
```

The refresh secret is a separate credential from the main gateway token. It is stored in `relay.yaml` (not `config.yaml`) and is independently revocable. The gateway recognizes it as a distinct credential type: requests with a refresh secret are only authorized for the `/api/v1/relay/token` endpoint — they cannot access any other API. The Worker validates the `relay_token` (Cloudflare-side auth). The DO stores the `access_token` (gateway-side auth). These are separate credentials serving separate trust boundaries.

## Relay Client (agency binary)

### Package: `internal/relay/`

- **`client.go`** — Tunnel client. Opens WSS, sends handshake, multiplexes frames. Manages reconnection. Periodically refreshes access token via local gateway call. Injects `X-Agency-Via: relay` header on all proxied requests for audit trail.
- **`proxy.go`** — Local HTTP proxy. Receives request frames, calls `localhost:8200`, sends response frames. For `ws_open` frames, opens local WebSocket to `localhost:8200/ws` and bridges bidirectionally.
- **`config.go`** — Reads/writes `~/.agency/relay.yaml`.
- **`token.go`** — Access token generation (HMAC-signed), refresh secret generation, validation.
- **`service.go`** — System service installer (systemd unit / launchd plist).

### Configuration: `~/.agency/relay.yaml`

```yaml
relay_token: "rt_..."           # authenticates tunnel to Cloudflare
refresh_secret: "rs_..."        # used to get short-lived access tokens from local gateway
account_id: "acc_..."
relay_url: "wss://relay.tinyfleck.io/tunnel"
enabled: true                   # auto-start with daemon
trust_acknowledged: true        # operator consented to trust model
trust_acknowledged_at: "2026-04-04T..."
```

### CLI Commands

```
agency relay connect              # start tunnel (device auth on first run)
agency relay disconnect           # stop tunnel, keep credentials for next time
agency relay status               # online/offline, relay URL, token expiry, latency
agency relay revoke               # delete refresh secret, access dies within 15 min
agency relay reissue              # generate new refresh secret + access token
agency relay destroy              # revoke + remove all relay config from machine
agency relay install              # install as systemd/launchd service
agency relay uninstall            # remove system service
```

### Gateway Integration

- If `relay.yaml` exists and `enabled: true`, relay client starts automatically with the daemon (same pattern as the comms bridge)
- New gateway endpoints:
  - `POST /api/v1/relay/connect` — signal daemon to start tunnel
  - `POST /api/v1/relay/disconnect` — signal daemon to stop tunnel
  - `GET /api/v1/relay/status` — tunnel state, relay URL, latency, token expiry
  - `POST /api/v1/relay/token` — refresh access token (authenticated by refresh secret)

### Reconnection

- Exponential backoff: 1s, 2s, 4s, 8s, 16s, 30s (cap)
- Jitter: ±25% on each interval
- Indefinite retry (no max attempts)
- On reconnect: re-sends handshake with fresh access token, DO re-establishes session
- On auth expired: relay token rejected → log error, prompt operator to re-auth

### First-Time Connect UX

```
$ agency relay connect

Connecting to tinyfleck.io relay service...

  The relay service proxies browser requests to your local Agency gateway.
  Your gateway credentials are protected by short-lived tokens that are
  refreshed locally — long-lived credentials never leave your machine.
  The relay runs on Cloudflare infrastructure. See docs for details.

  Accept and continue? [Y/n] y

Open this URL in your browser:

  https://app.tinyfleck.io/device?code=ABCD-1234

Waiting for confirmation...
✓ Authenticated as geoff (GitHub)
✓ Tunnel established

Your Agency is available at: https://app.tinyfleck.io

Relay connected. Press Ctrl+C to disconnect.
Run 'agency relay install' to keep connected in background.
```

## Waitlist & Onboarding

### Landing Page (`app.tinyfleck.io/waitlist`)

For unapproved visitors. No sign-in required.

**Required fields:** Name, Email
**Optional fields:** Company/Org, Role/Title, What are you building with Agency?, How did you hear about us?

**Tracking (automatic):** UTM parameters (source, medium, campaign, content), HTTP Referer, geo (country/region from Cloudflare headers), hashed IP, timestamp.

Standard web analytics (e.g., Cloudflare Web Analytics — no third-party JS, privacy-respecting) on the landing page for conversion tracking.

### Approval Flow

1. User submits waitlist form → D1 row in `waitlist` table (no sign-in required)
2. You review via admin interface → approve or deny
3. On approval: waitlist entry marked `approved`, email stored for matching
4. Approved user visits `app.tinyfleck.io` → signs in with GitHub/Google → Worker matches OAuth email to approved waitlist entry → `accounts` row created with `status: active`
5. User runs `agency relay connect` → device auth flow → tunnel established

If an unapproved user tries to sign in, they see "You're on the waitlist" (or "Sign up for the waitlist" if not yet submitted).

### Admin Interface

Accessible at `app.tinyfleck.io/admin`, protected by your account ID.

**V1 scope (custom UI):**
- Waitlist queue: review entries, approve/deny with optional notes
- Account list: view all accounts, enable/disable, view connection history
- Instance list: online/offline, last seen, agency version, edge location

**Deferred to Cloudflare dashboard:**
- Usage metrics (Worker invocations, DO duration, bandwidth)
- Error rates and logs (Workers Logs)
- Geographic distribution (Cloudflare Analytics)

**Audit:** All admin actions (approve, deny, enable, disable) logged in `admin_audit` table with timestamp, your identity, target account, and action.

## Agency-Web Integration

Agency-web works identically in both local and relay contexts. The only difference is what `/__agency/config` returns:

**Local (served by gateway):**
```json
{
  "api_base": "http://localhost:8200",
  "token": "local-gateway-token"
}
```

**Relay (intercepted by Worker):**
```json
{
  "api_base": "https://app.tinyfleck.io",
  "token": "relay-session-token",
  "via": "relay"
}
```

Agency-web reads this on startup and uses the returned values. No code changes required. The `via` field is optional metadata the UI could use to show a "Connected via relay" indicator.

## Cost Optimization

### Hibernatable WebSockets (primary lever)

Durable Objects use the Hibernatable WebSockets API. When no browser sessions are connected, the DO sleeps. The tunnel WebSocket stays connected at the Cloudflare platform level. The DO wakes only when a message arrives (browser request or tunnel frame). This reduces DO duration charges by ~91% for typical usage (2 hrs active browser/day).

### Worker-Level Heartbeats

WebSocket protocol-level pings handle connection liveness. No application-level heartbeat that would wake the DO. Heartbeat cost: ~$0.

### Edge Caching

Worker caches GET responses using the Cache API, keyed per account:
- `GET /api/v1/agents` — 5s TTL
- `GET /api/v1/hub/presets` — 30s TTL
- `GET /api/v1/hub/*` — 60s TTL
- `GET /api/v1/infra/status` — 5s TTL
- `GET /api/v1/graph/stats` — 10s TTL

POST requests and real-time data (messages, logs, WebSocket) are never cached. Reduces DO wakeups by ~30-50% for typical browsing.

### R2 Static Serving

Agency-web SPA served from R2 with CDN caching. The entire UI loads without touching the tunnel. Only API calls traverse the tunnel.

### Frame Batching

When multiple WebSocket events arrive in rapid succession (agent signals fire in bursts), the DO coalesces them into a single outbound frame to the browser. Reduces browser-side processing and DO wake duration.

### Projected Costs (all optimizations applied)

| Scale | Monthly Cost | Per-User |
|-------|-------------|----------|
| 100 instances | $8-15 | ~$0.10 |
| 1,000 instances | $60-120 | ~$0.08 |
| 10,000 instances | $500-900 | ~$0.07 |
| 100,000 instances | $4-7K | ~$0.05 |

Assumptions: avg 2 hrs active browser/day, hibernation enabled, Worker-level heartbeats, edge cache on GET endpoints. Cloudflare has zero egress fees.

## Rate Limiting

Two enforcement layers:

- **Worker (per browser session):** 1000 requests/min. Protects the tunnel from browser abuse.
- **Relay client (per tunnel):** 500 requests/min. Protects the gateway from a compromised Worker. Enforced locally in the relay client.

Both are hard limits returning 429 Too Many Requests.

## Error Handling

| Scenario | Behavior |
|----------|----------|
| Tunnel disconnects | DO notifies browsers: `tunnel_offline`. Relay client reconnects with backoff. |
| Tunnel reconnects | DO notifies browsers: `tunnel_online`. Pending requests during disconnect already timed out. |
| Gateway down (local) | Tunnel stays connected. Proxied requests return 502 from relay client → DO → 502 to browser. |
| Access token expired | DO rejects proxied request. Relay client should have refreshed before expiry. If refresh fails (gateway down), tunnel stays up but requests fail. |
| Relay token expired/revoked | Worker rejects tunnel connection. Relay client logs error, prompts re-auth. |
| Account disabled (admin) | Worker rejects session cookie. Browser redirected to "account disabled" page. DO closes tunnel if connected. |
| Browser session expires | Worker rejects cookie, redirects to OAuth login. |
| Operator behind corporate firewall | Outbound WSS on port 443 is almost universally allowed. If blocked, relay cannot function — same limitation as any outbound tunnel. |

## ASK Framework Compliance

The relay is a **managed network access layer**, not a governance component. It has no authority over the operator's Agency.

### Trust Model

- The relay is opt-in. The operator explicitly consents on first connect.
- The relay cannot halt agents, modify constraints, change trust levels, or alter enforcement.
- If the relay goes down or the account is disabled, local access is unaffected.
- The operator can revoke relay access instantly (`agency relay revoke`).

### Trust Grant to Cloudflare

The relay introduces a trust relationship with Cloudflare infrastructure. The Durable Object holds a short-lived access token (≤15 min TTL) that can authenticate to the operator's gateway. This is disclosed to the operator on first connect and requires explicit consent. The refresh secret that generates new access tokens never leaves the operator's machine.

### Audit Trail

- All proxied requests carry `X-Agency-Via: relay` header. Gateway logs this, distinguishing relay access from local access.
- All admin actions (approve, deny, enable, disable) are logged in the `admin_audit` D1 table.
- Connection events (connect, disconnect, token refresh, errors) are logged in `connection_log`.

### Tenet Compliance

| Tenet | Status | Notes |
|-------|--------|-------|
| 1 (Constraints external) | Pass | Relay doesn't touch constraints |
| 2 (Every action traced) | Pass | `X-Agency-Via: relay` header distinguishes access path |
| 3 (Mediation complete) | Pass | Relay is operator access path, not agent path |
| 4 (Fail-closed) | Pass | Tunnel down = 503, token expired = 401 |
| 6 (Trust explicit) | Pass | Consent prompt on first connect, documented trust grant |
| 7 (Least privilege) | Pass | Access token scoped to relay, short-lived |
| 8 (Ops bounded) | Pass | Rate limits enforced at Worker and relay client |
| 13 (Authority monitored) | Pass | Admin actions logged in admin_audit |
| 18 (Hierarchy inviolable) | Pass | Agents cannot reach relay — isolated Docker networks |

### XPIA Posture

The relay does not create new agent-level XPIA vectors. It is an operator access path. Agent inputs still flow through enforcers and guardrails. The risk surface is operator session compromise (same as any remote access), mitigated by OAuth, invite-only access, short-lived tokens, and session management.

## Scope Boundaries

### V1 Includes
- Cloudflare Worker + DO + D1 + R2 infrastructure
- GitHub + Google OAuth
- Device auth flow for CLI
- Tunnel protocol (multiplexed WSS)
- Short-lived access tokens with local refresh
- Relay client in agency binary (`internal/relay/`)
- CLI commands: connect, disconnect, status, revoke, reissue, destroy, install, uninstall
- Waitlist landing page with tracking
- Admin UI for waitlist/account management
- Agency-web served from R2

### V1 Excludes
- Multi-user access / invite links (architecture supports it: `role` field in sessions)
- Multiple instances per account (D1 schema supports it: `instances` table)
- Custom domains
- End-to-end encryption
- Paid tiers / billing
- Full admin analytics dashboard (use Cloudflare dashboard)
- Email notifications for waitlist approval (show on next login)

### Future Extensibility

The design intentionally leaves room for:
- **Multi-user:** Session `role` field defaults to `owner`. Add `viewer` role, enforce at Worker layer before proxying. Gateway token stays full-access; relay layer constrains per role.
- **Multiple instances:** `instances` table supports it. UI and CLI would need instance selection.
- **Paid tiers:** D1 `accounts` table can add plan/billing fields. Worker can enforce tier limits.
- **Custom domains:** Cloudflare for SaaS (SSL for SaaS) can map custom domains to the Worker.
