---
title: "Quick Start"
description: "Get Agency running and your first agent working in under 10 minutes."
---


Get Agency running and your first agent working in under 10 minutes.

## Before You Start

You need an API key from at least one LLM provider. If you don't have one yet, see [Getting API Keys](/getting-api-keys) — Google Gemini has a free tier that works immediately, no credit card required.

## Install

**Linux / macOS / WSL2:**

```bash
curl -fsSL https://raw.githubusercontent.com/geoffbelknap/agency/main/install.sh | bash
```

**Windows:**

```powershell
irm https://raw.githubusercontent.com/geoffbelknap/agency/main/install.ps1 | iex
```

The installer handles everything: Docker, the `agency` Go binary, and initial configuration. Safe to run multiple times.

## Setup

```bash
agency setup
```

The setup wizard walks you through everything:

1. **Detects your environment** — Docker, platform
2. **Configures API keys** — auto-detects keys from environment variables, or prompts you
3. **Builds and starts infrastructure** — shared services (egress, comms, knowledge)
4. **Detects your tools** — offers to configure Claude Code or Cursor integration
5. **Creates your first agent** — picks a preset, names it, starts it

After setup, you'll have a running agent ready for work.

### Manual Setup (if you prefer)

If you want more control, the setup wizard wraps these individual steps:

```bash
agency init --api-key $ANTHROPIC_API_KEY   # Initialize ~/.agency/
agency infra up                             # Build images + start services
agency create dev-assistant --preset engineer  # Create an agent
agency start dev-assistant                  # Start it
```

## Three Ways to Work

### 1. AI Assistant (MCP Plugin)

Operate agents through natural conversation in Claude Code:

```bash
# Setup wizard configures this automatically, or run manually:
claude mcp add agency -- agency mcp-server
```

Then just talk to your assistant:

> "Send dev-assistant a message to fix the failing tests in test_api.py"

> "What's dev-assistant working on? Check the logs."

> "Read the findings channel — has it found anything yet?"

All 85 platform operations are available as MCP tools. You never need to remember CLI syntax.

### 2. CLI

Deliver tasks directly from the command line:

```bash
agency send dev-assistant "List all Python files and summarize what each one does"
```

The agent works autonomously — calling tools, reading files, executing commands. Output streams to the terminal in real-time.

## Seeing What Agents Produce

Agents work inside an isolated `/workspace` directory. Access it from the host:

```
~/.agency/agents/dev-assistant/workspace-data/
```

To see what agents have been doing:

```bash
agency log dev-assistant      # Audit log — every tool call and state change
agency status                 # Platform-wide status
agency channel list           # List communication channels
agency channel read findings  # Read a specific channel
```

## Delivering More Tasks

Agents stay running between tasks. Send them messages:

```bash
agency send dev-assistant "Now add unit tests for the changes you just made"
```

For fire-and-forget:

```bash
agency send dev-assistant --detach "Run a full security audit of the codebase"
```

Check in later with `agency log` or `agency channel read`.

For ongoing tasks, create a [mission](/missions) instead of sending individual messages. Missions give agents standing instructions and only wake them for relevant events — no idle LLM calls.

## Deploy a Whole Team

Packs deploy pre-configured teams in one command:

```bash
agency hub search slack         # Find packs
agency hub install slack-ops    # Install a pack
agency deploy slack-ops.yaml    # Deploy the team
```

This creates multiple agents, sets up channels, configures connectors, and starts everything.

## Stop and Resume

```bash
agency stop dev-assistant       # Supervised halt — finishes current action
agency resume dev-assistant     # Pick up where you left off
```

Workspace, memory, and history are preserved across stops and resumes.

## Verify Security

```bash
agency admin doctor
```

Checks credential isolation, network mediation, read-only constraints, audit logging, and more.

## Next Steps

- **[Getting API Keys](/getting-api-keys)** — Set up additional providers for cost optimization
- **[Core Concepts](/concepts)** — Understand how the pieces fit together
- **[Presets](/presets)** — Choose the right preset for your use case (15 built-in)
- **[Channels and Messaging](/channels-and-messaging)** — Agent communication
- **[Teams](/teams)** — Multi-agent operations with coordinators
- **[Packs](/packs)** — Declarative team deployment
- **[Model Routing](/model-routing)** — Optimize cost with multi-provider routing
