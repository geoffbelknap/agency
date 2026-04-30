---
title: "Quick Start"
description: "Get Agency running, create your first governed agent, and use the direct-message workflow."
---


Get Agency running and your first agent working in under 10 minutes.

## Before You Start

You need:

1. **A supported microVM runtime path** for your platform
2. **Host tools** for egress mediation and microVM rootfs creation
3. **An API key** from at least one supported model provider

If you need a provider key first, see [Getting API Keys](/getting-api-keys).
Google Gemini is the easiest no-credit-card starting point for many users.

On Linux, Agency's strategic runtime path is Firecracker. The host needs KVM
and vsock available to the operator account:

```bash
test -r /dev/kvm && test -w /dev/kvm
test -r /dev/vhost-vsock && test -w /dev/vhost-vsock
```

On macOS Apple silicon, the supported runtime path is `apple-vf-microvm`,
backed by Apple's Virtualization framework.

Both supported runtime paths need `mitmdump` with Agency's egress addon Python
dependencies for host-managed egress and `mke2fs` from e2fsprogs for root
filesystem creation. Source installs run `scripts/install/host-dependencies.sh`
automatically. To verify or install them manually from a source checkout:

```bash
./scripts/install/host-dependencies.sh --check
./scripts/install/host-dependencies.sh
```

The script uses Homebrew on macOS/Linuxbrew when available, or common Linux
package managers such as `apt-get`, `dnf`, `yum`, `pacman`, or `zypper`. It
installs system packages such as Python and e2fsprogs, then installs the pinned
mitmproxy and egress addon dependencies into the repo `.venv`.

Dockerfiles remain part of Agency because they define OCI image filesystems
that microVM backends can convert into bootable root filesystems. Docker,
Podman, containerd, and Apple Container execution backends are legacy adapter
history and are no longer selectable through setup or quickstart.

## Install

**macOS / Linux (Homebrew or Linuxbrew):**

```bash
brew install geoffbelknap/tap/agency
```

**From source (any Linux or macOS with Go 1.24+):**

```bash
git clone https://github.com/geoffbelknap/agency.git
cd agency && make install
```

`make install` installs the required host tools through
`scripts/install/host-dependencies.sh`. Use `SKIP_HOST_DEPS=1 make install`
only when those dependencies are already managed by your package or image
build.

**Windows:** install inside a WSL2 Ubuntu distro and follow the Linux path
above. There is no native Windows installer.

The hosted `install.sh` at `geoffbelknap.github.io/agency/install.sh`
deliberately does not install anything; it prints the commands above and
politely suggests you don't pipe scripts from the internet into your
shell. See `install.sh` in this repo for the source.

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
