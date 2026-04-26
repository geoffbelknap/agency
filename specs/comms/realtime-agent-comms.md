---
description: "The current comms system is entirely pull-based. Agents poll for tasks (0.25s), check for mentions (10s), and triage ..."
status: "Implemented (core WebSocket push, interest matching, interruption controller)"
---

# Real-Time Agent Communications

**Date:** 2026-03-14
**Status:** Implemented (core WebSocket push, interest matching, interruption controller)
**Last updated:** 2026-04-01
**Goal:** Replace polling-based agent communication with WebSocket push, add interest-based relevance filtering, and route knowledge graph updates through the same channel. Enable "Slack for agents" — real-time, bidirectional, with controlled interruption.

**Implementation notes:** The WebSocket push layer is implemented. Comms server WebSocket endpoint and connection registry are in `images/comms/websocket.py`, with the matching engine in `images/comms/matcher.py` and subscription manager in `images/comms/subscriptions.py`. The body runtime WebSocket listener is in `images/body/ws_listener.py` — handles connection, reconnection with exponential backoff, and event queueing. The body runtime (`body.py`) integrates the WebSocket listener for event-driven task delivery and message push. The v2 semantic matching path (sqlite-vec) is not yet implemented. Swarm mode WebSocket forwarding is not yet implemented.

## Problem

The current comms system is entirely pull-based. Agents poll for tasks (0.25s), check for mentions (10s), and triage unreads via Haiku classification (30s heartbeat). This creates significant dwell time, adds LLM cost for triage, and undermines the compounding knowledge hypothesis — new knowledge contributed by one agent isn't surfaced to others until they happen to start a new task or manually read the right channel.

## Architecture Overview

Three new components replace the polling model:

1. **WebSocket endpoint on comms server** — persistent connection per agent. All inbound communication (tasks, messages, knowledge updates) arrives as typed events over this connection. Agents still POST to send messages.

2. **Subscription manager in comms server** — tracks channel memberships and per-task interest declarations. When a message arrives, the server matches it against subscriber interests and pushes with a match classification.

3. **Interruption controller in body runtime** — applies operator-defined policy to incoming events. Decides whether to interrupt the active conversation, notify at the next pause, or queue for later.

### What Stays

- HTTP POST for sending messages
- Channel model (team, direct, system)
- Message flags (decision, question, blocker, urgent)
- Cursor tracking for reconnect catch-up
- Comms store (JSONL + FTS5)
- Knowledge graph tools (query_knowledge, who_knows_about, etc.)
- Knowledge context retrieval at task start
- Heartbeat for health metrics

### What Gets Removed

- `_poll_task()` and `TASK_POLL_INTERVAL` — replaced by WebSocket task events
- `_check_urgent_mentions()` and `MENTION_CHECK_INTERVAL` — replaced by push + direct match
- `_triage_notification()` and Haiku triage calls — replaced by interest matching
- Heartbeat-based unread checking — heartbeat stays for health only
- `_process_queued_notifications()` — replaced by interruption controller
- Sleep-poll main loop — replaced by event-driven wait
- `session-context.json` for task delivery — kept only as read-once reconnect fallback

## WebSocket Connection Lifecycle

### Connection

Agent connects to `ws://comms:18091/ws/{agent_name}` on body runtime startup.

**Identity verification:** The comms server resolves agent identity from the Docker network the connection originates from. Each agent runs on its own internal network, and the comms container is attached to each with a known alias. The comms server maps the source network to the expected agent name and rejects connections where the requested `{agent_name}` doesn't match the source network's agent. This matches the existing trust model — network isolation is the authentication boundary, not tokens or credentials.

On connection, the server automatically subscribes the agent to all channels they're a member of and sends an acknowledgment with current channel list and unread counts.

### Event Types

All inbound communication arrives as typed events:

```json
{"v": 1, "type": "task", "task": {"task_id": "abc-123", "content": "...", "source": "operator"}}
{"v": 1, "type": "message", "channel": "team-platform", "match": "interest_match", "matched_keywords": ["payments"], "message": {...}}
{"v": 1, "type": "knowledge", "channel": "_knowledge-updates", "match": "interest_match", "message": {...}}
{"v": 1, "type": "system", "event": "constraint_update", ...}
```

All events include a `v` field for protocol versioning. The body runtime should ignore events with unrecognized versions rather than crashing.

### Reconnection

If the WebSocket drops, the body runtime reconnects with exponential backoff (1s, 2s, 4s, max 30s). On reconnect, it uses the existing cursor system to catch up on messages missed during the disconnect window. The cursor system becomes the durability layer behind the real-time layer.

On reconnect, the runtime also reads the context file once to check for any task delivered while disconnected.

### Graceful Degradation

If the WebSocket cannot be established, the body runtime falls back to the current polling behavior. Push is an enhancement, not a hard dependency.

## Subscription & Interest Matching

### Two Layers

**Layer 1 — Channel membership (static).** Agent is subscribed to their channels on connect. Messages in these channels arrive classified as `ambient` unless they match Layer 2.

**Layer 2 — Task interests (dynamic).** When an agent starts a task, the body runtime registers an interest declaration:

```json
POST /subscriptions/{agent_name}/interests
{
  "task_id": "abc-123",
  "description": "Investigating API latency in payments service",
  "keywords": ["payments", "latency", "p99", "timeout", "gateway"],
  "knowledge_filter": {
    "kinds": ["incident", "finding", "decision"],
    "topics": ["payments", "api-gateway"]
  }
}
```

The body runtime auto-generates this from task content using simple keyword extraction (noun/technical term extraction, no LLM call). If extraction produces no keywords, the agent starts with no interests registered and all channel messages arrive as `ambient`. The `set_task_interests` tool serves as the refinement escape hatch. Interests are cleared when the task completes.

Agents can also refine interests mid-task via a `set_task_interests` tool if they discover the task scope differs from the initial parse.

**Interest declaration bounds (ASK tenet 5):** Interest declarations are capped at a maximum of 20 keywords and 10 knowledge filter entries (kinds + topics combined). The comms server rejects declarations exceeding these limits. This prevents an agent from registering overly broad interests to receive all traffic within its channel memberships. The body runtime's auto-extraction is bounded by the same limits. All interest registrations and clearings are logged by the comms server for audit (ASK tenet 2).

### Match Classification

For comms messages:

1. `direct` — message contains `@{agent_name}`
2. `interest_match` — message content matches interest keywords via FTS5 (OR semantics, minimum 3-character keywords, `unicode61` tokenizer, multi-word keywords use FTS5 phrase matching)
3. `ambient` — channel member, no keyword match

For knowledge updates (on `_knowledge-updates` system channel):

1. Structural match — node `kind` or `topic` matches `knowledge_filter`
2. FTS5 match — node summary matches interest keywords
3. No match — not forwarded

### v2 Path: Semantic Matching

The `description` field in interest declarations is stored but unused in v1. In v2, both the description and incoming messages/nodes get embedded via sqlite-vec. Cosine similarity above a threshold promotes `ambient` to `interest_match`. The agent-facing API does not change.

## Interruption Controller

### Operator-Defined Policy

Stored in the agent's config directory as `comms-policy.yaml`. Operators define rules mapping match classifications and flags to actions:

```yaml
interruption:
  rules:
    - match: direct
      flags: [urgent, blocker]
      action: interrupt
    - match: direct
      action: notify_at_pause
    - match: interest_match
      flags: [urgent, blocker]
      action: interrupt
    - match: interest_match
      action: notify_at_pause
    - match: ambient
      action: queue
  max_interrupts_per_task: 3
  cooldown_seconds: 60
```

### Actions

- **`interrupt`** — the **comms server** generates a concise summary (max 200 chars) of the original message content, sanitizing untrusted content (strip control characters, limit to printable ASCII/unicode). The body runtime injects this server-generated summary as a system-role message: `"[Comms interrupt] #{channel} @{author}: {server-generated summary}. Use read_messages('{channel}') for full context."` The LLM sees it on its next turn and decides whether to read the full message or continue. Summaries are never passed through verbatim from the sending agent — the comms server is the trust boundary for injection content. The existing LLM proxy pre-call XPIA guardrails provide a second layer of defense since the injected summary becomes part of the conversation sent to the LLM. (ASK tenet 17)
- **`notify_at_pause`** — hold until the next turn boundary. Inject a batched summary using the same server-generated summaries: `"[Comms] {n} new messages may be relevant to your current task: {channel}: {one-line each}. Use read_messages to review."` Multiple notifications within the same pause window are batched into a single injection.
- **`queue`** — store for after task completion. No mid-task visibility.

### Safety Guardrails

- `max_interrupts_per_task` — caps interruptions per task. After the limit, further matches downgrade to `notify_at_pause`. Prevents denial-of-service via chatty agents.
- `cooldown_seconds` — minimum time between interrupts.
- Operator owns the policy file; agent cannot modify it (ASK tenet 5).
- `ambient` messages never interrupt by default.
- Default policy (no config file):

```yaml
interruption:
  rules:
    - match: direct
      flags: [urgent, blocker]
      action: interrupt
    - match: direct
      action: notify_at_pause
    - match: interest_match
      action: notify_at_pause
    - match: ambient
      action: queue
  max_interrupts_per_task: 3
  cooldown_seconds: 60
```

## Knowledge Graph Push Integration

**Channel lifecycle:** The `_knowledge-updates` channel is created during infrastructure startup (in `_ensure_comms`) as a `SYSTEM` channel with platform-write visibility. Only the knowledge service can publish to it — agents cannot post fake knowledge updates. The knowledge server publishes via HTTP POST to the comms server with the `X-Agency-Platform: true` header, matching the existing pattern for platform-write channels.

The knowledge server publishes to this channel whenever new nodes or edges are created:

```json
{
  "channel": "_knowledge-updates",
  "author": "_knowledge-service",
  "content": "New finding: payments gateway p99 latency exceeded 500ms threshold",
  "metadata": {
    "node_id": "n-4a2f",
    "kind": "finding",
    "topic": "payments",
    "contributed_by": "agent-monitor",
    "properties": {"p99_ms": 520, "threshold_ms": 500}
  }
}
```

Matching uses both layers: structural matching on metadata fields against `knowledge_filter`, and FTS5 on content against interest keywords. Before forwarding a knowledge update to an agent, the matching engine also checks that the agent is authorized to access the originating knowledge node based on its scope (visible channels, team membership). The `_knowledge-updates` channel is a broadcast channel, so authorization filtering must happen per-agent at the matching stage, not at the channel level. If an agent is not authorized to access the underlying knowledge, the update is silently dropped. (ASK tenet 24)

Existing knowledge tools (`query_knowledge`, `who_knows_about`, `get_context`) remain for intentional deep-dives. Push is the notification; tools are the investigation.

Knowledge context retrieval at task start also remains — push keeps the agent updated during the task.

## Body Runtime Changes

### New Components

- **WebSocket listener thread** — maintains connection, handles reconnection, puts events into a thread-safe queue.
- **Interruption controller** — the main loop checks the event queue at defined synchronization points (before each LLM call, after each tool call). These are the "turn boundaries." The controller applies operator policy and either injects a system-role message into the conversation, batches a notification summary, or queues for later. No concurrent list mutation — the main thread is the only writer to the messages list.
- **`set_task_interests` tool** — lets agent refine interests mid-task.

### Event-Driven Main Loop

```
while running:
    event = wait_for_event(timeout=30s)
    match event.type:
      "system"    → handle_system_event(event)  # always first, never throttled
      "task"      → run_conversation(event.task)
      "message"   → interruption_controller.handle(event)
      "knowledge" → interruption_controller.handle(event)
      None        → heartbeat if due
```

**System events (halt, constraint updates) are always processed immediately and are never subject to interruption policy throttling.** They bypass `max_interrupts_per_task`, `cooldown_seconds`, and the circuit breaker entirely.

**Constraint update acknowledgment (ASK tenet 6):** When a constraint update arrives via WebSocket, the body runtime sends an explicit acknowledgment back over the WebSocket connection. If the comms server does not receive acknowledgment within 10 seconds, it flags the agent for operator review. Constraint updates are also persisted to the agent's state directory as a durable fallback, ensuring delivery survives WebSocket disconnection.

No sleep-poll cycle. The runtime blocks on the event queue and wakes when something arrives.

### Idle Behavior

When no task is active, incoming messages that match interests can trigger self-briefing — the agent picks up the message as a lightweight task. Configurable via policy (`idle_action: self_brief | queue`). Self-briefing is rate-limited to at most once per 60 seconds to prevent continuous LLM cost from busy channels. Multiple messages arriving within the cooldown are batched into a single self-brief task.

## Comms Server Changes

### New WebSocket Endpoint

`ws://comms:18091/ws/{agent_name}`

### Connection Registry

In-memory dict of active WebSocket connections keyed by agent name. On message POST:

1. Write to JSONL store (existing)
2. Index in FTS5 (existing)
3. Look up connected members of the channel
4. Run interest matching for each
5. Push with match classification

### New HTTP Endpoints

- `POST /subscriptions/{agent_name}/interests` — register task interests
- `DELETE /subscriptions/{agent_name}/interests` — clear interests
- `POST /tasks/{agent_name}` — deliver a task. Replaces the existing `/tasks/deliver` endpoint. Pushes a `type: task` event over WebSocket if the agent is connected, and also writes to `session-context.json` for durability (reconnect fallback). The intake service and CLI are updated to call this endpoint. If a task arrives while the agent is busy, it is queued and delivered when the current task completes.

### Interest Store

In-memory during runtime, persisted to SQLite for server restart survival.

### Matching Engine

Isolated module: takes a message and an agent's interest declaration, returns match classification. v1 uses literal keyword check + FTS5. Designed for sqlite-vec upgrade in v2.

## Dependency Changes

- **Adds:** `websockets` Python package
- **Removes:** Haiku triage calls (cost savings, latency reduction)
- **No new infrastructure services**

## Security Considerations

- Interruption policy is operator-owned and read-only (ASK tenet 5)
- `max_interrupts_per_task` and `cooldown_seconds` prevent DoS via message flooding
- Agents cannot modify their own interruption rules
- Channel ACLs still enforced — agents only receive messages from channels they're members of
- Knowledge updates respect existing visibility rules
- The `urgent` flag on `send_message` is restricted to operator-only (set via platform HTTP with `X-Agency-Platform: true`, not available in the agent's `send_message` tool). This prevents agents from influencing other agents' interruption behavior by marking their own messages as urgent.

### WebSocket Trust Boundary (ASK Tenet 3)

The WebSocket path from agent workspace to comms server does not pass through the enforcer. This is consistent with the existing HTTP comms path — comms is on the `NO_PROXY` list and is considered internal infrastructure within the agent's trust boundary, not an external resource. The comms server is a platform service that the agent depends on but cannot compromise; it enforces channel ACLs and write permissions independently.

The WebSocket carries task delivery events that currently go through `session-context.json` (a filesystem path). Both paths are within the same trust boundary — the agent's internal Docker network. The comms server logs all events (messages, task deliveries) to its JSONL store, providing the audit trail required by ASK tenet 2.

### Audit Logging (ASK Tenet 2)

The following events are logged by the comms server or body runtime's platform-level code (not by the agent's cognitive process):

- **Interest registration and clearing** — comms server logs each `POST/DELETE /subscriptions/{agent_name}/interests` with the full declaration and timestamp
- **Interruption controller decisions** — body runtime logs each event received from the queue and the action taken (interrupt, notify_at_pause, queue) with the policy rule that matched
- **Circuit breaker state changes** — body runtime logs when the circuit breaker activates (downgrades interrupts) and when it recovers, with the action rate that triggered the change
- **WebSocket lifecycle events** — comms server logs connection, disconnection, reconnection, and identity verification failures
- **Constraint update acknowledgments** — comms server logs acknowledgment receipt or timeout for each constraint update delivered via WebSocket

### WebSocket Identity Verification

Agent identity on WebSocket connections is verified via Docker network isolation. Each agent runs on its own internal network. The comms container is attached to each agent's network with a known alias. The comms server maps the source network of the incoming connection to the expected agent name and rejects connections where the requested `{agent_name}` in the URL does not match. This is the same trust model used for HTTP requests — network isolation is the authentication boundary.

## LLM Cost Impact

### What Changes

**Removed cost:**
- Haiku triage calls (`_triage_notification`) — currently fires every 30s heartbeat for each unread message. Replaced by deterministic FTS5 interest matching. Zero LLM calls for relevance classification.

**New cost:**
- `set_task_interests` tool call — one additional turn at task start for the agent to register interests. Cheap (tool call, no deep reasoning).
- Interrupt response cycles — when an agent receives an interrupt injection, it may spend 1-3 additional turns reading the full message and deciding how to act. Bounded by `max_interrupts_per_task` (default 3) and `cooldown_seconds` (default 60). Worst case: ~9 extra turns per task.
- Context window growth — each injection (even the concise summary) stays in the conversation history, increasing input tokens on all subsequent LLM calls for the remainder of the task. Concise summaries (max 200 chars) and pointer-based injections limit this, but it compounds over turns.
- Idle self-briefing — new LLM spend when no task is active. Rate-limited (60s cooldown, batched). Disabled entirely with `idle_action: queue`.

**Expected savings:**
- Agents with better real-time context should need fewer exploratory turns — less redundant discovery, fewer dead-end investigations, earlier task abandonment when work is superseded.
- The net impact depends on whether the compounding knowledge benefit outweighs the interrupt overhead. This is the hypothesis we're testing.

### Observability & Measurement

To validate whether push comms improves agent effectiveness (and to tune interruption policy), the platform must track:

**Per-task metrics (emitted in `task_complete` signal):**
- `turns_total` — total LLM turns in the conversation (already tracked)
- `turns_from_interrupts` — turns spent responding to interrupt injections (new)
- `interrupts_received` — count of interrupts injected (new)
- `interrupts_acted_on` — count of interrupts where the agent called `read_messages` or changed approach (new)
- `input_tokens_total` — total input tokens across all LLM calls (new)
- `notifications_queued` — count of messages that matched interests but were queued, not injected (new)

**Derived metrics (computed by operator tooling):**
- **Interrupt-to-action rate** — `interrupts_acted_on / interrupts_received`. If this is consistently low, the interest matching is too broad or the interruption policy too aggressive. Operator should tighten keywords or raise the match threshold.
- **Interrupt cost ratio** — `turns_from_interrupts / turns_total`. If interrupts are consuming >20% of turns, the agent is spending more time on comms than work.
- **Turns-per-task trend** — compare before/after push rollout. If push is working, tasks of similar complexity should trend toward fewer turns over time as agents benefit from better knowledge.
- **Token cost per task** — total input + output tokens. Watch for regression after push is enabled.

**Circuit breaker:**
If `interrupt-to-action rate` drops below a configurable threshold (default 0.2) over a rolling window, the interruption controller automatically downgrades `interrupt` actions to `notify_at_pause` until the rate recovers. This prevents low-value interrupts from burning tokens. The threshold is operator-configurable in `comms-policy.yaml`:

```yaml
interruption:
  circuit_breaker:
    min_action_rate: 0.2
    window_size: 20  # last N interrupts
```

## Scope Limitations

### Swarm Mode

This design targets local (single-host) deployments. Swarm mode — where agents run on multiple Docker hosts with a manager/cache comms topology — is not addressed. WebSocket forwarding between cache and manager comms instances, and interest subscription propagation across hosts, are deferred to a follow-up design. Swarm users will continue to use the polling-based comms model until this is addressed.
