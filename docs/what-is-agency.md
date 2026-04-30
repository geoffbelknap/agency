---
title: "What is Agency?"
description: "Agency is a governed AI agent platform for running real agents with isolation, mediation, graph-backed context, and auditability."
---


Agency is a platform for running AI agents that can do real work without being
trusted with your host environment, your network, or your credentials.

The point of Agency is not just to spin up an agent. The point is to give that
agent a governed runtime around it.

## What Problem It Solves

Plain chat tools are good for short-lived, conversational work. They are much
less convincing when the agent needs to:

- work across real files and tools
- persist context over time
- operate under security boundaries
- be observable and interruptible
- leave an audit trail an operator can trust

Agency is built around those requirements first.

## The Core Agency Experience

The core workflow is intentionally narrow:

1. Create an agent with a preset.
2. Start it in an isolated workspace.
3. Talk to it through a direct-message workflow in the web UI or CLI.
4. Let it use governed tools through the mediation layer.
5. Inspect logs, usage, budget, and status when needed.
6. Let it reuse graph-backed context from prior work so it improves over time.

That is the current heart of the product.

## How It Works

Every agent gets:

- an isolated workspace microVM
- an enforcer sidecar that mediates requests
- read-only operator-owned constraints
- durable agent-owned identity
- infrastructure-written audit logs

The agent does not get:

- direct internet access
- raw service credentials
- write access to its own constraints
- control over its own audit trail

This is the difference between "an agent we hope behaves" and "an agent running
inside structural boundaries."

## Why The Graph Matters

Agency includes a durable knowledge graph because agents should get better over
time.

The important story is not that Agency has every possible graph feature. The
important story is that:

- useful knowledge survives across sessions
- agents can retrieve relevant prior context
- repeated work gets faster and smarter over time

That compounding context is core. The broader ingestion and governance surface
is not the center of the product right now.

## Why Event-Driven Matters

Agency is designed to be event-driven.

That means the platform can wake agents based on:

- direct messages
- platform state changes
- webhook-style external events
- subscription routing

The important architectural point is that Agency is built to react to events,
not to make polling loops the default mental model.

## What Keeps It Safe

Agency follows the ASK model:

- enforcement remains outside the agent boundary
- mediation remains complete
- auditability remains complete
- least privilege remains explicit
- trust, identity, and knowledge boundaries remain visible

In practice, that means:

- all meaningful traffic goes through mediation
- credentials are injected at the network boundary, not handed to the agent
- audit logs are written by infrastructure, not by the agent
- agents can be halted and inspected without trusting their own self-reporting

## How It Feels To Use

You can use Agency through:

- the web UI
- the CLI
- the REST API
- the MCP server from an AI assistant

For most people, the main experience is:

- set up a provider
- create an agent
- open the DM chat
- give it work
- inspect activity and audit when needed

That is the product path Agency should feel best at first.

## Who It Is For

Agency is for people who want AI agents doing real work, but need stronger
operational guarantees than a normal chatbot or lightly wrapped coding agent
tool can provide.

That includes:

- researchers and analysts
- developers and security teams
- operators who need auditable automation
- builders who want a stable API and MCP surface to build against

## What Agency Is Not Trying To Be First

Agency has broader platform work in the repo, but the mainline story right now
is not:

- giant connector breadth
- a giant coordination platform
- a marketplace-first ecosystem
- graph governance as a standalone product

Those may matter later. The believable core today is governed agents with real
runtime boundaries, durable context, and auditable work.

## What's Next

- **[Quick Start](/quickstart)** — Install Agency and run your first agent
- **[Core Concepts](/concepts)** — Understand the runtime model and governance boundaries
- **[Agents](/agents)** — Learn the lifecycle and operator workflows
