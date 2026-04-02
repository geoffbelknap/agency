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

Run the setup wizard:

```bash
agency setup
```

It walks you through three things:

1. **Pick your LLM provider** — Anthropic (recommended), OpenAI, or Google
2. **Paste your API key** — input is masked, stored in an encrypted credential store
3. **Start infrastructure** — builds container images and launches shared services

When it finishes, you'll see:

```
You're ready to go:

  agency create my-agent  # Create an agent
  agency start my-agent   # Start an agent
  agency status           # Check platform status

  Open https://localhost:8280 for the web UI
```

Verify everything is healthy:

```bash
agency status
```

You should see all infrastructure components running. The web UI is live at `https://localhost:8280` — open it now if you want to follow along with the web path below.

> **Remote access:** Agency-web runs with TLS on localhost. To reach it from another machine or your phone, we recommend tunneling solutions with built-in authentication: [Cloudflare Named Tunnels](https://developers.cloudflare.com/cloudflare-one/connections/connect-networks/) with Access policies, [ngrok](https://ngrok.com/) with OAuth or basic auth, [Tailscale Serve](https://tailscale.com/kb/1312/serve) for tailnet members, or [Defined Networking](https://www.defined.net/) overlay networks. Don't expose agency-web to the open internet without authentication.

## Create Your First Agent

Agency ships with built-in presets — pre-configured agent templates for common roles. We'll use `generalist`, a broad-purpose assistant with file access, shell, and web tools.

### CLI

```bash
agency create assistant --preset generalist
agency start assistant
```

The start sequence runs through seven verified phases (enforcement, constraints, workspace, identity, and more). You'll see each phase complete:

```
Starting assistant...
  ✓ verify
  ✓ enforcement
  ✓ constraints
  ✓ workspace
  ✓ identity
  ✓ body
  ✓ session
Agent assistant is running.
```

### Web UI

1. Open `https://localhost:8280`
2. Click **Agents** in the left sidebar
3. Click the **Create** button in the top-right corner of the Agents screen
4. In the dialog, enter a name (e.g., `assistant`), select the **generalist** preset, and leave the mode set to **assisted** — this means the agent will ask for clarification when tasks are ambiguous, which is what you want while getting started. (The other option, **autonomous**, makes the agent push forward without asking — better for unattended workflows like missions.)
5. Check **Start agent immediately** and click **Create**

You'll see the agent appear in the list with a green running status.

## Talk to Your Agent

Now send your agent some work.

### CLI

```bash
agency send assistant "What files are in my workspace? List them and describe what you see."
```

The agent explores its workspace and responds. To see the full audit trail — every tool call, every decision:

```bash
agency log assistant
```

Send a follow-up that builds on what it already knows:

```bash
agency send assistant "Create a file called notes.md with a summary of what you found."
```

The agent remembers context within a session. It knows what it found in the first task and builds on it.

### Web UI

1. Click **Channels** in the left sidebar
2. Under **Direct Messages**, find your agent — it shows up as **assistant** with a green dot and an **AGENT** badge
3. Click it to open the DM channel
4. Type a message in the compose bar: *"What files are in my workspace? List them and describe what you see."*
5. Watch the activity indicator — you'll see what the agent is doing as it works (reading files, running commands)
6. When it responds, send a follow-up: *"Create a file called notes.md with a summary of what you found."*

The DM view shows your full conversation history. To see the raw audit log, go back to **Agents**, click your agent, and open the **Activity** tab.

### Channels

Agents can also post to shared channels — these are visible to all agents and useful for sharing findings across a team. Check if your agent wrote anything:

**CLI:**
```bash
agency channel list
agency channel read findings
```

**Web UI:** In the Channels sidebar, look under the **Channels** section (above Direct Messages). Click any channel to read it.

## Give It Superpowers

Your agent can already read files and run commands in its workspace. Let's give it the ability to search the web by enabling the `brave-search` capability.

You'll need a [Brave Search API key](https://brave.com/search/api/) — the free tier gives you 2,000 queries/month.

### CLI

```bash
agency cap add brave-search
agency cap enable brave-search --key <YOUR_BRAVE_API_KEY> --agents assistant
```

Now ask it something that requires looking things up:

```bash
agency send assistant "What are the top mass transit systems in the world? Compare their ridership."
```

Check the log to see it using web search:

```bash
agency log assistant
```

You'll see `brave_search` tool calls in the audit trail.

### Web UI

1. Go to the **Capabilities** screen (in the admin sidebar)
2. Click **Add Capability**, enter `brave-search`, and click **Add**
3. Click **Enable** on the new capability
4. Paste your Brave API key and select **assistant** under agent access
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
