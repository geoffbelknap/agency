## Purpose

Every infrastructure service must follow this contract. It defines the minimum behavior the gateway expects across the full container lifecycle: image resolution, startup, health checking, status reporting, teardown, and logging.

Infrastructure services come in two flavors:

- **Built images** — built from Dockerfiles in `images/` (e.g., comms, knowledge, enforcer). Source-built in dev mode, pulled from GHCR in release mode.
- **Upstream images** — pulled from an external registry and retagged to GHCR (e.g., embeddings/Ollama). Never built from source.

Both flavors follow the same gateway-side lifecycle. The only difference is image resolution.

---

## Image Resolution

All infra images resolve to a local tag: `agency-<name>:latest`.

### Built images

Resolution order (in `images.Resolve()`):

1. **Dev mode** (`sourceDir` set): Build from `images/<name>/Dockerfile`. Failure is fatal — no fallback.
2. **Release mode**: Pull `ghcr.io/geoffbelknap/agency-<name>:v<version>`, retag to `agency-<name>:latest`.

Staleness detection: compare `agency.build.id` label on the local image against the current gateway build ID. Skip rebuild if they match and neither is dirty.

After every resolve (build or pull), old images for that service are pruned.

### Upstream images

Resolution order:

1. **Dev mode**: Pull `ghcr.io/geoffbelknap/agency-<name>:v<version>` (same as release — no local build). If the GHCR image is unavailable, fall back to pulling the upstream source image directly (e.g., `ollama/ollama:<pinned-version>`).
2. **Release mode**: Pull `ghcr.io/geoffbelknap/agency-<name>:v<version>`, retag to `agency-<name>:latest`.

Upstream images are pinned to a specific version in the gateway source (not `latest`). The CI pipeline pulls the upstream image, retags it to GHCR with the Agency release version, and pushes it. This ensures:

- Version is locked to each Agency release (no silent drift).
- No Docker Hub rate limit issues for users.
- Reproducible deployments — users and CI get the same image.
- Supply chain checkpoint — we test the specific upstream version before release.

### Image registry

All Agency images (built and upstream) live at `ghcr.io/geoffbelknap/agency-<name>`.

---

## Registration

Every infra service must be registered in three places:

| Location | What to add |
|---|---|
| `defaultImages` map in `infra.go` | `"<role>": "agency-<name>:latest"` |
| `defaultHealthChecks` map in `infra.go` | Health check command for the container |
| `components` slice in `EnsureRunningWithProgress()` | `{name, description, ensure func}` |

And for discovery and teardown:

| Location | What to add |
|---|---|
| `InfraStatus()` in `docker/client.go` | Add to `components` slice |
| `TeardownWithProgress()` in `infra.go` | Add to `roles` slice |
| CI workflow `release-images.yml` | Add build/push job (or retag job for upstream) |

The `containerName()` function generates the Docker container name: `agency-infra-<role>`.

The `agency-infra-` prefix is how `InfraStatus()` discovers all running infra containers. Any container following this naming convention automatically appears in the container list response. The `components` slice in `InfraStatus()` defines which components are *expected* — missing ones are reported as `state: "missing"`.

---

## Container Configuration

All infra containers use `containers.HostConfigDefaults(RoleInfra)` as the baseline, which provides:

| Setting | Value |
|---|---|
| Memory | 256 MB (override per-service if needed) |
| CPU | 1 core |
| PID limit | 1024 |
| Restart policy | `unless-stopped` |
| Log driver | json-file, 10MB max, 3 files |
| Capabilities | `CAP_DROP ALL`, `CAP_ADD NET_BIND_SERVICE` |
| Security | `no-new-privileges:true` |
| Root filesystem | Read-write (infra role default) |

Services that need more resources (e.g., embeddings container needs 3GB for model inference) override the memory limit after calling `HostConfigDefaults`.

### Labels

Every infra container gets these labels:

| Label | Value | Purpose |
|---|---|---|
| `agency.managed` | `true` | Lifecycle management — orphan cleanup |
| `agency.build.gateway` | Build ID | Staleness detection in `agency status` |
| `agency.build.id` | Image build ID | Image-level staleness detection |
| `services.LabelServiceEnabled` | `true` | Service discovery (if applicable) |

### Networks

Infra containers connect to one or more of:

- `agency-mediation` — internal service mesh (most services)
- `agency-egress-net` — outbound internet access via proxy (egress only)
- `agency-internal` — agent-facing network

Network assignment is per-service. Most infra containers go on `agency-mediation` only.

### Data persistence

Infra containers persist data via host bind mounts from `~/.agency/infrastructure/<name>/`. Named Docker volumes are not used.

Data directories are preserved through `agency admin destroy` (default). Only wiped with `--permadeath`.

---

## Health

Every service MUST expose a health endpoint.

**Built images:** `GET /health` on the primary port, returning:
```json
{"status": "ok"}
```

Additional fields are allowed (e.g., `mode`, `connectors_loaded`) but `status: "ok"` is required.

**Upstream images:** Use whatever health endpoint the upstream provides. Document the endpoint and adapt the health check command accordingly (e.g., Ollama uses `GET /` returning 200).

**Failure:** Return non-200 or don't respond. Docker health check marks the container unhealthy after 3 retries.

Health checks are defined in `defaultHealthChecks` with consistent timing:
- Interval: 2s
- Timeout: 3s
- Start period: 1–2s
- Retries: 3

Built images use `python -c "import urllib.request; ..."` or `wget` for the health check command. Upstream images use whatever tool is available in the upstream image (e.g., `curl`, `wget`, or a simple TCP probe).

---

## Startup

**Requirements:**
1. Log a single startup line: `Starting {service} on port {port}`
2. Accept requests within 5 seconds of container start
3. Background loops (polling, curation, ingestion) start async — they MUST NOT block the health endpoint from responding
4. If the service depends on another service (e.g., knowledge needs comms for ingestion), the dependency is best-effort with retry — startup MUST NOT fail if the dependency is temporarily unavailable

**Upstream images** may not follow logging conventions. This is acceptable — the health check is the authoritative readiness signal.

All infra containers are started in parallel during `agency infra up`. Each `ensure*` function follows this pattern:

1. `images.Resolve()` — pull or build the image
2. Check if already running with current build ID — skip if so
3. `stopAndRemove()` old container if stale
4. Prepare bind mounts, env vars, network config
5. `containers.CreateAndStart()` — creates and starts (auto-cleans orphans on failure)
6. `waitRunning()` (10s timeout)
7. `waitHealthy()` (30s timeout)

---

## Shutdown

**Requirements:**
1. Handle SIGTERM by initiating graceful shutdown
2. Close all open connections within 3 seconds
3. Cancel all background tasks
4. Log a shutdown line: `{service} shutting down`
5. Exit with code 0

If the service cannot shut down within 3 seconds, Docker SIGKILL's it. Acceptable but should be rare.

Teardown runs all containers in parallel via `TeardownWithProgress()`. Stop timeout is per-service via `stopTimeoutFor()`.

---

## Status Reporting

`agency status` / `GET /api/v1/infra/status` reports all infra components.

`InfraStatus()` in `docker/client.go`:
1. Calls `ContainerList` filtered by `agency-infra-` prefix
2. Matches against the expected components list
3. Reports per-component: `name`, `state` (running/exited/missing), `health` (healthy/unhealthy/none), `build_id`

All infra containers — built and upstream — appear here identically. The user sees the same status format regardless of image source.

Build ID mismatch between the container's `agency.build.gateway` label and the current gateway version is flagged by `agency status`.

---

## Logging

**Required log events (JSON format to stderr) for built images:**
1. **Startup:** `Starting {service} on port {port}` — before accepting requests
2. **Ready:** `{service} ready` — after health endpoint is live
3. **Shutdown:** `{service} shutting down` — on SIGTERM received
4. **Shutdown complete:** `{service} stopped` — before process exit

**Optional but recommended:**
- Background loop status: `{loop} started`, `{loop} stopped`
- Dependency connectivity: `Connected to {dep}`, `{dep} unavailable, retrying`

**Upstream images** use their own log format. This is acceptable.

---

## Configuration Reload

Services that support hot-reload SHOULD handle SIGHUP to reload configuration without restart.

---

## Conditional Services

Some infra services are optional and only started when a feature is enabled. Conditional services:

- Register in `defaultImages` and `defaultHealthChecks` unconditionally
- Check their enable condition inside their `ensure*` function and return nil early if disabled
- Are included in `InfraStatus()` component list so they show as `missing` (not silently absent) when disabled
- Are included in the teardown role list (no-op if not running)

Example: the embeddings container is conditional on `KNOWLEDGE_EMBED_PROVIDER=ollama`.

---

## Implementation Checklist

For each new infra service, verify:

**Gateway-side:**
- [ ] Added to `defaultImages` map
- [ ] Added to `defaultHealthChecks` map
- [ ] Added to `components` in `EnsureRunningWithProgress()`
- [ ] Added to `components` in `InfraStatus()`
- [ ] Added to `roles` in `TeardownWithProgress()`
- [ ] `ensure*` function follows the standard pattern (resolve → check stale → stop old → create → wait)
- [ ] Uses `containers.HostConfigDefaults(RoleInfra)` as baseline
- [ ] Container name follows `agency-infra-<role>` convention
- [ ] Labels include `agency.managed=true` and `agency.build.gateway`
- [ ] Data directory at `~/.agency/infrastructure/<name>/` if stateful
- [ ] CI workflow builds/pushes (or retags) the image

**Container-side:**
- [ ] Health endpoint responds within startup period
- [ ] Handles SIGTERM gracefully
- [ ] Background tasks don't block health endpoint
- [ ] Dependencies are best-effort with retry
