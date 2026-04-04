# Agency-Web Container Spec

**Status:** Partially implemented — Dockerfile and nginx.conf exist in agency-web. Integration with `agency infra up` and Makefile target still needed.

## Goal

Containerize agency-web so `agency infra up` starts the web UI automatically. Users don't need Node/npm installed.

## Container Design

- **Image**: Multi-stage build. Stage 1: `node:22-alpine` builds the React app. Stage 2: `alpine:3.21` with nginx + openssl serves `dist/` over HTTPS.
- **Networking**: Host networking mode. No agency-managed Docker networks. Serves HTTPS on port 8280. The browser makes API calls to the same origin — nginx proxies them to the gateway.
- **API proxying**: nginx reverse-proxies `/api/` and `/ws` to `127.0.0.1:8200` (gateway on localhost, reachable via host networking). Single origin, no CORS needed.
- **TLS**: Self-signed certificate auto-generated at container startup via openssl. Certs written to `/tmp/certs/` (writable on read-only rootfs). Users can mount trusted certs at `/etc/nginx/certs/`.
- **Resource limits**: 64MB memory, 0.5 CPU, 64 PIDs. Readonly rootfs with tmpfs for nginx cache and cert generation.
- **Health check**: `wget --no-check-certificate -q -O /dev/null https://127.0.0.1:8280/health || exit 1`
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
