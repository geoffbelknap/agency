# Agency Platform

Core Agency platform repo. This is the main implementation of the Agency runtime, gateway, orchestration layer, MCP server, and web UI.

## Non-Negotiable Constraint

ASK tenets apply to all work here. If a design would violate ASK, the design is wrong.

Treat these as hard constraints:
- enforcement must remain external to the agent boundary
- auditability must remain complete
- mediation must remain complete
- least privilege must remain explicit
- trust, identity, and knowledge boundaries must remain visible and recoverable

When in doubt, verify against `../ask/FRAMEWORK.md`.

## High-Level Architecture

Agency is a single Go binary that provides:
- CLI
- gateway daemon
- REST API at `localhost:8200`
- native Go MCP server

Primary areas:
- `cmd/gateway/` for the main binary entrypoint
- `internal/` for platform logic, API, orchestration, policy, and models
- `web/` for the web UI
- `images/` for runtime container images and related tests

## Repo Rules

- The Go gateway is the source of truth.
- `internal/api/openapi.yaml` is the canonical API spec.
- `web/` is a REST client only and must stay aligned with backend API behavior.
- Preserve fail-closed behavior during startup, enforcement, mediation, and teardown.
- Do not loosen container, network, credential, or capability boundaries casually.
- Hub-managed files must not be edited directly when the expected customization point is elsewhere.

## Build And Test

Use the smallest sufficient validation for the change, but validate shipped behavior.

Common commands:

```bash
go test ./...
go build ./cmd/gateway/
pytest images/tests/
```

Repo-specific end-to-end paths also exist and should be used when the change warrants them.

## Docs Conventions

- specs: `docs/specs/`
- plans: `docs/plans/`
- delete plans once fully implemented
- keep specs as reference

Do not save specs or plans under `docs/superpowers/`.
