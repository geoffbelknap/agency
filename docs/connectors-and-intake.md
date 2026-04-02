---
title: "Connectors and Intake"
description: "Connectors bring work from external systems into Agency. The intake service receives incoming work, applies routing rules, and delivers it to agents or teams."
---


Connectors bring work from external systems into Agency. The intake service receives incoming work, applies routing rules, and delivers it to agents or teams.

## Source Types

Agency supports four types of connectors, each suited to different integration patterns.

### Webhook

Receives HTTP push events from external services. Best for real-time integrations.

```yaml
source:
  type: webhook
  config:
    path: /hooks/slack-events
    secret_env: SLACK_SIGNING_SECRET
    verification:
      type: hmac-sha256
      header: X-Slack-Signature
```

**Use cases:** Slack Events API, GitHub webhooks, PagerDuty alerts, custom webhook endpoints.

The intake service runs an HTTP listener that receives webhook payloads, verifies signatures, and routes work items to agents.

### Poll

Periodically checks an API for changes. Uses SHA-256 change detection to avoid processing the same data twice.

```yaml
source:
  type: poll
  config:
    url: https://api.example.com/issues
    interval: 5m
    auth_env: API_TOKEN
    change_detection: sha256
```

**Use cases:** Jira ticket monitoring, RSS feeds, API endpoints without webhook support.

The poll loop runs in the background, fetching the API at the configured interval and comparing response hashes. Only changed responses generate new work items.

### Schedule

Triggers tasks on a cron schedule. Includes double-fire prevention to avoid duplicate work items.

```yaml
source:
  type: schedule
  config:
    cron: "0 9 * * 1-5"    # Weekdays at 9am
    task_template: "Run the daily security scan"
```

**Use cases:** Daily reports, periodic scans, scheduled maintenance tasks.

### Channel-Watch

Matches regex patterns in agent communication channels. Creates work items when patterns match.

```yaml
source:
  type: channel-watch
  config:
    channel: escalations
    patterns:
      - regex: "CRITICAL|URGENT"
        priority: high
      - regex: "review requested"
        priority: normal
```

**Use cases:** Escalation detection, keyword-triggered workflows, inter-agent coordination.

## Event Delivery

Connectors deliver events through the [event bus](/events). When a connector receives data from an external system, it wraps the data as an event and passes it to the gateway's event router. Mission triggers and subscriptions determine which agents receive the event.

This replaces direct task delivery — connectors produce events, and the event framework routes them to the right agents based on their mission subscriptions.

## Routing

Each connector defines routing rules that determine where work items go:

```yaml
routing:
  default_target: security-ops        # Team name
  rules:
    - match:
        field: priority
        value: critical
      target: lead-agent              # Specific agent
    - match:
        field: source
        value: slack
      target: slack-responder
```

Work items can be routed to:
- A specific agent (by name)
- A team (the coordinator receives it)
- The default target (catch-all)

### Task Templates

Connectors can include Jinja2 templates that format the incoming data into a task brief:

```yaml
task_template: |
  New {{ source }} event received:

  **Summary:** {{ payload.summary }}
  **Priority:** {{ payload.priority }}
  **Details:** {{ payload.description }}

  Please investigate and report findings to the findings channel.
```

## Managing Connectors

### List Connectors

```bash
agency connector list
```

Shows all registered connectors, their type, status (active/inactive), and last activity.

### Activate and Deactivate

```bash
agency connector activate slack-events
agency connector deactivate slack-events
```

Activating a connector starts it — webhook listeners begin accepting requests, poll loops start running, schedules start firing.

### Check Status

```bash
agency connector status slack-events
```

Shows detailed status: last trigger time, work items generated, errors, and health.

## Intake Service

The intake service is shared infrastructure that manages all connectors:

```bash
agency intake items                   # List work items
agency intake stats                   # Intake statistics
```

### Work Item States

Each incoming piece of work goes through a state machine:

```
received → queued → assigned → in_progress → completed
                                            → failed
                                            → escalated
```

Work items are stored in SQLite with full state tracking.

### Rate Limiting

The intake service enforces rate limits to prevent external systems from overwhelming agents. Excess work items are queued and processed in order.

## Example: Slack Integration

A connector that receives Slack events and routes them to a response team:

```yaml
name: slack-events
description: "Slack Events API integration"
source:
  type: webhook
  config:
    path: /hooks/slack
    secret_env: SLACK_SIGNING_SECRET
    verification:
      type: hmac-sha256
      header: X-Slack-Signature
      timestamp_header: X-Slack-Request-Timestamp
routing:
  default_target: slack-ops
  rules:
    - match:
        field: event.type
        value: app_mention
      target: slack-responder
task_template: |
  Slack event from {{ event.user }}:
  {{ event.text }}
```

## Example: Jira Polling

A connector that polls Jira for new issues:

```yaml
name: jira-poll
description: "Jira issue polling"
source:
  type: poll
  config:
    url: https://mycompany.atlassian.net/rest/api/3/search
    interval: 5m
    auth_env: JIRA_API_TOKEN
    change_detection: sha256
    query_params:
      jql: "project = OPS AND status = Open"
routing:
  default_target: ops-team
task_template: |
  New Jira issue: {{ key }} - {{ fields.summary }}
  Priority: {{ fields.priority.name }}
  Description: {{ fields.description }}
```

## Installing Connectors from the Hub

Pre-built connectors are available from the [Hub](/hub):

```bash
agency hub search connector
agency hub install slack-events
agency connector activate slack-events
```
