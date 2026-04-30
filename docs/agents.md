---
title: "Agents"
description: "An agent is an autonomous AI worker running inside an isolated microVM runtime, covering the full lifecycle from creation and configuration to starting and stopping."
---


An agent is an autonomous AI worker running inside an isolated microVM runtime. This page covers the full agent lifecycle — creating, configuring, starting, sending tasks, monitoring, and stopping agents.

> Status: Core reference with a few experimental extensions called out inline.
> The default `0.2.x` path is one operator working with one or a few agents
> through direct messages.

## Creating an Agent

```bash
agency create my-agent
```

This creates an agent with the default `generalist` preset. To use a specific preset:

```bash
agency create my-agent --preset engineer
```

To create a specific agent type:

```bash
agency create my-agent --type coordinator --preset coordinator
agency create my-agent --type function --preset security-reviewer
```

`coordinator` and `function` types are experimental relative to the default
single-agent `0.2.x` path.

See [Presets](/presets) for the full list of built-in presets.

### What Gets Created

When you create an agent, Agency generates these files under `~/.agency/agents/my-agent/`:

| File | Purpose |
|------|---------|
| `agent.yaml` | Agent manifest — name, type, preset, model tier |
| `constraints.yaml` | Rules the agent must follow (mounted read-only) |
| `identity.md` | Who the agent is and how it works |
| `workspace.yaml` | Workspace/runtime specification |
| `policy.yaml` | Agent-level policy |
| `services.yaml` | Granted service credentials |

You can edit any of these before starting the agent.

## Configuring an Agent

### Identity

The identity file (`identity.md`) is the agent's seed document — it defines purpose, working style, and behavioral guidelines. Edit it to customize the agent's personality and approach:

```bash
# Edit directly
nano ~/.agency/agents/my-agent/identity.md
```

### Constraints

The constraints file (`constraints.yaml`) defines what the agent is and isn't allowed to do. It includes hard limits (absolute rules), escalation triggers, and tool restrictions.

Constraints are mounted **read-only** into the runtime — the agent cannot modify them.

### Services

Grant external service access to an agent:

```bash
agency grant my-agent github --key-env GITHUB_TOKEN
agency grant my-agent brave-search --key-env BRAVE_API_KEY
```

The agent never sees the real API key. The egress proxy swaps a scoped token for the real credential at the network boundary.

Revoke access:

```bash
agency revoke my-agent github
```

Grants and revocations take effect immediately on running agents — no restart required.

## Starting an Agent

```bash
agency start my-agent
```

This triggers the **seven-phase start sequence**:

1. **Verify** — Validates all config files and policy chain
2. **Enforcement** — Starts shared infrastructure (if needed), enforcer sidecar, applies custom seccomp profile
3. **Constraints** — Computes effective policy, generates internal manifests
4. **Workspace** — Verifies the workspace provides required tools
5. **Identity** — Loads and verifies the identity document
6. **Body** — Mounts skills read-only, starts the microVM runtime, verifies isolation
7. **Session** — Constructs session context and records it in the audit log

If any phase fails, everything created so far is torn down. This is **fail-closed** — no partial starts.

## Sending Tasks

Deliver a task to a running agent via DM:

```bash
agency send my-agent "Fix the failing tests in test_api.py"
```

For detached execution (returns immediately):

```bash
agency send my-agent --detach "Run a security scan of the codebase"
```

Agents prioritize quality over speed. When a request is ambiguous, the agent will ask clarifying questions rather than guess. When research would improve the answer — web search, knowledge queries, reading files — the agent does it first. When the agent learns facts about people (location, role, preferences), it saves them to the knowledge graph for future sessions.

Conversations continue naturally across multiple messages. There are no turn limits — cost is controlled by USD budgets, not message counts.

## Monitoring

### Status

See what's happening across the platform:

```bash
agency status
```

View details for a specific agent:

```bash
agency show my-agent
```

### Audit Logs

Every action an agent takes is logged by the platform (not by the agent):

```bash
agency log my-agent                     # Current session log
agency log my-agent --all               # All sessions
agency log my-agent --filter security   # Filter by category
```

### Channel Messages

If the agent is part of a team and sends messages to channels:

```bash
agency comms read findings            # Read a specific channel
agency comms search "error"           # Search across all channels
```

## Stopping an Agent

Three halt tiers, each more forceful than the last:

```bash
agency stop my-agent                    # Supervised halt (graceful)
agency stop my-agent --immediate        # Immediate halt (SIGTERM)
agency stop my-agent --emergency        # Emergency halt (SIGKILL + silent)
```

**Supervised halt** lets the agent finish its current action and save state. **Immediate halt** sends SIGTERM and waits briefly. **Emergency halt** kills the process immediately — use only when something has gone wrong.

Emergency halt requires a reason (every action must leave a trace):

```bash
agency stop my-agent --emergency --reason "Agent producing harmful output"
```

## Resuming an Agent

Resume a previously stopped agent:

```bash
agency resume my-agent
```

This restores the agent's state and reloads its constraints. The agent's persistent memory and workspace are preserved.

Then send it a new task:

```bash
agency send my-agent "Continue where you left off"
```

## Restarting an Agent

Full teardown and restart:

```bash
agency restart my-agent
```

This stops the agent, tears down its runtime, and starts fresh through the full seven-phase sequence.

## Deleting an Agent

```bash
agency delete my-agent
```

This removes the agent and archives its audit logs. The agent must be stopped first.

## Listing Agents

```bash
agency list                             # All agents
agency list --active                    # Only running agents
```

## Agent Files on Disk

```
~/.agency/agents/my-agent/
├── agent.yaml              # Manifest
├── constraints.yaml        # Read-only constraints
├── identity.md             # Identity seed
├── workspace.yaml          # Workspace/runtime spec
├── policy.yaml             # Agent policy
├── services.yaml           # Granted services
├── AGENTS.md               # Generated from constraints (read-only)
├── workspace-data/         # The agent's /workspace directory
├── memory/                 # Persistent memory (topic files)
├── state/                  # Runtime state and signals
├── skills-manifest.json    # Generated skills manifest
└── mcp-servers.json        # Generated MCP config
```

## Missions

Experimental. Missions are not part of the default `0.2.x` core workflow.

Agents can be assigned missions — standing instructions that define their ongoing purpose. An agent with an active mission only wakes for matching triggers and @mentions, eliminating idle LLM calls.

```bash
agency mission assign ticket-triage henrybot900
```

Missions declare what events trigger work, what capabilities are required, and what healthy operation looks like. For ongoing tasks, missions are far more efficient than sending individual messages.

See [Missions](/missions) for full details.

## Persistent Memory

Agents preserve knowledge across sessions using topic-based memory. Inside the agent runtime, agents use built-in tools:

- `save_memory(topic, content)` — Save knowledge to a topic
- `search_memory(query)` — Search across all memory topics
- `list_memories()` — List all saved topics
- `delete_memory(topic)` — Remove a topic

Memory is stored as markdown files in `~/.agency/agents/my-agent/memory/` and the memory index is injected into the agent's system prompt so it knows what it has stored.

Memory survives agent stops, resumes, and restarts.
