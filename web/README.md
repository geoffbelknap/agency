
# Agency Web

The web frontend for [Agency](https://github.com/geoffbelknap/agency). Built with React 19, Vite 8, Tailwind CSS v4, and shadcn/ui components.

## Prerequisites

- **Node.js** v22 or later
- **npm** v10 or later
- **mkcert** for local HTTPS (`brew install mkcert` on macOS, `sudo apt install mkcert` on Linux)
- A running Agency gateway (defaults to `127.0.0.1:8200`)

## Quick Start

```bash
# Install dependencies
npm install

# Start the web UI (builds, generates TLS certs, and serves on port 8280)
npx agency-web
```

The app will be available at `https://localhost:8280`.

## Features

- **Agents** — list, detail view with grouped tabs (Overview, Activity, Operations, System), memory panel, signal renderer
- **Channels** — real-time messaging with WebSocket push, typing indicators, instant message display
- **Missions** — list, detail, creation wizard with cost/quality step
- **Knowledge** — browser (all nodes by kind), interactive graph visualization, full-text search
- **Usage** — LLM usage metrics by agent/model/provider/source, recent errors with full detail
- **Teams** — team membership and activity view
- **Admin** — infrastructure status, hub management, capabilities, intake stats, events, webhooks, notifications, presets, policy, egress, trust, audit, doctor
- **Real-time** — WebSocket auto-reconnect with exponential backoff (500ms-10s), agent activity signals, processing indicators

## CLI Usage

```
npx agency-web [command] [options]
```

| Command   | Description                                      |
| --------- | ------------------------------------------------ |
| `start`   | Build and serve in the background (default)      |
| `stop`    | Stop the background server                       |
| `restart` | Stop then start                                  |
| `status`  | Check if the server is running                   |
| `build`   | Build for production without starting            |
| `dev`     | Start the Vite dev server (foreground, with HMR) |

| Option         | Description                                      |
| -------------- | ------------------------------------------------ |
| `--port PORT`  | Serve on a custom port (default: 8280)           |
| `--host HOST`  | Bind to a specific interface or IP (default: all) |

Examples:

```bash
npx agency-web                     # Start on default port 8280
npx agency-web --port 9000         # Start on a custom port
npx agency-web --host eth0         # Bind to a specific interface
npx agency-web stop                # Stop the server
npx agency-web status              # Check if running
npx agency-web dev                 # Dev mode with hot reload
```

## Gateway Configuration

The dev server reads your gateway address and auth token from `~/.agency/config.yaml`:

```yaml
gateway_addr: 127.0.0.1:8200
token: your-token-here
```

API requests to `/api/v1` and WebSocket connections on `/ws` are automatically proxied to the gateway.

## Development Scripts

| Command              | Description                      |
| -------------------- | -------------------------------- |
| `npm run dev`        | Start the Vite dev server        |
| `npm run build`      | Build for production             |
| `npm test`           | Run tests once (Vitest)          |
| `npm run test:watch` | Run tests in watch mode          |
| `npm run test:e2e`   | Run mocked Playwright browser smoke tests |
| `npm run test:e2e:live` | Run Playwright against a live local Agency stack |

## Browser E2E

Install Playwright locally:

```bash
cd web
npm install
npx playwright install chromium
```

Mocked browser smoke:

```bash
cd web
npm run test:e2e
```

Live local stack smoke:

```bash
./scripts/e2e-live-web.sh
```

The live harness is dev-only. It lives in repo test paths and scripts, uses `devDependencies`, and is not part of the shipped runtime or container images.

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
  test/           # Test setup and helpers
bin/
  agency-web.mjs  # CLI entry point
  postinstall.mjs # npm postinstall
```

## License

Apache 2.0 — see [LICENSE](LICENSE).
