# Agency-Web Container Spec

## Goal

Containerize agency-web so `agency infra up` starts the web UI automatically. Users don't need Node/npm installed.

## Container Design

- **Image**: Multi-stage build. Stage 1: `node:22-alpine` builds the React app. Stage 2: `nginx:alpine` serves `dist/`.
- **Networking**: Not on any agency-managed network (mediation, egress, internal). Published on host port 8280. The browser makes API calls to `localhost:8200` — the container just serves static files.
- **API proxying**: nginx reverse-proxies `/api/` and `/ws` to `host.docker.internal:8200` so the browser doesn't need CORS for a different port. Single origin.
- **Resource limits**: Uses `HostConfigDefaults(RoleInfra)` baseline — 256MB memory, 1 CPU, 1024 PIDs, log rotation (10m x 3), `unless-stopped` restart policy. Readonly rootfs with tmpfs for nginx pid/cache.
- **Health check**: `curl -f http://localhost:80/ || exit 1`
- **Container name**: `agency-infra-web` (follows existing `agency-infra-{role}` convention)
- **Image name**: `agency-web:latest` (added to `defaultImages` map)
- **Build**: Added to Makefile as `make web` target. Built from `agency-web/` source tree in the workspace (not inside the agency repo).

## What does NOT change

- agency-web source code — no modifications needed
- Gateway API, CORS, or authentication
- Other infra containers or networks
- Dev workflow — `npm run dev` still works for agency-web development

## Infra lifecycle

- `agency infra up` starts agency-web alongside egress/comms/knowledge/intake/web-fetch
- `agency infra down` stops it
- `agency infra rebuild web` rebuilds the image and restarts
- `agency status` shows it in the infrastructure section
