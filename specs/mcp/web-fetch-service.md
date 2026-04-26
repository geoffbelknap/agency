---
description: "Platform-wide capability for agents to fetch and read web content. Shared infrastructure container with layered secur..."
---

# Web Fetch Service

Platform-wide capability for agents to fetch and read web content. Shared infrastructure container with layered security, content extraction, caching, and audit.

## Architecture

### Container: `infra-web-fetch`

Go binary on the `agency-mediation` network. Agents reach it through enforcer mediation. No direct internet access — all external requests route through the egress proxy.

```
agent (workspace)
  → enforcer /mediation/web-fetch/fetch
    → web-fetch service (infra-web-fetch:8080)
      → egress proxy (infra-egress:3128)
        → internet
```

The enforcer adds `/mediation/web-fetch` alongside `/mediation/comms` and `/mediation/knowledge`. No credential swap needed — web-fetch is an internal infra service, not an external API.

### Endpoints

| Endpoint | Method | Purpose |
|----------|--------|---------|
| `/fetch` | POST | Fetch URL, extract content, return markdown + metadata |
| `/health` | GET | Health check |
| `/metrics` | GET | Cache stats, request counts, blocklist stats |
| `/blocklists/reload` | POST | Hot-reload blocklists (called by gateway on SIGHUP) |

### Request/Response

```json
// POST /fetch
// Agent identity comes from X-Agency-Agent header (injected by enforcer)
{
  "url": "https://example.com/article",
  "options": {
    "include_links": true,
    "max_content_length": 50000,
    "timeout_seconds": 15
  }
}
```

```json
// Response
{
  "url": "https://example.com/article",
  "final_url": "https://example.com/article",
  "status_code": 200,
  "metadata": {
    "title": "Article Title",
    "description": "Meta description",
    "language": "en",
    "published_date": "2026-03-15",
    "canonical_url": "https://example.com/article",
    "og_image": "https://example.com/og.png"
  },
  "content": "# Article Title\n\nMarkdown content...",
  "content_length": 4523,
  "cached": false,
  "xpia_scan": {
    "clean": true,
    "flags": []
  }
}
```

Error responses use the same envelope with `"error"` field:

```json
{
  "url": "https://malware.example.com",
  "error": "blocked by platform DNS blocklist",
  "block_reason": "dns_blocklist",
  "blocked": true
}
```

## Security Layers

Evaluated in order. First rejection wins.

```
Request arrives at web-fetch service
  1. Content-type pre-check (HEAD request)
  2. DNS blocklist check
  3. Operator policy check (allow/deny lists)
  4. Per-domain rate limit check
  5. Global throughput check
  6. Fetch via egress proxy
  7. Response size enforcement
  8. XPIA scan on extracted content
  9. Return to agent (or reject)
```

### DNS Blocklists

Three tiers matching the policy hierarchy. Most restrictive wins.

| Tier | Source | Update | Override |
|------|--------|--------|----------|
| **Platform** | Bundled threat intel (malware, phishing, C2) | Auto-refreshed from feeds | Cannot be overridden — hard floor |
| **Operator** | `~/.agency/web-fetch/blocklists/operator.yaml` | Manual edit, hot-reload via SIGHUP | Adds to platform list |
| **Agent** | Agent's `egress-domains.yaml` (existing enforcer mechanism) | Per-agent, already implemented | Most restrictive wins |

Platform blocklist sources (fetched through egress proxy on a refresh schedule):

```yaml
version: 1
updated: "2026-03-26T00:00:00Z"
sources:
  - name: urlhaus
    url: https://urlhaus.abuse.ch/downloads/text/
    format: plain
    refresh: 6h
  - name: phishtank
    url: https://data.phishtank.com/data/online-valid.json
    format: phishtank_json
    refresh: 1h
blocklist:
  # Static entries (always blocked regardless of feeds)
  - "*.onion"
  - "169.254.*"
  - "metadata.google.internal"
  - "*.internal"
```

Operator blocklist:

```yaml
# ~/.agency/web-fetch/blocklists/operator.yaml
deny:
  - "*.pastebin.com"
  - "*.bit.ly"
  - "*.tinyurl.com"
allow_override: []  # domains that bypass operator deny (never bypasses platform)
```

Agent-level domain filtering is already enforced by the enforcer's domain gate as the request passes through the egress proxy. The web-fetch service does not duplicate it.

### Content-Type Restrictions

Pre-flight HEAD request before fetching the body. Allowed types (configurable by operator):

- `text/html`, `text/plain`, `text/xml`, `text/markdown`, `text/csv`
- `application/json`, `application/xml`

All other types rejected. Prevents agents from fetching binaries, executables, archives, images.

If the target server doesn't support HEAD, the service falls back to a GET with a range header (`Range: bytes=0-0`) to probe content type, then aborts or continues based on the response headers.

### Response Size Limits

- **Raw response**: 2MB max. Truncated, not rejected — returns what was received up to the limit.
- **Extracted content**: 100KB max markdown. Truncated with a `[content truncated at 100KB limit]` marker.

Both limits configurable via operator config.

### XPIA Scanning

Reuses the enforcer's existing XPIA scanning logic as a shared Go package. Scans extracted markdown content before returning it to the agent. Detects:

- Instruction injection attempts ("ignore previous instructions", "you are now", system prompt overrides)
- Cross-tool reference attacks
- Hidden text / invisible unicode characters

Scan results attached to the response in `xpia_scan`:
- `clean: true` — no flags detected
- `clean: false, flags: [...]` — suspicious patterns found

The `xpia.block_on_flag` config controls behavior:
- `false` (default): Content returned with flags attached. The agent's enforcer makes the block/pass decision based on agent policy.
- `true`: Flagged content rejected entirely. Response contains error with `block_reason: "xpia_scan"`.

### Rate Limiting

Split across two layers based on blast radius.

| Limit | Where | Default | Purpose |
|-------|-------|---------|---------|
| Per-agent request rate | Enforcer | 600 req/min (existing) | Blast radius = one agent |
| Per-domain rate | Web-fetch service | 10 req/min per domain | Protects external targets |
| Global throughput | Web-fetch service | 200 req/min total | Protects the service |

Per-agent rate limiting lives on the enforcer where it already exists. The web-fetch service only enforces shared-resource limits (per-domain and global).

Rate limit exceeded → `429 Too Many Requests` with `Retry-After` header.

## Content Extraction

### Pipeline

```
Raw HTML
  → readability extraction (strip nav, ads, sidebars, scripts)
  → HTML → markdown conversion (preserve headings, links, code, tables, lists)
  → metadata extraction (title, description, OG tags, published date, language)
  → size enforcement (truncate if over limit)
  → XPIA scan
```

### Dependencies

- `goquery` — HTML parsing and CSS selector queries
- `go-readability` — article content extraction (port of Mozilla Readability)
- `html-to-markdown` — structural HTML → markdown conversion
- Standard library `net/html` for metadata extraction

### Extraction Behavior

- **include_links** (default true): Preserve hyperlinks as markdown `[text](url)`. When false, links rendered as plain text.
- **Tables**: Converted to markdown tables. Complex tables (colspan/rowspan) simplified to best effort.
- **Code blocks**: Preserved with language hints from `class` attributes.
- **Images**: Stripped from content (agents can't render them). Alt text preserved as `[Image: alt text]`.
- **Non-HTML content types**: JSON returned as-is (pretty-printed). Plain text, CSV, XML returned as-is with appropriate markdown code fencing.

## Caching

In-memory LRU cache with TTL eviction. Not persisted across restarts.

| Parameter | Default | Configurable |
|-----------|---------|-------------|
| Max entries | 1000 | Yes |
| TTL | 15 minutes | Yes |
| Max cached entry size | 100KB | Yes |
| Cache key | SHA-256 of normalized URL | — |

### URL Normalization

Before generating cache key: lowercase scheme and host, sort query parameters, strip fragments, strip tracking parameters (`utm_*`, `fbclid`, `gclid`, etc.).

### Cache Rules

- Cache hit → return cached content with `"cached": true`. No XPIA re-scan (scanned on first fetch).
- Cache miss → fetch, extract, scan, cache, return.
- Request with `"no_cache": true` option → bypass cache, fetch fresh.
- HTTP `Cache-Control: no-store` in response → not cached.
- Error responses (4xx, 5xx) → not cached.

## Audit Trail

Dedicated audit log, separate from the enforcer's general audit. Written by the web-fetch service.

### Storage

Date-rotated JSONL files at `~/.agency/audit/web-fetch/web-fetch-YYYY-MM-DD.jsonl`. Mounted into the container at `/agency/web-fetch/audit/`.

### Log Entry

```json
{
  "ts": "2026-03-26T17:30:00.000Z",
  "agent": "jarvis",
  "url": "https://example.com/article",
  "final_url": "https://example.com/article",
  "method": "GET",
  "status_code": 200,
  "content_type": "text/html",
  "raw_bytes": 45230,
  "extracted_bytes": 4523,
  "cached": false,
  "blocked": false,
  "block_reason": "",
  "xpia_flags": [],
  "dns_blocklist_hit": "",
  "duration_ms": 342,
  "request_id": "a1b2c3d4"
}
```

Blocked requests logged with `blocked: true` and `block_reason` indicating which layer rejected it: `dns_blocklist`, `content_type`, `rate_limit`, `size_limit`, `operator_policy`, `xpia_scan`.

### Integrity

HMAC-signed using `WEB_FETCH_AUDIT_HMAC_KEY` environment variable. Same pattern as enforcer audit (ASK tenet 2: tamper-evident logs).

No agent write access. Logs written by the service, not the agent. Agents cannot read, modify, or delete fetch audit logs.

## Body Runtime Tool

### Service Definition

```yaml
# agency_core/services/web-fetch.yaml
service: web-fetch
display_name: Web Fetch
description: Fetch web pages and extract content as clean markdown with metadata
api_base: http://infra-web-fetch:8080
credential:
  env_var: ""
  header: ""
  scoped_prefix: ""
tools:
  - name: web_fetch
    description: >
      Fetch a web page and return its content as clean markdown with metadata.
      Use this to read articles, documentation, blog posts, and other web content.
      Returns extracted text content — does not render JavaScript.
    parameters:
      - name: url
        description: The URL to fetch
        required: true
      - name: include_links
        description: Preserve hyperlinks in extracted markdown (default true)
        required: false
        default: "true"
    method: POST
    path: /fetch
```

### Mediation Wiring

Web-fetch is wired as a mediation endpoint on the enforcer, not a service credential swap. The enforcer routes `/mediation/web-fetch/*` to `http://infra-web-fetch:8080/*`, same pattern as comms and knowledge.

The enforcer injects `X-Agency-Agent` header (agent identity) on mediation requests. The web-fetch service uses this for audit logging and per-agent metrics.

### Capability Grant Flow

1. `agency cap add web-fetch --kind service` — registers in capability registry
2. `agency cap enable web-fetch` — makes available to agents
3. Per-agent: capability listed in `constraints.yaml` granted capabilities
4. Enforcer allows `/mediation/web-fetch` path for granted agents
5. Body runtime receives `web_fetch` tool in services manifest

Agents without the `web-fetch` capability get no tool definition and the enforcer rejects mediation requests to `/mediation/web-fetch`.

## Container & Infrastructure

### Image: `agency-web-fetch`

Built from `images/web-fetch/`. Build command: `make web-fetch`. Content-aware build ID stamped via `--build-arg BUILD_ID` (same as all other images).

### Go Dependencies

- `goquery` — HTML parsing
- `go-readability` — article extraction
- `html-to-markdown` — HTML → markdown
- Standard library: `net/http`, `crypto/sha256`, `encoding/json`
- Shared XPIA scanning package from enforcer

### Container Spec

| Property | Value |
|----------|-------|
| Network | `agency-mediation` |
| Port | 8080 (internal only) |
| Memory limit | 256MB |
| Restart policy | `unless-stopped` |
| Volumes | `~/.agency/audit/web-fetch/` → `/agency/web-fetch/audit/` |
| | `~/.agency/web-fetch/` → `/agency/web-fetch/config/` |
| Environment | `WEB_FETCH_AUDIT_HMAC_KEY`, `HTTP_PROXY=http://infra-egress:3128` |

### Startup Sequence

1. Load platform blocklist (bundled in image)
2. Load operator blocklist + config from config mount
3. Start blocklist refresh goroutine (fetches threat feeds via egress proxy)
4. Initialize in-memory cache
5. Start HTTP server on :8080
6. Health endpoint returns 200

### Infrastructure Integration

- Started by `agency infra up` alongside comms, knowledge, intake, egress
- Torn down by `agency infra down`
- Visible in `agency infra status`
- SIGHUP reloads operator blocklists and config from disk

## Configuration

### Operator Config: `~/.agency/web-fetch/config.yaml`

```yaml
# Fetch behavior
fetch:
  timeout_seconds: 15
  max_response_bytes: 2097152    # 2MB
  max_content_bytes: 102400      # 100KB extracted
  user_agent: "Agency/1.0 (web-fetch)"
  follow_redirects: true
  max_redirects: 5

# Allowed content types (pre-flight HEAD check)
content_types:
  allowed:
    - "text/html"
    - "text/plain"
    - "text/xml"
    - "text/markdown"
    - "text/csv"
    - "application/json"
    - "application/xml"

# Rate limiting (service-level, shared across all agents)
rate_limits:
  per_domain_rpm: 10
  global_rpm: 200

# Cache
cache:
  max_entries: 1000
  ttl_minutes: 15
  max_entry_bytes: 102400

# XPIA scanning
xpia:
  enabled: true
  block_on_flag: false

# Blocklist refresh
blocklists:
  auto_refresh: true
  refresh_interval: 6h
```

### Per-Agent Enforcement

Per-agent rate limiting: enforcer (existing 600 req/min general limit).
Per-agent domain filtering: enforcer's `egress-domains.yaml` (existing mechanism).
Per-agent capability grant: `constraints.yaml` granted capabilities list.

No per-agent config in the web-fetch service. All agent-scoped enforcement stays on the enforcer where it belongs.

## Future: Security Provider Interface

The security layers in this service (DNS blocklists, XPIA scanning, content policy) are designed with clean internal interfaces (`BlocklistChecker`, `ContentScanner`, `PolicyEvaluator`). This is intentional preparation for a future pluggable security framework where third-party providers (Microsoft, Palo Alto Networks, and others) can supply value-added security services — similar to how endpoint security products integrate into workstations and servers.

The provider interface design is out of scope for this spec but the internal interfaces should be kept clean and swappable to support it.
