---
title: "Agency User Guide"
description: "Agency is a governed AI agent platform focused on secure runtime, direct-message workflows, graph-backed context, and auditable execution."
---


Agency is a platform for running governed AI agents that do real work with
strong security boundaries, complete auditability, and a usable direct-message
workflow.

This guide is organized around the current core product, not every possible
platform surface in the repo.

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
mainline onboarding path right now:

- teams and coordinator-heavy workflows
- packs and hub lifecycle
- broad connector inventory
- graph governance and ontology operations
- advanced routing optimization surfaces

Those can still be useful, but they should not be mistaken for the core Agency
product today.

They remain documented under the **Experimental Surfaces** section of the docs
navigation so work can continue without diluting the default product path.
