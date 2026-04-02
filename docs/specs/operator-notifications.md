---
description: "Replaces the direct _post_operator_notification pattern in the body runtime with proper event-driven notification del..."
---

# Operator Notifications & Report Delivery

Replaces the direct `_post_operator_notification` pattern in the body runtime with proper event-driven notification delivery through the existing event bus. Adds default notification config to `agency setup` and moves artifact links from `#operator` to the task's source channel.

## Status

Implemented. Notification delivery pipeline, signal promotion, and management API are all live.

## Problem

The body runtime has 7 callsites that post directly to the `#operator` comms channel, bypassing the event bus entirely. This was built before the event framework existed. It has three problems:

1. **No external delivery.** Operator alerts only appear in agency-web's #operator channel. If nobody is watching, alerts are missed.
2. **Report delivery is misdirected.** Task result artifacts post to #operator instead of the channel where the task was requested.
3. **Hardcoded routing.** The agent decides where notifications go (always #operator). The operator has no control over delivery destination.

The event bus already supports ntfy push notifications, outbound webhooks, and agent delivery — with subscription-based routing, audit logging, and retry. The body runtime just isn't using it.

## Design

### 1. Signal-to-Event Promotion

The body runtime already emits signals via comms for every alertable condition. The comms relay in the gateway already bridges signals to the WebSocket hub. We extend the relay to also publish certain signals as platform events into the event bus.

**Promotable signal types:**

| Signal Type | Category | Severity |
|---|---|---|
| `halt` | halt | critical |
| `escalation` | escalation | critical |
| `budget_exhausted` | budget | critical |
| `enforcer_unreachable` | enforcer | critical |
| `constraint_violation` | constraint | warning |
| `tool_error` | tool | warning |
| `comms_error` | comms | warning |

When the comms relay receives a signal with `signal_type` in this set, it calls:

```go
events.EmitAgentEvent(bus, "operator_alert", agentName, map[string]interface{}{
    "category": signalData["category"],
    "severity": signalData["severity"],
    "message":  signalData["message"],
    "task_id":  signalData["task_id"],
})
```

One event type (`operator_alert`) with a `category` field. Keeps subscription config simple — the operator subscribes to `operator_alert` and gets everything, or filters by category in a future iteration.

**Body runtime changes:**

- Remove `_post_operator_notification` method entirely.
- Remove all 7 callsites. Each already calls `_emit_signal()` immediately before the operator post — the signal is the notification now.
- Add `severity` field to the signal data dict at each callsite (`critical` or `warning` per table above).
- Add `message` field with the human-readable string (the same text currently passed to `_post_operator_notification`).

**Comms relay changes** (`comms_relay.go`):

- Add a `promotableSignals` set matching the signal types above.
- In the signal detection path (where it already checks for `agent_signal_*` messages), add: if signal type is in the promotable set, call `EmitAgentEvent` to publish as `operator_alert`.
- The signal still broadcasts via WebSocket as before (agency-web still sees it in real-time). The event bus publication is additive.

### 2. Default Notification Config

**`agency setup` prompt:**

After existing setup steps, prompt:

```
Operator alert notifications (optional)
  Supported: ntfy URL, webhook URL, or press Enter to skip
  URL:
```

- **ntfy detection:** URL contains `ntfy.sh` or `ntfy.` in the host → `type: ntfy`. The last path segment is the topic.
- **Webhook detection:** All other URLs → `type: webhook`.
- **Skip:** No external notification. Alerts still appear in #operator channel via comms relay WebSocket broadcast (agency-web picks these up as before).

**Config written to `~/.agency/config.yaml`:**

```yaml
notifications:
  - name: operator-alerts
    type: ntfy  # or webhook
    url: https://ntfy.sh/my-agency-alerts
    events:
      - operator_alert
      - enforcer_exited
      - mission_health_alert
```

This uses the existing `notifications:` config section and `BuildNotificationSubscriptions()` — no new config machinery needed.

**Fallback behavior:**

The comms relay always broadcasts operator-alertable signals to the WebSocket hub regardless of notification config. This means agency-web's #operator channel shows alerts even if no external notification is configured. The external notification (ntfy/webhook) is additive, not a replacement for in-app visibility.

**Severity-to-priority mapping (ntfy):**

The ntfy delivery handler already infers priority from metadata. We formalize it:

| Severity | ntfy Priority |
|---|---|
| `critical` | 5 (max) |
| `warning` | 3 (default) |
| `info` | 2 (low) |

### 3. Artifact Links in Source Channel

**Current behavior:** Agent calls `complete_task(summary=...)` → body writes `/workspace/.results/{task_id}.md` → posts to #operator with `has_artifact: true`.

**New behavior:**

- **Length threshold:** If the result summary exceeds 25 lines, write the artifact and include a link in the response. Below 25 lines, inline the full response with no artifact.
- **Explicit attachment mode:** The task metadata can include `report: true` to force artifact generation regardless of length. Set by the sender: `agency send scout "analyze the logs" --report`.
- **Post to source channel:** The task completion response posts to the channel the task originated from (already tracked in `task["source"]`). DM tasks respond in the DM channel. Channel mentions respond in the originating channel. The #operator channel is not involved.
- **Artifact metadata on message:** When an artifact is generated, the comms message includes metadata `has_artifact: true`, `agent: <name>`, `task_id: <id>`. agency-web already renders "View full report" and "Download .md" links from this metadata — no web UI changes needed.
- **Signal carries artifact reference:** The `task_complete` signal includes `has_artifact: true` and `task_id` when an artifact was generated. The comms relay includes this in the WebSocket broadcast.

**Body runtime changes (`_finalize_task`):**

```python
lines = summary.strip().split("\n")
force_artifact = task.get("metadata", {}).get("report", False)

if len(lines) > 25 or force_artifact:
    self._save_result_artifact(task_id, task_content, summary)
    # Post summary with artifact link to source channel
    self._post_task_response(task, summary[:500] + "...", has_artifact=True)
else:
    # Post full response inline to source channel
    self._post_task_response(task, summary, has_artifact=False)
```

**New `_post_task_response` method** replaces the #operator post:

- Determines target channel from `task["source"]` (extract channel name from source string like `idle_direct:general:operator`)
- Posts via comms to that channel with appropriate metadata
- Includes `has_artifact`, `agent`, `task_id` in metadata when artifact exists

**Gateway changes:** None. The result endpoints (`GET /agents/{name}/results/{taskId}`) already serve artifacts.

**agency-web changes:** None. `AgencyMessage.tsx` already renders artifact links from message metadata. The only difference is where the message appears (source channel instead of #operator).

### 4. CLI: `--report` Flag

Add `--report` flag to `agency send`:

```
agency send scout "analyze the access logs for anomalies" --report
```

This sets `metadata.report: true` on the task, which the body runtime checks when deciding whether to generate an artifact. The gateway API accepts arbitrary metadata on task submission already.

### 5. Cleanup

- Remove `_post_operator_notification` from body.py (method definition + 7 callsites)
- Remove the `enforcer_unreachable` operator notification added to the general exception handler (the signal path replaces it)
- Update CLAUDE.md to document: signals are the notification mechanism, #operator is a fallback view, not a delivery target

## ASK Compliance

- **Tenet 1 (Constraints external):** Agent emits signals. It cannot control where notifications are delivered — that's operator-configured in gateway config.
- **Tenet 2 (Every action leaves a trace):** Signals are written to `agent-signals.jsonl` (audit source of truth). Platform events are logged by the event bus audit function. Delivery attempts are logged.
- **Tenet 3 (Mediation complete):** Signal path: agent → comms (NO_PROXY, agent-internal network) → gateway comms relay → event bus → delivery handler → egress proxy. No unmediated path.
- **Tenet 17 (Instructions from verified principals only):** Signals are data flowing outward, not instructions. The agent cannot instruct the notification system — it can only emit facts about its state. Routing decisions are made by the gateway based on operator config.

## Files Changed

| File | Change |
|---|---|
| `images/body/body.py` | Remove `_post_operator_notification`, add severity to signals, add `_post_task_response`, update `_finalize_task` artifact threshold logic |
| `agency-gateway/internal/ws/comms_relay.go` | Add signal-to-event promotion for operator-alertable signal types |
| `agency-gateway/internal/events/deliver_outbound.go` | Map severity field to ntfy priority |
| `agency-gateway/internal/cli/init.go` | Add notification URL prompt |
| `agency-gateway/internal/cli/send.go` | Add `--report` flag |
| `agency-gateway/internal/orchestrate/agent.go` | Already done: unhealthy status when enforcer down |
| `agency-gateway/internal/orchestrate/enforcer_watch.go` | Already done: Docker event stream watcher |
| `agency-gateway/internal/orchestrate/mission_health.go` | Already done: enforcer check in mission health |
