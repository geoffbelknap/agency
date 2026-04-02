---
title: "Channels and Messaging"
description: "Channels are named, asynchronous message streams through which agents send and receive messages, with all communication mediated by the platform."
---


Channels are how agents communicate. They're named, asynchronous message streams — agents send messages to channels, and other agents read them. All messaging goes through the comms service on the mediation network.

## Creating Channels

```bash
agency channel create findings
agency channel create discussion
agency channel create escalations
```

Channel names are lowercase alphanumeric with hyphens. Create channels **before** starting agents that need them.

## Sending Messages

From the CLI (as the operator):

```bash
agency channel send findings "Initial scan targets: 192.168.1.0/24"
```

From inside an agent (using built-in tools):

```python
send_message("findings", "Found SQL injection in auth.py line 42")
```

Messages include the sender's identity and timestamp automatically.

## Reading Messages

```bash
agency channel read findings
agency channel read findings --limit 10       # Last 10 messages
agency channel read findings --since 1h       # Last hour
```

## Searching Messages

Search across all channels with full-text search:

```bash
agency channel search "SQL injection"
agency channel search "vulnerability" --channel findings
```

The comms service uses SQLite FTS5 for fast full-text search across all message history.

## Listing Channels

```bash
agency channel list
```

Shows all channels with message counts and last activity time.

## How Agents Use Channels

Inside the agent runtime, agents have these built-in communication tools:

| Tool | Purpose |
|------|---------|
| `send_message(channel, content)` | Send a message to a channel |
| `read_channel(channel)` | Read messages from a channel |
| `list_channels()` | List available channels |
| `search_messages(query)` | Full-text search across channels |
| `create_channel(name)` | Create a new channel |

### Unread Messages

Each agent's system prompt includes unread message counts for channels they're subscribed to. This means agents naturally check channels that have new activity — they don't need to poll.

```
Unread messages: findings (3), discussion (1)
```

## Communication Patterns

### Direct Coordination

Two agents working on related tasks share a channel:

```bash
agency channel create code-review
agency create engineer-a --preset engineer
agency create reviewer-b --preset reviewer
# Both agents can send/read on code-review
```

### Team Broadcast

A coordinator sends tasks through a team channel:

```bash
agency channel create team-ops
# Coordinator posts task assignments
# Workers post completion reports
# Coordinator synthesizes results
```

### Escalation Pipeline

A dedicated channel for issues that need human attention:

```bash
agency channel create escalations
# Any agent can send escalations
# Operator monitors: agency channel read escalations
```

### Findings Collection

Multiple agents contribute findings to a shared channel:

```bash
agency channel create findings
# Agent A: "Found XSS in login.html line 23"
# Agent B: "Found open redirect in /api/redirect"
# Coordinator reads and synthesizes
```

## Architecture

The comms service runs as shared infrastructure on the mediation network:

- **Storage:** JSONL files per channel + SQLite FTS5 index
- **Port:** 18091 (internal, not exposed to agents directly)
- **Access:** Agents use built-in tools which route through the enforcer to the comms service
- **Search:** Full-text search via SQLite FTS5

Agents never connect to the comms service directly. All messages route through the agent's enforcer sidecar on the mediation network.

## Knowledge Graph

In addition to channels, Agency maintains a **knowledge graph** — organizational knowledge that compounds over time.

The knowledge graph is built from:
- **Rule-based ingestion** — Important messages from channels are automatically captured
- **Periodic LLM synthesis** — A background process uses Claude Haiku to synthesize knowledge from accumulated messages

Agents query the knowledge graph with built-in tools:

```python
query_knowledge("authentication architecture")
who_knows_about("database migrations")
what_changed_since("2 hours ago")
get_context("payment processing")
```

The knowledge graph survives `agency admin destroy` — it's designed to compound organizational knowledge over time.

See [Infrastructure](/infrastructure) for more on the knowledge service.
