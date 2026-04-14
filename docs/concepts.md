---
title: "Core Concepts"
description: "This page explains the key ideas behind Agency. Understanding these concepts makes everything else click."
---


This page explains the key ideas behind Agency. Understanding these concepts makes everything else click.

> Status: Core reference. This page describes the default `0.2.x` core Agency
> path first. Experimental surfaces such as teams, packs, connectors, and
> missions are called out inline where they appear.

## The Big Picture

Agency is a platform where **you** (the operator) manage **agents** that do work. Each agent runs inside an isolated container — it has its own workspace, identity, and constraints. The platform enforces security boundaries between agents, and between agents and the outside world.

```
You (operator)
    │
    ├── CLI commands or AI assistant (MCP tools)
    │
    ▼
Agency Platform
    │
    ├── Shared Infrastructure (egress, comms, knowledge, web)
    │
    └── Per-Agent Containers
        ├── Agent A (engineer, working on code)
        ├── Agent B (analyst, reviewing data)
        └── Agent C (coordinator, delegating tasks)
```

Agents never talk to the internet directly. They never see API keys. They can't modify their own rules. Every action they take is logged by the platform, not by the agent. This is structural enforcement — it's not something agents opt into, it's something they can't opt out of.

## Agents

An **agent** is an autonomous AI worker running inside a hardened container. Each agent has:

- **A name** — lowercase alphanumeric, like `dev-assistant` or `security-checker`
- **A preset** — determines what model tier, tools, and identity the agent gets
- **An identity** — a seed document that defines who the agent is and how it works
- **Constraints** — rules the agent must follow, mounted read-only so it can't change them
- **A workspace** — a `/workspace` directory where it does its work
- **Persistent memory** — topic-based knowledge that survives across sessions

### How Agents Respond

Agents are conversational, not one-shot. When you send a task via `agency send`, the agent enters a loop — asking clarifying questions if the request is ambiguous, researching before answering when it would improve quality, and saving learned facts about people (location, role, preferences) to the knowledge graph. The conversation continues naturally across multiple messages.

Tasks come through DMs (`agency send <agent> <message>`) or channel messages routed through the event bus. There are no turn limits — cost is controlled by USD budgets at task, daily, and monthly granularity. Budget exhaustion is a hard stop; the agent does not auto-continue.

### Agent Types

| Type | Purpose | Example |
|------|---------|---------|
| **Standard** | Does the actual work — writes code, analyzes data, writes docs | engineer, analyst, writer |
| **Coordinator** | Experimental multi-agent coordination role | coordinator |
| **Function** | Experimental oversight role for broader team workflows | security-reviewer, compliance-auditor |

## Presets

A **preset** is a template that configures an agent for a specific role. It sets the model tier, available tools, identity prompt, hard limits, and escalation rules.

Agency ships with 15 built-in presets organized by model tier:

| Tier | Presets | Cost |
|------|---------|------|
| **Frontier** | engineer, researcher, generalist | Highest (but needed for complex reasoning) |
| **Standard** | analyst, writer, ops | Moderate |
| **Fast** | coordinator, reviewer, minimal | Low |
| **Mini** | security-reviewer, compliance-auditor, privacy-monitor, code-reviewer, ops-monitor | Lowest |

The key insight: **not every agent needs a frontier model**. A security reviewer classifying messages produces the same results on a mini model as on a frontier model — at 50x lower cost.

See [Presets](/presets) for details on each one.

## Channels

**Channels** are how agents communicate. They're named, async message streams — like Slack channels, but for agents.

```bash
# Operator creates a channel
agency comms create findings

# Agents send messages through their built-in tools
# (inside the agent runtime, not shown here)
send_message("findings", "Found SQL injection in auth.py line 42")

# Operator reads the channel
agency comms read findings
```

Channels support full-text search across all messages. Unread message counts appear in each agent's system prompt, so agents know when there's something to check.

See [Channels and Messaging](/channels-and-messaging) for more.

## Teams

A **team** groups agents (and humans) together with defined roles. Teams enable:

- **Coordinators** that decompose tasks and delegate to team members
- **Function agents** that have oversight authority (like a security reviewer who can halt other agents)
- **Shared channels** for team communication
- **Activity tracking** across the team

Teams are still experimental relative to the default `0.2.x` core product. The
same security model can extend to coordinated teams, but the supported
first-user path remains one operator using one or a few agents through the
direct-message workflow.

See [Teams](/teams) for more.

## Capabilities

**Capabilities** are things agents can use — organized into three categories:

| Category | What It Is | Example |
|----------|-----------|---------|
| **MCP Servers** | Tool providers that run operator-side | browser-tools, code-search |
| **Skills** | Instruction packages following the agentskills.io standard | agency-operator, agency-concepts |
| **Services** | External APIs with managed credentials | github, brave-search, slack |

The operator controls which capabilities are available. Agents discover them through the capability registry. Service credentials are managed through the egress proxy — agents never see real API keys.

See [Capabilities](/capabilities) for more.

## Policies

**Policies** define what agents are allowed to do. They form a hierarchy:

```
Platform defaults (hardcoded)
    └── Organization policy
        └── Department policy (optional)
            └── Team policy
                └── Agent policy
```

Each level can only **restrict** what the level above allows — never expand. Some rules are **hard floors** that can't be changed at any level:

- Logging is always required
- Constraints are always read-only
- LLM credentials are always isolated
- Network mediation is always required

See [Policies and Governance](/policies-and-governance) for more.

## Packs

A **pack** is a YAML file that declares an entire deployment — agents, teams, channels, and connectors — in one file. One command creates everything:

```bash
agency deploy red-team/pack.yaml    # Creates agents, teams, channels, starts everything
agency teardown red-team             # Reverses the deployment
```

Packs are an experimental deployment surface for multi-agent teams. They are
useful for platform and ecosystem work, but they are not part of the default
`0.2.x` core Agency path.

See [Packs](/packs) for more.

## Connectors

**Connectors** bring work from external systems into Agency. Four source types:

| Type | How It Works | Example |
|------|-------------|---------|
| **Webhook** | Receives HTTP push events | Slack Events API, GitHub webhooks |
| **Poll** | Periodically checks an API for changes | Jira ticket polling |
| **Schedule** | Triggers on a cron schedule | Daily security scan |
| **Channel-watch** | Matches regex patterns in agent channels | Escalation keywords |

Connectors route incoming work to specific agents or teams through the intake
service. Event-driven architecture is core; the broader connector inventory is
still experimental.

See [Connectors and Intake](/connectors-and-intake) for more.

## Infrastructure

Agency runs shared infrastructure that all agents use. The default `0.2.x`
core path centers on egress, comms, knowledge, per-agent enforcers, and the
web UI. Other services remain optional or experimental.

| Component | Role |
|-----------|------|
| **Egress** | Proxy between agents and the internet. Handles credential swap — real API keys are injected here, not in agent containers. |
| **Comms** | Channel-based messaging service with full-text search. |
| **Knowledge** | Organizational knowledge graph — compounds over time from agent communications. |
| **Web** | Operator UI for setup, direct-message workflow, activity, and audit visibility. |
| **Intake** | Receives external work from connectors and routes it to agents. Experimental relative to the `0.2.x` core path. |

All of this runs on a mediation network that agents access through their enforcer sidecar — never directly.

See [Infrastructure](/infrastructure) for more.

## Security Model

Agency's security is structural, not behavioral. Agents don't need to "choose" to be secure — the platform makes insecure behavior impossible:

1. **Credentials are isolated** — API keys live in the egress proxy, not agent containers
2. **Network is mediated** — all traffic goes through the enforcer and egress proxy
3. **Constraints are read-only** — agents can't modify their own rules
4. **Everything is logged** — audit logs are written by infrastructure, not agents
5. **Agents are stoppable** — the operator can halt any agent immediately

This implements the [ASK framework](https://github.com/geoffbelknap/ask) — a set of 24 tenets for governing AI agents.

See [Security](/security) for more.
