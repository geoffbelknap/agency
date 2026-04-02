## What This Document Covers

The design for automatically ingesting channel messages from the comms service into the knowledge graph. Covers the ingestion trigger mechanism, filtering pipeline, entity extraction, authorization scoping, and the architecture decision of where the pipeline runs.

> **Scope:** This spec covers ingestion of comms channel messages only. Other knowledge sources (external documents, tool outputs, file system artifacts) are out of scope. The existing `contribute_knowledge` agent tool and manual `/ingest/nodes` API are unaffected.

## Problem Statement

Today, channel messages stay in the comms service. The knowledge graph exists but is mostly empty because nothing feeds it automatically. Agents can call `query_knowledge`, but there is nothing to query unless an agent explicitly calls `contribute_knowledge`. This means organizational knowledge — decisions, blockers, discoveries, context — evaporates after a conversation scrolls past.

The knowledge service already has the machinery for ingestion: a `RuleIngester` that extracts structural graph data (decisions, blockers, agent-channel membership) and an `LLMSynthesizer` that batches messages and calls an LLM for entity/relationship extraction. It also has a polling-based `_ingestion_loop` that polls comms every 10 seconds. The problem is that this loop is fragile (polling misses, startup ordering, no backpressure), and the pipeline lacks selective filtering — every message hits the ingester regardless of signal quality.

## Goals

1. Channel messages flow into the knowledge graph automatically, with no agent action required.
2. Ingestion is selective — greetings, acknowledgements, and low-signal chatter are filtered out before they pollute the graph.
3. Knowledge persists independently of agent lifecycles (ASK Tenet 23).
4. Knowledge access remains bounded by authorization scope via `source_channels` tagging (ASK Tenet 24).
5. New knowledge is available within seconds of a message for rule-based extraction, and within minutes for LLM synthesis.
6. The pipeline is observable — operators can see ingestion throughput, filter hit rates, and synthesis lag.

## Non-Goals

- Ingesting data from external sources (Slack, email, documents). That is a connector concern.
- Replacing the existing `contribute_knowledge` agent tool. Agents should still be able to explicitly contribute high-quality knowledge.
- Real-time streaming of knowledge updates to agents. The existing `_knowledge-updates` channel and `query_knowledge` at task start are sufficient.
- Building a new graph database. The SQLite-backed `KnowledgeStore` is the target.

## ASK Tenets Enforced

| Tenet | How |
|---|---|
| 2 — Every action leaves a trace | Ingestion writes audit records (synthesis_audit logs). Provenance IDs link every knowledge node back to the source message. |
| 23 — Knowledge is durable infrastructure | Knowledge nodes persist in the knowledge service's SQLite store, independent of any agent's lifecycle. Agent termination does not affect ingested knowledge. |
| 24 — Knowledge access bounded by authorization | Every node carries `source_channels`. Queries accept `visible_channels` and filter results accordingly. An agent can only retrieve knowledge from channels it has access to. |
| 12 — Synthesis cannot exceed individual authorization | LLM synthesis batches are scoped per-channel (or per-channel-set with shared membership). The synthesized output inherits the source channels of its input messages, preventing cross-channel information leakage. |

## Design Decisions

| Decision | Choice | Rationale |
|---|---|---|
| Trigger mechanism | WebSocket push from comms, not polling | The comms service already pushes events via WebSocket. The gateway already consumes this stream (`comms_relay.go`). Polling every 10s (current approach) introduces latency and misses bursts. WebSocket gives real-time, ordered delivery. |
| Where the pipeline runs | Inside the knowledge service container | The knowledge service already owns the `RuleIngester`, `LLMSynthesizer`, `Curator`, and `KnowledgeStore`. Moving ingestion logic elsewhere would split ownership. The knowledge service subscribes to the comms WebSocket directly on the mediation network. |
| Pre-filter location | Knowledge service, before rule ingestion | Filtering requires content analysis. Running it at the source (comms) would couple comms to knowledge concerns. Running it at the gateway would add latency to the relay path. The knowledge service is the right place because it owns the "is this worth remembering?" decision. |
| Filter approach | Rule-based heuristics, not LLM | Filtering runs on every message and must be fast (<1ms). LLM calls for filtering would be too slow and too expensive. Simple heuristics (message length, flag presence, structural markers) are sufficient to reject noise. |
| LLM synthesis trigger | Batch threshold (message count + time), unchanged | The existing `LLMSynthesizer` trigger logic (10 messages or 1 hour, min 5-minute interval) is well-calibrated. No change needed. |
| Channel scoping for synthesis | Per-channel batching | Synthesis batches must not mix messages from channels with different access policies. Each batch is scoped to a single channel (or a set of channels with identical membership). Output nodes inherit `source_channels` from the batch. |

---

## Part 1: Ingestion Pipeline

### Overview

```
Comms Service                    Knowledge Service
┌──────────────┐                ┌──────────────────────────────────────┐
│              │   WebSocket    │                                      │
│  fan_out     │───push────────▶│  CommsSubscriber                     │
│  message     │                │    │                                 │
│              │                │    ▼                                 │
│  POST        │                │  SignalFilter                        │
│  /messages   │                │    │ (drop noise, pass signal)       │
│              │                │    ▼                                 │
└──────────────┘                │  RuleIngester                        │
                                │    │ (structural: agent, channel,    │
                                │    │  decision, blocker nodes/edges) │
                                │    ▼                                 │
                                │  LLMSynthesizer (batched)            │
                                │    │ (entity/relationship extraction)│
                                │    ▼                                 │
                                │  KnowledgeStore (SQLite + FTS5)      │
                                │    │                                 │
                                │    ▼                                 │
                                │  Curator (periodic quality pass)      │
                                └──────────────────────────────────────┘
```

### Step 1: CommsSubscriber

Replace the polling-based `_ingestion_loop` with a WebSocket subscriber that connects to comms at `ws://comms:18091/ws?agent=_knowledge-service`.

The knowledge service registers as a system agent named `_knowledge-service`. Like `_gateway`, it receives all messages across all channels (comms already supports system observers). On disconnect, it reconnects with exponential backoff (same pattern as `comms_relay.go` in the gateway).

The subscriber maintains a high-water mark (last processed message timestamp per channel) persisted to disk. On reconnect, it replays missed messages by calling `GET /channels/{name}/messages?since={hwm}` for each channel. This guarantees at-least-once delivery without requiring comms to implement a durable queue.

**Key property:** The knowledge service is a consumer of comms, not a component of it. Comms does not know or care what the knowledge service does with messages. This preserves separation of concerns.

### Step 2: SignalFilter

A fast, rule-based filter that decides whether a message is worth ingesting. Runs synchronously on every incoming message. No LLM calls.

**Pass rules** (message is ingested if ANY match):
- Message has `flags.decision = true` or `flags.blocker = true` (explicit signal)
- Message content length > 100 characters (likely substantive)
- Message is a reply (`reply_to` is set) to a previously ingested message
- Message mentions another agent (`@agent-name` pattern)
- Message contains structured data markers (code blocks, URLs, error traces)
- Channel is in the `always_ingest` list (configurable, e.g., `_decisions`, `_blockers`)

**Drop rules** (message is dropped if ALL match):
- Content length < 50 characters
- No flags set
- Not a reply
- Channel is not in `always_ingest`

**Metrics:** The filter emits counters for `passed` and `dropped` messages, exposed via the `/stats` endpoint.

Messages that pass the filter go to the RuleIngester (immediate, structural extraction) and are queued for the LLMSynthesizer (batched, semantic extraction).

### Step 3: RuleIngester (existing, unchanged)

The existing `RuleIngester.ingest_message()` already handles:
- Creating `agent` and `channel` nodes
- Creating `member_of` edges (agent -> channel)
- Creating `decision` nodes from flagged messages
- Creating `blocker` nodes from flagged messages
- Creating `replied_to` edges

No changes needed. The only difference is that it now receives messages from the WebSocket subscriber instead of the polling loop.

### Step 4: LLMSynthesizer (existing, minor change)

The existing `LLMSynthesizer` already handles batched entity/relationship extraction. The only change:

**Channel-scoped batching.** Currently, `synthesize()` receives a flat list of messages and a flat list of `source_channels`. Change this so that messages are batched per-channel (or per-channel-group with identical membership). Each synthesis call produces nodes tagged with the correct `source_channels`, ensuring authorization scoping is preserved.

The synthesis trigger conditions (10 messages / 1 hour / 5-minute cooldown) remain unchanged.

### Step 5: Curator (existing, unchanged)

The existing `Curator` and `CurationLoop` run periodic quality passes: deduplication, low-quality flagging, merge detection. No changes needed. The curator naturally handles increased ingestion volume because it operates on the store, not the ingestion stream.

---

## Part 2: Authorization Scoping

### Channel-Based Access Control

The knowledge store already supports authorization scoping:
- Every node has a `source_channels` field (JSON array of channel names).
- `find_nodes()` accepts `visible_channels` and filters results.
- The body runtime's `_retrieve_knowledge_context()` passes the agent's channel list.
- The `/query` endpoint accepts `visible_channels` in the request body.

**What this means for auto-ingestion:** Every node created by the pipeline inherits the channel it came from. An agent deployed to channels `[alpha, beta]` can only query knowledge derived from those channels. An agent on `[gamma]` sees a different knowledge graph.

### Cross-Channel Knowledge

Some knowledge is inherently cross-channel (e.g., an entity mentioned in multiple channels). The `LLMSynthesizer` handles this by merging into existing nodes (`find_nodes` + label match). When a node is updated from a new channel, its `source_channels` list grows to include the new channel. This means cross-channel knowledge is visible to agents with access to ANY of the contributing channels.

This is correct behavior: if an agent has access to channel `alpha` and a node was derived from both `alpha` and `beta`, the agent should see it because it has legitimate access to one of the sources. The node's provenance is fully auditable.

---

## Part 3: Configuration

The pipeline is configured via environment variables on the knowledge service container, consistent with existing patterns:

| Variable | Default | Description |
|---|---|---|
| `KNOWLEDGE_INGESTION` | `true` | Enable/disable the auto-ingestion pipeline |
| `KNOWLEDGE_COMMS_WS_URL` | `ws://comms:18091/ws?agent=_knowledge-service` | Comms WebSocket endpoint |
| `KNOWLEDGE_FILTER_MIN_LENGTH` | `50` | Minimum message length to pass filter |
| `KNOWLEDGE_FILTER_SIGNAL_LENGTH` | `100` | Length threshold for "likely substantive" |
| `KNOWLEDGE_ALWAYS_INGEST_CHANNELS` | `_decisions,_blockers` | Channels where all messages are ingested |
| `KNOWLEDGE_SYNTH_CHANNEL_SCOPED` | `true` | Enforce per-channel synthesis batching |

Existing synthesis variables (`AGENCY_SYNTH_MSG_THRESHOLD`, `AGENCY_SYNTH_TIME_THRESHOLD_HOURS`, `AGENCY_SYNTH_MIN_INTERVAL_SECS`) remain unchanged.

---

## Part 4: Observability

### Metrics (via `/stats` endpoint)

Extend the existing `/stats` response with ingestion pipeline metrics:

```json
{
  "nodes": 142,
  "edges": 387,
  "kinds": {"agent": 5, "channel": 3, "decision": 12, "concept": 98},
  "ingestion": {
    "messages_received": 4821,
    "messages_passed_filter": 1203,
    "messages_dropped": 3618,
    "filter_pass_rate": 0.25,
    "rule_nodes_created": 89,
    "synthesis_runs": 42,
    "synthesis_entities_extracted": 312,
    "last_message_at": "2026-03-21T14:32:01Z",
    "last_synthesis_at": "2026-03-21T14:28:00Z",
    "ws_connected": true,
    "ws_reconnects": 2
  }
}
```

### Logs

The existing `synthesis_audit` structured log entries continue. Add a `filter_audit` entry for periodic filter statistics (every 100 messages or every 5 minutes):

```json
{
  "event": "filter_audit",
  "window_messages": 100,
  "passed": 24,
  "dropped": 76,
  "pass_reasons": {"length": 15, "flag": 4, "reply": 3, "mention": 2},
  "drop_reasons": {"short_no_signal": 76}
}
```

---

## Part 5: Failure Modes

| Failure | Behavior |
|---|---|
| Comms service unavailable | WebSocket reconnects with exponential backoff (1s to 30s). Missed messages replayed via HTTP on reconnect using high-water mark. |
| Knowledge service restarts | High-water mark loaded from disk. Replay from last checkpoint. Duplicate messages rejected by `RuleIngester._processed_messages` set (repopulated from store on startup). |
| LLM synthesis fails | Already handled: fallback from local model to cloud model. If both fail, messages stay in pending queue for next synthesis attempt. No data loss. |
| SQLite write fails | Existing behavior: exception logged, message skipped. Store uses WAL mode for crash safety. |
| Filter is too aggressive | Operator adjusts `KNOWLEDGE_FILTER_MIN_LENGTH` or adds channels to `KNOWLEDGE_ALWAYS_INGEST_CHANNELS`. Filter metrics make this visible. |
| Filter is too permissive | Curator catches low-quality nodes in periodic pass. Operator can also lower thresholds. |

---

## Part 6: What Changes

| Component | Change |
|---|---|
| `knowledge/server.py` | Replace `_ingestion_loop` (polling) with `CommsSubscriber` (WebSocket). Add `SignalFilter` class. Add ingestion metrics to `/stats`. |
| `knowledge/ingester.py` | No changes. |
| `knowledge/synthesizer.py` | Add channel-scoped batching to `synthesize()`. |
| `knowledge/store.py` | Add high-water mark persistence methods. |
| `comms/server.py` | No changes. Knowledge service connects as a WebSocket client like any other agent. |
| `comms_relay.go` | No changes. The gateway relay and knowledge ingestion are independent consumers. |
| Docker compose | Ensure knowledge service starts after comms (existing dependency). No new containers. |

---

## Testing Approach

### Unit Tests

1. **SignalFilter**: Test each pass/drop rule independently. Verify filter metrics accumulate correctly.
2. **CommsSubscriber**: Mock WebSocket connection. Verify high-water mark tracking, reconnect behavior, and message replay on reconnect.
3. **Channel-scoped batching**: Verify that `LLMSynthesizer` does not mix messages from channels with different membership in the same synthesis batch.
4. **Authorization scoping**: Verify that nodes created from channel `alpha` messages are only returned when `visible_channels` includes `alpha`.

### Integration Tests

1. **End-to-end flow**: Post a substantive message to comms. Verify that within 10 seconds, corresponding nodes and edges appear in the knowledge store.
2. **Filter verification**: Post a short greeting ("hi") and a substantive message. Verify only the substantive message produces knowledge nodes.
3. **Cross-channel scoping**: Post to two channels with different membership. Query knowledge with each agent's channel list. Verify isolation.
4. **Reconnect resilience**: Kill the comms WebSocket connection. Post messages during the outage. Verify they are ingested after reconnect via replay.

### Validation Runbook

Add a manual validation entry to `tests/validation/` that:
1. Starts the platform with `agency infra up`.
2. Creates two agents on different channels.
3. Has agents exchange substantive messages.
4. Queries the knowledge graph and verifies nodes exist with correct `source_channels`.
5. Verifies that each agent's `query_knowledge` only returns knowledge from its own channels.
