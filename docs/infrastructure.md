---
title: "Infrastructure"
description: "Agency runs shared infrastructure that all agents use. This page covers what each component does, how to manage it, and how to troubleshoot problems."
---


Agency runs shared infrastructure for agents: egress, messaging, knowledge,
per-agent enforcers, and the web UI. This page explains what each component
does and how to manage the local stack.

> Status: Mixed reference. The default `0.2.x` core stack is egress, comms,
> knowledge, per-agent enforcers, and the web UI. `web-fetch`, `intake`, and
> similar optional services are experimental and are not part of the default
> first-user path.

## Components

### Egress Proxy

The egress proxy is the only component that holds real API keys. It sits between agents and the internet, handling:

- **Credential swap** — Replaces scoped tokens with real API keys for outbound requests
- **Domain filtering** — Controls which external domains agents can reach
- **Audit logging** — Records all outbound requests

The egress proxy is built on mitmproxy. Credentials are resolved from the
gateway's encrypted credential store through a dedicated Unix socket
(`~/.agency/run/gateway-cred.sock`). The `SocketKeyResolver` handles credential
lookups at request time, so real API keys never touch the agent runtime or cross
an agent-accessible network. See [Credentials](credentials.md) for the operator
guide.

### Web-Fetch Service

Experimental relative to the default `0.2.x` core path.

Shared infrastructure for agents to fetch and read web pages:

- **Content extraction** — Returns extracted markdown and metadata from web pages
- **Security layers** — DNS blocklists (platform hard floor + operator), content-type allowlist, XPIA scanning, per-domain rate limiting
- **Mediation** — Agents reach it via enforcer mediation (`/mediation/web-fetch`); external requests route through the egress proxy
- **Audit** — Request log at `~/.agency/audit/web-fetch/`

Granted as a capability (`agency cap add web-fetch`). Config at `~/.agency/web-fetch/config.yaml`.

### Comms Service

Channel-based messaging between agents:

- **JSONL storage** — One file per channel
- **SQLite FTS5** — Full-text search across all messages
- **Unread tracking** — Per-agent unread counts injected into system prompts

Runs as an aiohttp server. The host-side gateway reaches it through the local gateway-proxy reverse bridge.

### Knowledge Service

Durable knowledge that becomes more useful over time:

- **SQLite graph** — Nodes and edges with FTS5 search
- **Rule-based ingestion** — Important channel messages captured in real-time
- **LLM synthesis** — Periodic background synthesis using Claude Haiku
- **Channel-based ACL** — Query results filtered by channel access

Runs as a shared service reached by the host-side gateway through the local gateway-proxy reverse bridge. Knowledge data survives `agency admin destroy`.

### Intake Service

Experimental relative to the default `0.2.x` core path.

External work source management:

- **Webhook receiver** — HTTP listener for incoming webhook events
- **Poll loops** — Background API polling with change detection
- **Schedule runner** — Cron-triggered task generation
- **Channel watcher** — Regex pattern matching on comms channels
- **Routing engine** — Routes work items to agents or teams
- **State machine** — Work item lifecycle tracking (SQLite)

See [Connectors and Intake](/connectors-and-intake) for details.

Runs as a shared service reached by the host-side gateway through the local gateway-proxy reverse bridge.

### Per-Agent Enforcer

Each agent gets its own Go HTTP proxy (32MB):

- **LLM routing** — Routes requests to upstream providers (Anthropic, OpenAI, Google)
- **Format translation** — Translates between Anthropic and OpenAI API formats
- **XPIA scanning** — Prompt injection detection on all LLM responses (auto-scans tool-role messages, cross-tool reference detection)
- **Budget tracking** — Hard caps, soft warnings, notify-only modes (per-task via X-Agency-Task-Id header)
- **Rate limiting** — Per-agent rate limits (600 req/min)
- **Trajectory monitoring** — Sliding window anomaly detection for stuck/looping agents
- **Domain allowlisting** — Enforces which domains the agent can reach
- **Audit logging** — HMAC-signed records of all proxied requests
- **Credential-free** — No API keys; forwards to egress for credential injection
- **Config delivery** — Serves hot-reloadable config files via `/config/{filename}` on port 8081

### Web UI

The web UI is part of the core operator path:

- **Setup flow** — Guided first-run provider and platform setup
- **Direct-message workflow** — Primary default way to work with agents
- **Activity and audit visibility** — Status, history, and operator inspection
- **Core-only default navigation** — Experimental sections stay hidden unless explicitly enabled

## Managing Infrastructure

### Start

```bash
agency infra up
```

Builds or realizes service images if needed and starts the default shared
infrastructure. This happens automatically on the first `agency start` if
infrastructure isn't running.

### Stop

```bash
agency infra down
```

Stops all shared infrastructure. Running agents lose access to comms and egress
until the stack comes back.

### Rebuild

```bash
agency infra rebuild
```

Rebuilds infrastructure service images. Use after updating Agency to pick up
changes to the shared services you have enabled.

### Status

```bash
agency infra status
```

Shows the status of shared infrastructure services and images:

- Service health (running, stopped, missing)
- Image versions
- Port bindings
- Resource usage

### Hot-Reload

```bash
agency infra reload
```

Reloads configuration without restarting services. Use after changing egress rules, routing config, or other infrastructure settings.

## Runtime Mediation

Agency uses a microVM runtime boundary per agent. The supported release path is
Agency on microagent. Microagent uses Firecracker on Linux/WSL and Apple's
Virtualization framework on macOS Apple silicon.

The runtime path must preserve these mediation properties:

- workload microVMs can reach only their assigned external enforcer boundary
- enforcers mediate provider, egress, comms, and knowledge access
- shared host infrastructure stays outside the agent trust boundary
- stale legacy container networks are deployment hygiene issues, not the
  supported runtime topology

## Egress Configuration

Control which domains agents can access:

```bash
agency admin egress show my-agent
```

Domain allowlists are configured per-agent in the enforcer configuration at `~/.agency/infrastructure/enforcer/`.

## Troubleshooting

### Infrastructure Won't Start

```bash
# Check runtime and service readiness
agency admin doctor

# Check for port conflicts
agency infra status

# Rebuild images
agency infra rebuild
agency infra up
```

### Agent Can't Reach Services

```bash
# Check infrastructure health
agency infra status

# Check the agent's enforcer
agency show my-agent

# Check network connectivity
agency admin doctor
```

### High Latency

The enforcer's rate limiter queues requests when providers return 429s. Check:

```bash
agency admin doctor
agency infra status
```

If rate limiting is too aggressive, check your provider's rate limits and adjust the enforcer configuration.

### Disk Space

Audit logs and channel messages accumulate over time:

```bash
# Check knowledge graph stats
agency admin knowledge stats

# Audit log retention
agency admin audit retention
```

Configure retention policies to manage disk usage.
