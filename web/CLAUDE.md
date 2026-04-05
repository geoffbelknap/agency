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

## Things to Know

- The `bin` field in `package.json` makes `npx agency-web` work. The postinstall also tries to symlink into Node's bin dir for direct `agency-web` access, but this silently fails when the dir isn't writable (common on macOS).
- `.certs/` is gitignored — never commit TLS certs.
- The preview server runs as a detached daemon. PID and state are stored in `~/.agency/web.pid` and `~/.agency/web.state.json`.
- `npm install` clears the `dist/` folder so the next start rebuilds with fresh code.
- The knowledge export endpoint returns nodes with various `source_type` values: `agent`, `llm`, `rule`, `local`. Do not filter out `source_type: "rule"` — these are connector-ingested nodes (DNS queries, devices, alerts) that operators need to see.
