---
title: "Events and Webhooks"
description: "A unified event routing system that connects external systems, agent activation, and operator notifications through a single event bus."
---


The event framework is a unified routing layer inside the gateway. Every agent activation, operator notification, and external integration flows through the same model: sources emit events, the bus routes them by subscription, subscribers act on matching events.

No polling. No keyword scanning. No separate code paths for different event types.

## How Events Flow

```
[Sources]                    [Event Bus]              [Subscribers]
connector: jira-ops    -->                       -->  henrybot900 (mission trigger)
connector: slack       -->    Gateway Event      -->  analyst (mission trigger)
channel: #incidents    -->    Router              -->  mks-a1b2 (@mention)
platform: agent_halted -->                       -->  operator notifications
schedule: daily-scan   -->                       -->  compliance-bot (mission trigger)
webhook: deploy-hook   -->                       -->  POST https://ntfy.sh/alerts
```

1. Source produces a raw event (HTTP POST, WebSocket message, cron fire, internal state change)
2. Gateway wraps it in a standard envelope and assigns an ID
3. Gateway evaluates all active subscriptions against the event
4. Matching events are delivered to each subscriber
5. Every step is logged

The event bus is not a separate service — it is a routing layer inside the gateway. No message queue, no external dependencies.

## Event Sources

### Connector

[Connectors](/connectors-and-intake) deliver events through the event bus. When a connector receives data from an external system, it becomes an event:

```json
{
  "id": "evt-a1b2c3d4",
  "source_type": "connector",
  "source_name": "jira-ops",
  "event_type": "issue_created",
  "timestamp": "2026-03-24T10:00:00Z",
  "data": {
    "key": "INC-1234",
    "summary": "Production database latency spike",
    "priority": "P1"
  }
}
```

### Channel

Channel messages from the comms system are wrapped as events. The gateway filters first and only delivers matching events to subscribed agents.

```json
{
  "source_type": "channel",
  "source_name": "incidents",
  "event_type": "message",
  "data": {"content": "severity:critical - database latency spike"},
  "metadata": {"author": "operator", "channel": "incidents"}
}
```

Two hard-coded routing rules bypass subscriptions:
- **Direct @mentions** — always delivered to the mentioned agent
- **Operator DMs** — always delivered to the target agent

These are unconditional. No subscription needed.

### Schedule

A cron-like timer inside the gateway. No external cron daemon required.

```json
{
  "source_type": "schedule",
  "source_name": "daily-compliance-scan",
  "event_type": "cron_fired",
  "data": {"schedule": "0 9 * * MON-FRI", "fire_time": "2026-03-24T09:00:00-07:00"}
}
```

Schedules are defined in [mission triggers](/missions#triggers):

```yaml
triggers:
  - source: schedule
    name: daily-compliance-scan
    cron: "0 9 * * MON-FRI"
```

When a mission with schedule triggers is assigned, the gateway registers the cron expression. On pause or completion, the schedule is deactivated or removed.

### Webhook

Inbound HTTP endpoint on the gateway for external systems. Any system that can POST JSON can trigger agent work.

```
POST /api/v1/events/webhook/deploy-notifications
Content-Type: application/json
X-Webhook-Secret: {secret}

{"deployment": "prod-v2.3", "status": "complete", "commit": "abc123"}
```

The gateway wraps the POST body as event data:

```json
{
  "source_type": "webhook",
  "source_name": "deploy-notifications",
  "event_type": "deployment_complete",
  "data": {"deployment": "prod-v2.3", "status": "complete", "commit": "abc123"}
}
```

Webhook registration is required — unregistered names get a 404. See [Webhook Management](#webhook-management) below.

### Platform

Internal gateway events exposed as first-class events. These are state changes the gateway already knows about:

**Agent lifecycle:** `agent_started`, `agent_stopped`, `agent_halted`, `agent_restarted`

**Mission lifecycle:** `mission_assigned`, `mission_paused`, `mission_resumed`, `mission_completed`, `mission_updated`, `mission_health_alert`

**Capability changes:** `capability_granted`, `capability_revoked`

**Meeseeks lifecycle:** `meeseeks_spawned`, `meeseeks_completed`, `meeseeks_distressed`, `meeseeks_terminated`

**Budget:** `budget_daily_exhausted`, `budget_monthly_exhausted`, `budget_input_rejected`

**Infrastructure:** `infra_up`, `infra_down`

Platform events enable missions that react to system state:

```yaml
triggers:
  - source: platform
    event_type: agent_halted
```

"When any agent is halted, investigate and report to operator."

## Subscriptions

A subscription routes events matching a pattern to a destination. Subscriptions are created implicitly — you don't manage them directly.

### How Missions Create Subscriptions

When a [mission](/missions) is assigned, its triggers create subscriptions automatically:

```
mission "ticket-triage" assigned to henrybot900
  -> subscribe: connector/jira-ops/issue_created -> henrybot900
  -> subscribe: channel/incidents/severity:*      -> henrybot900
```

When the mission is paused, subscriptions are deactivated. When resumed, they reactivate. When completed, they are removed.

### How Notifications Create Subscriptions

Operator notification config creates outbound subscriptions:

```
notifications config has ntfy for mission_health
  -> subscribe: platform/*/mission_health -> POST https://ntfy.sh/my-alerts
```

## Inbound Webhooks

### Registering a Webhook

```bash
agency webhook create deploy-notifications --event-type deployment_complete
```

This creates a webhook with:
- A unique name (used as the URL path segment)
- A generated secret token (for authentication)
- A required event type (all events from this webhook use this type)

The webhook URL is: `POST /api/v1/events/webhook/deploy-notifications`

External systems include the secret in the `X-Webhook-Secret` header. Requests with invalid or missing secrets are rejected.

### Webhook Management

```bash
agency webhook create <name> --event-type <type>   # Register webhook, get secret
agency webhook list                                 # List registered webhooks
agency webhook show <name>                          # Show URL and secret
agency webhook rotate-secret <name>                 # New secret, same URL
agency webhook delete <name>                        # Remove webhook
```

If a webhook needs to emit multiple event types, register multiple webhooks.

## Outbound Notifications

Configure external notification forwarding in `~/.agency/config.yaml`:

```yaml
notifications:
  - name: ops-alerts
    type: ntfy
    url: https://ntfy.sh/my-agency-alerts
    events: [mission_health, agent_halted, quarantine, meeseeks_distressed]

  - name: slack-feed
    type: webhook
    url: https://hooks.slack.com/services/T00/B00/xxx
    events: [mission_health, mission_completed, agent_started]
    headers:
      Content-Type: application/json
```

When an event's type matches an entry in `events`, the gateway POSTs the event envelope to the URL. For `type: ntfy`, the gateway formats the body as ntfy expects (title, message, priority).

Delivery is fire-and-forget with one retry on failure. Failed deliveries are logged in the audit trail.

## Event Delivery to Agents

When an event matches an agent subscription:

1. Gateway wraps the event data into a task
2. Task is delivered via the comms path (POST to comms, WebSocket to body runtime)
3. Agent processes the event with its mission instructions in the system prompt

The agent sees the trigger context:

```
[Mission trigger: connector jira-ops / issue_created]

New event from jira-ops:
  Key: INC-1234
  Summary: Production database latency spike
  Priority: P1

Process this according to your mission instructions.
```

Events are data, not instructions. The event tells the agent "something happened." The agent's mission instructions (from the operator) define how to respond.

## Event Observability

The gateway retains the last 1000 events in a ring buffer. Older events are in the audit log.

```bash
agency event list
```

```
  ID              Source                   Type              Delivered To
  -----------------------------------------------------------------------
  evt-a1b2c3      connector/jira-ops       issue_created     henrybot900
  evt-d4e5f6      channel/incidents        message           henrybot900
  evt-g7h8i9      schedule/daily-scan      cron_fired        compliance-bot
  evt-j0k1l2      platform/gateway         agent_halted      POST ntfy.sh
```

```bash
agency event show evt-a1b2c3          # Full event detail with delivery log
agency event subscriptions             # Active subscriptions with source, pattern, destination
```

## CLI Commands

| Command | Description |
|---------|-------------|
| `agency event list` | Recent events with routing info |
| `agency event show <id>` | Full event detail with delivery log |
| `agency event subscriptions` | Active subscriptions |
| `agency webhook create <name> --event-type <type>` | Register inbound webhook |
| `agency webhook list` | List registered webhooks |
| `agency webhook show <name>` | Show webhook URL and secret |
| `agency webhook rotate-secret <name>` | New secret, same URL |
| `agency webhook delete <name>` | Remove webhook |

## Example: GitHub Webhook to Trigger an Agent

Set up a webhook to trigger an agent when a deployment completes.

### 1. Register the webhook

```bash
agency webhook create github-deploy --event-type deployment_complete
```

Note the URL and secret from the output.

### 2. Create a mission with a webhook trigger

```yaml
name: deploy-monitor
description: Monitor deployments and run post-deploy checks
instructions: |
  When a deployment completes, run the post-deployment checklist:
  1. Check health endpoints for the deployed service
  2. Verify no error rate spike in the last 5 minutes
  3. Post results to #deployments

triggers:
  - source: webhook
    name: github-deploy
    event_type: deployment_complete

requires:
  channels: [deployments]
```

### 3. Assign to an agent

```bash
agency mission create deploy-monitor.yaml
agency mission assign deploy-monitor ops-bot
```

### 4. Configure GitHub

In your GitHub repo settings, add a webhook:
- URL: `https://your-gateway:8200/api/v1/events/webhook/github-deploy`
- Secret: the secret from step 1
- Events: Deployments

Now every GitHub deployment fires an event through the bus, wakes `ops-bot`, and triggers the post-deploy checklist.

## Next Steps

- **[Missions](/missions)** — Define triggers that create event subscriptions
- **[Meeseeks](/meeseeks)** — Ephemeral agents spawned in response to events
- **[Connectors and Intake](/connectors-and-intake)** — How connectors produce events
