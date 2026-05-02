---
title: "Agency User Guide"
description: "Agency is a governed AI agent platform focused on secure runtime, direct-message workflows, graph-backed context, and auditable execution."
---


Agency runs governed AI agents in isolated runtimes, with mediated access to
tools and services, audit trails you can inspect, and a direct-message workflow
that feels like the normal way to use the product.

This guide follows the current core product path. The repo contains broader
platform work, but you do not need it to get started.

## Start Here

- **[What is Agency?](/what-is-agency)** — What Agency is, what problem it solves, and how it differs from a chat-only agent experience.
- **[Quick Start](/quickstart)** — Install Agency, run quickstart, create your first agent, and send your first task.
- **[Getting API Keys](/getting-api-keys)** — Set up a supported model provider.
- **[Core Concepts](/concepts)** — Agents, constraints, identity, channels, graph context, and governance.
- **[Glossary](/glossary)** — Definitions for the main terms.

## Core Workflows

- **[Agents](/agents)** — Create, configure, start, stop, and inspect agents.
- **[Presets](/presets)** — Choose a built-in role as a starting point.
- **[Channels and Messaging](/channels-and-messaging)** — Direct messages, shared channels, and message history.
- **[Model Routing](/model-routing)** — Provider setup and basic routing behavior.
- **[Security](/security)** — Isolation, mediation, audit, and credential boundaries.

## Operating Agency

- **[Infrastructure](/infrastructure)** — Shared services, mediation plane, and local stack operations.
- **[CLI Reference](/cli-reference)** — Command reference for the current CLI surface.
- **[Troubleshooting](/troubleshooting)** — Common issues, recovery steps, and `agency admin doctor`.

## Building On Agency

- **REST API** — The gateway exposes a REST API on `localhost:8200`.
- **OpenAPI contract** — Canonical spec at `/api/v1/openapi.yaml`.
- **Core API view** — Supported default API subset at `/api/v1/openapi-core.yaml`.
- **MCP server** — `agency mcp-server` exposes Agency operations to AI assistants and other MCP clients.

## Not Core To Start With

Some broader platform areas exist in the repo and docs, but they are not the
main onboarding path right now:

- teams and coordinator-heavy workflows
- packs and hub lifecycle
- broad connector inventory
- graph governance and ontology operations
- advanced routing optimization surfaces

They can still be useful. They are documented under **Experimental Surfaces** so
that work can continue without making the default path harder to understand.
