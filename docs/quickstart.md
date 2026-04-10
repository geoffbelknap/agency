---
title: "Quick Start"
description: "Get Agency running and your first agent working in under 10 minutes."
---


Get Agency running and your first agent working in under 10 minutes.

## Before You Start

You need two things:

1. **Docker** running on your machine ([Docker Desktop](https://docs.docker.com/get-docker/) on Mac/Windows, or `curl -fsSL https://get.docker.com | sh` on Linux)
2. **An API key** from at least one LLM provider — if you don't have one yet, see [Getting API Keys](/getting-api-keys). Google Gemini has a free tier that works immediately, no credit card required.

## Install

**macOS (Homebrew):**

```bash
brew install geoffbelknap/tap/agency
```

**Linux / macOS / WSL2:**

```bash
curl -fsSL https://geoffbelknap.github.io/agency/install.sh | bash
```

**Windows (PowerShell):**

```powershell
irm https://raw.githubusercontent.com/geoffbelknap/agency/main/install.ps1 | iex
```

## Setup

Run the quickstart wizard:

```bash
agency quickstart
```

It walks you through five things:

1. **Pick your LLM provider** — Anthropic (recommended), OpenAI, or Google
2. **Paste your API key** — input is masked, stored in an encrypted credential store
3. **Start infrastructure** — builds container images and launches shared services
4. **Create your first agent** — choose a starter role and name
5. **Run a demo task** — verifies the agent can respond

When it finishes, you'll see:

```
Agent is running.

Web UI:   http://localhost:8280
Chat:     http://localhost:8280/channels/dm-henry
CLI:      agency send henry "your task here"
```

Verify everything is healthy:

```bash
agency status
```

You should see all infrastructure components running. Quickstart opens the browser directly to the first agent's chat by default. If the browser does not open, use the printed **Chat** URL, or open `http://localhost:8280` and select the agent under **Direct Messages**.

> **Remote access:** Agency-web is intended for local access during alpha testing. To reach it from another machine or your phone, use a tunnel or overlay network with built-in authentication: [Cloudflare Named Tunnels](https://developers.cloudflare.com/cloudflare-one/connections/connect-networks/) with Access policies, [ngrok](https://ngrok.com/) with OAuth or basic auth, [Tailscale Serve](https://tailscale.com/kb/1312/serve) for tailnet members, or [Defined Networking](https://www.defined.net/) overlay networks. Don't expose agency-web to the open internet without authentication.

`agency setup` is still available for idempotent infrastructure setup, but `agency quickstart` is the recommended first-run path.

## Choose Your Path

For basic users, the Web UI is the primary experience. Use the terminal for initial setup and recovery only.

For developers, security operators, and other power users, the CLI exposes the same platform primitives directly and is useful for scripting, debugging, and advanced workflows.

## Add Another Agent

Quickstart already creates and starts your first agent. If you want another one, Agency ships with built-in presets — pre-configured agent templates for common roles. The `generalist` preset is a broad-purpose assistant with file access, shell, and web tools.

### Web UI

1. Open `http://localhost:8280`
2. Click **Agents** in the left sidebar
3. Click the **Create** button in the top-right corner of the Agents screen
4. In the dialog, enter a name (e.g., `researcher`) and select the **generalist** preset.
5. Check **Start agent immediately** and click **Create**

You'll see the agent appear in the list with a green running status.

### CLI

If you prefer the terminal or need a fallback:

```bash
agency create researcher --preset generalist
agency start researcher
```

The start sequence runs through seven verified phases: enforcement, constraints, workspace, identity, body, session, and related checks.

## Talk to Your Agent

Now send your agent some work.

### Web UI

1. Use the agent chat that quickstart opened, or open the printed **Chat** URL.
2. If you are starting from `http://localhost:8280`, click **Channels** and select your agent under **Direct Messages**. It has an **AGENT** badge.
3. Type a message in the compose bar: *"What files are in my workspace? List them and describe what you see."*
4. Watch the activity indicator. You'll see what the agent is doing as it works.
5. When it responds, send a follow-up: *"Create a file called notes.md with a summary of what you found."*

The DM view shows your full conversation history. To see the raw audit log, go back to **Agents**, click your agent, and open the **Activity** tab.

### CLI

If you prefer the terminal or need a fallback:

```bash
agency send henry "What files are in my workspace? List them and describe what you see."
agency send henry "Create a file called notes.md with a summary of what you found."
```

To see the full audit trail:

```bash
agency log henry
```

### Channels

Agents can also post to shared channels — these are visible to all agents and useful for sharing findings across a team. Check if your agent wrote anything:

**CLI:**
```bash
agency comms list
agency comms read findings
```

**Web UI:** In the Channels sidebar, look under the **Channels** section (above Direct Messages). Click any channel to read it.

## Give It Superpowers

Your agent can already read files and run commands in its workspace. Let's give it the ability to search the web by enabling the `brave-search` capability.

You'll need a [Brave Search API key](https://brave.com/search/api/) — the free tier gives you 2,000 queries/month.

### CLI

```bash
agency cap add brave-search
agency cap enable brave-search --key <YOUR_BRAVE_API_KEY> --agents henry
```

Now ask it something that requires looking things up:

```bash
agency send henry "What are the top mass transit systems in the world? Compare their ridership."
```

Check the log to see it using web search:

```bash
agency log henry
```

You'll see `brave_search` tool calls in the audit trail.

### Web UI

1. Go to the **Capabilities** screen (in the admin sidebar)
2. Click **Add Capability**, enter `brave-search`, and click **Add**
3. Click **Enable** on the new capability
4. Paste your Brave API key and select **henry** under agent access
5. Click **Enable**

Now go back to the chat with your agent and ask something that needs web search. You'll see it research and respond with current information.

## What's Next

You have a working personal assistant that can read files, run commands, and search the web. Here's where to go from here:

- **[Missions](/missions)** — Give agents standing instructions that persist across sessions. "Monitor this repo and flag security issues" instead of one-off messages.
- **[Presets](/presets)** — Browse all 15 built-in presets (engineer, researcher, analyst, security-reviewer, and more) or create your own.
- **[Teams & Packs](/packs)** — Deploy multi-agent teams in one command. A coordinator routes work to specialized agents.
- **[Connectors](/hub)** — Connect external work sources — Slack, Jira, LimaCharlie, and more — so agents respond to real events.
- **[Model Routing](/model-routing)** — Add multiple LLM providers and let Agency route to the best model for each task tier.
- **[Core Concepts](/concepts)** — Understand the full architecture: enforcement, mediation, audit, and the ASK security framework.
