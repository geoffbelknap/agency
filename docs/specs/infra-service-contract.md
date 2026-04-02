## Purpose

Every infrastructure service (egress, comms, knowledge, intake) must follow this contract. It defines the minimum behavior the gateway expects for health checking, graceful shutdown, logging, and readiness.

---

## Health

Every service MUST expose `GET /health` on its primary port.

**Response (200 OK):**
```json
{"status": "ok"}
```

Additional fields are allowed (e.g., `mode`, `connectors_loaded`) but `status: "ok"` is required.

**Failure:** Return non-200 or don't respond. Docker health check marks the container unhealthy after 3 retries.

**Current gaps:**
- Egress has no `/health` endpoint (uses socket probe instead)

---

## Startup

**Requirements:**
1. Log a single startup line: `Starting {service} on port {port}`
2. Accept requests within 5 seconds of container start
3. Background loops (polling, curation, ingestion) start async — they MUST NOT block the health endpoint from responding
4. If the service depends on another service (e.g., knowledge needs comms for ingestion), the dependency is best-effort with retry — startup MUST NOT fail if the dependency is temporarily unavailable

**Current compliance:**
| Service | Startup log | Ready < 5s | Async background | Tolerant of deps |
|---------|-------------|------------|------------------|------------------|
| Egress | Yes (policy load) | Yes | N/A | N/A |
| Comms | Yes | Yes | N/A | N/A |
| Knowledge | Partial (ingestion log, no startup line) | Yes* | Yes | Yes (polls comms) |
| Intake | No startup line | Yes | Yes | N/A |

*Knowledge runs ontology migration synchronously but completes in <1s

---

## Shutdown

**Requirements:**
1. Handle SIGTERM by initiating graceful shutdown
2. Close all open connections (HTTP clients, WebSocket connections, database handles) within 3 seconds
3. Cancel all background tasks (polling loops, curation, scheduled jobs)
4. Log a single shutdown line: `{service} shutting down`
5. Exit with code 0

If the service cannot shut down within 3 seconds, Docker SIGKILL's it. This is acceptable but should be rare — if it happens consistently, the service has a shutdown bug.

**Current compliance:**
| Service | SIGTERM | Closes connections | Cancels tasks | Shutdown log | Exits < 3s |
|---------|---------|-------------------|---------------|--------------|------------|
| Egress | mitmproxy default | No explicit | N/A | No | Yes (0.65s) |
| Comms | aiohttp default | Close httpx only | No WS close | No | **No (2.6s, hits timeout)** |
| Knowledge | aiohttp default | Close httpx | Cancel 2 tasks | No | Yes (0.67s) |
| Intake | aiohttp default | N/A | Cancel 3 tasks | No | Yes (0.45s) |

**Comms is the only non-compliant service.** It has open WebSocket connections to agents and the gateway relay that block aiohttp's graceful shutdown. Fix: add an `on_shutdown` handler that closes all WebSocket connections.

---

## Logging

**Required log events (JSON format to stderr):**
1. **Startup:** `Starting {service} on port {port}` — before accepting requests
2. **Ready:** `{service} ready` — after health endpoint is live
3. **Shutdown:** `{service} shutting down` — on SIGTERM received
4. **Shutdown complete:** `{service} stopped` — before process exit

**Optional but recommended:**
- Background loop status: `{loop} started`, `{loop} stopped`
- Dependency connectivity: `Connected to {dep}`, `{dep} unavailable, retrying`

---

## Configuration Reload

Services that support hot-reload SHOULD handle SIGHUP to reload configuration without restart.

**Current support:**
| Service | SIGHUP |
|---------|--------|
| Egress | No |
| Comms | No |
| Knowledge | No |
| Intake | Yes (reloads connectors) |

---

## Implementation Checklist

For each service, verify:
- [ ] `GET /health` returns `{"status": "ok"}` with 200
- [ ] Logs startup line on boot
- [ ] Handles SIGTERM — closes connections, cancels tasks
- [ ] Exits within 3 seconds of SIGTERM
- [ ] Logs shutdown line on SIGTERM
- [ ] Background tasks don't block health endpoint
- [ ] Dependencies are best-effort with retry (no hard failures)
