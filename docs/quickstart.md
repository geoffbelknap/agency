---
title: "Quick Start"
description: "Get Agency running, create your first governed agent, and use the direct-message workflow."
---


Get Agency running and your first agent working in under 10 minutes.

## Before You Start

You need:

1. **Docker** running on your machine
2. **An API key** from at least one supported model provider

If you need a provider key first, see [Getting API Keys](/getting-api-keys).
Google Gemini is the easiest no-credit-card starting point for many users.

### Podman On WSL2

Agency can use rootless Podman as a container backend on WSL2, but use the
Linux distro packages. Homebrew Podman inside WSL can miss rootless helpers
such as `newuidmap`, `slirp4netns`, or the systemd user socket.

Install the WSL distro packages:

```bash
sudo apt-get install -y podman uidmap slirp4netns fuse-overlayfs crun
systemctl --user enable --now podman.socket
```

Verify the rootless API socket:

```text
curl --unix-socket "$XDG_RUNTIME_DIR/podman/podman.sock" http://d/v1.41/_ping
```

Expected output:

```text
OK
```

Then configure Agency:

```yaml
hub:
  deployment_backend: podman
  deployment_backend_config:
    host: /run/user/1000/podman/podman.sock
```

Replace `1000` with your user ID if needed:

```bash
id -u
```

Agency keeps its gateway, mediation, egress, and operator-facing network
boundaries intact. On WSL2 rootless Podman, Agency avoids publishing direct
host ports for internal services that are already reachable through the gateway
proxy.

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

## First Run

Run:

```bash
agency quickstart
```

Quickstart walks you through:

1. choosing a provider
2. storing the API key
3. starting shared infrastructure
4. creating your first agent
5. opening the web UI and DM chat

When it finishes, Agency should be ready for the core workflow:

```bash
agency send henry "What files are in my workspace?"
agency log henry
agency admin doctor
```

## Verify The Core Path

After quickstart:

1. Open the printed **Web UI** URL.
2. Open the printed **Chat** URL or select the agent under **Direct Messages**.
3. Send a task.
4. Confirm the agent replies.
5. Open the agent activity view and confirm you can inspect what happened.

If the browser does not open automatically, open `http://localhost:8280`.

`agency status` should show the local stack running.

## Create Another Agent

If you want a second agent:

```bash
agency create researcher --preset generalist
agency start researcher
```

The standard start path runs through Agency's staged startup and verification
flow before the agent becomes available.

## Talk To The Agent

The main product workflow is a direct-message conversation.

### Web UI

1. Open the DM for the agent.
2. Send a task like:
   `Summarize the files in this workspace and tell me what looks important.`
3. Wait for the response.
4. Open the activity/audit view if you want to inspect execution details.

### CLI

```bash
agency send henry "Summarize the files in this workspace and tell me what looks important."
agency log henry
```

## Channels And Context

Agency also supports shared channels and graph-backed context.

For the current core product, think of them this way:

- **channels** help agents and operators share message history
- **graph context** helps useful knowledge survive and be retrieved later

You do not need to set up teams, missions, packs, or connectors to get value
from the core workflow.

## Use Agency Through An AI Assistant

Agency exposes an MCP server:

```bash
agency mcp-server
```

You can add it to tools like Claude Code, Codex, or Copilot so those clients
can operate Agency through the same underlying API surface.

Examples:

```bash
claude mcp add agency -- agency mcp-server
codex mcp add agency -- agency mcp-server
gh copilot mcp add agency -- agency mcp-server
```

## If Something Looks Wrong

Use:

```bash
agency status
agency admin doctor
agency log henry
```

Those three commands are the fastest way to confirm:

- the stack is up
- the security guarantees are holding
- the agent actually executed work

## What's Next

- **[Agents](/agents)** — lifecycle, configuration, and operator controls
- **[Channels and Messaging](/channels-and-messaging)** — DM and shared channel behavior
- **[Model Routing](/model-routing)** — providers and routing configuration
- **[Security](/security)** — mediation, audit, and ASK guarantees
- **[Core Concepts](/concepts)** — the mental model behind the runtime
