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

### `live-risky`

Opt-in live suite for flows that can talk to outside systems, install or remove shared components, toggle connectors, or otherwise perturb a developer environment beyond simple CRUD cleanup.

Suite location:
- `tests/e2e-live-risky/`

### `live-danger`

Explicit opt-in only. Covers high-blast-radius actions that can destroy shared state or tear down the environment.

Suite location:
- `tests/e2e-live-danger/`

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
- Risky live agent create / start / pause / resume / restart / delete with observable lifecycle state
- Risky live connector install / deactivate / reactivate with cleanup
- Risky live pack deploy / teardown for an installed pack
- Risky live ontology promote / reject / restore with deterministic seed and cleanup
- Ontology candidate contract normalized across web, REST, CLI, and MCP paths

### Conditionally exercised when the local stack has prerequisites

- Connector configure / activate against a real local target only when credentials and requirements are present

### Blocked `live-risky` targets

- Assigned mission lifecycle cleanup until pause/complete/delete semantics are proven stable end-to-end.

### Explicitly excluded from default live suite

- Outbound notification sends
- Connector activation/configuration against external services
- Hub install/remove/upgrade/deploy/teardown
- Infrastructure lifecycle actions
- Setup wizard side effects
- Danger Zone execution
