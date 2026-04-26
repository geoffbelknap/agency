Stamp every build artifact with a content-aware build ID so version mismatches between the gateway binary, Docker images, containers, and enforcer binaries are immediately detectable and automatically resolved.

## Build ID Format

A build ID is a short git commit hash, optionally suffixed with `-dirty`:

- `f40978c` — clean build from committed code
- `f40978c-dirty` — built with uncommitted changes

The binary also carries `version` (semver, currently `0.1.0`), `commit`, and `date` (ISO 8601) for human reference. **Mismatch detection uses the build ID only** (commit + dirty flag).

### Makefile Changes

```makefile
DIRTY    := $(shell git diff --quiet && git diff --cached --quiet || echo "-dirty")
BUILD_ID := $(COMMIT)$(DIRTY)
LDFLAGS  := -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.date=$(DATE) -X main.buildID=$(BUILD_ID)
```

`BUILD_ID` is passed to `go build` via ldflags alongside the existing variables. The root `Makefile` passes `BUILD_ID` to `make images` so Docker builds also receive it.

## Stamping Layers

### 1. Gateway Binary

Already has `version`, `commit`, `date` via ldflags. Add `buildID`:

```go
var buildID = "unknown"  // set by -X main.buildID=...
```

Propagation path: `main.go buildID` → `cfg.BuildID` (new field on `config.Config`) → `Infra.BuildID`, `Enforcer.BuildID`, `Workspace.BuildID` (new field on each struct) → `images.Resolve()`.

### 2. Docker Images

Every Dockerfile gets:

```dockerfile
ARG BUILD_ID=unknown
LABEL agency.build.id=${BUILD_ID}
```

Applies to all images: enforcer, body, comms, knowledge, intake, egress.

At build time, pass `--build-arg BUILD_ID=\{buildID\}`:
- `make images` — Makefile passes `--build-arg BUILD_ID=$(BUILD_ID)` to each `docker build`
- Embedded builds in `images.Resolve()` — `dockerBuild()` gains a `buildArgs map[string]*string` parameter, added to `ImageBuildOptions.BuildArgs`
- `buildFromSource()` and `buildFromEmbed()` forward `buildID` to `dockerBuild()`

### 3. Containers

At `ContainerCreate()` in `orchestrate/enforcer.go`, `orchestrate/workspace.go`, and `orchestrate/infra.go`, add two labels:

```go
"agency.build.id":      imageBuildID,   // read from image's label
"agency.build.gateway": cfg.BuildID,    // which binary created this container
```

To read `imageBuildID`: call `cli.ImageInspectWithRaw(ctx, imageRef)` before container creation, read `imageInspect.Config.Labels["agency.build.id"]`. If the label is missing (pre-versioning image), use `"unknown"`.

### 4. Audit Events

Lifecycle audit events include a `build_id` field (snake_case, matching existing audit field conventions):

- `agent_started` — `"build_id": "f40978c"`
- `agent_restarted` — `"build_id": "f40978c"`
- `start_phase` — `"build_id": "f40978c"`
- `agent_halted` — `"build_id": "f40978c"`

This enables forensic reconstruction of which build was running at any point in time.

### 5. Enforcer Binary

The enforcer Dockerfile gets a `BUILD_ID` build arg passed through to the Go binary:

```dockerfile
ARG BUILD_ID=unknown
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w -X main.buildID=${BUILD_ID}" -o /enforcer .
```

The enforcer's `/health` endpoint includes `build_id` in its response.

## Mismatch Detection

### `agency status`

Adds a version header, build column on infrastructure containers, and build column on agents:

```
Agency v0.1.0 (f40978c, 2026-03-23)

Infrastructure:
  ● egress         running   f40978c ✓
  ● comms          running   f40978c ✓
  ● knowledge      running   a1b2c3d ⚠ stale
  ...

Agents:
  Name            Status    Enforcer    Build
  ─────────────────────────────────────────────
  henrybot900     running   running     f40978c ✓
  researchbot     running   running     a1b2c3d ⚠ stale
```

Build column logic: read `agency.build.id` label from each running container's image via Docker inspect. If it differs from the gateway's `buildID`, show `⚠ stale`.

### `agency admin doctor`

New check `build_consistency`:

```
[PASS] build_consistency (infra): All infrastructure images match gateway build f40978c
[FAIL] build_consistency (infra): knowledge image build a1b2c3d != gateway f40978c
[PASS] build_consistency (henrybot900): All images match gateway build f40978c
[FAIL] build_consistency (researchbot): enforcer image build a1b2c3d != gateway f40978c
```

Checks both infrastructure containers and per-agent containers. Reads `agency.build.id` label from each running container's image. Compares to the gateway binary's `buildID`. Any mismatch = FAIL.

### `/health` Endpoint

Fix the hardcoded `"dev"` response. Return actual version and build ID:

```json
{"status": "ok", "version": "0.1.0", "build_id": "f40978c"}
```

## Auto-Rebuild on Start

`images.Resolve()` gains a `buildID` parameter (7 call sites updated). Resolution order matches the existing code with a new staleness check inserted:

```
Resolve(ctx, cli, name, version, sourceDir, buildID, logger):
  1. Dev mode (sourceDir set)?
     Yes → always build from source (existing behavior, unchanged)
  2. Image exists locally?
     No  → fall through to GHCR pull / embedded build
     Yes → inspect image for agency.build.id label
       Label missing or != buildID → rebuild from embedded context
       Label matches → return (image is current)
  3. GHCR pull (release mode, version != "" and version != "dev")
     → pulled images retain their labels; no staleness check against
       local buildID since GHCR images are built by CI with their own
       build ID. If pull succeeds, use it as-is.
  4. Embedded build (final fallback, passes --build-arg BUILD_ID=buildID)
```

When a rebuild is triggered during agent start, the CLI shows progress:

```
  ✓ Starting henrybot900
  ⟳ Rebuilding enforcer image (stale: a1b2c3d → f40978c)
  ✓ Starting enforcement containers
  ...
```

### Infrastructure container staleness

Infrastructure containers (egress, comms, etc.) are long-lived and shared. The existing `Infra.EnsureRunning()` returns early if a container is already running. With build versioning, it gains a staleness check:

1. Container is running → inspect its `agency.build.gateway` label
2. If label is missing or differs from current `buildID` → recreate the container (stop, remove, create with new image)
3. If label matches → keep running

This ensures `agency infra up` and the infra phase of `agency start` both pick up new images after a rebuild.

### Dirty flag behavior

A `-dirty` build ID will never match a clean-commit image, forcing a rebuild. This is intentional — dirty means source has changed, so images should be rebuilt to match. In dev mode (`sourceDir` set), images always rebuild from source regardless of build ID, so the dirty flag has no additional effect there.

## What This Prevents

The scenario that motivated this spec: a `make all` rebuilds images and the binary, but the gateway daemon is still running the old binary. The old daemon creates containers from new images but with old mount paths. Nothing reports the mismatch.

With build versioning:
- `agency status` would show the build ID mismatch immediately on both infra and agent containers
- `agency admin doctor` would flag `build_consistency` as FAIL
- On next `agency start`, stale images would be auto-rebuilt and stale infra containers would be recreated
