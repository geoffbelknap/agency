# Agency Web E2E Coverage Tiers

This test taxonomy keeps default live coverage trustworthy while still allowing full product coverage when you explicitly opt into higher-blast-radius flows.

## Tiers

### `mocked`

Deterministic browser coverage for route rendering, empty states, and edge conditions that are hard or unsafe to force against a live stack.

Current suites:
- `tests/e2e/setup-smoke.spec.ts`
- `tests/e2e/app-routes.spec.ts`
- `tests/e2e/admin-tabs.spec.ts`

### `live-safe`

Default live suite. Runs against a real local Agency stack and may create temporary data, but must clean up after itself and must not trigger destructive or external side effects.

Current suites:
- `tests/e2e-live/platform-shell.spec.ts`
- `tests/e2e-live/app-surface.spec.ts`
- `tests/e2e-live/mutable-flows.spec.ts`

Recommended disposable runner:
- `./scripts/e2e-live-disposable.sh --skip-build`

### `live-risky`

Opt-in live suite for flows that can talk to outside systems, install or remove shared components, toggle connectors, or otherwise perturb a developer environment beyond simple CRUD cleanup.

Suite location:
- `tests/e2e-live-risky/`

Recommended disposable runner:
- `./scripts/e2e-live-disposable.sh --skip-build --risky`

Related operator-path live scripts:
- `./scripts/test-live-hub-oci.sh`
- `./scripts/test-live-hub-operator-oci.sh`

`test-live-hub-operator-oci.sh` uses a disposable Agency home and isolated gateway port to validate the normal operator CLI/API path against the published GHCR hub catalog. It verifies that connector, service, provider, routing, setup, and skill artifacts sync from OCI; Markdown skills and the default setup wizard are searchable; setup config is served from the OCI cache; hub-managed routing remains update/upgrade surface rather than an installable search result; and, when `cosign` is installed, provider install/remove verifies signatures and cleans routing.

### `live-danger`

Explicit opt-in only. Covers high-blast-radius actions that can destroy shared state or tear down the environment.

Suite location:
- `tests/e2e-live-danger/`

Recommended runner:
- `./scripts/e2e-live-danger-disposable.sh`

The disposable runner clones the current Agency home, assigns an isolated infra namespace, binds alternate host ports, and then runs the guarded danger suite. `Destroy All` tears down the web proxy serving the browser, so live-danger browser assertions verify the explicit confirmation and resulting web shutdown rather than expecting a same-origin response body to survive teardown.

## Classification

### Safe

- Top-level route rendering
- Admin section traversal, excluding destructive action execution
- Agent detail navigation and tab changes
- Knowledge mode switching and read-only graph/search traversal
- Profile create, edit, delete
- Webhook create, rotate secret, delete
- Notification destination add and remove
- Preset create, edit, delete

### Risky

- Notification test-send
- Channel create/send/archive flows that alter shared comms history
- Agent create/start/pause/resume/restart
- Mission create, update, assign, pause, resume, complete, delete
- Capability add, enable, disable, delete
- Connector activate, deactivate, configure
- Hub install, remove, upgrade, deploy, teardown
- Setup wizard steps that store credentials, install providers, or sync hub content
- Infrastructure rebuild, reload, start, stop
- Team create and membership changes
- Knowledge ontology promote/reject/restore
- Audit summarize if it mutates server-side summaries or caches

### Danger

- `Admin > Danger Zone > Destroy All`
- Any future full-reset, wipe, or irreversible teardown operation

## Coverage Inventory

### Covered now

- Full initialized route surface
- Full admin route surface, including visibility of Danger Zone controls
- Read-only direct entity drill-downs
- Interactive non-mutating browser flows
- Safe live CRUD flows for profiles, webhooks, notifications, and presets
- Risky live capability add / enable / disable / delete
- Risky live channel create / send / archive
- Risky live team create / delete with cleanup
- Risky live notification test-send to a contained local sink
- Risky live hub install / remove for an eligible local catalog component
- Risky live mission create / update / delete for an unassigned mission
- Risky live assigned mission pause / resume / complete / delete with cleanup
- Risky live agent create / start / pause / resume / restart / delete with observable lifecycle state
- Risky live connector install / deactivate / reactivate with cleanup
- Risky disposable live connector setup / configure / activate with cleanup
- Risky live pack deploy / teardown for an installed pack
- Risky live ontology promote / reject / restore with deterministic seed and cleanup
- Ontology candidate contract normalized across web, REST, CLI, and MCP paths
- Live Hub OCI manager and operator-path catalog coverage against GHCR

### Explicitly excluded from default live suite

- Outbound notification sends
- Connector activation/configuration against external services
- Hub install/remove/upgrade/deploy/teardown
- Infrastructure lifecycle actions
- Setup wizard side effects
- Danger Zone execution

## Danger Guardrails

- `live-danger` only runs with explicit opt-in.
- Harness guard: `./scripts/e2e-live-web.sh --allow-danger --danger-confirm destroy-all --config playwright.live.danger.config.ts`
- Direct Playwright guard: `AGENCY_E2E_ALLOW_DANGER=1 AGENCY_E2E_DANGER_CONFIRM=destroy-all`
- The first destructive flow is `Destroy All`, and it is intended for disposable local stacks only.
