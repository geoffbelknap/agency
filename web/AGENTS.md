# Agency Web

Web frontend for Agency. Pure REST/WebSocket client for the Go gateway.

## Non-Negotiable Constraint

ASK tenets still apply here. A frontend change is wrong if it obscures or weakens:

- external enforcement
- complete mediation
- complete auditability
- explicit least privilege
- visible trust, identity, and knowledge boundaries

When in doubt, preserve the backend contract rather than smoothing over it in UI code.

## Core Rules

- The Go gateway is the source of truth.
- The web app must stay a REST/WebSocket client only.
- Do not recreate backend runtime logic in the browser.
- Do not infer runtime state from Docker/container conventions when a backend API exists.
- Keep experimental and non-core product surfaces explicitly gated in routes, navigation, tests, and copy.

## Current Contracts

- Agent DM establishment is a first-class backend contract at `POST /api/v1/agents/{name}/dm`.
- Agent runtime introspection is a supported operator surface:
  - `GET /api/v1/agents/{name}/runtime/manifest`
  - `GET /api/v1/agents/{name}/runtime/status`
  - `POST /api/v1/agents/{name}/runtime/validate`
- Durable memory lifecycle is a backend-owned graph contract:
  - `GET /api/v1/graph/memory`
  - `POST /api/v1/graph/memory/{id}/actions`
  - `GET /api/v1/graph/memory/proposals`
  - `POST /api/v1/graph/memory/proposals/{id}/review`
- `agency admin doctor` separates runtime checks from backend-specific hygiene checks.

UI/operator behavior should preserve those distinctions:

- runtime health comes from runtime APIs
- backend hygiene warnings stay backend-specific
- Docker-specific warnings should not be presented as generic runtime failure
- durable memory review and revocation must stay visibly operator-owned; do not auto-hide or silently apply preference-affecting memory changes in the UI

## Architecture

- React 19 + Vite 8
- Tailwind CSS v4
- shadcn/ui on Radix primitives
- react-router v7
- API/WebSocket proxy to the gateway from `~/.agency/config.yaml`

Primary source areas:

- `src/app/components/`
- `src/app/screens/`
- `src/app/hooks/`
- `src/app/lib/`
- `src/app/routes.tsx`

## Validation

Use the smallest sufficient validation, but prefer live browser coverage when changing runtime/lifecycle/operator flows.

```bash
npm test
./scripts/e2e/e2e-live-disposable.sh --skip-build
```

The disposable live suite is the highest-signal path for:

- agent lifecycle
- DM flows
- runtime-backed status/diagnostics
- admin/operator surfaces

## Notes

- Do not commit `.certs/`
- Keep UI aligned with `internal/api/openapi.yaml`
- Prefer shadcn/Radix patterns over introducing new UI systems
