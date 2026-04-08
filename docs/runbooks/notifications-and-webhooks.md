# Notifications & Webhooks

## Trigger

Setting up operator alerting, configuring notification destinations, managing inbound webhooks, or troubleshooting event delivery.

## Notification Destinations

Notifications route platform events (agent errors, escalations, mission health alerts) to external services.

### List destinations

```bash
agency notifications list
```

Via API: `GET /api/v1/events/notifications` (headers redacted in responses).

### Add a destination

```bash
# ntfy (auto-detected from URL)
agency notifications add --name ops-alerts --url https://ntfy.sh/my-agency-alerts

# Generic webhook
agency notifications add --name pagerduty --url https://events.pagerduty.com/v2/enqueue \
  --header "Authorization: Token token=my-key"
```

Type is auto-detected from URL: `ntfy.sh` URLs → ntfy type, others → webhook type.

Default subscribed events: `operator_alert`, `enforcer_exited`, `mission_health_alert`.

### Configure subscribed events

```bash
agency notifications add --name ops-alerts \
  --url https://ntfy.sh/my-agency-alerts \
  --events operator_alert,enforcer_exited,mission_health_alert,trajectory_anomaly
```

### Test a destination

```bash
agency notifications test ops-alerts
```

Sends a test event to verify delivery.

### Remove a destination

```bash
agency notifications remove ops-alerts
```

### How delivery works

All notification delivery goes through the gateway event bus. Agent signals (error, escalation, self_halt) are promoted to `operator_alert` platform events by the comms bridge, then routed to matching notification destinations.

Destinations are stored in `~/.agency/notifications.yaml`. The event bus hot-reloads subscriptions on add/remove — no restart needed.

Fallback: if no notification destinations are configured, operator alerts are delivered to the `#operator` channel in agency-web.

## Inbound Webhooks

Webhooks let external services push events into Agency's event bus.

### Create a webhook

```bash
agency webhook create --name github-events --event-type connector
```

Returns: webhook URL and secret. The URL is `POST /api/v1/events/webhook/{name}`.

### List webhooks

```bash
agency webhook list
```

### Show webhook details

```bash
agency webhook show github-events
```

### Delete a webhook

```bash
agency webhook delete github-events
```

### Rotate webhook secret

```bash
agency webhook rotate github-events
```

Generates a new HMAC secret. Update the sending service with the new secret.

### Webhook authentication

Inbound webhooks are validated using HMAC-SHA256 signatures. The sending service must include the signature in the request. Rate limited to 60 requests/minute per webhook name.

### Intake webhooks

The intake service has its own webhook endpoint for connector data:

```bash
agency intake webhook   # trigger manual intake
```

Intake webhook auth is enforced via `AGENCY_INTAKE_REQUIRE_AUTH` env var on the intake container.

## Event Bus

The gateway runs a unified event bus. Events come from:

| Source Type | Examples |
|-------------|---------|
| `connector` | Connector poll results |
| `channel` | Channel messages |
| `schedule` | Scheduled triggers |
| `webhook` | Inbound webhook payloads |
| `platform` | Agent signals, infra events |

### View events

```bash
agency event list
agency event show <event-id>
```

### View subscriptions

```bash
agency event subscribe   # show current subscriptions
```

Via API: `GET /api/v1/events/subscriptions`

Subscriptions are derived from missions + notification config. At-most-once delivery. 1000-event ring buffer.

## Intake

The intake service manages work items from connectors and webhooks:

```bash
agency intake items    # list pending work items
agency intake stats    # intake statistics
agency intake poll <connector>  # trigger immediate poll
```

### Intake poll health

Via API: `GET /api/v1/hub/intake/poll-health`

## Troubleshooting

### Notifications not delivering

1. Test the destination:
   ```bash
   agency notifications test <name>
   ```

2. Check event bus is running:
   ```bash
   agency infra status
   ```

3. Check the destination URL is reachable from the gateway:
   ```bash
   curl -sf <destination-url>
   ```

4. Check gateway logs for delivery errors:
   ```bash
   tail -50 ~/.agency/gateway.log | grep -i "notif"
   ```

### Webhook payloads not arriving

1. Verify the webhook exists and has the correct event type:
   ```bash
   agency webhook show <name>
   ```

2. Check rate limiting (60 req/min per webhook):
   ```bash
   agency event list   # look for rejected events
   ```

3. Verify HMAC signature in the sending service matches the webhook secret.

### Events not reaching agents

Events are routed to agents via mission subscriptions. Verify:

1. The mission is assigned and active: `agency mission show <name>`
2. The event type matches a mission subscription
3. The agent is running: `agency show <agent-name>`

## Verification

- [ ] `agency notifications list` shows configured destinations
- [ ] `agency notifications test <name>` delivers successfully
- [ ] `agency webhook list` shows configured webhooks
- [ ] `agency event list` shows recent events
- [ ] Agent responds to events within expected timeframe

## See Also

- [Mission Management](mission-management.md) — event subscriptions via missions
- [Infrastructure Recovery](infrastructure-recovery.md) — event bus and intake issues
- [Monitoring & Observability](monitoring-and-observability.md) — signal-to-notification flow
