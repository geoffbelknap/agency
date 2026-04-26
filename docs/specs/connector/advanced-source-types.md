# Advanced Source Types

## Goal

Extend the connector system beyond webhooks with three new source types: poll, schedule, and channel-watch. All source types produce work items that flow through the existing routing/delivery pipeline.

## Architecture

All three source types run as async background tasks inside the existing intake service. No new containers. `ConnectorSource.type` expands from `Literal["webhook"]` to `Literal["webhook", "poll", "schedule", "channel-watch"]`, with type-specific fields on ConnectorSource.

## Poll Source

Periodically fetches an external API and creates work items for new/changed data.

Connector YAML fields:
- `url` — the endpoint to fetch
- `method` — HTTP method (default GET)
- `headers` — optional request headers (dict)
- `interval` — polling frequency (e.g., `5m`, `1h`)
- `response_key` — optional JSON path to a list field; each list item is hashed individually and gets its own work item; without this, the entire response is hashed as one blob

Implementation:
- Async task per active poll connector in the intake service
- On each tick: fetch URL, hash response (or per-item if `response_key` set), compare against stored hashes in SQLite `poll_state` table, create work items for new/changed entries
- Poll failures: log warning, retry next interval; after 3 consecutive failures log error and continue (no auto-disable)

Example:
```yaml
source:
  type: poll
  url: "https://api.github.com/repos/owner/repo/issues?state=open"
  method: GET
  headers:
    Accept: "application/vnd.github.v3+json"
  interval: "5m"
  response_key: "$"
```

## Schedule Source

Creates work items on a cron schedule. Useful for periodic reports, daily standups, weekly reviews.

Connector YAML fields:
- `cron` — cron expression (e.g., `0 9 * * 1-5` for weekday mornings)

Implementation:
- Single asyncio task checks all schedule connectors every 60 seconds
- When a cron expression matches: create work item with synthetic payload `{"triggered_at": "...", "connector": "..."}`
- Route brief template can use `{{ now }}` and `{{ schedule_name }}` variables
- Last-fired timestamps stored in `schedule_state` table to prevent double-firing
- If service was down when schedule should have fired, do NOT retroactively fire — only fire on present-time matches

Example:
```yaml
source:
  type: schedule
  cron: "0 9 * * 1-5"
```

## Channel-Watch Source

Monitors a comms channel for messages matching a pattern and creates work items.

Connector YAML fields:
- `channel` — comms channel name to monitor
- `pattern` — regex pattern to match against message content

Implementation:
- Async task per active channel-watch connector
- Polls comms service for new messages, tracking last-seen message ID in `channel_watch_state` table
- Applies regex; matching messages become work item payloads
- Comms unavailable: log warning, retry next tick (no auto-disable)

Example:
```yaml
source:
  type: channel-watch
  channel: "support-requests"
  pattern: "^/request\\s+"
```

## Connector Model Changes

`ConnectorSource` gains optional fields for each source type. Validation ensures the right fields are present for each type:
- `webhook`: no additional required fields (existing behavior)
- `poll`: `url` and `interval` required
- `schedule`: `cron` required
- `channel-watch`: `channel` and `pattern` required

## Intake Service Changes

Three new async background tasks started on app startup:
- `_poll_loop()` — manages per-connector poll timers
- `_schedule_loop()` — single 60s tick checking all cron expressions
- `_channel_watch_loop()` — manages per-connector channel monitors

All three use the existing `WorkItemStore` and `_deliver_task`. Three new SQLite tables for state tracking: `poll_state`, `schedule_state`, `channel_watch_state`.

SIGHUP reload (already implemented) picks up new/changed connectors for all source types.

## New Dependencies

- `croniter` — cron expression parsing (lightweight, well-maintained, no transitive deps)

## Testing

- Unit tests for poll hashing (blob mode, per-item mode, response_key extraction)
- Unit tests for cron matching and double-fire prevention
- Unit tests for channel-watch pattern matching and message tracking
- Integration tests for each source type creating work items through the routing/delivery pipeline
