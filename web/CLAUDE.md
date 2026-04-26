# Agency Web

Web frontend for Agency. Pure REST/WebSocket client — no shared code with the Agency backend.

## Quick Reference

```bash
npm install              # Install deps (runs postinstall which sets up CLI)
npx agency-web           # Build + serve production on https://localhost:8280
npx agency-web dev       # Dev mode with HMR
npx agency-web stop      # Stop the background server
npm test                 # Run tests (Vitest)
```

## Architecture

- **Stack:** React 19, Vite 8, Tailwind CSS v4, shadcn/ui (Radix primitives), Recharts 3
- **Routing:** react-router v7, defined in `src/app/routes.tsx`
- **Path alias:** `@` → `src/`
- **Gateway proxy:** Both dev and preview servers proxy `/api/v1` and `/ws` to the gateway address from `~/.agency/config.yaml` (default `127.0.0.1:8200`)
- **Auth token:** Served at `/__agency/config` via a Vite plugin that reads `~/.agency/config.yaml`
- **TLS:** Auto-generated via mkcert on first `npx agency-web` start. Certs live in `.certs/` (gitignored).
- **Build ID:** Each build is stamped with the git commit hash via Vite's `define` option (`__BUILD_ID__`, `__BUILD_TIME__`). Declared in `src/vite-env.d.ts`. Displayed at the bottom of the channel sidebar.

### Backend Contract Rules

- The web app is a pure REST/WebSocket client. Do not recreate backend logic in the browser.
- Agent DM establishment is a first-class backend contract at `POST /api/v1/agents/{name}/dm`. UI flows should use it instead of reconstructing DM state ad hoc.
- Agent runtime introspection is now a supported operator surface:
  - `GET /api/v1/agents/{name}/runtime/manifest`
  - `GET /api/v1/agents/{name}/runtime/status`
  - `POST /api/v1/agents/{name}/runtime/validate`
- Durable memory lifecycle is backend-owned and graph-backed:
  - `GET /api/v1/graph/memory` lists promoted durable memories
  - `POST /api/v1/graph/memory/{id}/actions` applies lifecycle actions such as `revoke`
  - `GET /api/v1/graph/memory/proposals` lists proposed durable memories by review status
  - `POST /api/v1/graph/memory/proposals/{id}/review` applies operator approval or rejection
- `agency admin doctor` now separates runtime checks from backend-specific hygiene checks. UI/operator copy should preserve that distinction and should not present Docker hygiene warnings as generic runtime failure.
- Runtime health should come from the runtime APIs and the backend contract, not from hardcoded Docker assumptions such as container names, network names, or enforcer hostnames.
- Durable memory review and revocation should remain visible operator actions. Do not infer approval in the browser or make preference-affecting memory changes appear automatic.

### Real-Time Features

- **Agent activity signals** — Agent signals (`processing`, `activity`, `task_complete`, `error`) stream over WebSocket. `TypingIndicator` renders activity labels (e.g. "searching the web", "composing response") sourced from `agent_signal_activity` events.
- **Instant message display** — Incoming channel messages from WebSocket events are appended directly to the message list with client-side deduplication, avoiding a full history refetch on each new message.
- **WebSocket auto-reconnect** — Disconnections trigger exponential backoff (500ms–10s), with a 5-minute total timeout. A "Reconnecting..." banner is shown during reconnect attempts.
- **Processing badge removed** — Agent processing state is consolidated into `TypingIndicator` with bouncing dots; there is no separate processing badge.

## Project Structure

```
src/
  app/
    components/   # Shared UI components
    screens/      # Page-level screen components
    hooks/        # Custom React hooks
    lib/          # Utilities and helpers
    data/         # Static data, constants
    routes.tsx    # Route definitions
    App.tsx       # Root app component
    types.ts      # Shared TypeScript types
  imports/        # Shared modules
  styles/         # Global styles
  test/           # Test setup (Vitest + jsdom + MSW)
bin/
  agency-web.mjs  # CLI entry point (start/stop/restart/status/build/dev)
  postinstall.mjs # npm postinstall — symlinks CLI, clears old builds
```

## Conventions

- Components use shadcn/ui patterns built on Radix primitives. Prefer these over raw HTML or MUI where possible.
- MUI is available (`@mui/material`, `@mui/icons-material`) but Radix/shadcn is preferred for new components.
- Tailwind v4 — styles via utility classes, not CSS modules.
- Tests use Vitest + React Testing Library + MSW for API mocking.
- Build produces vendor chunks: `vendor-react`, `vendor-mui`, `vendor-radix`, `vendor-charts`, `vendor-icons`.
- Keep feature-gated surfaces explicitly gated in routes, navigation, and live tests. Do not make experimental or non-core product areas look like default/core flows accidentally.
- Preserve the current product line bias: direct messages, agent lifecycle, runtime visibility, and operator diagnostics are the default path; broader platform surfaces remain gated.

## Validation

Use the smallest sufficient frontend validation, but use live browser coverage when the change touches runtime/lifecycle/operator flows.

```bash
npm test
./scripts/e2e/e2e-live-disposable.sh --skip-build
```

The disposable live suite is the highest-signal validation path when changes affect:

- agent create/start/pause/resume/restart
- DM/reply behavior
- runtime-backed agent status or diagnostics
- operator/admin surfaces

## Things to Know

- The `bin` field in `package.json` makes `npx agency-web` work. The postinstall also tries to symlink into Node's bin dir for direct `agency-web` access, but this silently fails when the dir isn't writable (common on macOS).
- `.certs/` is gitignored — never commit TLS certs.
- The preview server runs as a detached daemon. PID and state are stored in `~/.agency/web.pid` and `~/.agency/web.state.json`.
- `npm install` clears the `dist/` folder so the next start rebuilds with fresh code.
- The knowledge export endpoint returns nodes with various `source_type` values: `agent`, `llm`, `rule`, `local`. Do not filter out `source_type: "rule"` — these are connector-ingested nodes (DNS queries, devices, alerts) that operators need to see.
