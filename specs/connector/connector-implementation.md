---
description: "Connectors bind external systems to the Agency intake pipeline. They define a source (how events arrive), routes (which agent gets which event), and..."
---

# Connector Implementation

Connectors bind external systems to the Agency intake pipeline. They define a
source (how events arrive), routes (which agent gets which event), and optional
MCP tools the agent can use to respond back to the source.

## Current Implementation

### Poll connectors

The `poll` source type fetches an external API on an interval and routes new
items to agents. Change detection uses SHA-256 hashing — items already seen
are not re-routed.

**Authentication**: poll sources can reference service keys via `${VAR}`
placeholders in `url` and `headers`. The intake container resolves these from
env vars injected by `infrastructure.py` (loaded from
`~/.agency/infrastructure/.service-keys.env`).

**Network**: the intake container is on the mediation network with no direct
internet access. Outbound poll requests route through the egress proxy
(`HTTP_PROXY=http://egress:3128`). The mitmproxy CA cert is mounted at
`/app/egress-ca.pem` and used by aiohttp for TLS verification.

**response_key format**: use `$.key` path syntax (e.g. `$.messages`), not a
bare key name. The `extract_items` function requires the `$.` prefix for
single-level extraction.

**Connector file layout**: intake globs `~/.agency/connectors/*.yaml` (flat).
Hub installs to `~/.agency/connectors/<name>/connector.yaml` — copy the inner
file to the flat path for intake to pick it up. This is a known gap; see
roadmap.

### Slack poll connector (current)

```
Slack API → (polling, 1m interval)
         → intake/_fetch_url (via egress proxy + mitmproxy CA)
         → router: match type=message → slack-test agent
         → agent: reads brief, calls slack_post_message to reply in thread
```

The bot token (`xoxb-`) must be a member of the channel being polled.
Currently requires manual `/invite @agency` in Slack because the bot token
lacks `channels:join` scope.

Relevant env vars passed to intake container:
- `SLACK_BOT_TOKEN` — bot user OAuth token
- `SLACK_CHANNEL_ID` — channel to poll (e.g. `C0AKGUFBBM5` = #all-agency)
- `HTTP_PROXY` / `HTTPS_PROXY` — egress proxy
- `EGRESS_CA_CERT` — path to mitmproxy CA cert

## Planned: Slack Webhook Model

Replace polling with Slack Events API for real-time delivery.

**Why**: polling has up to 1-minute latency, misses edits/deletions, and wastes
API calls when quiet. Webhooks deliver events in real time and support richer
event types (edits, reactions, thread replies, file shares).

**What's needed**:

1. **Public HTTPS endpoint** — the intake service must be reachable by Slack.
   In dev: ngrok (`ngrok http 18095`). In prod: reverse proxy with TLS cert.

2. **Slack App config** — in the Slack App dashboard:
   - Enable Event Subscriptions → set Request URL to `https://<host>/webhook/<connector-name>`
   - Subscribe to bot events: `message.channels`, `message.groups` (private),
     `app_mention` for @-mentions only
   - Add `channels:join` scope to allow programmatic channel joining

3. **HMAC signature verification** — Slack signs every webhook payload with
   `X-Slack-Signature` (HMAC-SHA256 of timestamp + body using the app's
   Signing Secret). The intake webhook handler must verify this before routing.
   Unsigned or invalid requests must be rejected 403.

4. **URL verification handshake** — on first setup Slack POSTs a `url_verification`
   challenge. Intake must respond with `{"challenge": "<value>"}`.

5. **Idempotency** — Slack retries failed deliveries (3x with exponential
   backoff over ~3 minutes). The intake work item store already deduplicates by
   hash, but the webhook handler should also check for duplicate `event_id`s.

**Connector YAML (future)**:

```yaml
kind: connector
name: slack-ops
source:
  type: webhook
  schema:
    event_id: string
    event:
      type: string
      text: string
      user: string
      channel: string
      ts: string

routes:
  - match:
      event.type: message
    target:
      agent: slack-test
    brief: |
      Slack message in channel {{ event.channel }} from {{ event.user }}:
      {{ event.text }}
      Reply with slack_post_message(channel={{ event.channel }}, thread_ts={{ event.ts }}, text=...).
```

**Implementation sketch for intake**:
- Add `X-Slack-Signature` verification middleware to the webhook handler
- Add `url_verification` challenge response (return early, no routing)
- Add `channels:join` call on connector activation (if `requires.services` includes `slack`)
- Filter out bot messages (payloads where `bot_id` is set) to avoid loops
