# Group 4: Communication & Knowledge

**Depends on:** Group 1 (infrastructure running).

---

## Channels & Messaging

**Purpose:** Channel creation, message send/read, and agent-to-channel communication.

### Step 1 — Create agents

```
agency_create(name="val-alice", preset="generalist")
agency_create(name="val-bob", preset="generalist")
```

### Step 2 — Create a channel

```
agency_channel_create(name="val-dev", topic="Development discussion", members=["val-alice", "val-bob"])
```

**Expected:** Channel created.

### Step 3 — List channels

```
agency_channel_list()
```

**Expected:** val-dev listed with topic and members.

### Step 4 — Send messages

```
agency_channel_send(channel="val-dev", content="Hello team! Let's discuss the API redesign.")
agency_channel_send(channel="val-dev", content="The auth endpoint needs OAuth 2.1 support.")
agency_channel_send(channel="val-dev", content="Deadline is end of sprint 42.")
```

**Expected:** All 3 messages sent.

### Step 5 — Read messages

```
agency_channel_read(channel="val-dev", limit=10)
```

**Expected:** All 3 messages returned in chronological order.

### Step 6 — Agent reads and sends

```
agency_start(agent="val-alice")
agency_brief(agent="val-alice", task="Read messages in the 'val-dev' channel using read_messages. Then send a message saying 'Alice here — I'll take the OAuth work.'")
```

**Expected:** Alice sees all 3 messages, sends a 4th.

### Step 7 — Verify agent message

```
agency_channel_read(channel="val-dev", limit=10)
```

**Expected:** 4 messages total. Last one from val-alice.

### Cleanup

```
agency_stop(agent="val-alice", halt_type="immediate", reason="cleanup")
agency_delete(agent="val-alice")
agency_delete(agent="val-bob")
```

---

## Full-Text Search

**Purpose:** FTS5 search across channels.

### Step 1 — Search for a keyword

```
agency_channel_search(query="OAuth")
```

**Expected:** Finds the OAuth message from the previous exercise.

### Step 2 — Search within a channel

```
agency_channel_search(query="deadline", channel="val-dev")
```

**Expected:** Finds the sprint 42 message.

### Step 3 — Search with no results

```
agency_channel_search(query="xyznonexistent12345")
```

**Expected:** No results (empty list, no error).

---

## Real-Time Push

**Purpose:** WebSocket-based message push — agents receive messages without polling.

### Step 1 — Create agents and channel

```
agency_create(name="val-rt-alice", preset="generalist")
agency_create(name="val-rt-bob", preset="generalist")
agency_channel_create(name="val-realtime", topic="Real-time test", members=["val-rt-alice", "val-rt-bob"])
```

### Step 2 — Start both agents

```
agency_start(agent="val-rt-alice")
agency_start(agent="val-rt-bob")
```

**Expected:** Both agents start. Check logs for WebSocket connection.

### Step 3 — Alice sends a message

```
agency_brief(agent="val-rt-alice", task="Send a message to 'val-realtime' saying 'Alice reporting in — starting work on the API.'")
```

### Step 4 — Bob receives via push

```
agency_brief(agent="val-rt-bob", task="Check if you received any notifications about Alice's activity. Do NOT use read_messages — just report what you already know from push notifications.")
```

**Expected:** Bob aware of Alice's message through WebSocket push (not polling).

### Step 5 — Verify with read

```
agency_brief(agent="val-rt-bob", task="Use read_messages on 'val-realtime' to confirm what Alice sent.")
```

**Expected:** Bob sees Alice's message.

### Cleanup

```
agency_stop(agent="val-rt-alice", halt_type="immediate", reason="cleanup")
agency_stop(agent="val-rt-bob", halt_type="immediate", reason="cleanup")
agency_delete(agent="val-rt-alice")
agency_delete(agent="val-rt-bob")
```

---

## Interest Matching

**Purpose:** Task-relevant keyword matching and @mention interruptions.

### Step 1 — Create agents and channel

```
agency_create(name="val-int-alice", preset="generalist")
agency_create(name="val-int-bob", preset="generalist")
agency_channel_create(name="val-interests", topic="Interest matching test", members=["val-int-alice", "val-int-bob"])
```

### Step 2 — Start Bob and set interests

```
agency_start(agent="val-int-bob")
agency_brief(agent="val-int-bob", task="Use set_task_interests with keywords: [payments, latency, p99, timeout]")
```

**Expected:** Interests registered.

### Step 3 — Start Alice and send test messages

```
agency_start(agent="val-int-alice")
agency_brief(agent="val-int-alice", task="Send these 3 messages to 'val-interests':\n1. 'ALERT: payments gateway p99 latency spiked to 800ms — investigating.'\n2. 'Updated the README with new onboarding docs.'\n3. '@val-int-bob can you share your latency findings?'")
```

### Step 4 — Check Bob's classifications

```
agency_brief(agent="val-int-bob", task="Report any notifications you received. Which were flagged as relevant to your interests vs ambient vs direct mention?")
```

**Expected:**
- Message 1: `interest_match` (matches "payments" and "latency")
- Message 2: `ambient` (no keyword match)
- Message 3: `direct` (@mention)

### Cleanup

```
agency_stop(agent="val-int-alice", halt_type="immediate", reason="cleanup")
agency_stop(agent="val-int-bob", halt_type="immediate", reason="cleanup")
agency_delete(agent="val-int-alice")
agency_delete(agent="val-int-bob")
```

---

## Knowledge Graph

**Purpose:** Knowledge ingestion from channels, query, and graph exploration.

### Step 1 — Baseline stats

```
agency_admin_knowledge(action="stats")
```

Note the node and edge counts.

### Step 2 — Seed knowledge via channel

```
agency_channel_create(name="val-knowledge", topic="Architecture decisions")
agency_channel_send(channel="val-knowledge", content="Decision: We will use PostgreSQL for the primary datastore.")
agency_channel_send(channel="val-knowledge", content="Alice is the lead on database migration.")
agency_channel_send(channel="val-knowledge", content="The API gateway handles rate limiting at 1000 req/s.")
agency_channel_send(channel="val-knowledge", content="Bob reviewed the security implications and approved the design.")
```

Wait a few seconds for ingestion.

### Step 3 — Query knowledge

```
agency_create(name="val-knowledge-q", preset="generalist")
agency_start(agent="val-knowledge-q")
agency_brief(agent="val-knowledge-q", task="Use query_knowledge to search for 'PostgreSQL'. Report what the knowledge graph returns.")
```

**Expected:** Returns information about the PostgreSQL decision.

### Step 4 — Who knows about

```
agency_brief(agent="val-knowledge-q", task="Use who_knows_about to find who knows about 'database migration'.")
```

**Expected:** Alice identified.

### Step 5 — What changed since

```
agency_brief(agent="val-knowledge-q", task="Use what_changed_since with a recent timestamp to see recent knowledge changes.")
```

**Expected:** Recent entries from the channel messages.

### Step 6 — Verify stats increased

```
agency_admin_knowledge(action="stats")
```

**Expected:** Node/edge counts higher than baseline.

### Cleanup

```
agency_stop(agent="val-knowledge-q", halt_type="immediate", reason="cleanup")
agency_delete(agent="val-knowledge-q")
```

---

## Knowledge Push

**Purpose:** Knowledge updates pushed to agents with matching interests in real time.

### Step 1 — Create agent and channel

```
agency_create(name="val-kpush", preset="generalist")
agency_channel_create(name="val-kpush-ops", topic="Operations", members=["val-kpush"])
```

### Step 2 — Start and set interests

```
agency_start(agent="val-kpush")
agency_brief(agent="val-kpush", task="Use set_task_interests with keywords: [payments, incident, outage]")
```

### Step 3 — Seed incident knowledge

```
agency_channel_send(channel="val-kpush-ops", content="INCIDENT: payments service returning 503 errors since 14:30 UTC. Root cause: database connection pool exhausted.")
agency_channel_send(channel="val-kpush-ops", content="FINDING: payments DB connection pool max was 50, needs increase to 200 for current traffic.")
```

Wait a few seconds for knowledge ingestion.

### Step 4 — Check agent received push

```
agency_brief(agent="val-kpush", task="Check if you received any knowledge update notifications. Also use query_knowledge to search for 'payments incident'.")
```

**Expected:** Agent aware of incident through push notifications. Knowledge query returns results.

### Step 5 — Verify stats

```
agency_admin_knowledge(action="stats")
```

**Expected:** Node/edge counts reflect new knowledge.

### Cleanup

```
agency_stop(agent="val-kpush", halt_type="immediate", reason="cleanup")
agency_delete(agent="val-kpush")
```
