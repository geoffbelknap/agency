---
description: "Adds CRUD for operator notification destinations — REST endpoints, CLI commands, and MCP tools. Replaces manual confi..."
---

# Notification Management API

Adds CRUD for operator notification destinations — REST endpoints, CLI commands, and MCP tools. Replaces manual `config.yaml` editing with a proper management layer.

## Status

Implemented.
**Last updated:** 2026-04-01

## Context

The notification delivery pipeline is fully implemented: event bus, ntfy/webhook delivery handlers, signal-to-event promotion, severity-to-priority mapping. The management layer described in this spec is now implemented.

**Implementation details:** REST handlers are in `internal/api/handlers_events.go` (5 notification endpoints: list, add, show, delete, test). Routes registered in `internal/api/routes.go`. NotificationStore implemented in `internal/events/notification_store.go` with tests. CLI commands are `agency notify list/add/remove/test` (shortened from `agency notifications`, with aliases `notifications`, `notification`). MCP tools: `agency_notification_list/add/remove/test` in `internal/api/mcp_credentials.go`. Headers are redacted from GET responses. Hot-reload of event bus subscriptions works on add/remove.

## Design

### Storage

**File:** `~/.agency/notifications.yaml` — a YAML list of notification destinations.

```yaml
- name: operator-alerts
  type: ntfy
  url: https://ntfy.sh/my-agency-ops
  events: [operator_alert, enforcer_exited, mission_health_alert]
- name: pagerduty
  type: webhook
  url: https://events.pagerduty.com/v2/enqueue
  events: [operator_alert]
  headers:
    Authorization: "Token token=xxx"
```

Schema is identical to today's `config.NotificationConfig`. File permissions `0600` (may contain webhook auth headers).

**`NotificationStore`** (in `internal/events/notification_store.go`):

- `NewNotificationStore(home string) *NotificationStore` — takes `~/.agency` path
- `Load() ([]config.NotificationConfig, error)` — reads and parses the file
- `Save(configs []config.NotificationConfig) error` — writes the file
- `List() []config.NotificationConfig` — returns current in-memory state
- `Get(name string) (*config.NotificationConfig, error)` — lookup by name
- `Add(nc config.NotificationConfig) error` — validates, checks name uniqueness, appends, saves
- `Remove(name string) error` — removes by name, saves

Follows the same pattern as `WebhookManager` and `BudgetStore`.

**Migration:** On gateway startup, if `config.yaml` has `notifications:` entries and `notifications.yaml` doesn't exist, copy the entries to `notifications.yaml`. The `config.yaml` entries are left in place (read but ignored once `notifications.yaml` exists) to avoid breaking downgrades.

**Hot-reload:** After any mutation (add/remove), the store rebuilds notification subscriptions in the event bus:

1. `RemoveByOrigin("notification", name)` for the affected entry
2. `BuildNotificationSubscriptions()` for the new/updated entry
3. Add resulting subscriptions to the table

No gateway restart needed.

### REST API

Five endpoints under `/api/v1/notifications`:

| Method | Path | Description |
|---|---|---|
| GET | `/notifications` | List all notification destinations |
| POST | `/notifications` | Add a new destination |
| GET | `/notifications/{name}` | Show a single destination |
| DELETE | `/notifications/{name}` | Remove a destination |
| POST | `/notifications/{name}/test` | Send a test event |

**POST /notifications** request body:

```json
{
  "name": "ops-ntfy",
  "type": "ntfy",
  "url": "https://ntfy.sh/my-topic",
  "events": ["operator_alert", "enforcer_exited", "mission_health_alert"],
  "headers": {}
}
```

- `name`: required, must be unique, validated `^[a-z0-9][a-z0-9-]*[a-z0-9]$`
- `type`: optional, auto-detected from URL if omitted (`ntfy.sh` or `ntfy.` in host → `ntfy`, else `webhook`)
- `url`: required
- `events`: optional, defaults to `["operator_alert", "enforcer_exited", "mission_health_alert"]`
- `headers`: optional, for webhook auth headers

Returns: 201 with the created config, 400 on validation error, 409 if name exists.

**POST /notifications/{name}/test** — no request body. Publishes a synthetic `operator_alert` event into the event bus with `category: test`, `severity: info`, `message: "Test notification from agency"`. The existing delivery pipeline handles routing. Returns 200 with `{"event_id": "evt-...", "status": "sent"}`.

**GET /notifications** and **GET /notifications/{name}** — responses omit the `headers` field to avoid leaking webhook auth tokens.

**DELETE /notifications/{name}** — removes from store, removes subscriptions from event bus. Returns 200 with `{"status": "deleted", "name": "..."}`. Returns 404 if not found.

### CLI Commands

`agency notify` command group (aliases: `notifications`, `notification`). All commands are REST clients to the endpoints above.

| Command | Description |
|---|---|
| `agency notify list` | Table output: name, type, url, events |
| `agency notify add <name> --url <url> [--type ntfy\|webhook] [--events ...]` | Add destination |
| `agency notify remove <name>` | Remove destination |
| `agency notify test <name>` | Send test notification |

- `--type` auto-detected from URL if omitted
- `--events` defaults to `operator_alert,enforcer_exited,mission_health_alert` if omitted

Also: wire `--notify-url` flag on `agency setup`. The `SetupOptions.NotifyURL` field exists but the CLI flag isn't connected. Setup writes to `notifications.yaml` instead of `config.yaml`.

### MCP Tools

Four MCP tools implemented (registered in `mcp_register.go`, dispatched via `mcp_call.go` to the REST API):

- `agency_notification_list` — list destinations
- `agency_notification_add` — add a destination (params: name, url, type?, events?)
- `agency_notification_remove` — remove a destination (params: name)
- `agency_notification_test` — send test notification (params: name)

These route through the gateway's MCP thin proxy to the REST endpoints (same as all MCP tools post-proxy migration).

### API Client

Four methods added to `apiclient/client.go` for CLI consumption:

- `ListNotifications() ([]NotificationConfig, error)`
- `AddNotification(nc NotificationConfig) error`
- `RemoveNotification(name string) error`
- `TestNotification(name string) (string, error)` — returns event ID

### Cleanup

1. Update `operator-notifications.md` spec status from "Design — not yet implemented" to "Implemented"
2. Delete `docs/plans/2026-03-25-operator-notifications.md` (work complete)
3. Add Notifications group to gateway-api.md endpoint reference
4. Update CLAUDE.md with `agency notifications` commands and notification management pattern

## ASK Compliance

- **Tenet 1 (Constraints external):** Notification routing is operator-configured infrastructure. Agents cannot influence where notifications are delivered.
- **Tenet 2 (Every action leaves a trace):** All notification CRUD is audit-logged. Test events are published through the normal event bus with full audit.
- **Tenet 4 (Least privilege):** `notifications.yaml` is `0600`. Webhook auth headers stored in the file are not exposed in list responses (headers field omitted from GET responses).
- **Tenet 17 (Instructions from verified principals only):** Notification management requires gateway auth token. No unauthenticated access.

## Key Files

| File | Role |
|---|---|
| `agency-gateway/internal/events/notification_store.go` | NotificationStore with CRUD + file persistence |
| `agency-gateway/internal/events/notification_store_test.go` | Tests for store operations |
| `agency-gateway/internal/api/handlers_events.go` | 5 notification REST handlers |
| `agency-gateway/internal/api/handlers_notifications_test.go` | Handler tests |
| `agency-gateway/internal/api/routes.go` | Notification routes registered |
| `agency-gateway/internal/api/mcp_register.go` | MCP tool definitions (notification tools registered here) |
| `agency-gateway/internal/apiclient/client.go` | CLI client methods for notifications |
| `agency-gateway/internal/cli/commands.go` | `agency notify` command group (aliases: notifications, notification) |
