# Agency Platform

Core Agency platform repo. This file is for contributors and coding agents
working inside the main runtime repository.

## ASK Tenets

These are hard constraints. If a design violates ASK, the design is wrong.

- enforcement remains external to the agent boundary
- mediation remains complete
- audit remains complete
- least privilege stays explicit
- trust, identity, and knowledge boundaries remain visible and recoverable

Reference: [ASK Framework](https://github.com/geoffbelknap/ask)

## Current Product Line

Assume the default product surface is the scoped `0.2.x` core line:

- governed single-agent or small-agent workflows
- direct-message workflow and simple channel activity
- event-driven execution
- basic provider routing
- graph-backed retrieval and context
- audit, budget, and usage visibility
- core web, API, and MCP builder surfaces

Do not treat the older broader platform as default product truth.

These areas are still present in the repo but are not default/core:

- missions and teams
- hub lifecycle and packs
- intake and connector breadth
- notifications, webhook management, and other side systems
- graph governance, ontology management, and advanced review workflows
- routing optimizer workflows beyond basic provider routing

If you touch one of those areas, keep it explicitly gated and do not expand the
default user path accidentally.

## Architecture

Agency is a single Go binary that provides:

- CLI
- gateway daemon
- REST API at `localhost:8200`
- native Go MCP server

Primary source areas:

- `cmd/gateway/` for the binary entrypoint
- `internal/` for API, CLI, orchestration, policy, routing, and runtime logic
- `web/` for the web UI
- `images/` for runtime container images

The Go gateway is the source of truth. The web app is a REST client only.

## Runtime Model

- Per agent: `workspace` + `enforcer`
- Shared core infra: `egress`, `comms`, `knowledge`, `web`
- Optional graph support: `embeddings` only when explicitly configured
- Experimental services such as `intake`, `web-fetch`, and relay-adjacent work stay out of the default core path unless explicitly enabled

Network rules that must remain true:

- enforcers stay on the internal mediation plane only
- enforcers must not attach to `agency-operator` or other external-facing networks
- external access stays mediated through the egress path

## Current Contracts

- `internal/api/openapi.yaml` is the canonical API spec
- `/api/v1/openapi-core.yaml` is the supported default API view
- agent DM establishment is a first-class backend contract at `POST /api/v1/agents/{name}/dm`
- `agency quickstart` is the guided first-run path
- `agency setup` is the idempotent setup/infrastructure command
- `agency admin doctor` is the authoritative deployment-safety check

## Feature Gating

Feature tiering must stay aligned across:

- API route registration
- OpenAPI metadata
- CLI command visibility
- web navigation and routes
- MCP tool registration
- infra startup and release publishing

The feature registry is the source of truth:

- `internal/features/registry.go`
- `web/src/app/lib/feature-registry.json`

Do not add a new surface in one place and forget to gate or classify it in the
others.

## Images And Build Rules

- Prefer stable, reusable base-image boundaries for expensive layers
- Keep volatile `BUILD_ID` / `SOURCE_HASH` labels at the end of Dockerfiles
- Do not reintroduce broad repo-root build contexts unless they are actually required
- `workspace-base` and `python-base` exist to stabilize heavy shared layers; use that pattern when appropriate

Build and test commands:

```bash
go test ./...
go build ./cmd/gateway/
pytest images/tests/
./agency admin doctor
```

Use the smallest sufficient validation for the change, but validate shipped
behavior when the change affects runtime, API, or release behavior.

## Operational Rules

- Preserve fail-closed behavior during startup, enforcement, mediation, and teardown
- Do not loosen credential, capability, network, or container boundaries casually
- Hub-managed files must not be edited directly when a customization path exists elsewhere
- Do not normalize experimental surfaces in docs, help text, or default UI copy
- Keep release/install paths honest: README, Homebrew caveats, OpenAPI, MCP, and web UX should all describe the same default product

## Docs Conventions

- specs belong in `docs/specs/`
- plans belong in `docs/plans/`
- delete plans once fully implemented
- keep specs as durable reference

Do not save plans or specs under `docs/superpowers/`.
