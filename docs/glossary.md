---
title: "Glossary"
description: "Quick definitions for every term you'll encounter in Agency."
---


Quick definitions for every term you'll encounter in Agency.

---

**Agent** — An AI worker that runs inside an isolated container. Each agent has a name, a role (defined by its preset), a workspace where it does its work, and constraints that limit what it can do. Agents work autonomously on tasks you assign them.

**ASK framework** — The security framework Agency implements. 25 tenets that govern how AI agents are isolated, monitored, and controlled. [Full specification](https://github.com/geoffbelknap/ask).

**Audit log** — A record of everything an agent does, written by the platform (not the agent). The agent can't modify or delete its own logs.

**Capability** — Something an agent can use: an MCP server (tool provider), a skill (instruction package), or a service (external API with managed credentials).

**Channel** — A named message stream where agents communicate. Like a group chat — agents send messages, read messages, and search history. Operators can read channels too.

**Connector** — A link between an external system and Agency. Connectors bring work in from Slack, Jira, GitHub, or any API. Four types: webhook (real-time push), poll (periodic check), schedule (cron-triggered), and channel-watch (pattern matching on agent messages).

**Constraints** — Rules that define what an agent can and can't do. Mounted read-only into the agent's container — the agent can't change its own rules.

**Credential store** — Encrypted storage (`~/.agency/credentials/store.enc`) for all API keys and secrets. AES-256-GCM encrypted. Credentials are managed via `agency creds` commands and never enter agent containers — the egress proxy resolves them at request time via a Unix socket to the gateway.

**Coordinator** — A special agent type that breaks complex tasks into sub-tasks and delegates them to other agents. Coordinators manage work but don't do implementation themselves.

**Deploy** — Create everything defined in a pack (agents, teams, channels) with a single command: `agency deploy pack.yaml`.

**Docker** — The container technology Agency uses to isolate agents. Each agent runs in its own container with controlled network access and no direct internet.

**Egress proxy** — The only component that holds real API keys. Sits between agents and the internet, swapping scoped tokens for real credentials. Agents never see actual keys.

**Event bus** — Unified event routing system in the gateway. Sources include connectors, channels, schedules, webhooks, and platform events. Subscriptions are derived from missions and notification config. At-most-once delivery with a 1000-event ring buffer.

**Enforcer** — A lightweight proxy (written in Go, 32MB) that sits in front of each agent. Routes LLM calls, enforces domain rules, performs XPIA scanning, tracks budgets, enforces rate limits, runs trajectory monitoring, and logs everything. The enforcer has no credentials itself — it forwards to the egress proxy.

**Function agent** — An oversight agent (security reviewer, compliance auditor, etc.) that can read other agents' workspaces and halt agents that violate constraints. The "checks and balances" of a team.

**Grant** — Give an agent access to an external service. `agency grant my-agent github` lets the agent use GitHub's API. The real credential is managed by the egress proxy; the agent gets a scoped token.

**Hard floor** — A rule that can't be overridden at any level: logging is always on, constraints are always read-only, credentials are always isolated, network access is always mediated.

**Hub** — A registry for sharing and installing pre-built packs, presets, and connectors. Like a package manager for Agency components.

**Identity** — A document (`identity.md`) that defines who an agent is — its purpose, working style, and behavioral guidelines. Think of it as the agent's personality and professional orientation.

**Infrastructure** — The shared services that all agents use: egress proxy (internet access), comms (messaging), knowledge (organizational memory), and intake (external work sources). XPIA scanning and budget tracking are handled by the enforcer.

**Intake** — The service that receives work from connectors and routes it to agents or teams. Manages a queue of work items with state tracking.

**Knowledge graph** — Organizational knowledge that builds up over time from agent communications. Agents can query it to find out who knows about what, what changed recently, and get context on topics.

**MCP (Model Context Protocol)** — A standard for connecting AI models to tools. Agency uses MCP to give agents access to external tools (code search, browser automation, etc.) with policy controls.

**Meeseeks** — An ephemeral single-purpose agent spawned by a parent agent via the `spawn_meeseeks` tool. Gets its own enforcer, an abbreviated startup sequence, a USD budget cap, and auto-terminates on completion.

**Mediation** — The principle that all agent access to external resources goes through a supervised intermediary. No direct paths. This is how Agency enforces security without trusting the agent.

**Mission** — A first-class managed object for agent standing instructions. Lifecycle: create, assign, pause, resume, complete. Missions are hot-reloaded via enforcer SIGHUP, and their instructions are injected into the agent's system prompt. Managed via `agency mission` commands.

**Memory** — Topic-based knowledge an agent saves for itself. Survives across sessions — the agent can pick up where it left off. Stored as markdown files in the agent's memory directory.

**Operator** — You. The human who manages Agency, creates agents, assigns work, and monitors results.

**Pack** — A YAML file that defines an entire team deployment: agents, roles, channels, and connectors. `agency deploy pack.yaml` creates everything; `agency teardown` reverses it.

**Policy** — Rules that govern what agents can do, organized in a hierarchy: platform > organization > department > team > agent. Each level can only restrict what the level above allows.

**Preset** — A template that configures an agent for a specific role. Sets the model tier, available tools, identity, and constraints. Agency ships with 15 built-in presets (engineer, researcher, analyst, coordinator, etc.).

**Principal** — Any entity that can take action in Agency: a human operator, an agent, or a team.

**Resume** — Restart a previously stopped agent, preserving its workspace, memory, and state.

**Revoke** — Remove an agent's access to an external service. Takes effect immediately, no restart needed.

**Seven-phase start sequence** — The mandatory process Agency runs when starting an agent. Validates configuration, starts enforcement, computes constraints, prepares the workspace, loads identity, boots the container, and establishes the session. If any phase fails, everything is torn down.

**Skill** — A package of procedural knowledge (following the agentskills.io standard) that gives an agent domain expertise. Described in the agent's system prompt and loaded on demand.

**Swarm** — Multi-host mode where agents run across multiple machines, coordinated through Docker Swarm. For scaling beyond a single machine.

**Teardown** — Reverse a pack deployment: stop all agents, remove the team, clean up channels. Audit logs are preserved.

**Trajectory monitoring** — Enforcer-side pattern detection for stuck or looping agents. A sliding window of 50 tool calls is analyzed by detectors for tool repetition, tool cycles, error cascades, budget velocity, and progress stalls. Anomalies are emitted to the audit log and gateway event bus. Always on with zero LLM cost.

**Trust** — A score (1-5) that Agency tracks for each agent based on observed behavior. Low trust triggers automatic restrictions. Operators can manually adjust trust levels.

**Workspace** — The `/workspace` directory inside an agent's container where it does all its work. Files created here are accessible from the host at `~/.agency/agents/<name>/workspace-data/`.

**XPIA** — Cross-Prompt Injection Attack. A technique where malicious content in one AI response tries to manipulate subsequent AI behavior. Agency's enforcer scans for this on all LLM responses.
