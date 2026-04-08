# Container Build Standards

Standards and patterns for building Agency container images. Follow these when creating new images or modifying existing ones.

## Image Taxonomy

Agency containers fall into four categories:

| Category | Base | Build Pattern | Examples |
|----------|------|---------------|----------|
| Python services | `agency-python-base:latest` | Single stage, repo-root context | body, comms, knowledge, intake |
| Python services (specialized) | `python:3.13-slim` | Single stage, own-dir or repo-root context | egress (mitmproxy deps too divergent for shared base) |
| Go services | `golang:1.26-{alpine,bookworm}` → minimal runtime | Multi-stage | enforcer, web-fetch, relay |
| Static/minimal | `alpine` or `debian:bookworm-slim` | Single stage | gateway-proxy, workspace |
| Web frontend | `node:22-alpine` → `alpine` + nginx | Multi-stage | web |

## Shared Python Base Image

`images/python-base/Dockerfile` pre-installs dependencies common to most Python services:

- httpx, aiohttp, pyyaml, pydantic (with pinned versions)

**When to use it:** Any new Python service image that needs 2+ of these deps should `FROM agency-python-base:latest`.

**When NOT to use it:** If your service has a completely different dependency profile (like egress/mitmproxy), use `python:3.13-slim` directly.

**Build order:** `make python-base` must run before any dependent image. The Makefile wires this automatically — `body`, `comms`, `knowledge`, and `intake` targets depend on `python-base`.

**Adding deps to the base:** Only add a dependency to python-base if 3+ service images use it. Two is not enough — it wastes space for the services that don't need it.

## Dockerfile Patterns

### Required elements

Every Agency Dockerfile must include:

```dockerfile
ARG BUILD_ID=unknown
LABEL agency.build.id=${BUILD_ID}
```

This enables build versioning and staleness detection. The gateway compares the running container's `agency.build.id` label against the current binary's commit hash to detect stale images.

### Python service template

```dockerfile
FROM agency-python-base:latest
ARG BUILD_ID=unknown
LABEL agency.build.id=${BUILD_ID}
WORKDIR /app

# Extra deps not in the shared base (skip if none)
COPY images/<service>/requirements.txt /app/
RUN pip install --no-cache-dir -r requirements.txt

# Shared logging infrastructure
COPY images/logging_config.py /app/logging_config.py
COPY images/_sitecustomize.py /app/sitecustomize.py

# Service code
COPY images/<service>/*.py /app/
RUN rm -f /app/test_*.py /app/*_test.py

EXPOSE 8080
CMD ["python", "-u", "/app/server.py"]
```

Key points:
- **Glob copy for source files** (`COPY images/<service>/*.py /app/`), not individual `COPY` per file. Fewer layers, less Dockerfile churn when adding files.
- **Strip test files** after glob copy (`RUN rm -f /app/test_*.py /app/*_test.py`). Only needed if the source directory contains test files.
- **requirements.txt lists only extras** — deps already in python-base should not be repeated.
- **Pin versions** in requirements.txt for reproducibility.
- **`--no-cache-dir`** on all pip installs to avoid bloating the image with pip's cache.

### Go service template (multi-stage)

```dockerfile
FROM golang:1.26-alpine AS builder
WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
ARG BUILD_ID=unknown
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w -X main.buildID=${BUILD_ID}" -o /binary .

FROM alpine:3.23
ARG BUILD_ID=unknown
LABEL agency.build.id=${BUILD_ID}
RUN apk add --no-cache ca-certificates
COPY --from=builder /binary /usr/local/bin/binary
CMD ["binary"]
```

Key points:
- **Always multi-stage.** Go build toolchain adds ~1GB that shouldn't be in the runtime image.
- **`go mod download` before `COPY . .`** — dependency layer caches across code changes.
- **`CGO_ENABLED=0`** for static binaries that run on minimal base images.
- **`-ldflags="-s -w"`** strips debug info and symbol tables (~30% smaller binaries).
- **Alpine or debian-slim runtime** — prefer alpine unless you need glibc.
- **`ca-certificates`** always — Go services usually make HTTPS calls.

### Web frontend template (multi-stage)

```dockerfile
FROM node:22-alpine AS build
WORKDIR /app
COPY package.json package-lock.json ./
RUN npm ci --ignore-scripts
COPY . .
RUN npm run build

FROM alpine:3.23
# ... nginx setup, copy dist from build stage
```

Key points:
- **`npm ci` before source copy** — dependency layer caches across code changes.
- **`--ignore-scripts`** on npm ci for security (postinstall scripts can be malicious).
- **Alpine + nginx runtime** — not node. Node runtime is ~180MB; alpine+nginx is ~8MB.

## Build Context Rules

Images that need shared code from sibling directories (shared models, logging infrastructure) must use the **repo root** as build context. All `COPY` paths in these Dockerfiles are relative to the repo root.

Images that are fully self-contained use their **own directory** as build context. This makes builds faster by reducing the context sent to the Docker daemon.

The Makefile tracks this via `REPO_CONTEXT_IMAGES`:

```makefile
REPO_CONTEXT_IMAGES = body comms knowledge intake egress
```

When adding a new image: if it needs files from outside its own directory, add it to `REPO_CONTEXT_IMAGES`.

## Layer Caching Strategy

**DO:**
- Place `COPY requirements.txt` + `RUN pip install` before `COPY *.py` — dependency layer survives code changes.
- Place `COPY go.mod go.sum` + `RUN go mod download` before `COPY . .` — same principle for Go.
- Place `COPY package.json package-lock.json` + `RUN npm ci` before `COPY . .` — same for Node.

**DON'T:**
- Use `CACHE_BUST` args before dependency install steps — it defeats caching for expensive pip/go/npm layers.
- Put `CACHE_BUST` only before the source code copy step (after deps are installed).

**The `CACHE_BUST` arg:** The Makefile passes `--build-arg CACHE_BUST=$(date +%s)` to force rebuilds of the code layers on every `make` invocation. Place this arg in the Dockerfile *after* dependency install steps, *before* source code copy steps.

## Image Size Budgets

Target sizes for new images:

| Category | Target | Current range |
|----------|--------|---------------|
| Static/minimal containers | < 20 MB | 14–18 MB |
| Go service (compiled binary) | < 50 MB | 25–39 MB |
| Python service (shared base) | < 250 MB | 227–250 MB |
| Python service (specialized) | < 350 MB | 335 MB (egress) |
| Workspace (debian + tools) | < 150 MB | 137 MB |

If a new image significantly exceeds these targets, investigate:
- Are there unnecessary system packages installed?
- Should this use multi-stage to exclude build tools?
- Are large files being copied that aren't needed at runtime?

## Non-Root Execution

All images should run as a non-root user:

```dockerfile
RUN useradd -m -s /bin/sh -u 61000 serviceuser
USER serviceuser
```

Or on alpine:
```dockerfile
USER nobody
```

Exceptions: images that need root for specific setup (e.g., nginx port binding) should drop to non-root after setup.

## Security Checklist

- [ ] No secrets, tokens, or credentials in the image
- [ ] Non-root user for runtime
- [ ] Minimal base image (slim/alpine, not full distro)
- [ ] `--no-cache-dir` on pip installs
- [ ] No test files in the final image
- [ ] `BUILD_ID` label present
- [ ] `.dockerignore` excludes `.git/`, `__pycache__/`, test dirs, docs

## CI/CD Integration

The release workflow (`release-images.yml`) builds and pushes all images on git tags:

1. `build-python-base` job runs first — pushes `agency-python-base` to GHCR.
2. `build-and-push` matrix job runs after — builds all service images (which `FROM agency-python-base`).
3. `build-and-push-web` runs independently (no python-base dependency).
4. `retag-upstream-embeddings` runs independently.

When adding a new image:
1. Add it to the `CORE_IMAGES` list in the Makefile.
2. Add a matrix entry in `release-images.yml`.
3. If it uses `agency-python-base`, ensure it's in the `build-and-push` job (which has `needs: build-python-base`).
4. If it's a new category (e.g., Rust), create a new job.

## Adding a New Container Image

1. Create `images/<name>/Dockerfile` following the appropriate template above.
2. Add to `CORE_IMAGES` in the Makefile.
3. If repo-context, add to `REPO_CONTEXT_IMAGES`.
4. If Python service using shared base, add `<name>: python-base` dependency in the Makefile.
5. Add matrix entry in `release-images.yml`.
6. Verify build: `make <name>`.
7. Check image size against budget table.
